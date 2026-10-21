// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/runner"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// These are the properties a stateful engine has to prove and an in-memory fake
// cannot: that work survives a crash, that two workers cannot take the same
// session, and that re-running converges instead of duplicating.

const org = "org-1"

type fixedIdentity struct{}

func (fixedIdentity) Resolve(_ context.Context, _, _ string) (attribution.Identity, error) {
	return attribution.Identity{AuthorIDs: []string{"vendor-1"}}, nil
}

type fixedCandidates struct{ prs []*attributionv1.PRCandidate }

func (f fixedCandidates) ListAuthoredPRs(_ context.Context, _ string, _ []string, _, _ time.Time) ([]*attributionv1.PRCandidate, error) {
	return f.prs, nil
}
func (f fixedCandidates) ListReviewedPRs(_ context.Context, _ string, _ []string, _, _ time.Time) ([]*attributionv1.PRCandidate, error) {
	return nil, nil
}

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir() + "/attribution.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func session(id string, at time.Time) *attributionv1.SessionEvidence {
	return &attributionv1.SessionEvidence{
		SessionId:    id,
		PersonId:     "p1",
		OrgId:        org,
		StartDate:    timestamppb.New(at),
		IdeType:      "IDE_TYPE_CLAUDE_CODE",
		PrNumber:     42,
		PrRepository: "org/repo",
	}
}

func matchingCandidate(at time.Time) *attributionv1.PRCandidate {
	return &attributionv1.PRCandidate{
		PrKey:        "org/repo#PR-42#merged",
		PrRepository: "org/repo",
		PrNumber:     42,
		MergedAt:     timestamppb.New(at),
	}
}

func newRunner(s *Store, at time.Time) *runner.Runner {
	e := attribution.New(attribution.DefaultParams(),
		fixedCandidates{prs: []*attributionv1.PRCandidate{matchingCandidate(at)}}, fixedIdentity{})
	return runner.New(e, s, nil)
}

func TestAttributionSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(24 * time.Hour)
	path := t.TempDir() + "/attribution.db"

	s, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, s.Put(ctx, org, session("s1", now)))
	res, err := newRunner(s, now).RunBatch(ctx, org, 10)
	require.NoError(t, err)
	require.Equal(t, 1, res.Processed)
	require.NoError(t, s.Close())

	// Reopen: the attribution must still be there, and the session must not be
	// offered again, because nothing about it changed.
	reopened, err := Open(path)
	require.NoError(t, err)
	defer func() { _ = reopened.Close() }()

	got, err := reopened.Get(ctx, org, "s1")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, got.GetStoredAttribution().GetMatches(), 1)
	assert.Equal(t, "org/repo#PR-42#merged", got.GetStoredAttribution().GetMatches()[0].GetPrKey())

	pending, err := reopened.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	assert.Empty(t, pending, "a settled session must not be re-offered")
}

// A worker that stops between the load and the write leaves the session as it
// was: still pending, offered again on the next load.
func TestACrashMidBatchLeavesTheSessionPending(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(24 * time.Hour)
	s := openTemp(t)
	require.NoError(t, s.Put(ctx, org, session("s1", now)))

	loaded, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	require.Len(t, loaded, 1)

	again, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	require.Len(t, again, 1)
	assert.Equal(t, "s1", again[0].GetSessionId())
}

func TestRerunningConvergesRatherThanDuplicating(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(24 * time.Hour)
	s := openTemp(t)
	require.NoError(t, s.Put(ctx, org, session("s1", now)))

	r := newRunner(s, now)
	first, err := r.RunBatch(ctx, org, 10)
	require.NoError(t, err)
	require.Equal(t, 1, first.Processed)

	// A second run has nothing to do; a third after a re-attribution request
	// recomputes and still ends with exactly one match, not two.
	second, err := r.RunBatch(ctx, org, 10)
	require.NoError(t, err)
	assert.Zero(t, second.Processed)

	require.NoError(t, s.RequestReattribution(ctx, org, "s1"))
	third, err := r.RunBatch(ctx, org, 10)
	require.NoError(t, err)
	require.Equal(t, 1, third.Processed)

	got, err := s.Get(ctx, org, "s1")
	require.NoError(t, err)
	assert.Len(t, got.GetStoredAttribution().GetMatches(), 1, "re-running must not duplicate a match")
}

func TestAbstainedSessionRetriesAfterCooldown(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(24 * time.Hour)
	s := openTemp(t)
	s.Params.RetryCooldownHours = 1

	// A controllable clock rather than a sleep: last_attempted_at is RFC3339, so
	// it resolves to the second, and any cooldown short enough to sleep through
	// is below the timestamp's own precision.
	clock := time.Now().UTC()
	s.now = func() time.Time { return clock }

	require.NoError(t, s.Put(ctx, org, session("s1", now)))

	// No candidates: the session abstains rather than matching.
	e := attribution.New(attribution.DefaultParams(), fixedCandidates{}, fixedIdentity{})
	r := runner.New(e, s, nil)

	first, err := r.RunBatch(ctx, org, 10)
	require.NoError(t, err)
	require.Equal(t, 1, first.Processed)

	immediate, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	assert.Empty(t, immediate, "an abstention must wait out its cooldown")

	clock = clock.Add(2 * time.Hour)
	later, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	assert.Len(t, later, 1, "an abstention is not final — the evidence may yet arrive")
}

// A row deleted between the load and the write must be reported, not silently
// counted as persisted. Those sessions keep their outstanding request and are
// recomputed every run, so a sustained short count is a starving session rather
// than noise — and it is only visible if the write counts what actually landed.
func TestVanishedRowIsReportedAsShortCount(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC().Truncate(24 * time.Hour)
	s := openTemp(t)
	require.NoError(t, s.Put(ctx, org, session("s1", now), session("s2", now)))

	loaded, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	require.Len(t, loaded, 2)

	// One session is purged while the batch is in flight.
	_, err = s.DB().ExecContext(ctx, `DELETE FROM sessions WHERE org_id = ? AND session_id = ?`, org, "s2")
	require.NoError(t, err)

	e := attribution.New(attribution.DefaultParams(),
		fixedCandidates{prs: []*attributionv1.PRCandidate{matchingCandidate(now)}}, fixedIdentity{})
	results, err := e.Attribute(ctx, org, loaded)
	require.NoError(t, err)

	reduction := attribution.Reduce(results)
	persisted, err := s.WriteResults(ctx, org, reduction, time.Now().UTC())
	require.NoError(t, err)

	assert.Equal(t, 1, persisted, "only the surviving row should count as persisted")
	assert.Len(t, reduction.Attributions, 2, "the batch still carried both")
}

// An attribution with no fields is stored as written, not as no attribution:
// the session is settled, not pending.
func TestAnEmptyAttributionIsStoredAsWritten(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	require.NoError(t, s.Put(ctx, org, session("s1", time.Now().UTC())))
	loaded, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	n, err := s.WriteResults(ctx, org, attribution.Reduction{
		Sessions: loaded, Attributions: []*attributionv1.PRAttribution{{}},
	}, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, 1, n)

	got, err := s.Get(ctx, org, "s1")
	require.NoError(t, err)
	assert.NotNil(t, got.GetStoredAttribution())
	pending, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	assert.Empty(t, pending)
}
