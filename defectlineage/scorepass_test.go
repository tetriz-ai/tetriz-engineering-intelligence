// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/store/memory"
	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// scoringProvider serves the two files a structural comparison reads.
type scoringProvider struct {
	content  []byte
	fileErr  error
	parentOf map[string]string
}

func (p scoringProvider) FirstParent(_ context.Context, sha string) (string, error) {
	return p.parentOf[sha], nil
}

func (p scoringProvider) FileAt(_ context.Context, _, _ string) ([]byte, error) {
	if p.fileErr != nil {
		return nil, p.fileErr
	}
	return p.content, nil
}

// blamedFix puts one unscored candidate in the store, the state a successful
// walk leaves behind.
func blamedFix(t *testing.T, s defectlineage.Store, line int32) defectlineage.FixRef {
	t.Helper()
	ctx := context.Background()
	ref := drainRef()
	require.NoError(t, s.Enqueue(ctx, ref))
	committed := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, s.SubmitBlame(ctx, ref, &defectlineagev1.LineageBlameOutcome{
		OrganizationId: ref.OrganizationID, BugFixPrId: ref.BugFixPRID,
		Status: defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_RESOLVED,
		Results: []*defectlineagev1.LineageBlameResult{{
			Path: "pkg/math.go", CulpritSha: "CULPRIT", CulpritPath: "pkg/math.go",
			CulpritLine:  line,
			CommittedAt:  timestamp(committed),
			EvidenceType: defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_DIRECT,
		}},
	}))
	return ref
}

func newPass(t *testing.T, s defectlineage.Store, r defectlineage.FixResolver, cmp defectlineage.Comparator) *defectlineage.ScoringPass {
	t.Helper()
	p, err := defectlineage.NewScoringPass(defectlineage.ScoringParams{
		Store: s, Resolver: r, Comparator: cmp,
	})
	require.NoError(t, err)
	return p
}

func resolverFor(in defectlineage.FixInput) defectlineage.FixResolver {
	return defectlineage.FixResolverFunc(
		func(context.Context, defectlineage.FixRef) (defectlineage.FixInput, error) { return in, nil })
}

func TestScoringPassScoresAndResolvesAFix(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	ref := blamedFix(t, s, 4)

	pass := newPass(t, s, resolverFor(goodInput()), defectlineage.CoverageComparator{})
	stats, err := pass.Run(ctx, ref.OrganizationID, ref.ConnectorID, 10,
		scoringProvider{content: []byte("package p\n\nfunc Divide(a, b int) int {\n\treturn a / b\n}\n"),
			parentOf: map[string]string{"FIXCOMMIT": "PARENT"}})
	require.NoError(t, err)
	assert.Equal(t, 1, stats.Scored)
	assert.Equal(t, 1, stats.FixesResolved)

	got, err := s.ListCandidates(ctx, ref.OrganizationID, ref.BugFixPRID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].ConfidenceScore)
	assert.Greater(t, *got[0].ConfidenceScore, 0.0)
	assert.LessOrEqual(t, *got[0].ConfidenceScore, 1.0)
	require.NotNil(t, got[0].ScoreEvidence)

	stage, outcome, _ := s.Stage(ref.OrganizationID, ref.BugFixPRID)
	assert.Equal(t, defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED, stage)
	assert.Equal(t, defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_SCORED_OK, outcome)
}

// TestScoringDegradesRatherThanFailing: a file it cannot read is "could not
// tell", not a missing score, and the stored derivation says so.
func TestScoringDegradesRatherThanFailing(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	ref := blamedFix(t, s, 4)

	pass := newPass(t, s, resolverFor(goodInput()), defectlineage.CoverageComparator{})
	stats, err := pass.Run(ctx, ref.OrganizationID, ref.ConnectorID, 10,
		scoringProvider{fileErr: errors.New("too large"), parentOf: map[string]string{"FIXCOMMIT": "PARENT"}})
	require.NoError(t, err)
	assert.Equal(t, 1, stats.Scored, "it still scores")
	assert.Equal(t, 1, stats.Degraded)

	got, err := s.ListCandidates(ctx, ref.OrganizationID, ref.BugFixPRID)
	require.NoError(t, err)
	require.NotNil(t, got[0].ScoreEvidence)
	assert.True(t, got[0].ScoreEvidence.GetDegraded(),
		"a reader must be able to tell coverage from a measured match")
}

// TestScoringTerminatesAFixWhoseInputIsGone is the strand guard. A fix past the
// blame stages cannot be settled by the blame drain, so the scoring pass owes it
// a terminal write.
func TestScoringTerminatesAFixWhoseInputIsGone(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	ref := blamedFix(t, s, 4)

	gone := defectlineage.FixResolverFunc(
		func(context.Context, defectlineage.FixRef) (defectlineage.FixInput, error) {
			return defectlineage.FixInput{}, defectlineage.ErrNoFixInput
		})
	pass := newPass(t, s, gone, defectlineage.CoverageComparator{})

	stats, err := pass.Run(ctx, ref.OrganizationID, ref.ConnectorID, 10, scoringProvider{})
	require.NoError(t, err)
	assert.Equal(t, 1, stats.Settled)
	assert.Zero(t, stats.Failed)

	stage, _, _ := s.Stage(ref.OrganizationID, ref.BugFixPRID)
	assert.Equal(t, defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED, stage,
		"a fix whose scoring input is permanently gone must not stay non-terminal")

	// And it stops costing work.
	next, err := pass.Run(ctx, ref.OrganizationID, ref.ConnectorID, 10, scoringProvider{})
	require.NoError(t, err)
	assert.Zero(t, next.Settled)
	assert.Zero(t, next.Failed)
}

// TestScoringLeavesATransientResolveFailure: anything other than the permanent
// answer is retried rather than settled.
func TestScoringLeavesATransientResolveFailure(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	ref := blamedFix(t, s, 4)

	flaky := defectlineage.FixResolverFunc(
		func(context.Context, defectlineage.FixRef) (defectlineage.FixInput, error) {
			return defectlineage.FixInput{}, errors.New("store unreachable")
		})
	pass := newPass(t, s, flaky, defectlineage.CoverageComparator{})
	stats, err := pass.Run(ctx, ref.OrganizationID, ref.ConnectorID, 10, scoringProvider{})
	require.NoError(t, err)
	assert.Equal(t, 1, stats.Failed)
	assert.Zero(t, stats.Settled)

	stage, _, _ := s.Stage(ref.OrganizationID, ref.BugFixPRID)
	assert.Equal(t, defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_CANDIDATES_FOUND, stage,
		"a transient failure leaves the fix for a later pass")
}

func TestNewScoringPassRequiresItsDependencies(t *testing.T) {
	_, err := defectlineage.NewScoringPass(defectlineage.ScoringParams{})
	require.Error(t, err)

	_, err = defectlineage.NewScoringPass(defectlineage.ScoringParams{Store: memory.New()})
	require.Error(t, err)
}

// recentFixes is a FixSupplier over a fixed list.
type recentFixes struct {
	refs []defectlineage.FixRef
	err  error
	last time.Time
}

func (r *recentFixes) RecentBugFixes(_ context.Context, _ string, since time.Time, limit int) ([]defectlineage.FixRef, error) {
	if r.err != nil {
		return nil, r.err
	}
	r.last = since
	if len(r.refs) > limit {
		return r.refs[:limit], nil
	}
	return r.refs, nil
}

// TestSweepEnqueuesWhatTheEventPathMissed is the backstop's whole purpose: a fix
// merged before tracing was switched on, or whose event was dropped.
func TestSweepEnqueuesWhatTheEventPathMissed(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	refs := []defectlineage.FixRef{
		{OrganizationID: "org-1", ConnectorID: "github", PlatformRepoID: "r", BugFixPRID: "a"},
		{OrganizationID: "org-1", ConnectorID: "github", PlatformRepoID: "r", BugFixPRID: "b"},
	}

	stats, err := defectlineage.RunSweep(ctx, s, &recentFixes{refs: refs}, "org-1", defectlineage.SweepConfig{})
	require.NoError(t, err)
	assert.Equal(t, 2, stats.Scanned)
	assert.Equal(t, 2, stats.Enqueued)

	claims, err := s.ClaimPendingBlame(ctx, "org-1", "github", 10, time.Minute)
	require.NoError(t, err)
	assert.Len(t, claims, 2, "both reach the blame queue")
}

// TestSweepDoesNotDisturbAFixAlreadyInFlight. The sweep runs on a schedule
// beside the event path, so it must be safe to re-offer what is already moving.
func TestSweepDoesNotDisturbAFixAlreadyInFlight(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	ref := defectlineage.FixRef{OrganizationID: "org-1", ConnectorID: "github", PlatformRepoID: "r", BugFixPRID: "a"}
	require.NoError(t, s.Enqueue(ctx, ref))
	claims, err := s.ClaimPendingBlame(ctx, "org-1", "github", 10, time.Minute)
	require.NoError(t, err)
	_, err = s.MarkPrepared(ctx, ref, claims[0].ClaimToken)
	require.NoError(t, err)

	stats, err := defectlineage.RunSweep(ctx, s, &recentFixes{refs: []defectlineage.FixRef{ref}}, "org-1", defectlineage.SweepConfig{})
	require.NoError(t, err)
	assert.Equal(t, 1, stats.Enqueued, "it is offered")

	stage, _, _ := s.Stage("org-1", "a")
	assert.Equal(t, defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_PREPARED, stage,
		"and ignored, because enqueueing is insert-if-absent")
}

func TestSweepBoundsItsScan(t *testing.T) {
	ctx := context.Background()
	var refs []defectlineage.FixRef
	for i := 0; i < 10; i++ {
		refs = append(refs, defectlineage.FixRef{
			OrganizationID: "org-1", ConnectorID: "github", PlatformRepoID: "r",
			BugFixPRID: string(rune('a' + i)),
		})
	}
	supplier := &recentFixes{refs: refs}
	stats, err := defectlineage.RunSweep(ctx, memory.New(), supplier, "org-1",
		defectlineage.SweepConfig{BatchSize: 3, Lookback: 48 * time.Hour})
	require.NoError(t, err)
	assert.Equal(t, 3, stats.Scanned, "a large backlog drains over several ticks")
	assert.WithinDuration(t, time.Now().UTC().Add(-48*time.Hour), supplier.last, time.Minute,
		"the lookback bounds how far back it reaches")
}

func TestSweepRequiresItsDependencies(t *testing.T) {
	ctx := context.Background()
	_, err := defectlineage.RunSweep(ctx, nil, &recentFixes{}, "org-1", defectlineage.SweepConfig{})
	require.Error(t, err)
	_, err = defectlineage.RunSweep(ctx, memory.New(), nil, "org-1", defectlineage.SweepConfig{})
	require.Error(t, err)
	_, err = defectlineage.RunSweep(ctx, memory.New(), &recentFixes{err: errors.New("down")}, "org-1", defectlineage.SweepConfig{})
	require.Error(t, err, "a supplier failure is the caller's to see")
}
