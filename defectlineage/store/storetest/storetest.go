// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package storetest is the conformance suite every [defectlineage.Store] must
// pass.
//
// It exists so the two shipped stores follow one set of rules rather than two.
// Each rule here is a property the pipeline depends on, asserted only through
// the Store interface — never through a store's own internals — so passing it
// means an implementation is interchangeable, not merely similar.
//
// A third store passes this suite or it is not a Store.
package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// NewStore builds a store for one sub-test. It is called per sub-test, so an
// implementation may return a fresh instance or a shared one; the suite never
// assumes an empty store and scopes every assertion to a fix of its own.
type NewStore func(t *testing.T) defectlineage.Store

const (
	connectorID = "github"
	repoID      = "conformance-repo"
	lease       = time.Minute
)

// RunConformance runs every rule against the store newStore builds.
func RunConformance(t *testing.T, newStore NewStore) {
	t.Helper()
	t.Run("APipelineRunsFromEnqueueToScoredCandidate", func(t *testing.T) {
		pipelineRunsEndToEnd(t, newStore(t))
	})
	t.Run("EnqueueDoesNotResetAnAdvancedFix", func(t *testing.T) {
		enqueueDoesNotReset(t, newStore(t))
	})
	t.Run("AStaleClaimTokenWritesNothing", func(t *testing.T) {
		staleTokenWritesNothing(t, newStore(t))
	})
	t.Run("ADuplicateSubmitNeverUnresolvesAFix", func(t *testing.T) {
		duplicateSubmitNeverUnresolves(t, newStore(t))
	})
	t.Run("ARedriveKeepsARecoveredCulpritLine", func(t *testing.T) {
		redriveKeepsLine(t, newStore(t))
	})
	t.Run("ATerminalFixIsNeverReoffered", func(t *testing.T) {
		terminalFixNeverReoffered(t, newStore(t))
	})
	t.Run("ABlamedFixAwaitingScoringIsNeverRewalked", func(t *testing.T) {
		blamedFixIsNeverRewalked(t, newStore(t))
	})
	t.Run("AScoringClaimIsLeased", func(t *testing.T) {
		scoringClaimIsLeased(t, newStore(t))
	})
	t.Run("ACandidatesFoundFixAlwaysHasAWayOut", func(t *testing.T) {
		candidatesFoundHasAWayOut(t, newStore(t))
	})
	t.Run("AResultNamingNoCulpritStoresNothing", func(t *testing.T) {
		noCulpritStoresNothing(t, newStore(t))
	})
	t.Run("LookupsReportAMissingRowAsSuch", func(t *testing.T) {
		lookupsReportMissing(t, newStore(t))
	})
	t.Run("ALiveClaimTokenCannotMoveAFixBackwards", func(t *testing.T) {
		liveTokenCannotRegress(t, newStore(t))
	})
	t.Run("ASubmitAfterATerminalSettleWritesNoCandidate", func(t *testing.T) {
		submitAfterSettleWritesNothing(t, newStore(t))
	})
}

// liveTokenCannotRegress pins the half of the lease contract a stale-token check
// cannot see. AStaleClaimTokenWritesNothing holds that a token the store has
// rotated past matches no row; this holds that the *current* token still cannot
// walk a fix backwards out of a stage it has already left.
//
// The sequence is the one the lease exists for: worker A overruns its lease,
// worker B re-claims and takes the live token, and A's walk then completes.
// Without a stage arm on MarkPrepared, B's token re-opens a RESOLVED fix to the
// blame drain and strands it at CANDIDATES_FOUND with every candidate already
// scored, where the scoring pass never looks again.
func liveTokenCannotRegress(t *testing.T, s defectlineage.Store) {
	ctx := context.Background()
	ref := freshRef()
	require.NoError(t, s.Enqueue(ctx, ref))

	first, ok := claimOne(t, s, ref, lease)
	require.True(t, ok)
	_, err := s.MarkPrepared(ctx, ref, first.ClaimToken)
	require.NoError(t, err)

	// A zero lease re-offers the row, so this is the live token from here on.
	live, ok := claimOne(t, s, ref, 0)
	require.True(t, ok)

	require.NoError(t, s.SubmitBlame(ctx, ref, resolvedOutcome(ref, "culprit")))
	groups, err := s.ClaimUnscored(ctx, ref.OrganizationID, ref.ConnectorID, 10, lease)
	require.NoError(t, err)
	require.NotEmpty(t, groups)
	require.NoError(t, s.WriteScore(ctx, ref.OrganizationID,
		groups[0].Candidates[0].GetCandidateLineageId(), 0.5, nil, 0))
	_, err = s.ResolveIfFullyScored(ctx, ref)
	require.NoError(t, err)

	// Resolved is observed through the interface: a resolved fix is no longer
	// offered for blame.
	_, stillOffered := claimOne(t, s, ref, 0)
	require.False(t, stillOffered, "a scored fix is resolved and off the blame queue")

	moved, err := s.MarkPrepared(ctx, ref, live.ClaimToken)
	require.NoError(t, err)
	assert.False(t, moved, "a live token must not re-open a resolved fix")

	moved, err = s.Settle(ctx, ref, live.ClaimToken,
		defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_ANCHORS_EMPTY)
	require.NoError(t, err)
	assert.False(t, moved, "nor stamp a terminal outcome over its candidates")

	_, ok = claimOne(t, s, ref, 0)
	assert.False(t, ok, "and the fix must still not be offered for blame")

	// Nothing is left unscored either: a regressed fix would hand its already
	// scored candidate back to the scoring pass.
	groups, err = s.ClaimUnscored(ctx, ref.OrganizationID, ref.ConnectorID, 10, lease)
	require.NoError(t, err)
	assert.Empty(t, groups, "a resolved fix owes no further score")
}

// submitAfterSettleWritesNothing pins that a late walk cannot attach candidates
// to a fix already settled. The stage move is forward-only on both stores, but
// the candidate writes run before it, so a fix could read UNATTRIBUTED — no
// culprit found — with candidate rows sitting beside it.
func submitAfterSettleWritesNothing(t *testing.T, s defectlineage.Store) {
	ctx := context.Background()
	ref := freshRef()
	require.NoError(t, s.Enqueue(ctx, ref))

	c, ok := claimOne(t, s, ref, lease)
	require.True(t, ok)
	moved, err := s.Settle(ctx, ref, c.ClaimToken,
		defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_UNATTRIBUTED)
	require.NoError(t, err)
	require.True(t, moved)

	require.NoError(t, s.SubmitBlame(ctx, ref, resolvedOutcome(ref, "late-culprit")))

	cands, err := s.ListCandidates(ctx, ref.OrganizationID, ref.BugFixPRID)
	require.NoError(t, err)
	assert.Empty(t, cands, "a settled fix must not gain candidates it says it never found")

	_, stillOffered := claimOne(t, s, ref, 0)
	assert.False(t, stillOffered, "and it stays terminal")
}

func freshRef() defectlineage.FixRef {
	return defectlineage.FixRef{
		OrganizationID: uuid.NewString(),
		ConnectorID:    connectorID,
		PlatformRepoID: repoID,
		BugFixPRID:     uuid.NewString(),
	}
}

func resolvedOutcome(ref defectlineage.FixRef, sha string, opts ...func(*defectlineagev1.LineageBlameResult)) *defectlineagev1.LineageBlameOutcome {
	r := &defectlineagev1.LineageBlameResult{
		Path: "pkg/a.go", StartLine: 10, CulpritSha: sha, CulpritPath: "pkg/a.go",
		EvidenceType: defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_DIRECT,
		Author:       &defectlineagev1.LineageBlameAuthor{Name: "A Dev", Email: "dev@example.com"},
	}
	for _, o := range opts {
		o(r)
	}
	return &defectlineagev1.LineageBlameOutcome{
		OrganizationId: ref.OrganizationID,
		BugFixPrId:     ref.BugFixPRID,
		Status:         defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_RESOLVED,
		Results:        []*defectlineagev1.LineageBlameResult{r},
	}
}

func claimOne(t *testing.T, s defectlineage.Store, ref defectlineage.FixRef, lease time.Duration) (defectlineage.Claim, bool) {
	t.Helper()
	claims, err := s.ClaimPendingBlame(context.Background(), ref.OrganizationID, ref.ConnectorID, 50, lease)
	require.NoError(t, err)
	for _, c := range claims {
		if c.Ref.BugFixPRID == ref.BugFixPRID {
			return c, true
		}
	}
	return defectlineage.Claim{}, false
}

func pipelineRunsEndToEnd(t *testing.T, s defectlineage.Store) {
	ctx := context.Background()
	ref := freshRef()
	require.NoError(t, s.Enqueue(ctx, ref))

	claim, ok := claimOne(t, s, ref, lease)
	require.True(t, ok, "an enqueued fix must be claimable")
	assert.Equal(t, defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_PENDING, claim.Stage)
	assert.EqualValues(t, 1, claim.AttemptCount, "the claim counts as one attempt")

	moved, err := s.MarkPrepared(ctx, ref, claim.ClaimToken)
	require.NoError(t, err)
	assert.True(t, moved)

	committedAt := time.Now().UTC().Add(-30 * 24 * time.Hour).Truncate(time.Second)
	require.NoError(t, s.SubmitBlame(ctx, ref, resolvedOutcome(ref, "abc123def456",
		func(r *defectlineagev1.LineageBlameResult) {
			r.CommittedAt = timestamppb.New(committedAt)
			r.CandidatePrNumber = 42
		})))

	groups, err := s.ClaimUnscored(ctx, ref.OrganizationID, ref.ConnectorID, 50, lease)
	require.NoError(t, err)
	require.Len(t, groups, 1, "the submitted candidate must appear as unscored")
	require.Len(t, groups[0].Candidates, 1)
	candidateID := groups[0].Candidates[0].GetCandidateLineageId()

	resolved, err := s.ResolveIfFullyScored(ctx, ref)
	require.NoError(t, err)
	assert.False(t, resolved, "a fix with an unscored candidate must not resolve")

	evidence := &defectlineagev1.DefectScoreEvidence{
		LineageStrength: 1.0, CodeContext: 0.3, Deployment: 0.6, Recency: 0.72,
		AstMatchLevel: "same_file", Degraded: true,
		DirectnessTier: defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_DIRECT,
	}
	require.NoError(t, s.WriteScore(ctx, ref.OrganizationID, candidateID, 0.653, evidence, 7))

	resolved, err = s.ResolveIfFullyScored(ctx, ref)
	require.NoError(t, err)
	assert.True(t, resolved, "a fully scored fix resolves")

	got, err := s.ListCandidates(ctx, ref.OrganizationID, ref.BugFixPRID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	c := got[0]
	assert.Equal(t, "abc123def456", c.CommitSHA)
	assert.Equal(t, "dev@example.com", c.AuthorEmail)
	assert.EqualValues(t, 42, c.CandidatePRNumber)
	require.NotNil(t, c.ConfidenceScore)
	assert.InDelta(t, 0.653, *c.ConfidenceScore, 1e-9)
	require.NotNil(t, c.CommittedAt)
	assert.True(t, committedAt.Equal(*c.CommittedAt), "the committer date round-trips as UTC seconds")
	require.NotNil(t, c.Line)
	assert.EqualValues(t, 7, *c.Line, "a recovered line is persisted")
	require.NotNil(t, c.ScoreEvidence)
	assert.True(t, c.ScoreEvidence.GetDegraded(), "the derivation round-trips")
	assert.Equal(t, "same_file", c.ScoreEvidence.GetAstMatchLevel())

	byID, err := s.GetCandidate(ctx, ref.OrganizationID, candidateID)
	require.NoError(t, err)
	assert.Equal(t, ref.BugFixPRID, byID.Ref.BugFixPRID, "a candidate knows the fix it belongs to")
	assert.Equal(t, ref.ConnectorID, byID.Ref.ConnectorID)
	require.NotNil(t, byID.ScoreEvidence,
		"a candidate read by id carries the same derivation as one read by fix")
	assert.Equal(t, "same_file", byID.ScoreEvidence.GetAstMatchLevel())
}

func enqueueDoesNotReset(t *testing.T, s defectlineage.Store) {
	ctx := context.Background()
	ref := freshRef()
	require.NoError(t, s.Enqueue(ctx, ref))
	claim, ok := claimOne(t, s, ref, lease)
	require.True(t, ok)
	_, err := s.MarkPrepared(ctx, ref, claim.ClaimToken)
	require.NoError(t, err)

	require.NoError(t, s.Enqueue(ctx, ref), "a re-delivery is accepted")

	// Re-claiming with a lapsed lease is how the stage and attempt count are read
	// back through the contract.
	again, ok := claimOne(t, s, ref, 0)
	require.True(t, ok)
	assert.Equal(t, defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_PREPARED, again.Stage,
		"the re-delivery must leave the advanced stage alone")
	assert.EqualValues(t, 2, again.AttemptCount,
		"the re-delivery itself must not count as an attempt")
}

func staleTokenWritesNothing(t *testing.T, s defectlineage.Store) {
	ctx := context.Background()
	ref := freshRef()
	require.NoError(t, s.Enqueue(ctx, ref))

	stale, ok := claimOne(t, s, ref, lease)
	require.True(t, ok)

	// A zero lease makes a claimed row immediately re-claimable, which is the
	// state a worker that overran its lease leaves behind.
	fresh, ok := claimOne(t, s, ref, 0)
	require.True(t, ok, "a lapsed claim is re-offered")
	require.NotEqual(t, stale.ClaimToken, fresh.ClaimToken, "re-claiming rotates the token")

	moved, err := s.Settle(ctx, ref, stale.ClaimToken,
		defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_UNATTRIBUTED)
	require.NoError(t, err)
	assert.False(t, moved, "the stale worker must match no row")

	moved, err = s.MarkPrepared(ctx, ref, stale.ClaimToken)
	require.NoError(t, err)
	assert.False(t, moved, "and must not advance it either")

	moved, err = s.Settle(ctx, ref, fresh.ClaimToken,
		defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_UNATTRIBUTED)
	require.NoError(t, err)
	assert.True(t, moved, "the holder of the live claim settles it")
}

func duplicateSubmitNeverUnresolves(t *testing.T, s defectlineage.Store) {
	ctx := context.Background()
	ref := freshRef()
	require.NoError(t, s.Enqueue(ctx, ref))
	outcome := resolvedOutcome(ref, "deadbeef1234")
	require.NoError(t, s.SubmitBlame(ctx, ref, outcome))

	groups, err := s.ClaimUnscored(ctx, ref.OrganizationID, ref.ConnectorID, 50, lease)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	require.NoError(t, s.WriteScore(ctx, ref.OrganizationID, groups[0].Candidates[0].GetCandidateLineageId(), 0.5, nil, 0))
	resolved, err := s.ResolveIfFullyScored(ctx, ref)
	require.NoError(t, err)
	require.True(t, resolved)

	// The duplicate arrives after the fix is terminal.
	require.NoError(t, s.SubmitBlame(ctx, ref, outcome))

	_, stillClaimable := claimOne(t, s, ref, 0)
	assert.False(t, stillClaimable, "a duplicate submit must not re-open a resolved fix")

	got, err := s.ListCandidates(ctx, ref.OrganizationID, ref.BugFixPRID)
	require.NoError(t, err)
	assert.Len(t, got, 1, "a re-drive converges on one row per culprit commit")
}

func redriveKeepsLine(t *testing.T, s defectlineage.Store) {
	ctx := context.Background()
	ref := freshRef()
	require.NoError(t, s.Enqueue(ctx, ref))

	require.NoError(t, s.SubmitBlame(ctx, ref, resolvedOutcome(ref, "cafebabe9999",
		func(r *defectlineagev1.LineageBlameResult) { r.CulpritLine = 31 })))
	// The second walk supplies neither the line nor the author — the shape a
	// provider returns when its blame covered the line but named nobody.
	require.NoError(t, s.SubmitBlame(ctx, ref, resolvedOutcome(ref, "cafebabe9999",
		func(r *defectlineagev1.LineageBlameResult) {
			r.CulpritLine = 0
			r.Author = nil
			r.EvidenceType = defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_AFTER_HOPS
		})))

	got, err := s.ListCandidates(ctx, ref.OrganizationID, ref.BugFixPRID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Line)
	assert.EqualValues(t, 31, *got[0].Line, "a re-drive without a line keeps the stored one")
	assert.Equal(t, defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_AFTER_HOPS,
		got[0].EvidenceType, "the newer evidence type does replace the old")
	assert.Equal(t, "dev@example.com", got[0].AuthorEmail,
		"a re-drive that named no author keeps the one an earlier walk found")
}

func terminalFixNeverReoffered(t *testing.T, s defectlineage.Store) {
	ctx := context.Background()
	ref := freshRef()
	require.NoError(t, s.Enqueue(ctx, ref))
	claim, ok := claimOne(t, s, ref, lease)
	require.True(t, ok)

	moved, err := s.Settle(ctx, ref, claim.ClaimToken,
		defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_ANCHORS_EMPTY)
	require.NoError(t, err)
	require.True(t, moved)

	_, stillClaimable := claimOne(t, s, ref, 0)
	assert.False(t, stillClaimable,
		"a settled fix must never be claimed again, however far its lease has lapsed")
}

// blamedFixIsNeverRewalked pins the rule that keeps blame paid for once. After a
// walk succeeds, the fix waits at CANDIDATES_FOUND for a score. Its lease
// eventually lapses — scoring can lag by far more than one lease — and if the
// claim treated "not terminal" as "claimable", every blamed fix would be walked
// again on each pass, buying the same answer from the provider repeatedly.
func blamedFixIsNeverRewalked(t *testing.T, s defectlineage.Store) {
	ctx := context.Background()
	ref := freshRef()
	require.NoError(t, s.Enqueue(ctx, ref))

	claim, ok := claimOne(t, s, ref, lease)
	require.True(t, ok)
	_, err := s.MarkPrepared(ctx, ref, claim.ClaimToken)
	require.NoError(t, err)
	require.NoError(t, s.SubmitBlame(ctx, ref, resolvedOutcome(ref, "beefcafe4321")))

	// The candidate is stored and unscored, so the fix sits at CANDIDATES_FOUND.
	groups, err := s.ClaimUnscored(ctx, ref.OrganizationID, ref.ConnectorID, 50, lease)
	require.NoError(t, err)
	require.Len(t, groups, 1, "the fix is waiting on a score")

	// A zero lease makes every live claim lapse, which is the state a scoring
	// backlog longer than one lease leaves behind.
	_, reoffered := claimOne(t, s, ref, 0)
	assert.False(t, reoffered,
		"a blamed fix awaiting scoring must not be offered for blame again")
}

// scoringClaimIsLeased pins that scoring is claimed, not merely read. Scoring a
// candidate costs provider calls, so two workers that both saw it unscored would
// both pay for the same answer — and a worker that dies mid-score must have its
// candidates come back rather than stall them.
func scoringClaimIsLeased(t *testing.T, s defectlineage.Store) {
	ctx := context.Background()
	ref := freshRef()
	require.NoError(t, s.Enqueue(ctx, ref))
	require.NoError(t, s.SubmitBlame(ctx, ref, resolvedOutcome(ref, "deadc0de1111")))

	first, err := s.ClaimUnscored(ctx, ref.OrganizationID, ref.ConnectorID, 50, lease)
	require.NoError(t, err)
	require.Len(t, first, 1, "an unscored candidate is offered")

	// A second worker, while the first still holds its lease.
	second, err := s.ClaimUnscored(ctx, ref.OrganizationID, ref.ConnectorID, 50, lease)
	require.NoError(t, err)
	assert.Empty(t, second, "a candidate under a live lease is not offered twice")

	// The first worker died. A zero lease is the state that leaves behind.
	third, err := s.ClaimUnscored(ctx, ref.OrganizationID, ref.ConnectorID, 50, 0)
	require.NoError(t, err)
	require.Len(t, third, 1, "a lapsed scoring lease is re-offered")
	assert.Equal(t, first[0].Candidates[0].GetCandidateLineageId(),
		third[0].Candidates[0].GetCandidateLineageId())
}

// candidatesFoundHasAWayOut is the mirror of the no-re-walk rule, and the reason
// that rule is safe. A fix past the blame stages can no longer be settled by the
// blame drain, and if its scoring input is permanently gone its candidates can
// never be scored — so without a terminal write of its own it would stay
// non-terminal forever. Closing the re-walk without opening this is a strand.
func candidatesFoundHasAWayOut(t *testing.T, s defectlineage.Store) {
	ctx := context.Background()
	ref := freshRef()
	require.NoError(t, s.Enqueue(ctx, ref))
	require.NoError(t, s.SubmitBlame(ctx, ref, resolvedOutcome(ref, "aaaabbbbcccc")))

	// It is not blame-claimable, and its candidate cannot be scored.
	_, claimable := claimOne(t, s, ref, 0)
	require.False(t, claimable, "a blamed fix is past the blame stages")

	before, err := s.ClaimUnscored(ctx, ref.OrganizationID, ref.ConnectorID, 50, lease)
	require.NoError(t, err)
	require.Len(t, before, 1, "its candidate is offered for scoring while the fix is live")

	settled, err := s.SettleUnscorable(ctx, ref,
		defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_ANCHORS_EMPTY)
	require.NoError(t, err)
	assert.True(t, settled, "a fix whose scoring input is gone can always be terminated")

	after, err := s.ClaimUnscored(ctx, ref.OrganizationID, ref.ConnectorID, 50, lease)
	require.NoError(t, err)
	assert.Empty(t, after,
		"and its unscored candidate stops being offered, so the dead set costs nothing per pass")

	// Forward-only: a second call changes nothing.
	again, err := s.SettleUnscorable(ctx, ref,
		defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_UNATTRIBUTED)
	require.NoError(t, err)
	assert.False(t, again, "a terminal fix is not re-settled")
}

func noCulpritStoresNothing(t *testing.T, s defectlineage.Store) {
	ctx := context.Background()
	ref := freshRef()
	require.NoError(t, s.Enqueue(ctx, ref))

	require.NoError(t, s.SubmitBlame(ctx, ref, &defectlineagev1.LineageBlameOutcome{
		OrganizationId: ref.OrganizationID,
		BugFixPrId:     ref.BugFixPRID,
		Status:         defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_RESOLVED,
		Results:        []*defectlineagev1.LineageBlameResult{{Path: "pkg/a.go", CulpritSha: ""}},
	}))

	got, err := s.ListCandidates(ctx, ref.OrganizationID, ref.BugFixPRID)
	require.NoError(t, err)
	assert.Empty(t, got, "a result naming no culprit stores no row")

	_, stillClaimable := claimOne(t, s, ref, 0)
	assert.False(t, stillClaimable, "an unattributed fix is terminal, not retried")

	// And it is genuinely terminal rather than parked awaiting a score that can
	// never arrive: there is no candidate row, so nothing would ever advance it.
	settled, err := s.SettleUnscorable(ctx, ref,
		defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_UNATTRIBUTED)
	require.NoError(t, err)
	assert.False(t, settled, "it reached a terminal stage at submit, not later")
}

func lookupsReportMissing(t *testing.T, s defectlineage.Store) {
	ctx := context.Background()
	ref := freshRef()

	_, err := s.LookupFix(ctx, ref.OrganizationID, ref.BugFixPRID)
	assert.ErrorIs(t, err, defectlineage.ErrFixNotFound)

	_, err = s.GetCandidate(ctx, ref.OrganizationID, uuid.NewString())
	assert.ErrorIs(t, err, defectlineage.ErrCandidateNotFound)

	// A fix with a lifecycle row but no candidates is found, and lists none.
	require.NoError(t, s.Enqueue(ctx, ref))
	found, err := s.LookupFix(ctx, ref.OrganizationID, ref.BugFixPRID)
	require.NoError(t, err)
	assert.Equal(t, ref.ConnectorID, found.ConnectorID)
	assert.Equal(t, ref.PlatformRepoID, found.PlatformRepoID)
}
