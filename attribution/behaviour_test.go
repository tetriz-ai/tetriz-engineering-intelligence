// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

var testDay = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

func authoredCandidate(key string, num int32, files ...string) *attributionv1.PRCandidate {
	return &attributionv1.PRCandidate{
		PrKey: key, PrRepository: "acme/widgets", PrNumber: num, FilesChanged: files,
		ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
	}
}

// A still-open pull request reaches the file-overlap signal through its open
// date, and its score must decay from that date: it is a confidence in [0, 1],
// never a number measured to a merge date that is not set.
func TestAnUnmergedCandidateDecaysFromItsOpenDate(t *testing.T) {
	session := &attributionv1.SessionEvidence{
		SessionId: "s", StartDate: timestamppb.New(testDay),
		FilesEdited: []string{"limiter/bucket.go", "limiter/window.go"},
	}
	for _, tt := range []struct {
		name     string
		openDays int
		want     float64
	}{
		{name: "opened the same day", openDays: 0, want: 1.0},
		{name: "opened a day later", openDays: 1, want: 0.95},
		{name: "opened at the window edge", openDays: 3, want: 0.85},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pr := authoredCandidate("acme/widgets#PR-1#opened", 1, "limiter/bucket.go", "limiter/window.go")
			pr.OpenedAt = timestamppb.New(testDay.AddDate(0, 0, tt.openDays))
			e := New(DefaultParams(), nil, nil)
			matches := e.computeMatches(session, []*attributionv1.PRCandidate{pr}).GetMatches()
			require.Len(t, matches, 1)
			assert.InDelta(t, tt.want, matches[0].GetConfidence(), 1e-9)
			assert.LessOrEqual(t, matches[0].GetConfidence(), 1.0)
		})
	}
}

type recordingObserver struct {
	mu     sync.Mutex
	counts []string
	debugs []map[string]any
}

func (r *recordingObserver) Count(event string, labels map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counts = append(r.counts, event+":"+labels["reason"])
}
func (r *recordingObserver) Observe(string, float64, map[string]string) {}
func (r *recordingObserver) Debug(event string, fields map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event == "person_skipped" {
		r.debugs = append(r.debugs, fields)
	}
}

type erroringCandidates struct{ authored, reviewed error }

func (e erroringCandidates) ListAuthoredPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return nil, e.authored
}
func (e erroringCandidates) ListReviewedPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return nil, e.reviewed
}

type staticResolver struct {
	id  Identity
	err error
}

func (s staticResolver) Resolve(context.Context, string, string) (Identity, error) {
	return s.id, s.err
}

// A person whose sessions are left pending with no result is never silent: the
// Observer hears the reason and the error, once per person.
func TestALeftPendingPersonIsReportedWithTheError(t *testing.T) {
	both := Identity{AuthorIDs: []string{"a"}, ReviewerIdentityIDs: []string{"r"}}
	matched := &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{{PrKey: "k"}}}
	for _, tt := range []struct {
		name       string
		candidates CandidateStore
		resolver   IdentityResolver
		stored     *attributionv1.PRAttribution
		wantReason string
	}{
		{name: "author-side fetch error", candidates: erroringCandidates{authored: errors.New("store down")},
			resolver: staticResolver{id: both}, wantReason: "candidate_fetch_error"},
		{name: "reviewer-side fetch error", candidates: erroringCandidates{reviewed: errors.New("store down")},
			resolver: staticResolver{id: both}, wantReason: "candidate_fetch_error"},
		{name: "identity error on a session that already has matches", candidates: erroringCandidates{},
			resolver: staticResolver{err: errors.New("store down")}, stored: matched, wantReason: "identity_error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			obs := &recordingObserver{}
			p := DefaultParams()
			p.Observer = obs
			s := &attributionv1.SessionEvidence{SessionId: "s1", PersonId: "p1", StartDate: timestamppb.New(testDay), StoredAttribution: tt.stored}
			results, err := New(p, tt.candidates, tt.resolver).Attribute(context.Background(), "o", []*attributionv1.SessionEvidence{s})
			require.NoError(t, err)
			assert.Empty(t, results, "the session is left pending")
			assert.Contains(t, obs.counts, "person_skipped:"+tt.wantReason)
			require.Len(t, obs.debugs, 1)
			assert.Equal(t, "p1", obs.debugs[0]["person_id"])
			assert.Contains(t, obs.debugs[0]["error"], "store down")
			assert.Equal(t, []string{"s1"}, obs.debugs[0]["session_ids"])
		})
	}
}

// Setting one threshold keeps it and leaves every other field at its default.
func TestNewFillsOnlyTheZeroFields(t *testing.T) {
	e := New(Params{StrongSignalWindowAfterDays: 90, IdentityResolveConcurrency: 2}, nil, nil)
	want := DefaultParams()
	want.StrongSignalWindowAfterDays = 90
	want.IdentityResolveConcurrency = 2
	assert.Equal(t, want, e.params)

	e = New(Params{TimeWindowAfterDays: 5, MinConfidencePercent: 40}, nil, nil)
	assert.Equal(t, 30, e.params.StrongSignalWindowAfterDays, "an unset field takes its default")
	assert.Equal(t, 10, e.params.ComputeMatchConcurrency, "concurrency never falls to unbounded")
	assert.Equal(t, 5, e.params.TimeWindowAfterDays)
}

func resultsWithCounts(n int) []MatchResult {
	rs := make([]MatchResult, 0, n)
	for i := 0; i < n; i++ {
		ms := make([]*attributionv1.PRMatch, i)
		rs = append(rs, MatchResult{Attribution: &attributionv1.PRAttribution{Matches: ms}})
	}
	return rs
}

// sharedCandidates hands the same *PRCandidate to a person's authored list and
// to another person's reviewed list, as a store that caches candidates does.
type sharedCandidates struct{ pr *attributionv1.PRCandidate }

func (s sharedCandidates) ListAuthoredPRs(_ context.Context, _ string, ids []string, _, _ time.Time) ([]*attributionv1.PRCandidate, error) {
	if len(ids) > 0 && ids[0] == "a-vendor" {
		return []*attributionv1.PRCandidate{s.pr}, nil
	}
	return nil, nil
}

func (s sharedCandidates) ListReviewedPRs(_ context.Context, _ string, ids []string, _, _ time.Time) ([]*attributionv1.PRCandidate, error) {
	if len(ids) > 0 && ids[0] == "b-rev" {
		return []*attributionv1.PRCandidate{s.pr}, nil
	}
	return nil, nil
}

type mapResolver map[string]Identity

func (m mapResolver) Resolve(_ context.Context, _, p string) (Identity, error) { return m[p], nil }

func sharedFixture() ([]*attributionv1.SessionEvidence, mapResolver, *attributionv1.PRCandidate) {
	pr := &attributionv1.PRCandidate{
		PrKey: "acme/w#7", PrRepository: "acme/w", PrNumber: 7, HeadBranch: "feat/x",
		FilesChanged: []string{"limiter/bucket.go", "limiter/window.go"},
		MergedAt:     timestamppb.New(testDay.AddDate(0, 0, 1)),
	}
	a := &attributionv1.SessionEvidence{SessionId: "sA", PersonId: "A", StartDate: timestamppb.New(testDay),
		RepoName: "acme/w", FilesEdited: []string{"limiter/bucket.go", "limiter/window.go"}}
	b := &attributionv1.SessionEvidence{SessionId: "sB", PersonId: "B", StartDate: timestamppb.New(testDay),
		RepoName: "acme/w", PrReferences: []*attributionv1.PRReference{{PrNumber: 7, PrRepository: "acme/w",
			Source: attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND}}}
	ids := mapResolver{"A": {AuthorIDs: []string{"a-vendor"}}, "B": {ReviewerIdentityIDs: []string{"b-rev"}}}
	return []*attributionv1.SessionEvidence{a, b}, ids, pr
}

// A kind the store already stamped is kept: a candidate a store lists as
// REVIEWED from the authored side stays REVIEWED.
func TestAStoreStampedKindIsKept(t *testing.T) {
	got := stampKind([]*attributionv1.PRCandidate{
		{PrKey: "k", ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED},
		{PrKey: "u"},
	}, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED)
	require.Len(t, got, 2)
	assert.Equal(t, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED, got[0].GetParticipationKind())
	assert.Equal(t, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED, got[1].GetParticipationKind())
}

type nilCandidates struct{}

func (nilCandidates) ListAuthoredPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return []*attributionv1.PRCandidate{nil}, nil
}

func (nilCandidates) ListReviewedPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return []*attributionv1.PRCandidate{nil}, nil
}

// Declined by the author: a merge inside a long session decays as far as its
// distance to the nearer end, so a merge in the middle gets the floor. Pinned.
func TestAMergeInsideALongSessionGetsTheFloorDecayDeclined(t *testing.T) {
	s := &attributionv1.SessionEvidence{SessionId: "s", StartDate: timestamppb.New(testDay),
		GitBranches: []string{"feat/x"}, SessionDurationSeconds: 20 * 86400}
	pr := &attributionv1.PRCandidate{PrKey: "acme/w#7", PrRepository: "acme/w", PrNumber: 7, HeadBranch: "feat/x",
		MergedAt:          timestamppb.New(testDay.AddDate(0, 0, 10)),
		ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED}
	e := New(Params{}, nil, nil)
	anchor := sessionMatchAnchor(s, e.params)
	assert.InDelta(t, 0.75, timeDecay(testDay, anchor, mergedAt(pr)), 1e-9)
	matches := e.computeMatches(s, []*attributionv1.PRCandidate{pr}).GetMatches()
	require.Len(t, matches, 1)
	assert.InDelta(t, 0.6375, matches[0].GetConfidence(), 1e-9)
}

// A dependency-bot head branch never branch-matches.
func TestARenovateBranchNeverMatches(t *testing.T) {
	s := &attributionv1.SessionEvidence{SessionId: "s", StartDate: timestamppb.New(testDay), GitBranches: []string{"renovate/go-deps"}}
	pr := &attributionv1.PRCandidate{PrKey: "acme/w#7", PrRepository: "acme/w", PrNumber: 7, HeadBranch: "renovate/go-deps",
		MergedAt:          timestamppb.New(testDay),
		ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED}
	assert.Empty(t, matchBranch(s, []*attributionv1.PRCandidate{pr}, DefaultParams()))
}

// Declined: only the enum name is gated. canonicalIDEType
// upper-cases and trims, so "kiro" and "Kiro" are not gated. The localsession
// adapter emits the enum name.
func TestOnlyTheEnumNameIsGatedDeclined(t *testing.T) {
	for _, v := range []string{"IDE_TYPE_KIRO", "ide_type_kiro", " IDE_TYPE_KIRO "} {
		assert.True(t, IDETypeIsGated(v), v)
	}
	for _, v := range []string{"", "kiro", "Kiro", "IDE_TYPE_CURSOR"} {
		assert.False(t, IDETypeIsGated(v), v)
	}
	assert.Equal(t, "KIRO", canonicalIDEType("kiro"), "the withdrawal label is the upper-cased input")
}

// Declined: the p99 index is int(0.99n), so for a batch of 100
// sessions or fewer the p99 is the maximum.
func TestMatchesDistributionP99Declined(t *testing.T) {
	for _, tt := range []struct{ n, want int }{{1, 0}, {10, 9}, {100, 99}, {200, 198}} {
		_, p99 := MatchesDistribution(resultsWithCounts(tt.n))
		assert.Equal(t, tt.want, p99, "n=%d", tt.n)
	}
}

// The match set is stable across runs. Its order is not: matches come back in
// map order, so a caller sorts them before it compares or prints.
func TestTheMatchSetIsStable(t *testing.T) {
	var prs []*attributionv1.PRCandidate
	var refs []*attributionv1.PRReference
	for i := int32(1); i <= 8; i++ {
		pr := authoredCandidate(string(rune('a'+i))+"#PR", i)
		pr.MergedAt = timestamppb.New(testDay)
		prs = append(prs, pr)
		refs = append(refs, &attributionv1.PRReference{PrNumber: i, PrRepository: "acme/widgets",
			Source: attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND})
	}
	s := &attributionv1.SessionEvidence{SessionId: "s", StartDate: timestamppb.New(testDay), PrReferences: refs}
	e := New(DefaultParams(), nil, nil)
	var first []string
	for i := 0; i < 20; i++ {
		var keys []string
		for _, m := range e.computeMatches(s, prs).GetMatches() {
			keys = append(keys, m.GetPrKey())
		}
		sort.Strings(keys)
		if first == nil {
			first = keys
		}
		assert.Equal(t, first, keys)
	}
	assert.Len(t, first, 8)
}

// Declined: Attribute does not compare a session's org_id with
// the batch org. The CandidateStore is called with the batch org only.
func TestAttributeDoesNotCheckTheSessionOrgDeclined(t *testing.T) {
	e := New(DefaultParams(), erroringCandidates{}, staticResolver{})
	other := &attributionv1.SessionEvidence{SessionId: "s", PersonId: "p", OrgId: "org-b", StartDate: timestamppb.New(testDay)}
	_, err := e.Attribute(context.Background(), "org-a", []*attributionv1.SessionEvidence{other})
	assert.NoError(t, err)
}

// Declined: the engine marks the side on the candidate object the
// store returned. One object shared between A's authored list and B's reviewed
// list is marked by whichever fetch reaches it first, so the store must return
// its own objects per person and per call.
func TestASharedCandidateIsWrittenToDeclined(t *testing.T) {
	sessions, ids, pr := sharedFixture()
	_, err := New(Params{}, sharedCandidates{pr}, ids).Attribute(context.Background(), "", sessions)
	require.NoError(t, err)
	assert.NotEqual(t, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_UNSPECIFIED,
		pr.GetParticipationKind(), "the caller's candidate carries the stamp")

	// With distinct objects per person, B is REVIEWED and A keeps its file-overlap match.
	results, err := New(Params{}, distinctCandidates{}, ids).Attribute(context.Background(), "", sessions)
	require.NoError(t, err)
	byID := map[string]*attributionv1.PRAttribution{}
	for _, r := range results {
		byID[r.Session.GetSessionId()] = r.Attribution
	}
	require.Len(t, byID["sB"].GetMatches(), 1)
	assert.Equal(t, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED, byID["sB"].GetMatches()[0].GetParticipationKind())
	require.Len(t, byID["sA"].GetMatches(), 1)
	assert.Equal(t, attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP, byID["sA"].GetMatches()[0].GetMethod())
}

// distinctCandidates returns a fresh object on every call.
type distinctCandidates struct{}

func (distinctCandidates) ListAuthoredPRs(_ context.Context, _ string, ids []string, _, _ time.Time) ([]*attributionv1.PRCandidate, error) {
	_, _, pr := sharedFixture()
	return sharedCandidates{pr}.ListAuthoredPRs(context.Background(), "", ids, time.Time{}, time.Time{})
}

func (distinctCandidates) ListReviewedPRs(_ context.Context, _ string, ids []string, _, _ time.Time) ([]*attributionv1.PRCandidate, error) {
	_, _, pr := sharedFixture()
	return sharedCandidates{pr}.ListReviewedPRs(context.Background(), "", ids, time.Time{}, time.Time{})
}

// Declined: a nil candidate from a store panics, and the panic
// escapes Attribute, because the candidate fetch runs outside the recovering group.
func TestANilCandidatePanicsDeclined(t *testing.T) {
	s := &attributionv1.SessionEvidence{SessionId: "s", PersonId: "A", StartDate: timestamppb.New(testDay)}
	ids := mapResolver{"A": {AuthorIDs: []string{"a"}}}
	assert.Panics(t, func() {
		_, _ = New(Params{}, nilCandidates{}, ids).Attribute(context.Background(), "", []*attributionv1.SessionEvidence{s})
	})
}

// Declined: a stored match with no participation kind is dropped
// by any run that does not reproduce it, whether or not the run fetched its pull
// request, and it is not reported as a withdrawal.
func TestAKindlessMatchIsDroppedUnreportedDeclined(t *testing.T) {
	s := &attributionv1.SessionEvidence{SessionId: "s", PersonId: "A", StartDate: timestamppb.New(testDay), ReattributionSeq: 1,
		StoredAttribution: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{{
			PrKey: "acme/w#7", PrNumber: 7, PrRepository: "acme/w", Confidence: 1,
			Method: attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF,
		}}}}
	results, err := New(Params{}, erroringCandidates{}, mapResolver{"A": {}}).Attribute(context.Background(), "", []*attributionv1.SessionEvidence{s})
	require.NoError(t, err)
	red := Reduce(results)
	assert.Empty(t, red.Attributions[0].GetMatches(), "not fetched, and still dropped")
	assert.Empty(t, red.Withdrawals, "and not reported")
}

// Declined: a negative Params value is kept as given.
func TestNegativeParamsAreKeptDeclined(t *testing.T) {
	e := New(Params{MinConfidencePercent: -1, RetryCooldownHours: -1}, nil, nil)
	assert.Equal(t, -1, e.params.MinConfidencePercent)
	assert.Equal(t, -1, e.params.RetryCooldownHours)

	now := time.Now().UTC()
	justTried := &attributionv1.SessionEvidence{SessionId: "s", StartDate: timestamppb.New(now),
		StoredAttribution: &attributionv1.PRAttribution{Unmatched: &attributionv1.PRUnmatched{
			Reason:          attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR,
			LastAttemptedAt: now.Format(time.RFC3339)}}}
	assert.True(t, IsPending(justTried, Params{RetryCooldownHours: -1}, now.Add(time.Second)), "a negative cooldown is no cooldown")
}

// IsPending's selection: the cooldown is strict, the retry budget runs through
// the last day of MaxRetryDays, a kind-less match is pending, and an abstention
// with an unknown reason is settled.
func TestIsPendingMatchesTheProdSelection(t *testing.T) {
	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	abstained := func(reason attributionv1.SessionToPRUnmatchedReason, last time.Time) *attributionv1.SessionEvidence {
		return &attributionv1.SessionEvidence{SessionId: "s", StartDate: timestamppb.New(start),
			StoredAttribution: &attributionv1.PRAttribution{Unmatched: &attributionv1.PRUnmatched{
				Reason: reason, LastAttemptedAt: last.Format(time.RFC3339)}}}
	}
	noMatch := attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR
	last := start.Add(time.Hour)
	assert.False(t, IsPending(abstained(noMatch, last), Params{}, last.Add(12*time.Hour)), "exactly the cooldown is not past it")
	assert.True(t, IsPending(abstained(noMatch, last), Params{}, last.Add(12*time.Hour+time.Second)), "one second past it")

	budgetDay := start.AddDate(0, 0, 30)
	assert.True(t, IsPending(abstained(noMatch, start), Params{}, budgetDay.Add(23*time.Hour)), "through the last day of the budget")
	assert.False(t, IsPending(abstained(noMatch, start), Params{}, budgetDay.AddDate(0, 0, 1)), "settled the day after")

	notLinked := attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_VCS_NOT_LINKED
	assert.True(t, IsPending(abstained(notLinked, start), Params{}, start.AddDate(1, 0, 0)), "not-linked retries without a budget")
	unknown := attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_UNSPECIFIED
	assert.False(t, IsPending(abstained(unknown, start), Params{}, start.AddDate(0, 0, 1)), "an unknown reason is settled")

	kindless := &attributionv1.SessionEvidence{SessionId: "s", StartDate: timestamppb.New(start),
		StoredAttribution: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{{PrKey: "k"}}}}
	assert.True(t, IsPending(kindless, Params{}, start), "a match with no participation kind is selected again")
	kinded := &attributionv1.SessionEvidence{SessionId: "s", StartDate: timestamppb.New(start),
		StoredAttribution: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{{PrKey: "k",
			ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED}}}}
	assert.False(t, IsPending(kinded, Params{}, start), "a match with a kind is settled")

	// (a) IDENTITY_ERROR waits for the cooldown, then retries.
	identityErr := attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_IDENTITY_ERROR
	assert.False(t, IsPending(abstained(identityErr, last), Params{}, last.Add(time.Hour)), "identity error inside the cooldown")
	assert.True(t, IsPending(abstained(identityErr, last), Params{}, last.Add(13*time.Hour)), "identity error past the cooldown")

	// (b) A missing last attempt counts as past the cooldown.
	never := abstained(noMatch, start)
	never.StoredAttribution.Unmatched.LastAttemptedAt = ""
	assert.True(t, IsPending(never, Params{}, start), "no recorded attempt is past the cooldown")

	// (c) The budget runs from the last-activity anchor, start plus duration,
	// with the duration clamped by MaxSessionSpanDays.
	long := abstained(noMatch, start)
	long.SessionDurationSeconds = 10 * 24 * 3600
	assert.True(t, IsPending(long, Params{}, start.AddDate(0, 0, 35)), "anchor at day 10, budget through day 40")
	clamped := abstained(noMatch, start)
	clamped.SessionDurationSeconds = 60 * 24 * 3600
	assert.True(t, IsPending(clamped, Params{}, start.AddDate(0, 0, 60).Add(23*time.Hour)), "anchor clamped to day 30, budget through day 60")
	assert.False(t, IsPending(clamped, Params{}, start.AddDate(0, 0, 61)), "settled on day 61")

	// (d) A request is not capped by MaxRetryDays.
	requested := abstained(noMatch, start)
	requested.ReattributionSeq = 1
	assert.True(t, IsPending(requested, Params{}, start.AddDate(1, 0, 0)), "a request a year on is still pending")

	// (e) NO_MATCHING_PR with no start date is settled.
	undated := abstained(noMatch, start)
	undated.StartDate = nil
	assert.False(t, IsPending(undated, Params{}, start.AddDate(0, 0, 1)), "no start date, no budget")

	// (f) The current day is the UTC date of now, whatever now's zone.
	west := time.FixedZone("UTC-5", -5*3600)
	nowWest := time.Date(2026, 4, 1, 2, 0, 0, 0, time.UTC).In(west) // 31 March locally, 1 April in UTC
	assert.False(t, IsPending(abstained(noMatch, start), Params{}, nowWest), "the budget ended on 31 March UTC")
}

// Declined: a known repository segment is stripped only within
// the first three path segments, so a checkout deep in an absolute path keeps its
// prefix and does not match a repo-relative path.
func TestADeepAbsolutePathKeepsItsPrefixDeclined(t *testing.T) {
	repos := map[string]struct{}{"widgets": {}}
	assert.Equal(t, 0.0, ScoreFileOverlap([]string{"/Users/dev/code/widgets/limiter/bucket.go"}, []string{"limiter/bucket.go"}, repos))
}
