// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package runner_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/runner"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/runner/memory"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/runner/sqlite"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

const org = "org-1"

type flakyCandidates struct{ down *bool }

func (f flakyCandidates) ListAuthoredPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	if *f.down {
		return nil, errors.New("candidate store down")
	}
	return nil, nil
}
func (flakyCandidates) ListReviewedPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return nil, nil
}

type linked struct{}

func (linked) Resolve(context.Context, string, string) (attribution.Identity, error) {
	return attribution.Identity{AuthorIDs: []string{"a"}}, nil
}

func sessionAt(id string, start time.Time) *attributionv1.SessionEvidence {
	return &attributionv1.SessionEvidence{SessionId: id, PersonId: "p", OrgId: org, StartDate: timestamppb.New(start)}
}

func abstainedAt(id string, start, lastAttempt time.Time, reason attributionv1.SessionToPRUnmatchedReason) *attributionv1.SessionEvidence {
	s := sessionAt(id, start)
	s.StoredAttribution = &attributionv1.PRAttribution{Unmatched: &attributionv1.PRUnmatched{
		Reason: reason, LastAttemptedAt: lastAttempt.UTC().Format(time.RFC3339),
	}}
	return s
}

// A session the engine leaves pending, because its candidates could not be
// fetched, is offered again once the store recovers.
func TestALeftPendingSessionIsOfferedAgain(t *testing.T) {
	ctx := context.Background()
	st := memory.New()
	st.Put(org, sessionAt("s1", time.Now().UTC()))
	down := true
	r := runner.New(attribution.New(attribution.DefaultParams(), flakyCandidates{&down}, linked{}), st, nil)

	first, err := r.RunBatch(ctx, org, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, first.Sessions)
	assert.Zero(t, first.Processed)

	down = false
	second, err := r.RunBatch(ctx, org, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, second.Sessions, "the session left pending is offered again")
	assert.Equal(t, 1, second.Processed)
	assert.NotNil(t, st.Get(org, "s1").GetStoredAttribution())
}

type loader interface {
	LoadPending(ctx context.Context, orgID string, limit int) ([]*attributionv1.SessionEvidence, error)
}

func ids(t *testing.T, l loader) []string {
	t.Helper()
	got, err := l.LoadPending(context.Background(), org, 100)
	require.NoError(t, err)
	out := make([]string, 0, len(got))
	for _, s := range got {
		out = append(out, s.GetSessionId())
	}
	return out
}

// Both stores apply the engine's retry policy with its defaults: a 12-hour
// cooldown, and NO_MATCHING_PR permanent after 30 days past last activity.
func TestBothStoresApplyTheRetryPolicy(t *testing.T) {
	now := time.Now().UTC()
	noMatch := attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR
	notLinked := attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_VCS_NOT_LINKED
	sessions := []*attributionv1.SessionEvidence{
		abstainedAt("cooling-down", now, now.Add(-time.Hour), noMatch),
		abstainedAt("cooled-down", now, now.Add(-13*time.Hour), noMatch),
		abstainedAt("past-the-retry-budget", now.AddDate(0, 0, -400), now.Add(-13*time.Hour), noMatch),
		abstainedAt("not-linked-retries-forever", now.AddDate(0, 0, -400), now.Add(-13*time.Hour), notLinked),
	}
	want := []string{"cooled-down", "not-linked-retries-forever"}

	mem := memory.New()
	mem.Put(org, sessions...)
	assert.ElementsMatch(t, want, ids(t, mem), "memory store")

	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.Put(context.Background(), org, sessions...))
	assert.ElementsMatch(t, want, ids(t, db), "sqlite store")
}

// SessionSource promises oldest first; with a limit the oldest is served first.
func TestSQLiteLoadsOldestFirst(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(":memory:")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	now := time.Now().UTC()
	require.NoError(t, db.Put(ctx, org, sessionAt("newer", now)))
	require.NoError(t, db.Put(ctx, org, sessionAt("older", now.AddDate(0, 0, -5))))
	got, err := db.LoadPending(ctx, org, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "older", got[0].GetSessionId())
}

// store is one reference store, so each rule below runs against both.
type store interface {
	put(sessions ...*attributionv1.SessionEvidence)
	request(id string)
	get(id string) *attributionv1.SessionEvidence
	load(limit int) ([]*attributionv1.SessionEvidence, error)
	write(r attribution.Reduction) (int, error)
	source() runner.SessionSource
}

type memStore struct{ s *memory.Store }

func (m memStore) put(ss ...*attributionv1.SessionEvidence) { m.s.Put(org, ss...) }
func (m memStore) request(id string)                        { m.s.RequestReattribution(org, id) }
func (m memStore) get(id string) *attributionv1.SessionEvidence {
	return m.s.Get(org, id)
}
func (m memStore) load(limit int) ([]*attributionv1.SessionEvidence, error) {
	return m.s.LoadPending(context.Background(), org, limit)
}
func (m memStore) write(r attribution.Reduction) (int, error) {
	return m.s.WriteResults(context.Background(), org, r, time.Now())
}
func (m memStore) source() runner.SessionSource { return m.s }

type sqlStore struct {
	t *testing.T
	s *sqlite.Store
}

func (q sqlStore) put(ss ...*attributionv1.SessionEvidence) {
	require.NoError(q.t, q.s.Put(context.Background(), org, ss...))
}
func (q sqlStore) request(id string) {
	require.NoError(q.t, q.s.RequestReattribution(context.Background(), org, id))
}
func (q sqlStore) get(id string) *attributionv1.SessionEvidence {
	got, err := q.s.Get(context.Background(), org, id)
	require.NoError(q.t, err)
	return got
}
func (q sqlStore) load(limit int) ([]*attributionv1.SessionEvidence, error) {
	return q.s.LoadPending(context.Background(), org, limit)
}
func (q sqlStore) write(r attribution.Reduction) (int, error) {
	return q.s.WriteResults(context.Background(), org, r, time.Now())
}
func (q sqlStore) source() runner.SessionSource { return q.s }

// eachStore runs fn against a fresh memory store and a fresh SQLite store.
func eachStore(t *testing.T, fn func(t *testing.T, st store)) {
	t.Run("memory", func(t *testing.T) { fn(t, memStore{memory.New()}) })
	t.Run("sqlite", func(t *testing.T) {
		db, err := sqlite.Open(":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		fn(t, sqlStore{t, db})
	})
}

func loadIDs(t *testing.T, st store, limit int) []string {
	t.Helper()
	got, err := st.load(limit)
	require.NoError(t, err)
	out := []string{}
	for _, s := range got {
		out = append(out, s.GetSessionId())
	}
	return out
}

func authored(key string) *attributionv1.PRAttribution {
	return &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{{
		PrKey: key, ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
	}}}
}

func writeAll(t *testing.T, st store, sessions []*attributionv1.SessionEvidence, key string) int {
	t.Helper()
	r := attribution.Reduction{Sessions: sessions}
	for range sessions {
		r.Attributions = append(r.Attributions, authored(key))
	}
	n, err := st.write(r)
	require.NoError(t, err)
	return n
}

// Selection order: oldest start date first, then session id, cut at the limit.
func TestLoadOrderIsStartDateThenSessionIDUpToTheLimit(t *testing.T) {
	eachStore(t, func(t *testing.T, st store) {
		day := time.Now().UTC().Truncate(24 * time.Hour)
		st.put(sessionAt("c", day), sessionAt("b", day), sessionAt("z", day.AddDate(0, 0, -1)), sessionAt("a", day.AddDate(0, 0, 1)))
		assert.Equal(t, []string{"z", "b", "c", "a"}, loadIDs(t, st, 10))
		assert.Equal(t, []string{"z", "b"}, loadIDs(t, st, 2))
	})
}

// A re-upload keeps the stored attribution, whatever the incoming session
// carries: only an attribution write changes it.
func TestAReuploadKeepsTheStoredAttribution(t *testing.T) {
	eachStore(t, func(t *testing.T, st store) {
		start := time.Now().UTC()
		st.put(sessionAt("s1", start))
		loaded, err := st.load(10)
		require.NoError(t, err)
		require.Equal(t, 1, writeAll(t, st, loaded, "a/w#1"))

		again := sessionAt("s1", start)
		again.SessionDurationSeconds = 60
		st.put(again)
		withOwn := sessionAt("s1", start)
		withOwn.StoredAttribution = authored("x/y#9")
		st.put(withOwn)

		got := st.get("s1")
		require.Len(t, got.GetStoredAttribution().GetMatches(), 1)
		assert.Equal(t, "a/w#1", got.GetStoredAttribution().GetMatches()[0].GetPrKey())
		assert.Equal(t, int64(0), got.GetSessionDurationSeconds(), "the evidence is replaced")
	})
}

// A re-upload keeps the start date and instant first stored.
func TestAReuploadKeepsTheFirstStart(t *testing.T) {
	eachStore(t, func(t *testing.T, st store) {
		first := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
		s := sessionAt("s1", first)
		s.StartedAt = timestamppb.New(first)
		st.put(s)
		later := sessionAt("s1", first.AddDate(0, 0, 3))
		later.StartedAt = timestamppb.New(first.AddDate(0, 0, 3))
		st.put(later)
		got := st.get("s1")
		assert.Equal(t, first, got.GetStartDate().AsTime())
		assert.Equal(t, first, got.GetStartedAt().AsTime())
	})
}

// A re-upload raises the request by one only when an input the matcher reads
// changed. Its own request value is ignored.
func TestAReuploadRaisesTheRequestOnlyWhenMatcherInputsChange(t *testing.T) {
	start := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	base := func() *attributionv1.SessionEvidence {
		s := sessionAt("s1", start)
		s.StartedAt = timestamppb.New(start)
		s.FilesEdited = []string{"a.go", "b.go"}
		s.GitBranches = []string{"feat/x"}
		s.SessionDurationSeconds = 3600
		return s
	}
	// Rows with a stored session of their own set from.
	edgeOfDay := func() *attributionv1.SessionEvidence {
		s := sessionAt("s1", start)
		s.StartedAt = timestamppb.New(time.Date(2026, 3, 1, 0, 0, 30, 0, time.UTC))
		return s
	}
	noonDateOnly := func() *attributionv1.SessionEvidence {
		s := sessionAt("s1", time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC))
		s.SessionDurationSeconds = 3600
		return s
	}
	tenYears := func() *attributionv1.SessionEvidence {
		s := base()
		s.SessionDurationSeconds = 3650 * 24 * 3600
		return s
	}
	tests := []struct {
		name   string
		from   func() *attributionv1.SessionEvidence
		change func(*attributionv1.SessionEvidence)
		raised bool
	}{
		{"nothing changed", nil, func(*attributionv1.SessionEvidence) {}, false},
		{"incoming request value", nil, func(s *attributionv1.SessionEvidence) { s.ReattributionSeq = 9 }, false},
		{"repo name", nil, func(s *attributionv1.SessionEvidence) { s.RepoName = "a/w" }, false},
		{"editor", nil, func(s *attributionv1.SessionEvidence) { s.IdeType = "IDE_TYPE_KIRO" }, false},
		{"confidence", nil, func(s *attributionv1.SessionEvidence) { s.AiAttributionConfidence = "high" }, false},
		{"duration within the same day", nil, func(s *attributionv1.SessionEvidence) { s.SessionDurationSeconds = 7200 }, false},
		{"duration into the next day", nil, func(s *attributionv1.SessionEvidence) { s.SessionDurationSeconds = 20 * 3600 }, true},
		{"pr references", nil, func(s *attributionv1.SessionEvidence) {
			s.PrReferences = []*attributionv1.PRReference{{PrNumber: 1, PrRepository: "a/w"}}
		}, true},
		{"branches", nil, func(s *attributionv1.SessionEvidence) { s.GitBranches = []string{"feat/y"} }, true},
		{"branch provenance", nil, func(s *attributionv1.SessionEvidence) {
			s.BranchSignals = []*attributionv1.BranchSignal{{Name: "feat/x"}}
		}, true},
		{"files edited", nil, func(s *attributionv1.SessionEvidence) { s.FilesEdited = []string{"a.go"} }, true},
		{"files reordered", nil, func(s *attributionv1.SessionEvidence) { s.FilesEdited = []string{"b.go", "a.go"} }, true},
		{"pr number", nil, func(s *attributionv1.SessionEvidence) { s.PrNumber = 7 }, true},
		{"pr repository", nil, func(s *attributionv1.SessionEvidence) { s.PrRepository = "a/w" }, true},
		{"single branch", nil, func(s *attributionv1.SessionEvidence) { s.GitBranch = "feat/x" }, true},
		{"a negative duration counts as zero", edgeOfDay, func(s *attributionv1.SessionEvidence) { s.SessionDurationSeconds = -60 }, false},
		{"a duration past ten years is clamped", tenYears, func(s *attributionv1.SessionEvidence) { s.SessionDurationSeconds = 4000 * 24 * 3600 }, false},
		{"with no start instant the span runs from the start date", noonDateOnly, func(s *attributionv1.SessionEvidence) { s.SessionDurationSeconds = 13 * 3600 }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eachStore(t, func(t *testing.T, st store) {
				from := base
				if tt.from != nil {
					from = tt.from
				}
				st.put(from())
				next := from()
				tt.change(next)
				st.put(next)
				want := int32(0)
				if tt.raised {
					want = 1
				}
				assert.Equal(t, want, st.get("s1").GetReattributionSeq())
			})
		})
	}
}

// A request raises the marker by one, and the session is pending again.
func TestARequestRaisesTheMarkerByOne(t *testing.T) {
	eachStore(t, func(t *testing.T, st store) {
		st.put(sessionAt("s1", time.Now().UTC()))
		loaded, err := st.load(10)
		require.NoError(t, err)
		writeAll(t, st, loaded, "a/w#1")
		assert.Empty(t, loadIDs(t, st, 10))

		st.request("s1")
		st.request("s1")
		assert.Equal(t, int32(2), st.get("s1").GetReattributionSeq())
		assert.Equal(t, []string{"s1"}, loadIDs(t, st, 10))
	})
}

// The write records the request read at load as serviced. A request raised
// while the batch ran stays outstanding, so the session is selected again.
func TestARequestRaisedMidBatchStaysOutstanding(t *testing.T) {
	eachStore(t, func(t *testing.T, st store) {
		ctx := context.Background()
		st.put(sessionAt("s1", time.Now().UTC()))
		down := false
		e := attribution.New(attribution.DefaultParams(), flakyCandidates{&down}, linked{})

		loaded, err := st.load(10)
		require.NoError(t, err)
		results, err := e.Attribute(ctx, org, loaded)
		require.NoError(t, err)
		st.request("s1")
		n, err := st.write(attribution.Reduce(results))
		require.NoError(t, err)
		require.Equal(t, 1, n)

		got := st.get("s1")
		assert.Equal(t, int32(0), got.GetStoredAttribution().GetServicedReattributionSeq())
		assert.Equal(t, int32(1), got.GetReattributionSeq())
		assert.Equal(t, []string{"s1"}, loadIDs(t, st, 10))
	})
}

// A re-upload with new inputs while the batch ran does not stop the write: the
// result lands on the new evidence, and the raised request selects the session
// again so the next run attributes what the store now holds.
func TestAReuploadMidBatchIsWrittenAndSelectedAgain(t *testing.T) {
	eachStore(t, func(t *testing.T, st store) {
		start := time.Now().UTC()
		st.put(sessionAt("s1", start))
		loaded, err := st.load(10)
		require.NoError(t, err)

		moved := sessionAt("s1", start)
		moved.FilesEdited = []string{"new.go"}
		st.put(moved)
		require.Equal(t, 1, writeAll(t, st, loaded, "a/w#1"))

		got := st.get("s1")
		assert.Equal(t, []string{"new.go"}, got.GetFilesEdited(), "the new evidence is kept")
		assert.Len(t, got.GetStoredAttribution().GetMatches(), 1, "the write landed")
		assert.Equal(t, []string{"s1"}, loadIDs(t, st, 10), "and the session is selected again")
	})
}

// A write for a session the store does not hold is not counted as written.
func TestAWriteForAnUnknownSessionCountsShort(t *testing.T) {
	eachStore(t, func(t *testing.T, st store) {
		st.put(sessionAt("s1", time.Now().UTC()))
		n := writeAll(t, st, []*attributionv1.SessionEvidence{sessionAt("s1", time.Now().UTC()), sessionAt("gone", time.Now().UTC())}, "a/w#1")
		assert.Equal(t, 1, n)
		assert.Nil(t, st.get("gone"))
	})
}

// A runner over either store converges: a second run has nothing to do.
func TestARunnerOverEitherStoreConverges(t *testing.T) {
	eachStore(t, func(t *testing.T, st store) {
		ctx := context.Background()
		st.put(sessionAt("s1", time.Now().UTC()))
		down := false
		r := runner.New(attribution.New(attribution.DefaultParams(), flakyCandidates{&down}, linked{}), st.source(), nil)
		first, err := r.RunBatch(ctx, org, 10)
		require.NoError(t, err)
		assert.Equal(t, 1, first.Processed)
		second, err := r.RunBatch(ctx, org, 10)
		require.NoError(t, err)
		assert.Zero(t, second.Sessions)
	})
}

// A re-upload keeps the person and org first stored, and the last activity
// becomes the later of the two.
func TestAReuploadKeepsThePersonAndTheLatestActivity(t *testing.T) {
	eachStore(t, func(t *testing.T, st store) {
		t0 := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
		first := sessionAt("s1", t0)
		first.PersonId = "ada"
		first.LastActivityAt = timestamppb.New(t0.Add(5 * time.Hour))
		st.put(first)
		second := sessionAt("s1", t0)
		second.PersonId = "bob"
		second.OrgId = "other-org"
		second.LastActivityAt = timestamppb.New(t0.Add(time.Hour))
		st.put(second)

		got := st.get("s1")
		assert.Equal(t, "ada", got.GetPersonId())
		assert.Equal(t, org, got.GetOrgId())
		assert.Equal(t, t0.Add(5*time.Hour), got.GetLastActivityAt().AsTime())

		third := sessionAt("s1", t0)
		third.LastActivityAt = timestamppb.New(t0.Add(8 * time.Hour))
		st.put(third)
		assert.Equal(t, t0.Add(8*time.Hour), st.get("s1").GetLastActivityAt().AsTime(), "a later activity moves it")
	})
}

// A session with no start date is loaded after every dated one.
func TestAnUndatedSessionLoadsLast(t *testing.T) {
	eachStore(t, func(t *testing.T, st store) {
		day := time.Now().UTC().Truncate(24 * time.Hour)
		undated := sessionAt("a-undated", day)
		undated.StartDate = nil
		st.put(undated, sessionAt("z", day), sessionAt("y", day.AddDate(0, 0, -1)))
		assert.Equal(t, []string{"y", "z", "a-undated"}, loadIDs(t, st, 10))
	})
}
