// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package memory is an in-process [defectlineage.Store]: the whole pipeline
// with no database.
//
// Use it to try defect lineage, and in tests. It implements the same contract
// store/postgres does, leases and all, so a run here behaves the way a run
// against a database does — it simply does not outlive the process and is not
// shared with another one.
//
// For anything you intend to keep, use store/postgres. Two things this cannot
// do are the reasons that store exists: survive a restart, and let several
// processes drain one queue.
package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// Store keeps the work list and the candidate rows in memory.
//
// Every method takes the same lock, so one Store is safe for concurrent use and
// two goroutines claiming at once take disjoint batches, exactly as two
// processes would against a database.
type Store struct {
	mu sync.Mutex
	// fixes is the lifecycle row per fix, keyed by organisation and fix.
	fixes map[fixKey]*lifecycle
	// candidates is the evidence rows, keyed by candidate id.
	candidates map[string]*candidate
	// now is the clock, swappable so a test can age a lease without sleeping.
	now func() time.Time
}

type fixKey struct{ orgID, fixID string }

type lifecycle struct {
	ref             defectlineage.FixRef
	stage           defectlineagev1.DefectLineageStage
	claimToken      string
	lastAttemptedAt time.Time
	terminalOutcome defectlineagev1.DefectLineageTerminalOutcome
	attemptCount    int32
	// seq orders claims deterministically where two rows share a lease clock,
	// standing in for the stored row id the database orders on.
	seq int64
}

type candidate struct {
	id  string
	ref defectlineage.FixRef
	// row is the candidate as a caller reads it. Named rather than embedded, so
	// every access says which of the two identical field sets it means.
	row       defectlineage.Candidate
	updatedAt time.Time
	seq       int64
	// The scoring lease, mirroring the PostgreSQL store's two columns.
	scoringClaimToken string
	scoringClaimedAt  time.Time
}

// New returns an empty Store.
func New() *Store {
	return &Store{
		fixes:      map[fixKey]*lifecycle{},
		candidates: map[string]*candidate{},
		now:        func() time.Time { return time.Now().UTC() },
	}
}

// SetClock replaces the Store's clock. It exists so a test can age a lease past
// its expiry without sleeping for it.
func (s *Store) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// seqCounter orders rows within one process. It is package-level, so two Store
// instances share it and it is incremented atomically — each Store's own mutex
// does not serialise the other's.
var seqCounter atomic.Int64

func nextSeq() int64 { return seqCounter.Add(1) }

// blameClaimable reports whether a blame drain may claim a fix at this stage. A
// fix at CANDIDATES_FOUND is blamed already and owes only a score, so offering
// it again would buy the same answer from the provider twice.
func blameClaimable(stage defectlineagev1.DefectLineageStage) bool {
	switch stage {
	case defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_PENDING,
		defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_PREPARED,
		defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_BLAMED:
		return true
	default:
		return false
	}
}

// Enqueue inserts a fix's PENDING lifecycle row, and does nothing when the fix
// already has one, so a re-delivered event never resets a fix that has advanced.
func (s *Store) Enqueue(_ context.Context, ref defectlineage.FixRef) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := fixKey{ref.OrganizationID, ref.BugFixPRID}
	if _, exists := s.fixes[k]; exists {
		return nil
	}
	s.fixes[k] = &lifecycle{
		ref:   ref,
		stage: defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_PENDING,
		seq:   nextSeq(),
	}
	return nil
}

// ClaimPendingBlame leases up to limit claimable fixes, rotating a fresh token
// on each and stamping the lease clock.
func (s *Store) ClaimPendingBlame(_ context.Context, orgID, connectorID string, limit int, lease time.Duration) ([]defectlineage.Claim, error) {
	if limit <= 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	cutoff := now.Add(-lease)

	var eligible []*lifecycle
	for _, f := range s.fixes {
		if f.ref.OrganizationID != orgID || f.ref.ConnectorID != connectorID {
			continue
		}
		if !blameClaimable(f.stage) {
			continue
		}
		// A never-claimed row carries no token; a claimed row always carries a
		// lease clock, so a lapsed lease is the second arm. Both are re-offerable.
		if f.claimToken != "" && !f.lastAttemptedAt.Before(cutoff) {
			continue
		}
		eligible = append(eligible, f)
	}

	// Never-claimed rows first, then the oldest lease: fresh work drains before
	// retries. Ties break on insertion order so a page is stable.
	sort.Slice(eligible, func(i, j int) bool {
		a, b := eligible[i], eligible[j]
		if a.lastAttemptedAt.Equal(b.lastAttemptedAt) {
			return a.seq < b.seq
		}
		return a.lastAttemptedAt.Before(b.lastAttemptedAt)
	})
	if len(eligible) > limit {
		eligible = eligible[:limit]
	}

	out := make([]defectlineage.Claim, 0, len(eligible))
	for _, f := range eligible {
		f.claimToken = uuid.NewString()
		f.lastAttemptedAt = now
		f.attemptCount++
		out = append(out, defectlineage.Claim{
			Ref:          f.ref,
			ClaimToken:   f.claimToken,
			Stage:        f.stage,
			AttemptCount: f.attemptCount,
		})
	}
	return out, nil
}

// MarkPrepared advances a claimed fix to PREPARED, gated on the claim token.
func (s *Store) MarkPrepared(_ context.Context, ref defectlineage.FixRef, claimToken string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.fixes[fixKey{ref.OrganizationID, ref.BugFixPRID}]
	if !ok || f.claimToken != claimToken {
		return false, nil
	}
	if !blameClaimable(f.stage) {
		// A live token is not enough. A worker that overran its lease still holds
		// its token, and the fix it was blaming may have been re-claimed, blamed
		// and scored while it ran. Letting that token set PREPARED walks the fix
		// backwards out of RESOLVED, re-opens it to the blame drain, and leaves
		// it at CANDIDATES_FOUND with every candidate already scored — a stage
		// the scoring pass never revisits, because it only resolves groups
		// ClaimUnscored returned.
		return false, nil
	}
	f.stage = defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_PREPARED
	return true, nil
}

// Settle writes a terminal outcome reached before any provider call, gated on
// the claim token.
func (s *Store) Settle(_ context.Context, ref defectlineage.FixRef, claimToken string, outcome defectlineagev1.DefectLineageTerminalOutcome) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.fixes[fixKey{ref.OrganizationID, ref.BugFixPRID}]
	if !ok || f.claimToken != claimToken {
		return false, nil
	}
	if !blameClaimable(f.stage) {
		// Settling a fix that is already blamed would stamp a terminal outcome
		// such as ANCHORS_EMPTY over a row that carries candidates, so the
		// outcome and the rows contradict each other.
		return false, nil
	}
	f.stage = defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED
	f.terminalOutcome = outcome
	return true, nil
}

// SubmitBlame writes a walk's candidates and advances the fix's stage.
//
// The stage move is forward-only and candidate writes are keyed on (fix,
// culprit commit), so a duplicate submit converges instead of un-resolving a
// scored fix or duplicating its rows.
func (s *Store) SubmitBlame(_ context.Context, ref defectlineage.FixRef, outcome *defectlineagev1.LineageBlameOutcome) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()

	if f, ok := s.fixes[fixKey{ref.OrganizationID, ref.BugFixPRID}]; ok &&
		f.stage == defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED {
		// Checked before the rows are written, not after. A late submit from a
		// worker whose lease lapsed would otherwise attach candidates to a fix
		// already settled UNATTRIBUTED, so the outcome says no culprit was found
		// while candidate rows sit beside it.
		return nil
	}

	for _, r := range outcome.GetResults() {
		sha := r.GetCulpritSha()
		if sha == "" {
			// A result naming no culprit would create a row no reader can act on.
			continue
		}
		existing := s.findCandidateLocked(ref.OrganizationID, ref.BugFixPRID, sha)
		if existing == nil {
			existing = &candidate{
				id:  uuid.NewString(),
				ref: ref,
				seq: nextSeq(),
			}
			existing.row.ID = existing.id
			existing.row.Ref = ref
			existing.row.CommitSHA = sha
			s.candidates[existing.id] = existing
		}
		c := &existing.row
		// Each field keeps what it had when the new walk cannot supply it, so a
		// second walk that lost the culprit's line does not erase the first's.
		if ts := r.GetCommittedAt(); ts != nil {
			t := ts.AsTime().UTC().Truncate(time.Second)
			c.CommittedAt = &t
		}
		if p := r.GetCulpritPath(); p != "" {
			path := p
			c.Path = &path
		}
		if l := r.GetCulpritLine(); l > 0 {
			line := l
			c.Line = &line
		}
		if n := r.GetCandidatePrNumber(); n > 0 {
			c.CandidatePRNumber = n
		}
		// The author is one fact, kept whole or not at all. Guarding the two
		// fields separately splices one walk's name onto another walk's email,
		// because blame routinely supplies a name and no address.
		if a := r.GetAuthor(); a.GetName() != "" || a.GetEmail() != "" {
			c.AuthorName, c.AuthorEmail = a.GetName(), a.GetEmail()
		}
		c.EvidenceType = r.GetEvidenceType()
		existing.updatedAt = now
	}

	f, ok := s.fixes[fixKey{ref.OrganizationID, ref.BugFixPRID}]
	if !ok {
		return nil
	}
	if f.stage == defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED {
		return nil
	}
	if o, terminal := defectlineage.OutcomeForState(outcome.GetStatus()); terminal {
		f.stage = defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED
		f.terminalOutcome = o
		return nil
	}
	if !s.hasCandidateLocked(ref.OrganizationID, ref.BugFixPRID) {
		// A resolve that stored no culprit is an unattributed fix, not one
		// awaiting a score. Resting it at CANDIDATES_FOUND would strand it: the
		// scoring pass only ever sees candidate rows, so nothing would advance it.
		f.stage = defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED
		f.terminalOutcome = defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_UNATTRIBUTED
		return nil
	}
	f.stage = defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_CANDIDATES_FOUND
	return nil
}

// hasCandidateLocked reports whether the fix stored any culprit. The caller
// holds the lock.
func (s *Store) hasCandidateLocked(orgID, fixID string) bool {
	for _, c := range s.candidates {
		if c.ref.OrganizationID == orgID && c.ref.BugFixPRID == fixID && c.row.CommitSHA != "" {
			return true
		}
	}
	return false
}

// ClaimUnscored returns unscored candidate rows grouped by fix, oldest first.
func (s *Store) ClaimUnscored(_ context.Context, orgID, connectorID string, limit int, lease time.Duration) ([]defectlineage.UnscoredFix, error) {
	if limit <= 0 {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	cutoff := now.Add(-lease)

	var unscored []*candidate
	for _, c := range s.candidates {
		if c.ref.OrganizationID != orgID || c.ref.ConnectorID != connectorID {
			continue
		}
		if c.row.CommitSHA == "" || c.row.ConfidenceScore != nil {
			continue
		}
		// Not a candidate of a fix that has already ended; see the PostgreSQL
		// store's ClaimUnscored for why.
		f, ok := s.fixes[fixKey{c.ref.OrganizationID, c.ref.BugFixPRID}]
		if !ok || f.stage == defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED {
			continue
		}
		// A never-claimed row carries no token; a claimed one always carries a
		// lease clock, so a lapsed lease is the second arm.
		if c.scoringClaimToken != "" && !c.scoringClaimedAt.Before(cutoff) {
			continue
		}
		unscored = append(unscored, c)
	}
	// Never-claimed rows first, then the oldest lease, as the blame claim orders.
	sort.Slice(unscored, func(i, j int) bool {
		a, b := unscored[i], unscored[j]
		if a.scoringClaimedAt.Equal(b.scoringClaimedAt) {
			return a.seq < b.seq
		}
		return a.scoringClaimedAt.Before(b.scoringClaimedAt)
	})
	if len(unscored) > limit {
		unscored = unscored[:limit]
	}

	var out []defectlineage.UnscoredFix
	index := map[string]int{}
	for _, c := range unscored {
		c.scoringClaimToken = uuid.NewString()
		c.scoringClaimedAt = now
		i, seen := index[c.ref.BugFixPRID]
		if !seen {
			out = append(out, defectlineage.UnscoredFix{Ref: c.ref})
			i = len(out) - 1
			index[c.ref.BugFixPRID] = i
		}
		path, line := "", int32(0)
		if c.row.Path != nil {
			path = *c.row.Path
		}
		if c.row.Line != nil {
			line = *c.row.Line
		}
		out[i].Candidates = append(out[i].Candidates, &defectlineagev1.PendingStructuralCandidate{
			CandidateLineageId: c.id,
			CandidateCommitSha: c.row.CommitSHA,
			CandidatePath:      path,
			CandidateLine:      line,
		})
	}
	return out, nil
}

// WriteScore records one candidate's score and derivation. A zero
// candidateLine leaves the stored line alone rather than erasing a good value.
func (s *Store) WriteScore(_ context.Context, orgID, candidateLineageID string, score float64, evidence *defectlineagev1.DefectScoreEvidence, candidateLine int32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.candidates[candidateLineageID]
	if !ok || c.ref.OrganizationID != orgID {
		return nil
	}
	v := score
	c.row.ConfidenceScore = &v
	c.row.ScoreEvidence = evidence
	if candidateLine > 0 {
		line := candidateLine
		c.row.Line = &line
	}
	c.updatedAt = s.now()
	return nil
}

// ResolveIfFullyScored moves a CANDIDATES_FOUND fix to RESOLVED once none of its
// candidates still lacks a score.
func (s *Store) ResolveIfFullyScored(_ context.Context, ref defectlineage.FixRef) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.fixes[fixKey{ref.OrganizationID, ref.BugFixPRID}]
	if !ok || f.stage != defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_CANDIDATES_FOUND {
		return false, nil
	}
	for _, c := range s.candidates {
		if c.ref.OrganizationID != ref.OrganizationID || c.ref.BugFixPRID != ref.BugFixPRID {
			continue
		}
		if c.row.CommitSHA != "" && c.row.ConfidenceScore == nil {
			return false, nil
		}
	}
	f.stage = defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED
	f.terminalOutcome = defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_SCORED_OK
	return true, nil
}

// SettleUnscorable terminates a fix whose scoring input is permanently gone.
func (s *Store) SettleUnscorable(_ context.Context, ref defectlineage.FixRef, outcome defectlineagev1.DefectLineageTerminalOutcome) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.fixes[fixKey{ref.OrganizationID, ref.BugFixPRID}]
	if !ok || f.stage == defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED {
		return false, nil
	}
	f.stage = defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED
	f.terminalOutcome = outcome
	return true, nil
}

// ListCandidates returns a fix's candidates, strongest first.
func (s *Store) ListCandidates(_ context.Context, orgID, bugFixPRID string) ([]defectlineage.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []defectlineage.Candidate
	for _, c := range s.candidates {
		if c.ref.OrganizationID != orgID || c.ref.BugFixPRID != bugFixPRID {
			continue
		}
		if c.row.CommitSHA == "" {
			continue
		}
		out = append(out, c.row)
	}
	// Scored candidates first, highest score leading; unscored rows follow, in a
	// stable order so repeated reads agree.
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		switch {
		case a.ConfidenceScore != nil && b.ConfidenceScore != nil:
			if *a.ConfidenceScore != *b.ConfidenceScore {
				return *a.ConfidenceScore > *b.ConfidenceScore
			}
		case a.ConfidenceScore != nil:
			return true
		case b.ConfidenceScore != nil:
			return false
		}
		return a.CommitSHA < b.CommitSHA
	})
	return out, nil
}

// GetCandidate returns one candidate row by its own id.
func (s *Store) GetCandidate(_ context.Context, orgID, candidateID string) (defectlineage.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.candidates[candidateID]
	if !ok || c.ref.OrganizationID != orgID {
		return defectlineage.Candidate{}, fmt.Errorf("reading candidate %s: %w", candidateID, defectlineage.ErrCandidateNotFound)
	}
	return c.row, nil
}

// LookupFix returns a fix's full reference from its lifecycle row.
func (s *Store) LookupFix(_ context.Context, orgID, bugFixPRID string) (defectlineage.FixRef, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.fixes[fixKey{orgID, bugFixPRID}]
	if !ok {
		return defectlineage.FixRef{}, fmt.Errorf("looking up fix %s: %w", bugFixPRID, defectlineage.ErrFixNotFound)
	}
	return f.ref, nil
}

// Stage reports one fix's lifecycle stage and terminal outcome. It is not part
// of the Store contract: it exists so a first run can see where a fix got to
// without a database to query.
func (s *Store) Stage(orgID, bugFixPRID string) (defectlineagev1.DefectLineageStage, defectlineagev1.DefectLineageTerminalOutcome, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.fixes[fixKey{orgID, bugFixPRID}]
	if !ok {
		return defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_UNSPECIFIED,
			defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_UNSPECIFIED, false
	}
	return f.stage, f.terminalOutcome, true
}

// findCandidateLocked returns the row for one (fix, culprit commit), or nil. The
// caller holds the lock.
func (s *Store) findCandidateLocked(orgID, fixID, sha string) *candidate {
	for _, c := range s.candidates {
		if c.ref.OrganizationID == orgID && c.ref.BugFixPRID == fixID && c.row.CommitSHA == sha {
			return c
		}
	}
	return nil
}

// Compile-time proof that this package satisfies the contract it claims.
var _ defectlineage.Store = (*Store)(nil)
