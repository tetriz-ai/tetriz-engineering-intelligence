// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// These pin limits the README lists under "Before you rely on it". Each test
// named "...Declined" holds behaviour the engine keeps as it is.

var limDay0 = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

func limDay(n int) time.Time { return limDay0.AddDate(0, 0, n) }

type limIdentity struct{}

func (limIdentity) Resolve(context.Context, string, string) (Identity, error) {
	return Identity{AuthorIDs: []string{"a"}, ReviewerIdentityIDs: []string{"r"}}, nil
}

// limStore returns its pull requests, keeping only those merged inside the
// requested window when filter is set, as a store that queries by date does.
type limStore struct {
	authored, reviewed []*attributionv1.PRCandidate
	filter             bool
}

func (s limStore) pick(prs []*attributionv1.PRCandidate, from, to time.Time) []*attributionv1.PRCandidate {
	if !s.filter {
		return prs
	}
	var out []*attributionv1.PRCandidate
	for _, p := range prs {
		if m := p.GetMergedAt().AsTime(); !m.Before(from) && !m.After(to) {
			out = append(out, p)
		}
	}
	return out
}

func (s limStore) ListAuthoredPRs(_ context.Context, _ string, _ []string, from, to time.Time) ([]*attributionv1.PRCandidate, error) {
	return s.pick(s.authored, from, to), nil
}

func (s limStore) ListReviewedPRs(_ context.Context, _ string, _ []string, from, to time.Time) ([]*attributionv1.PRCandidate, error) {
	return s.pick(s.reviewed, from, to), nil
}

type limPanickingStore struct{}

func (limPanickingStore) ListAuthoredPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	panic("store bug")
}

func (limPanickingStore) ListReviewedPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return nil, nil
}

type limPanickingIdentity struct{}

func (limPanickingIdentity) Resolve(context.Context, string, string) (Identity, error) {
	panic("resolver bug")
}

func limPR(merged time.Time) *attributionv1.PRCandidate {
	return &attributionv1.PRCandidate{PrKey: "a/w#1", PrRepository: "a/w", PrNumber: 1, MergedAt: timestamppb.New(merged)}
}

func limRefSession(id string, start time.Time) *attributionv1.SessionEvidence {
	s := &attributionv1.SessionEvidence{SessionId: id, PersonId: "p", OrgId: "org-1",
		PrReferences: []*attributionv1.PRReference{{PrNumber: 1, PrRepository: "a/w"}}}
	if !start.IsZero() {
		s.StartDate = timestamppb.New(start)
	}
	return s
}

func limResult(t *testing.T, results []MatchResult, id string) *attributionv1.PRAttribution {
	t.Helper()
	for _, r := range results {
		if r.Session.GetSessionId() == id {
			return r.Attribution
		}
	}
	t.Fatalf("no result for %s", id)
	return nil
}

// Declined: candidate fetches run outside the recovering worker group, so a
// panicking CandidateStore panics out of Attribute. A panicking resolver is
// recovered and returned as an error.
func TestAPanickingCandidateStoreEscapesDeclined(t *testing.T) {
	batch := []*attributionv1.SessionEvidence{limRefSession("s", limDay0)}
	assert.Panics(t, func() {
		_, _ = New(DefaultParams(), limPanickingStore{}, limIdentity{}).Attribute(context.Background(), "org-1", batch)
	})
	_, err := New(DefaultParams(), limStore{}, limPanickingIdentity{}).Attribute(context.Background(), "org-1", batch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "panic")
}

// Declined: a session with no start date matches nothing, abstains with
// NO_MATCHING_PR, and is never offered again, because the retry budget is
// measured from a date it does not have.
func TestASessionWithNoStartDateIsNeverMatchedDeclined(t *testing.T) {
	results, err := New(DefaultParams(), limStore{authored: []*attributionv1.PRCandidate{limPR(limDay0)}}, limIdentity{}).
		Attribute(context.Background(), "org-1", []*attributionv1.SessionEvidence{limRefSession("s", time.Time{})})
	require.NoError(t, err)
	attr := limResult(t, results, "s")
	assert.Empty(t, attr.GetMatches())
	assert.Equal(t, attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR, attr.GetUnmatched().GetReason())

	s := results[0].Session
	s.StoredAttribution = Reduce(results).Attributions[0]
	for _, now := range []time.Time{time.Now(), time.Now().AddDate(1, 0, 0)} {
		assert.False(t, IsPending(s, DefaultParams(), now), "never retried")
	}
}

// Declined: a code-review session that names a pull request its person wrote
// is credited as REVIEWED only when the store returns that pull request from
// both ListAuthoredPRs and ListReviewedPRs. From the authored list alone it is
// AUTHORED.
func TestSelfCodeReviewNeedsThePullRequestInBothListsDeclined(t *testing.T) {
	run := func(store limStore) attributionv1.SessionParticipationKind {
		s := limRefSession("s", limDay0)
		s.TaskType = "code-review"
		results, err := New(DefaultParams(), store, limIdentity{}).
			Attribute(context.Background(), "org-1", []*attributionv1.SessionEvidence{s})
		require.NoError(t, err)
		attr := limResult(t, results, "s")
		require.Len(t, attr.GetMatches(), 1)
		return attr.GetMatches()[0].GetParticipationKind()
	}
	authored := limPR(limDay0)
	authored.ParticipationKind = attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED
	reviewed := limPR(limDay0)
	reviewed.ParticipationKind = attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED
	assert.Equal(t, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
		run(limStore{authored: []*attributionv1.PRCandidate{authored}}))
	assert.Equal(t, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED,
		run(limStore{authored: []*attributionv1.PRCandidate{authored}, reviewed: []*attributionv1.PRCandidate{reviewed}}))
}

// Declined: candidates are fetched once per person over the window spanning the
// whole batch, so a pull request merged before a session can still match it
// when an earlier session of the same person widens the fetch.
func TestTheFetchWindowDependsOnTheBatchDeclined(t *testing.T) {
	e := New(DefaultParams(), limStore{authored: []*attributionv1.PRCandidate{limPR(limDay(5))}, filter: true}, limIdentity{})
	b := limRefSession("B", limDay(10))
	a := &attributionv1.SessionEvidence{SessionId: "A", PersonId: "p", OrgId: "org-1", StartDate: timestamppb.New(limDay0)}

	alone, err := e.Attribute(context.Background(), "org-1", []*attributionv1.SessionEvidence{b})
	require.NoError(t, err)
	assert.Empty(t, limResult(t, alone, "B").GetMatches())

	both, err := e.Attribute(context.Background(), "org-1", []*attributionv1.SessionEvidence{a, b})
	require.NoError(t, err)
	assert.Len(t, limResult(t, both, "B").GetMatches(), 1)
}

// Declined: a stored match is withdrawn only when this run fetched its pull
// request, and the fetch spans the person's whole batch, so whether it is
// withdrawn depends on which other sessions were batched with it.
func TestAWithdrawalDependsOnTheBatchDeclined(t *testing.T) {
	e := New(DefaultParams(), limStore{authored: []*attributionv1.PRCandidate{limPR(limDay(35))}, filter: true}, limIdentity{})
	withStored := func() *attributionv1.SessionEvidence {
		// A gated editor whose reference to the pull request was only seen in
		// the workspace: a stored match on it is one the rules withdraw.
		a := limRefSession("A", limDay0)
		a.IdeType = "IDE_TYPE_KIRO"
		a.PrReferences[0].Source = attributionv1.SignalSource_SIGNAL_SOURCE_LOCAL_GIT_WINDOWED
		a.StoredAttribution = &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{{
			PrKey: "a/w#1", PrRepository: "a/w", PrNumber: 1, Confidence: 1,
			Method:            attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF,
			ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
		}}}
		return a
	}
	b := &attributionv1.SessionEvidence{SessionId: "B", PersonId: "p", OrgId: "org-1", StartDate: timestamppb.New(limDay(30))}

	alone, err := e.Attribute(context.Background(), "org-1", []*attributionv1.SessionEvidence{withStored()})
	require.NoError(t, err)
	assert.Empty(t, Reduce(alone).Withdrawals, "alone, the pull request is not fetched and the match is kept")

	both, err := e.Attribute(context.Background(), "org-1", []*attributionv1.SessionEvidence{withStored(), b})
	require.NoError(t, err)
	assert.Len(t, Reduce(both).Withdrawals, 1, "with B, the pull request is fetched and the match withdrawn")
}

// The file-overlap floor is inclusive: a score exactly at MinConfidencePercent
// matches.
func TestTheFileOverlapFloorIsInclusive(t *testing.T) {
	var files []string
	for i := 0; i < 10; i++ {
		files = append(files, fmt.Sprintf("pkg/f%d.go", i))
	}
	pr := limPR(limDay0)
	pr.FilesChanged = files[:3]
	s := &attributionv1.SessionEvidence{SessionId: "s", PersonId: "p", OrgId: "org-1",
		StartDate: timestamppb.New(limDay0), FilesEdited: files}
	require.InDelta(t, 0.3, ScoreFileOverlap(files, pr.GetFilesChanged(), nil), 0)

	results, err := New(DefaultParams(), limStore{authored: []*attributionv1.PRCandidate{pr}}, limIdentity{}).
		Attribute(context.Background(), "org-1", []*attributionv1.SessionEvidence{s})
	require.NoError(t, err)
	attr := limResult(t, results, "s")
	require.Len(t, attr.GetMatches(), 1, "3 of 10 files is exactly the 30% floor")
	assert.Equal(t, attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP, attr.GetMatches()[0].GetMethod())
}

// A branch match never credits a pull request that merged before the session
// started.
func TestABranchMatchNeedsAMergeOnOrAfterTheStart(t *testing.T) {
	run := func(merged time.Time) int {
		pr := limPR(merged)
		pr.HeadBranch = "feat/x"
		s := &attributionv1.SessionEvidence{SessionId: "s", PersonId: "p", OrgId: "org-1",
			IdeType: "IDE_TYPE_CLAUDE_CODE", StartDate: timestamppb.New(limDay(1)),
			BranchSignals: []*attributionv1.BranchSignal{{Name: "feat/x", Source: attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND}}}
		results, err := New(DefaultParams(), limStore{authored: []*attributionv1.PRCandidate{pr}}, limIdentity{}).
			Attribute(context.Background(), "org-1", []*attributionv1.SessionEvidence{s})
		require.NoError(t, err)
		return len(limResult(t, results, "s").GetMatches())
	}
	assert.Zero(t, run(limDay0), "merged the day before the session")
	assert.Equal(t, 1, run(limDay(1)), "merged the day the session started")
}

// Declined: a path written with a leading "./" is not normalized, so it does
// not meet the same file without one.
func TestALeadingDotSlashIsNotNormalizedDeclined(t *testing.T) {
	assert.InDelta(t, 0.5, ScoreFileOverlap([]string{"a.go", "./b.go"}, []string{"a.go", "b.go"}, nil), 1e-9)
	assert.Equal(t, []string{"a.go"}, MatchedFiles([]string{"a.go", "./b.go"}, []string{"a.go", "b.go"}, nil))
}
