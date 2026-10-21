// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage

import (
	"context"
	"errors"
	"time"

	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// FixRef identifies one bug fix in the pipeline. It is the logical key both
// tables share: there is no foreign key between them, so a candidate row and its
// lifecycle row relate only by (OrganizationID, BugFixPRID).
type FixRef struct {
	// OrganizationID scopes every read and write. A single-tenant deployment can
	// use one constant value; nothing here interprets it.
	OrganizationID string
	// ConnectorID names the provider whose walk can blame this fix, so a walk
	// only ever claims fixes it can answer.
	ConnectorID string
	// PlatformRepoID is the provider's stable repository id.
	PlatformRepoID string
	// BugFixPRID is the fix's own identifier, as the caller knows it.
	BugFixPRID string
}

// FixInput is what a caller supplies for a claimed fix so its anchors can be
// built. This package owns no pull requests, commits or diffs, so the three
// facts a blame walk needs come from the caller.
type FixInput struct {
	// RepoFullName is owner/name, the form a provider's blame query addresses.
	RepoFullName string
	// FixCommitSHA is the commit that LANDED the fix — a merge commit, a squash,
	// or the last replayed commit of a fast-forward. The walk resolves its first
	// parent, which is the revision blame runs against.
	FixCommitSHA string
	// Diff is the fix's stored diff, as bytes already held.
	Diff []byte
	// DiffFormat selects the parser: [DiffFormatUnified] or [DiffFormatGitLab].
	DiffFormat string
	// ChangedFiles is how many files the fix touched, used against
	// MaxChangedFiles to skip an oversized change before any provider call. Zero
	// means unknown, and no size check is applied.
	ChangedFiles int
	// MergedAt is when the fix landed. Recency scores the gap from a candidate's
	// commit to this moment, so a zero value leaves every candidate's recency at
	// 0 rather than inventing a gap.
	MergedAt time.Time
}

// Diff formats [FixInput.DiffFormat] accepts.
const (
	// DiffFormatUnified is a git unified patch, what most providers serve a pull
	// request's diff as.
	DiffFormatUnified = "unified"
	// DiffFormatGitLab is GitLab's JSON array of per-file diffs.
	DiffFormatGitLab = "gitlab"
)

// DefaultMaxChangedFiles is the changed-file ceiling above which a fix is
// settled SKIPPED_OVERSIZED without a provider call.
//
// A change this broad is a mislabelled refactor rather than a bug fix, and a
// line-level walk cannot name the origin of one anyway — so the ceiling saves
// the provider budget rather than trading accuracy for it.
const DefaultMaxChangedFiles = 100

// Store is the pipeline's durable state: the work list a walk claims from, and
// the candidate rows a walk writes to. store/postgres is the reference
// implementation.
//
// Every method is scoped by organisation. A claim is leased rather than locked,
// so a worker that dies mid-walk has its fix re-offered once the lease lapses.
//
// [Store.MarkPrepared] and [Store.Settle] are gated on the claim token, so a
// worker whose lease was re-claimed cannot advance or settle the fix underneath
// its replacement. [Store.SubmitBlame] is not gated, because a provider's submit
// carries no token; it is safe without one by being idempotent per culprit
// commit and forward-only. Each method is safe to call from several workers at
// once.
type Store interface {
	// Enqueue inserts a fix's PENDING lifecycle row. It is insert-if-absent, so a
	// re-delivered event or a sweep re-scan never resets a fix already advanced.
	Enqueue(ctx context.Context, ref FixRef) error

	// ClaimPendingBlame takes the lease on up to limit claimable fixes for one
	// connector — a fix that is PENDING, PREPARED or BLAMED and either never
	// claimed or past its lease — rotating a fresh claim token per row and
	// stamping the lease clock. The returned claims carry their new tokens.
	ClaimPendingBlame(ctx context.Context, orgID, connectorID string, limit int, lease time.Duration) ([]Claim, error)

	// MarkPrepared advances a claimed fix to PREPARED. It is gated on the claim
	// token, so a stale worker whose lease was re-claimed matches no row, and
	// reports whether the gate matched.
	MarkPrepared(ctx context.Context, ref FixRef, claimToken string) (bool, error)

	// Settle writes a terminal outcome reached before any provider call, while
	// the worker still holds the claim. Gated on the claim token; reports whether
	// the gate matched.
	Settle(ctx context.Context, ref FixRef, claimToken string, outcome defectlineagev1.DefectLineageTerminalOutcome) (bool, error)

	// SubmitBlame applies a walk's outcome: it writes the candidate rows and
	// advances the fix's stage. It carries no claim token, and it is
	// forward-only — it never disturbs a fix already RESOLVED, so a re-offered
	// duplicate submit cannot un-resolve one. Writing candidates is idempotent: a
	// re-drive of the same fix converges on the same rows.
	SubmitBlame(ctx context.Context, ref FixRef, outcome *defectlineagev1.LineageBlameOutcome) error

	// ClaimUnscored leases up to limit of the connector's candidate rows that
	// still have no confidence score, grouped by fix so each fix's diff is read
	// once.
	//
	// It is a lease, not a read, for the same reason the blame claim is: scoring
	// a candidate costs provider calls, and two workers that both saw it unscored
	// would both pay for the same answer. A worker that dies mid-score has its
	// candidates re-offered once the lease lapses.
	//
	// It returns the stored rows only: a fix's anchors are not stored, so pairing
	// each group with its [FixInput] to build a scoring request is the
	// orchestrator's step, not the store's.
	ClaimUnscored(ctx context.Context, orgID, connectorID string, limit int, lease time.Duration) ([]UnscoredFix, error)

	// WriteScore records one candidate's confidence score and the derivation
	// behind it. candidateLine is persisted only when non-zero, so a failed
	// recovery leaves the stored column unchanged rather than zeroing it.
	WriteScore(ctx context.Context, orgID, candidateLineageID string, score float64, evidence *defectlineagev1.DefectScoreEvidence, candidateLine int32) error

	// SettleUnscorable terminates a fix whose scoring input is permanently gone.
	//
	// It is the escape hatch for a fix past the blame stages: those are no longer
	// claimable, so the blame drain cannot settle it, and ResolveIfFullyScored
	// never fires because the candidate it waits on can never be scored. Without
	// this a fix in that position stays non-terminal forever.
	//
	// It carries no claim token, because the caller holds no claim on a fix at
	// this stage, and it is forward-only: a fix already RESOLVED is left alone.
	SettleUnscorable(ctx context.Context, ref FixRef, outcome defectlineagev1.DefectLineageTerminalOutcome) (bool, error)

	// ResolveIfFullyScored moves a CANDIDATES_FOUND fix to RESOLVED once no
	// candidate of the fix still lacks a score. It is a no-op while any candidate
	// is unscored, and once the fix is already terminal. It reports whether it
	// advanced the fix.
	ResolveIfFullyScored(ctx context.Context, ref FixRef) (bool, error)

	// ListCandidates returns every stored candidate of one fix, which is the read
	// a caller surfaces.
	ListCandidates(ctx context.Context, orgID, bugFixPRID string) ([]Candidate, error)

	// GetCandidate returns one candidate row by its own id. A structural verdict
	// names the candidate it scored and not the fix it belongs to, so this is how
	// the fix is recovered. It returns [ErrCandidateNotFound] when the row is
	// gone, which happens when a purge or a re-drive ran between the claim and
	// the verdict.
	GetCandidate(ctx context.Context, orgID, candidateID string) (Candidate, error)

	// LookupFix returns a fix's full reference from its lifecycle row. A submit
	// arriving from a provider carries only the organisation and the fix, so this
	// is how the connector and repository are recovered rather than guessed. It
	// returns [ErrFixNotFound] when the fix has no lifecycle row.
	LookupFix(ctx context.Context, orgID, bugFixPRID string) (FixRef, error)
}

// ErrFixNotFound is returned by [Store.LookupFix] for a fix with no lifecycle
// row — one never enqueued, or already purged.
var ErrFixNotFound = errors.New("defectlineage: fix not found")

// ErrCandidateNotFound is returned by [Store.GetCandidate] for a candidate row
// that no longer exists.
var ErrCandidateNotFound = errors.New("defectlineage: candidate not found")

// ErrUnprocessable wraps a write a store rejected for the input's own shape —
// an identifier the store's schema cannot represent, say — rather than for a
// condition that might pass later.
//
// It exists so a queue-driven caller can tell the two apart. Retrying an
// unprocessable event redelivers it forever, which is why a store that knows a
// failure is permanent says so instead of leaving the caller to guess from the
// driver's error text.
var ErrUnprocessable = errors.New("defectlineage: event cannot be stored")

// Claim is one leased fix: the reference the lease was taken on, and the token
// that gates every write against it.
type Claim struct {
	// Ref identifies the fix.
	Ref FixRef
	// ClaimToken gates MarkPrepared and Settle. It rotates on every claim, so a
	// worker holding an old token writes nothing.
	ClaimToken string
	// Stage is the fix's stage at claim time.
	Stage defectlineagev1.DefectLineageStage
	// AttemptCount is how many times this fix has been claimed, including this
	// one — the observable retry depth.
	AttemptCount int32
}

// UnscoredFix is one fix's candidate rows that still lack a confidence score.
type UnscoredFix struct {
	// Ref identifies the fix.
	Ref FixRef
	// Candidates are its unscored candidate rows.
	Candidates []*defectlineagev1.PendingStructuralCandidate
}

// Candidate is one stored candidate culprit commit and the evidence behind it.
// The culprit commit is the durable key: it always exists, where the pull
// request that carried it may not.
type Candidate struct {
	// ID is the candidate row's own identifier.
	ID string
	// Ref identifies the fix this candidate was found for.
	Ref FixRef
	// CommitSHA is the culprit commit.
	CommitSHA string
	// CommittedAt is the culprit's committer date, nil when the provider
	// supplied none. The window verdict is derived from it at read time rather
	// than stored, because a stored verdict would keep reporting a candidate as
	// out of window after a widened window brought it back in.
	CommittedAt *time.Time
	// Path and Line locate the culprit's changed region in the culprit's own
	// revision, captured at blame time so the structural scorer needs no
	// re-blame. Nil when the walk could not supply them, and the scorer then
	// degrades to same-file coverage.
	Path *string
	Line *int32
	// CandidatePRNumber is the provider pull request that introduced the culprit,
	// or 0 when the walk resolved none.
	CandidatePRNumber int32
	// EvidenceType is how the culprit was reached.
	EvidenceType defectlineagev1.DefectLineageEvidenceType
	// AuthorName and AuthorEmail are the culprit commit's author, as the
	// provider's blame reported it. Mapping an email to a person is the caller's
	// concern, so no person identifier is stored here.
	AuthorName  string
	AuthorEmail string
	// ConfidenceScore is nil until a scoring pass writes it. The blame drain
	// never sets it: rescoring must not force a re-blame.
	ConfidenceScore *float64
	// ScoreEvidence is the derivation behind ConfidenceScore, nil until scored.
	ScoreEvidence *defectlineagev1.DefectScoreEvidence
}

// BuildClaim turns a claimed fix's input into the blame request a walk consumes.
//
// It returns ok=false with a terminal outcome when there is nothing to blame, so
// the caller settles the fix instead of spending a provider call on it: a fix
// above maxChangedFiles is SKIPPED_OVERSIZED, and a fix whose diff yields no
// blameable anchor — only added guards, only configuration, or a diff the
// provider refused to render — is ANCHORS_EMPTY. Both are answers, not failures.
func BuildClaim(ref FixRef, in FixInput, maxChangedFiles int, opts Options) (*defectlineagev1.PendingLineageBlame, defectlineagev1.DefectLineageTerminalOutcome, bool) {
	if maxChangedFiles <= 0 {
		maxChangedFiles = DefaultMaxChangedFiles
	}
	if in.ChangedFiles > maxChangedFiles {
		opts.obs().Count("fix_skipped_oversized", nil)
		return nil, defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_SKIPPED_OVERSIZED, false
	}

	anchors := ExtractAnchors(in, opts)
	if len(anchors) == 0 {
		return nil, defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_ANCHORS_EMPTY, false
	}

	claim := &defectlineagev1.PendingLineageBlame{
		BugFixPrId:      ref.BugFixPRID,
		PlatformRepoId:  ref.PlatformRepoID,
		RepoFullName:    in.RepoFullName,
		FixCommitSha:    in.FixCommitSHA,
		LocationAnchors: make([]*defectlineagev1.LineageBlameAnchor, 0, len(anchors)),
	}
	for _, a := range anchors {
		claim.LocationAnchors = append(claim.LocationAnchors, &defectlineagev1.LineageBlameAnchor{
			Path: a.OldPath,
			// The removed run, not the hunk span. The field is defined as the
			// first and last removed line, and a hunk carries leading context, so
			// the span would be systematically early by the context count — and
			// would disagree with the same field built on the scoring path.
			StartLine:   int32(a.RemovedStartLine),
			EndLine:     int32(a.RemovedEndLine),
			RemovedText: a.RemovedText,
		})
	}
	return claim, defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_UNSPECIFIED, true
}

// OutcomeForState maps a walk's terminal state to the outcome its lifecycle row
// records. A RESOLVED walk is deliberately absent: it has candidates to score,
// so its stage advances to CANDIDATES_FOUND and the outcome stays unset until
// scoring finishes.
func OutcomeForState(s defectlineagev1.DefectLineageState) (defectlineagev1.DefectLineageTerminalOutcome, bool) {
	switch s {
	case defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_UNATTRIBUTED:
		return defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_UNATTRIBUTED, true
	case defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_ANCHORS_EMPTY:
		return defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_ANCHORS_EMPTY, true
	case defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_SKIPPED_OVERSIZED:
		return defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_SKIPPED_OVERSIZED, true
	case defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_BLAME_UNAVAILABLE,
		defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_ANCHOR_UNREACHABLE:
		return defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_BLAME_UNAVAILABLE, true
	default:
		return defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_UNSPECIFIED, false
	}
}
