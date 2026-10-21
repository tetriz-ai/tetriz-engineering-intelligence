// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package postgres is the reference [defectlineage.Store]: the defect-lineage
// work list and candidate rows in PostgreSQL.
//
// schema.sql is the schema, and it is the file to load before the first run. It
// is idempotent, so loading it again on an existing database is a no-op.
//
// Claims are leased, not locked. A claim rotates a fresh token and stamps a
// lease clock, so a worker that dies mid-walk has its fix re-offered once the
// lease lapses.
//
// Two writes are gated on that token, MarkPrepared and Settle, so a worker whose
// lease was re-claimed cannot advance or settle the fix underneath its
// replacement. SubmitBlame deliberately is not: a provider's submit carries no
// token, and it is safe without one because it is idempotent per culprit commit
// and moves the stage forward only. A stale submit therefore converges on the
// rows the live worker would write rather than contradicting them.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// Store is a [defectlineage.Store] over a PostgreSQL database.
type Store struct {
	db *sql.DB
}

// New wraps an open database handle. It does not load the schema: run
// schema.sql once before the first use.
//
// The handle is not closed by this package, because the caller owns its
// lifetime and commonly shares it.
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, errors.New("defectlineage/postgres: nil database handle")
	}
	return &Store{db: db}, nil
}

// Enqueue inserts a fix's PENDING lifecycle row, and does nothing when the fix
// already has one.
//
// Insert-if-absent is what makes a re-delivered event or a sweep re-scan
// harmless: without it, a second delivery would reset a fix already blamed back
// to PENDING and the walk would be paid for twice.
func (s *Store) Enqueue(ctx context.Context, ref defectlineage.FixRef) error {
	const q = `
		INSERT INTO defect_lineage_pipeline_state
			(organization_id, connector_id, platform_repo_id, bug_fix_pr_id, stage, attempt_count)
		VALUES ($1, $2, $3, $4, $5, 0)
		ON CONFLICT (organization_id, bug_fix_pr_id) WHERE deleted_at IS NULL
		DO NOTHING`
	_, err := s.db.ExecContext(ctx, q,
		ref.OrganizationID, ref.ConnectorID, ref.PlatformRepoID, ref.BugFixPRID,
		defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_PENDING.String())
	if err != nil {
		return fmt.Errorf("enqueueing fix %s: %w", ref.BugFixPRID, classify(err))
	}
	return nil
}

// permanentCodes are the SQLSTATEs whose cause is the input itself: text that is
// not a valid uuid, a value past a column's width or range, a null or a check
// the row can never satisfy. Redelivering such a write reproduces the error
// forever.
//
// The list is exhaustive by intent rather than a class prefix. A unique
// violation and a foreign-key violation are integrity errors too, but both can
// pass on a later attempt — one is a lost race, the other a parent row that has
// not landed yet — so taking the whole class would drop an event that merely
// arrived early.
var permanentCodes = map[string]bool{
	"22P02": true, // invalid text representation, e.g. a non-uuid identifier
	"22001": true, // string data right truncation
	"22003": true, // numeric value out of range
	"22007": true, // invalid datetime format
	"23502": true, // not-null violation
	"23514": true, // check violation
}

// classify marks a write that can never succeed on retry. Everything else — a
// dropped connection, a lock timeout, a concurrent insert — is left as it is.
func classify(err error) error {
	var pgErr *pq.Error
	if !errors.As(err, &pgErr) {
		return err
	}
	if permanentCodes[string(pgErr.Code)] {
		return fmt.Errorf("%w: %s", defectlineage.ErrUnprocessable, pgErr.Message)
	}
	return err
}

// ClaimPendingBlame leases up to limit claimable fixes for one connector.
//
// A fix is claimable when it is not yet terminal and either was never claimed or
// its lease has lapsed. The scan takes row locks with SKIP LOCKED so two workers
// claiming at once take disjoint batches rather than one blocking on the other,
// and both the scan and the per-row update run in one transaction so a row
// cannot be claimed twice.
func (s *Store) ClaimPendingBlame(ctx context.Context, orgID, connectorID string, limit int, lease time.Duration) ([]defectlineage.Claim, error) {
	if limit <= 0 {
		return nil, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning claim transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	const selectQ = `
		SELECT id, platform_repo_id, bug_fix_pr_id, stage, attempt_count
		FROM defect_lineage_pipeline_state
		WHERE organization_id = $1
		  AND connector_id = $2
		  AND deleted_at IS NULL
		  AND stage = ANY($3)  -- the blame stages only; see blameClaimableStages
		  -- A NULL token is a never-claimed row; a claimed row always carries a
		  -- lease clock, so a lapsed lease is the second arm. Both are re-offerable.
		  --
		  -- The cutoff is computed here rather than passed in, so it is measured on
		  -- the same clock that stamps the lease. Mixing the two means a database
		  -- running even a fraction of a second ahead of the caller makes a lapsed
		  -- claim look live, and the fix is never re-offered.
		  AND (claim_token IS NULL OR last_attempted_at < now() - make_interval(secs => $4))
		-- Never-claimed rows first, then the oldest lease: fresh work drains before
		-- retries. Ending on id keeps a page stable under concurrent claims.
		ORDER BY last_attempted_at ASC NULLS FIRST, id
		LIMIT $5
		FOR UPDATE SKIP LOCKED`

	rows, err := tx.QueryContext(ctx, selectQ, orgID, connectorID,
		pq.Array(blameClaimableStages()), lease.Seconds(), limit)
	if err != nil {
		return nil, fmt.Errorf("scanning claimable fixes: %w", err)
	}

	type claimable struct {
		id, platformRepoID, bugFixPRID, stage string
		attemptCount                          int32
	}
	var found []claimable
	for rows.Next() {
		var c claimable
		if err := rows.Scan(&c.id, &c.platformRepoID, &c.bugFixPRID, &c.stage, &c.attemptCount); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("reading claimable fix: %w", err)
		}
		found = append(found, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("reading claimable fixes: %w", err)
	}
	_ = rows.Close()

	const updateQ = `
		UPDATE defect_lineage_pipeline_state
		SET claim_token = gen_random_uuid(),
		    last_attempted_at = now(),
		    attempt_count = attempt_count + 1,
		    updated_at = now()
		WHERE id = $1
		RETURNING claim_token, attempt_count`

	out := make([]defectlineage.Claim, 0, len(found))
	for _, c := range found {
		var token string
		var attempts int32
		if err := tx.QueryRowContext(ctx, updateQ, c.id).Scan(&token, &attempts); err != nil {
			return nil, fmt.Errorf("leasing fix %s: %w", c.bugFixPRID, err)
		}
		stage, _ := parseStage(c.stage)
		out = append(out, defectlineage.Claim{
			Ref: defectlineage.FixRef{
				OrganizationID: orgID,
				ConnectorID:    connectorID,
				PlatformRepoID: c.platformRepoID,
				BugFixPRID:     c.bugFixPRID,
			},
			ClaimToken:   token,
			Stage:        stage,
			AttemptCount: attempts,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing claim: %w", err)
	}
	return out, nil
}

// MarkPrepared advances a claimed fix to PREPARED, gated on the claim token and
// on the fix still being in a blame stage.
//
// The token alone is not enough. A worker that overran its lease still holds its
// token, and the fix may have been re-claimed, blamed and scored while it ran.
// Without the stage arm that token walks the fix backwards out of RESOLVED,
// re-opens it to the blame drain, and strands it at CANDIDATES_FOUND with every
// candidate already scored — a stage the scoring pass never revisits, because it
// resolves only the groups ClaimUnscored returned.
func (s *Store) MarkPrepared(ctx context.Context, ref defectlineage.FixRef, claimToken string) (bool, error) {
	const q = `
		UPDATE defect_lineage_pipeline_state
		SET stage = $1, updated_at = now()
		WHERE organization_id = $2 AND bug_fix_pr_id = $3 AND claim_token = $4 AND deleted_at IS NULL
		  AND stage = ANY($5)`
	res, err := s.db.ExecContext(ctx, q,
		defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_PREPARED.String(),
		ref.OrganizationID, ref.BugFixPRID, claimToken, pq.Array(blameClaimableStages()))
	if err != nil {
		return false, fmt.Errorf("marking fix %s prepared: %w", ref.BugFixPRID, err)
	}
	return affected(res)
}

// Settle writes a terminal outcome reached before any provider call, gated on
// the claim token and on the fix still being in a blame stage — settling one
// that is already blamed would stamp an outcome such as ANCHORS_EMPTY over a row
// that carries candidates, so the outcome and the rows contradict each other.
func (s *Store) Settle(ctx context.Context, ref defectlineage.FixRef, claimToken string, outcome defectlineagev1.DefectLineageTerminalOutcome) (bool, error) {
	const q = `
		UPDATE defect_lineage_pipeline_state
		SET stage = $1, terminal_outcome = $2, updated_at = now()
		WHERE organization_id = $3 AND bug_fix_pr_id = $4 AND claim_token = $5 AND deleted_at IS NULL
		  AND stage = ANY($6)`
	res, err := s.db.ExecContext(ctx, q,
		defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED.String(), outcome.String(),
		ref.OrganizationID, ref.BugFixPRID, claimToken, pq.Array(blameClaimableStages()))
	if err != nil {
		return false, fmt.Errorf("settling fix %s: %w", ref.BugFixPRID, err)
	}
	return affected(res)
}

// SubmitBlame writes a walk's candidates and advances the fix's stage, in one
// transaction so a fix never reads as advanced with its candidates missing.
//
// The stage move is forward-only: a fix already RESOLVED is left alone, so a
// duplicate submit of a re-offered fix cannot un-resolve one that scoring has
// already finished. Candidate writes are upserts on (fix, culprit commit), so a
// re-drive converges on the same rows instead of accumulating duplicates.
func (s *Store) SubmitBlame(ctx context.Context, ref defectlineage.FixRef, outcome *defectlineagev1.LineageBlameOutcome) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning submit transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The stage move below is already forward-only, but the candidate upserts run
	// before it. Read the stage first, inside this transaction and under a row
	// lock, so a late submit from a worker whose lease lapsed cannot attach
	// candidates to a fix already settled UNATTRIBUTED — which would leave the
	// outcome saying no culprit was found while candidate rows sit beside it.
	const stageQ = `
		SELECT stage FROM defect_lineage_pipeline_state
		WHERE organization_id = $1 AND bug_fix_pr_id = $2 AND deleted_at IS NULL
		FOR UPDATE`
	var currentStage string
	switch err := tx.QueryRowContext(ctx, stageQ, ref.OrganizationID, ref.BugFixPRID).Scan(&currentStage); {
	case errors.Is(err, sql.ErrNoRows):
		return nil // nothing enqueued for this fix
	case err != nil:
		return fmt.Errorf("reading stage for fix %s: %w", ref.BugFixPRID, err)
	case currentStage == defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED.String():
		return nil
	}

	const upsertQ = `
		INSERT INTO bugfix_pr_defect_lineage
			(organization_id, connector_id, platform_repo_id, bug_fix_pr_id,
			 candidate_pr_number, candidate_commit_sha, candidate_committed_at,
			 candidate_path, candidate_line, candidate_author_name, candidate_author_email,
			 evidence_type)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (organization_id, bug_fix_pr_id, candidate_commit_sha) WHERE deleted_at IS NULL
		DO UPDATE SET
			candidate_pr_number    = COALESCE(EXCLUDED.candidate_pr_number, bugfix_pr_defect_lineage.candidate_pr_number),
			candidate_committed_at = COALESCE(EXCLUDED.candidate_committed_at, bugfix_pr_defect_lineage.candidate_committed_at),
			candidate_path         = COALESCE(EXCLUDED.candidate_path, bugfix_pr_defect_lineage.candidate_path),
			candidate_line         = COALESCE(EXCLUDED.candidate_line, bugfix_pr_defect_lineage.candidate_line),
			-- NULLIF, not a plain assignment: a walk that returned no author
			-- sends empty strings, and overwriting a known author with those
			-- would lose information a previous walk had.
			-- The author is one fact, so the two columns move together or not at
			-- all. Guarding them separately splices one walk's name onto another
			-- walk's email, because blame routinely supplies a name and no address.
			candidate_author_name = CASE
				WHEN EXCLUDED.candidate_author_name <> '' OR EXCLUDED.candidate_author_email <> ''
				THEN EXCLUDED.candidate_author_name ELSE bugfix_pr_defect_lineage.candidate_author_name END,
			candidate_author_email = CASE
				WHEN EXCLUDED.candidate_author_name <> '' OR EXCLUDED.candidate_author_email <> ''
				THEN EXCLUDED.candidate_author_email ELSE bugfix_pr_defect_lineage.candidate_author_email END,
			evidence_type          = EXCLUDED.evidence_type,
			updated_at             = now()`

	for _, r := range outcome.GetResults() {
		if r.GetCulpritSha() == "" {
			// A result with no culprit names nothing. Storing it would create a row
			// that the unique key treats as the fix's state marker and that no
			// reader can act on.
			continue
		}
		var committedAt any
		if ts := r.GetCommittedAt(); ts != nil {
			committedAt = ts.AsTime().UTC().Truncate(time.Second)
		}
		var prNumber any
		if r.GetCandidatePrNumber() > 0 {
			prNumber = r.GetCandidatePrNumber()
		}
		var path any
		if r.GetCulpritPath() != "" {
			path = r.GetCulpritPath()
		}
		var line any
		if r.GetCulpritLine() > 0 {
			line = r.GetCulpritLine()
		}
		if _, err := tx.ExecContext(ctx, upsertQ,
			ref.OrganizationID, ref.ConnectorID, ref.PlatformRepoID, ref.BugFixPRID,
			prNumber, r.GetCulpritSha(), committedAt, path, line,
			r.GetAuthor().GetName(), r.GetAuthor().GetEmail(),
			r.GetEvidenceType().String(),
		); err != nil {
			return fmt.Errorf("writing candidate %s for fix %s: %w", r.GetCulpritSha(), ref.BugFixPRID, err)
		}
	}

	stage := defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_CANDIDATES_FOUND
	var terminal any
	if o, ok := defectlineage.OutcomeForState(outcome.GetStatus()); ok {
		stage = defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED
		terminal = o.String()
	}

	// A resolve that stored no culprit is an unattributed fix, not one awaiting a
	// score. Resting it at CANDIDATES_FOUND would strand it: the scoring pass
	// only ever sees candidate rows, so nothing would advance it.
	if stage == defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_CANDIDATES_FOUND {
		var stored bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (SELECT 1 FROM bugfix_pr_defect_lineage
			               WHERE organization_id = $1 AND bug_fix_pr_id = $2
			                 AND candidate_commit_sha <> '' AND deleted_at IS NULL)`,
			ref.OrganizationID, ref.BugFixPRID).Scan(&stored); err != nil {
			return fmt.Errorf("checking candidates for fix %s: %w", ref.BugFixPRID, err)
		}
		if !stored {
			stage = defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED
			terminal = defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_UNATTRIBUTED.String()
		}
	}

	const advanceQ = `
		UPDATE defect_lineage_pipeline_state
		SET stage = $1, terminal_outcome = $2, updated_at = now()
		WHERE organization_id = $3 AND bug_fix_pr_id = $4 AND deleted_at IS NULL AND stage <> $5`
	if _, err := tx.ExecContext(ctx, advanceQ, stage.String(), terminal,
		ref.OrganizationID, ref.BugFixPRID,
		defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED.String()); err != nil {
		return fmt.Errorf("advancing fix %s: %w", ref.BugFixPRID, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing submit for fix %s: %w", ref.BugFixPRID, err)
	}
	return nil
}

// ClaimUnscored returns up to limit unscored candidate rows, grouped by fix.
//
// Rows are ordered oldest-updated first so a batch drains the backlog rather
// than revisiting the newest rows, and groups preserve that order.
func (s *Store) ClaimUnscored(ctx context.Context, orgID, connectorID string, limit int, lease time.Duration) ([]defectlineage.UnscoredFix, error) {
	if limit <= 0 {
		return nil, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning scoring claim transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// The scan and the lease stamp share one transaction, and SKIP LOCKED makes
	// two workers take disjoint batches rather than one waiting on the other.
	// The cutoff is computed in the database, on the same clock that stamps the
	// lease, so a caller running fast or slow cannot make a live claim look
	// lapsed.
	const q = `
		SELECT c.id, c.platform_repo_id, c.bug_fix_pr_id, c.candidate_commit_sha,
		       COALESCE(c.candidate_path, ''), COALESCE(c.candidate_line, 0)
		FROM bugfix_pr_defect_lineage c
		WHERE c.organization_id = $1
		  AND c.connector_id = $2
		  AND c.candidate_commit_sha <> ''
		  AND c.confidence_score IS NULL
		  AND c.deleted_at IS NULL
		  -- Not a candidate of a fix that has already ended. One whose scoring
		  -- input is permanently gone keeps its unscored rows forever, and
		  -- without this every pass would re-offer them and re-discover that
		  -- they cannot be scored — work that grows with the dead set.
		  AND (c.scoring_claim_token IS NULL OR c.scoring_claimed_at < now() - make_interval(secs => $5))
		  AND EXISTS (SELECT 1 FROM defect_lineage_pipeline_state p
		              WHERE p.organization_id = c.organization_id
		                AND p.bug_fix_pr_id = c.bug_fix_pr_id
		                AND p.deleted_at IS NULL
		                AND p.stage <> $4)
		ORDER BY c.scoring_claimed_at ASC NULLS FIRST, c.id
		LIMIT $3
		FOR UPDATE OF c SKIP LOCKED`
	rows, err := tx.QueryContext(ctx, q, orgID, connectorID, limit,
		defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED.String(), lease.Seconds())
	if err != nil {
		return nil, fmt.Errorf("scanning unscored candidates: %w", err)
	}

	var out []defectlineage.UnscoredFix
	index := map[string]int{}
	for rows.Next() {
		var id, platformRepoID, bugFixPRID, sha, path string
		var line int32
		if err := rows.Scan(&id, &platformRepoID, &bugFixPRID, &sha, &path, &line); err != nil {
			return nil, fmt.Errorf("reading unscored candidate: %w", err)
		}
		i, seen := index[bugFixPRID]
		if !seen {
			out = append(out, defectlineage.UnscoredFix{Ref: defectlineage.FixRef{
				OrganizationID: orgID,
				ConnectorID:    connectorID,
				PlatformRepoID: platformRepoID,
				BugFixPRID:     bugFixPRID,
			}})
			i = len(out) - 1
			index[bugFixPRID] = i
		}
		out[i].Candidates = append(out[i].Candidates, &defectlineagev1.PendingStructuralCandidate{
			CandidateLineageId: id,
			CandidateCommitSha: sha,
			CandidatePath:      path,
			CandidateLine:      line,
		})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("reading unscored candidates: %w", err)
	}
	_ = rows.Close()

	const leaseQ = `
		UPDATE bugfix_pr_defect_lineage
		SET scoring_claim_token = gen_random_uuid(), scoring_claimed_at = now()
		WHERE id = $1`
	for _, g := range out {
		for _, c := range g.Candidates {
			if _, err := tx.ExecContext(ctx, leaseQ, c.GetCandidateLineageId()); err != nil {
				return nil, fmt.Errorf("leasing candidate %s: %w", c.GetCandidateLineageId(), err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing scoring claim: %w", err)
	}
	return out, nil
}

// WriteScore records one candidate's confidence score and its derivation.
//
// candidateLine is written only when non-zero: a failed line recovery must leave
// the stored line alone rather than overwrite a good value with 0.
func (s *Store) WriteScore(ctx context.Context, orgID, candidateLineageID string, score float64, evidence *defectlineagev1.DefectScoreEvidence, candidateLine int32) error {
	var evidenceJSON any
	if evidence != nil {
		b, err := protojson.Marshal(evidence)
		if err != nil {
			return fmt.Errorf("encoding score evidence for candidate %s: %w", candidateLineageID, err)
		}
		evidenceJSON = string(b)
	}
	var line any
	if candidateLine > 0 {
		line = candidateLine
	}
	const q = `
		UPDATE bugfix_pr_defect_lineage
		SET confidence_score = $1,
		    score_evidence = $2,
		    candidate_line = COALESCE($3, candidate_line),
		    updated_at = now()
		WHERE organization_id = $4 AND id = $5 AND deleted_at IS NULL`
	if _, err := s.db.ExecContext(ctx, q, score, evidenceJSON, line, orgID, candidateLineageID); err != nil {
		return fmt.Errorf("writing score for candidate %s: %w", candidateLineageID, err)
	}
	return nil
}

// ResolveIfFullyScored moves a CANDIDATES_FOUND fix to RESOLVED once none of its
// candidates still lacks a score.
//
// "Fully scored" is derived by asking whether an unscored candidate exists,
// rather than kept as a counter, so the answer cannot drift from the rows it
// describes.
func (s *Store) ResolveIfFullyScored(ctx context.Context, ref defectlineage.FixRef) (bool, error) {
	const q = `
		UPDATE defect_lineage_pipeline_state p
		SET stage = $1, terminal_outcome = $2, updated_at = now()
		WHERE p.organization_id = $3
		  AND p.bug_fix_pr_id = $4
		  AND p.deleted_at IS NULL
		  AND p.stage = $5
		  AND NOT EXISTS (
			SELECT 1 FROM bugfix_pr_defect_lineage c
			WHERE c.organization_id = p.organization_id
			  AND c.bug_fix_pr_id = p.bug_fix_pr_id
			  AND c.candidate_commit_sha <> ''
			  AND c.confidence_score IS NULL
			  AND c.deleted_at IS NULL
		  )`
	res, err := s.db.ExecContext(ctx, q,
		defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED.String(),
		defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_SCORED_OK.String(),
		ref.OrganizationID, ref.BugFixPRID,
		defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_CANDIDATES_FOUND.String())
	if err != nil {
		return false, fmt.Errorf("resolving fix %s: %w", ref.BugFixPRID, err)
	}
	return affected(res)
}

// SettleUnscorable terminates a fix whose scoring input is permanently gone.
func (s *Store) SettleUnscorable(ctx context.Context, ref defectlineage.FixRef, outcome defectlineagev1.DefectLineageTerminalOutcome) (bool, error) {
	const q = `
		UPDATE defect_lineage_pipeline_state
		SET stage = $1, terminal_outcome = $2, updated_at = now()
		WHERE organization_id = $3 AND bug_fix_pr_id = $4 AND deleted_at IS NULL AND stage <> $1`
	res, err := s.db.ExecContext(ctx, q,
		defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED.String(), outcome.String(),
		ref.OrganizationID, ref.BugFixPRID)
	if err != nil {
		return false, fmt.Errorf("settling unscorable fix %s: %w", ref.BugFixPRID, err)
	}
	return affected(res)
}

// ListCandidates returns every stored candidate of one fix, strongest first.
func (s *Store) ListCandidates(ctx context.Context, orgID, bugFixPRID string) ([]defectlineage.Candidate, error) {
	const q = `
		SELECT id, connector_id, platform_repo_id, candidate_commit_sha,
		       candidate_committed_at, candidate_path, candidate_line,
		       COALESCE(candidate_pr_number, 0),
		       COALESCE(candidate_author_name, ''), COALESCE(candidate_author_email, ''),
		       evidence_type, confidence_score, score_evidence
		FROM bugfix_pr_defect_lineage
		WHERE organization_id = $1 AND bug_fix_pr_id = $2
		  AND candidate_commit_sha <> '' AND deleted_at IS NULL
		ORDER BY confidence_score DESC NULLS LAST, candidate_commit_sha`
	rows, err := s.db.QueryContext(ctx, q, orgID, bugFixPRID)
	if err != nil {
		return nil, fmt.Errorf("listing candidates for fix %s: %w", bugFixPRID, err)
	}
	defer func() { _ = rows.Close() }()

	var out []defectlineage.Candidate
	for rows.Next() {
		var (
			c           defectlineage.Candidate
			evidenceStr string
			scoreRaw    sql.NullFloat64
			evidenceRaw sql.NullString
			committedAt sql.NullTime
			path        sql.NullString
			line        sql.NullInt32
		)
		if err := rows.Scan(&c.ID, &c.Ref.ConnectorID, &c.Ref.PlatformRepoID, &c.CommitSHA,
			&committedAt, &path, &line, &c.CandidatePRNumber,
			&c.AuthorName, &c.AuthorEmail, &evidenceStr, &scoreRaw, &evidenceRaw); err != nil {
			return nil, fmt.Errorf("reading candidate for fix %s: %w", bugFixPRID, err)
		}
		c.Ref.OrganizationID = orgID
		c.Ref.BugFixPRID = bugFixPRID
		c.EvidenceType, _ = parseEvidenceType(evidenceStr)
		if committedAt.Valid {
			t := committedAt.Time.UTC()
			c.CommittedAt = &t
		}
		if path.Valid {
			p := path.String
			c.Path = &p
		}
		if line.Valid {
			l := line.Int32
			c.Line = &l
		}
		if scoreRaw.Valid {
			v := scoreRaw.Float64
			c.ConfidenceScore = &v
		}
		if evidenceRaw.Valid && evidenceRaw.String != "" {
			var ev defectlineagev1.DefectScoreEvidence
			if err := protojson.Unmarshal([]byte(evidenceRaw.String), &ev); err != nil {
				return nil, fmt.Errorf("decoding score evidence for candidate %s: %w", c.ID, err)
			}
			c.ScoreEvidence = &ev
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing candidates for fix %s: %w", bugFixPRID, err)
	}
	return out, nil
}

// affected reports whether a statement matched a row, which is how a
// token-gated update tells "wrote it" from "the lease had already moved on".
func affected(res sql.Result) (bool, error) {
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("reading affected rows: %w", err)
	}
	return n > 0, nil
}

// parseStage reads the stage label stored in the stage column. An unreadable
// label yields (UNSPECIFIED, false) rather than a guess, so a caller can tell a
// real unset stage from one it cannot interpret.
func parseStage(s string) (defectlineagev1.DefectLineageStage, bool) {
	v, ok := defectlineagev1.DefectLineageStage_value[s]
	if !ok {
		return defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_UNSPECIFIED, false
	}
	return defectlineagev1.DefectLineageStage(v), true
}

// parseEvidenceType reads the evidence-type label stored on a candidate row,
// with the same refusal-to-guess as parseStage.
func parseEvidenceType(s string) (defectlineagev1.DefectLineageEvidenceType, bool) {
	v, ok := defectlineagev1.DefectLineageEvidenceType_value[s]
	if !ok {
		return defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_UNSPECIFIED, false
	}
	return defectlineagev1.DefectLineageEvidenceType(v), true
}

// Compile-time proof that this package satisfies the contract it claims.
var _ defectlineage.Store = (*Store)(nil)

// LookupFix reads a fix's connector and repository from its lifecycle row.
//
// A provider's submit carries only the organisation and the fix id, so this is
// where the rest of the reference comes from. Reading it rather than trusting
// the caller is what stops a candidate row being filed under the wrong
// connector.
func (s *Store) LookupFix(ctx context.Context, orgID, bugFixPRID string) (defectlineage.FixRef, error) {
	const q = `
		SELECT connector_id, platform_repo_id
		FROM defect_lineage_pipeline_state
		WHERE organization_id = $1 AND bug_fix_pr_id = $2 AND deleted_at IS NULL`
	ref := defectlineage.FixRef{OrganizationID: orgID, BugFixPRID: bugFixPRID}
	err := s.db.QueryRowContext(ctx, q, orgID, bugFixPRID).Scan(&ref.ConnectorID, &ref.PlatformRepoID)
	if errors.Is(err, sql.ErrNoRows) {
		return defectlineage.FixRef{}, fmt.Errorf("looking up fix %s: %w", bugFixPRID, defectlineage.ErrFixNotFound)
	}
	if err != nil {
		return defectlineage.FixRef{}, fmt.Errorf("looking up fix %s: %w", bugFixPRID, err)
	}
	return ref, nil
}

// GetCandidate returns one candidate row by its own id, with the fix reference
// it belongs to.
func (s *Store) GetCandidate(ctx context.Context, orgID, candidateID string) (defectlineage.Candidate, error) {
	const q = `
		SELECT connector_id, platform_repo_id, bug_fix_pr_id, candidate_commit_sha,
		       candidate_committed_at, candidate_path, candidate_line,
		       COALESCE(candidate_pr_number, 0),
		       COALESCE(candidate_author_name, ''), COALESCE(candidate_author_email, ''),
		       evidence_type, confidence_score, score_evidence
		FROM bugfix_pr_defect_lineage
		WHERE organization_id = $1 AND id = $2 AND deleted_at IS NULL`

	var (
		c           defectlineage.Candidate
		evidenceStr string
		scoreRaw    sql.NullFloat64
		evidenceRaw sql.NullString
		committedAt sql.NullTime
		path        sql.NullString
		line        sql.NullInt32
	)
	err := s.db.QueryRowContext(ctx, q, orgID, candidateID).Scan(
		&c.Ref.ConnectorID, &c.Ref.PlatformRepoID, &c.Ref.BugFixPRID, &c.CommitSHA,
		&committedAt, &path, &line, &c.CandidatePRNumber,
		&c.AuthorName, &c.AuthorEmail, &evidenceStr, &scoreRaw, &evidenceRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return defectlineage.Candidate{}, fmt.Errorf("reading candidate %s: %w", candidateID, defectlineage.ErrCandidateNotFound)
	}
	if err != nil {
		return defectlineage.Candidate{}, fmt.Errorf("reading candidate %s: %w", candidateID, err)
	}
	c.ID = candidateID
	c.Ref.OrganizationID = orgID
	c.EvidenceType, _ = parseEvidenceType(evidenceStr)
	if committedAt.Valid {
		t := committedAt.Time.UTC()
		c.CommittedAt = &t
	}
	if path.Valid {
		p := path.String
		c.Path = &p
	}
	if line.Valid {
		l := line.Int32
		c.Line = &l
	}
	if scoreRaw.Valid {
		v := scoreRaw.Float64
		c.ConfidenceScore = &v
	}
	if evidenceRaw.Valid && evidenceRaw.String != "" {
		var ev defectlineagev1.DefectScoreEvidence
		if err := protojson.Unmarshal([]byte(evidenceRaw.String), &ev); err != nil {
			return defectlineage.Candidate{}, fmt.Errorf("decoding score evidence for candidate %s: %w", candidateID, err)
		}
		c.ScoreEvidence = &ev
	}
	return c, nil
}

// blameClaimableStages are the stages a blame drain may claim: a fix not yet
// walked, or one whose walk did not finish. CANDIDATES_FOUND is deliberately
// absent — that fix has been blamed and owes only a score, so re-offering it
// would buy the same answer from the provider twice.
func blameClaimableStages() []string {
	return []string{
		defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_PENDING.String(),
		defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_PREPARED.String(),
		defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_BLAMED.String(),
	}
}
