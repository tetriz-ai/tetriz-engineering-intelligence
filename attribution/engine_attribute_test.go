// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// These cover Engine.Attribute's own orchestration — the phases, and what each
// failure mode returns — against fakes for the two interfaces.

type fakeIdentity struct {
	resolved map[string]Identity
	errs     map[string]error
}

func (f *fakeIdentity) Resolve(_ context.Context, _, personID string) (Identity, error) {
	if err, ok := f.errs[personID]; ok {
		return Identity{}, err
	}
	return f.resolved[personID], nil
}

type fakeCandidates struct {
	authored  []*attributionv1.PRCandidate
	reviewed  []*attributionv1.PRCandidate
	authorErr error
	reviewErr error
}

func (f *fakeCandidates) ListAuthoredPRs(_ context.Context, _ string, _ []string, _, _ time.Time) ([]*attributionv1.PRCandidate, error) {
	return f.authored, f.authorErr
}

func (f *fakeCandidates) ListReviewedPRs(_ context.Context, _ string, _ []string, _, _ time.Time) ([]*attributionv1.PRCandidate, error) {
	return f.reviewed, f.reviewErr
}

func linked(personID string) *fakeIdentity {
	return &fakeIdentity{resolved: map[string]Identity{
		personID: {AuthorIDs: []string{"vendor-1"}, ReviewerIdentityIDs: []string{"rev-1"}},
	}}
}

func sessionFor(personID string, start time.Time, mutate func(*attributionv1.SessionEvidence)) *attributionv1.SessionEvidence {
	s := &attributionv1.SessionEvidence{
		SessionId: "s-" + personID,
		PersonId:  personID,
		OrgId:     "org-1",
		StartDate: ts(start),
		IdeType:   "IDE_TYPE_CLAUDE_CODE",
	}
	if mutate != nil {
		mutate(s)
	}
	return s
}

// firstResultFor returns the single result for a session id, failing when the
// session produced none — "left pending" is a distinct outcome from "unmatched",
// and conflating them is exactly the bug these tests exist to catch.
func firstResultFor(t *testing.T, results []MatchResult, sessionID string) MatchResult {
	t.Helper()
	for _, r := range results {
		if r.Session.GetSessionId() == sessionID {
			return r
		}
	}
	t.Fatalf("no result for session %q (it was left pending)", sessionID)
	return MatchResult{}
}

func TestEngineAttribute(t *testing.T) {
	now := time.Now().UTC().Truncate(24 * time.Hour)

	tests := []struct {
		name       string
		sessions   []*attributionv1.SessionEvidence
		identities *fakeIdentity
		candidates *fakeCandidates

		wantResults int
		wantMatched bool
		wantMethod  attributionv1.SessionToPRMatchMethod
		wantReason  attributionv1.SessionToPRUnmatchedReason
		wantPending bool // the session must produce no result at all
	}{
		{
			name:        "no sessions produces no work",
			sessions:    nil,
			identities:  &fakeIdentity{},
			candidates:  &fakeCandidates{},
			wantResults: 0,
		},
		{
			name: "a named pull request matches on the direct reference",
			sessions: []*attributionv1.SessionEvidence{sessionFor("p1", now, func(s *attributionv1.SessionEvidence) {
				s.PrNumber = 42
				s.PrRepository = "org/repo"
				s.FilesEdited = []string{"src/handler.go"}
			})},
			identities: linked("p1"),
			candidates: &fakeCandidates{authored: []*attributionv1.PRCandidate{
				testPRCandidate("org/repo#PR-42#merged", now, map[string]any{
					"repo": "org/repo", "files_changed": []string{"src/handler.go"},
				}),
			}},
			wantResults: 1,
			wantMatched: true,
			wantMethod:  attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF,
		},
		{
			name: "a shared branch matches on the branch signal",
			sessions: []*attributionv1.SessionEvidence{sessionFor("p1", now, func(s *attributionv1.SessionEvidence) {
				s.GitBranches = []string{"feat/thing"}
				s.RepoName = "org/repo"
			})},
			identities: linked("p1"),
			candidates: &fakeCandidates{authored: []*attributionv1.PRCandidate{
				testPRCandidate("org/repo#PR-7#merged", now, map[string]any{
					"repo": "org/repo", "head_branch": "feat/thing",
				}),
			}},
			wantResults: 1,
			wantMatched: true,
			wantMethod:  attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH,
		},
		{
			name: "overlapping edits match on file overlap",
			sessions: []*attributionv1.SessionEvidence{sessionFor("p1", now, func(s *attributionv1.SessionEvidence) {
				s.RepoName = "org/repo"
				s.FilesEdited = []string{"a.go", "b.go", "c.go"}
			})},
			identities: linked("p1"),
			candidates: &fakeCandidates{authored: []*attributionv1.PRCandidate{
				testPRCandidate("org/repo#PR-9#merged", now, map[string]any{
					"repo": "org/repo", "files_changed": []string{"a.go", "b.go", "c.go"},
				}),
			}},
			wantResults: 1,
			wantMatched: true,
			wantMethod:  attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP,
		},
		{
			name:       "a person with no linked vendor account abstains",
			sessions:   []*attributionv1.SessionEvidence{sessionFor("p1", now, nil)},
			identities: &fakeIdentity{resolved: map[string]Identity{"p1": {}}},
			candidates: &fakeCandidates{},

			wantResults: 1,
			wantReason:  attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_VCS_NOT_LINKED,
		},
		{
			name: "a pull request past the window abstains",
			sessions: []*attributionv1.SessionEvidence{sessionFor("p1", now, func(s *attributionv1.SessionEvidence) {
				s.RepoName = "org/repo"
				s.FilesEdited = []string{"a.go"}
			})},
			identities: linked("p1"),
			candidates: &fakeCandidates{authored: []*attributionv1.PRCandidate{
				testPRCandidate("org/repo#PR-9#merged", now.AddDate(0, 0, 90), map[string]any{
					"repo": "org/repo", "files_changed": []string{"a.go"},
				}),
			}},
			wantResults: 1,
			wantReason:  attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR,
		},
		{
			name: "overlap below the threshold abstains",
			sessions: []*attributionv1.SessionEvidence{sessionFor("p1", now, func(s *attributionv1.SessionEvidence) {
				s.RepoName = "org/repo"
				s.FilesEdited = []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go", "h.go", "i.go", "j.go"}
			})},
			identities: linked("p1"),
			candidates: &fakeCandidates{authored: []*attributionv1.PRCandidate{
				testPRCandidate("org/repo#PR-9#merged", now, map[string]any{
					"repo": "org/repo", "files_changed": []string{"a.go"},
				}),
			}},
			wantResults: 1,
			wantReason:  attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR,
		},
		{
			// A linked person with candidates, and a session that recorded no
			// branch, reference or edited file: nothing can name a pull request.
			name:       "a session with no evidence abstains even when its person has candidates",
			sessions:   []*attributionv1.SessionEvidence{sessionFor("p1", now, nil)},
			identities: linked("p1"),
			candidates: &fakeCandidates{authored: []*attributionv1.PRCandidate{
				testPRCandidate("org/repo#PR-9#merged", now, map[string]any{
					"repo": "org/repo", "head_branch": "feat/x", "files_changed": []string{"a.go"},
				}),
			}},
			wantResults: 1,
			wantReason:  attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR,
		},
		{
			name:     "a failed identity lookup is recorded so it retries on the cooldown",
			sessions: []*attributionv1.SessionEvidence{sessionFor("p1", now, nil)},
			identities: &fakeIdentity{
				errs: map[string]error{"p1": errors.New("identity service unavailable")},
			},
			candidates:  &fakeCandidates{},
			wantResults: 1,
			wantReason:  attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_IDENTITY_ERROR,
		},
		{
			name: "a failed identity lookup never overwrites matches the session already has",
			sessions: []*attributionv1.SessionEvidence{sessionFor("p1", now, func(s *attributionv1.SessionEvidence) {
				s.StoredAttribution = &attributionv1.PRAttribution{
					Matches: []*attributionv1.PRMatch{{PrKey: "org/repo#PR-1#merged", PrNumber: 1}},
				}
			})},
			identities: &fakeIdentity{
				errs: map[string]error{"p1": errors.New("identity service unavailable")},
			},
			candidates:  &fakeCandidates{},
			wantResults: 0,
			wantPending: true,
		},
		{
			name:        "a failed candidate fetch leaves the session pending rather than abstaining",
			sessions:    []*attributionv1.SessionEvidence{sessionFor("p1", now, nil)},
			identities:  linked("p1"),
			candidates:  &fakeCandidates{authorErr: errors.New("vendor unavailable")},
			wantResults: 0,
			wantPending: true,
		},
		{
			name:        "a failed reviewer fetch also leaves the session pending, not author-only",
			sessions:    []*attributionv1.SessionEvidence{sessionFor("p1", now, nil)},
			identities:  linked("p1"),
			candidates:  &fakeCandidates{reviewErr: errors.New("vendor unavailable")},
			wantResults: 0,
			wantPending: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := New(testParams(), tt.candidates, tt.identities)
			results, err := e.Attribute(context.Background(), "org-1", tt.sessions)
			require.NoError(t, err)
			require.Len(t, results, tt.wantResults)

			if tt.wantResults == 0 {
				return
			}
			got := firstResultFor(t, results, tt.sessions[0].GetSessionId())
			if tt.wantMatched {
				require.NotEmpty(t, got.Attribution.GetMatches(), "expected a match")
				assert.Equal(t, tt.wantMethod, got.Attribution.GetMatches()[0].GetMethod())
				return
			}
			require.Empty(t, got.Attribution.GetMatches())
			assert.Equal(t, tt.wantReason, got.Attribution.GetUnmatched().GetReason())
		})
	}
}

// A batch spanning several people resolves each independently: one person's
// failure must not decide another's sessions.
func TestEngineAttributeIsolatesPeople(t *testing.T) {
	now := time.Now().UTC().Truncate(24 * time.Hour)

	sessions := []*attributionv1.SessionEvidence{
		sessionFor("good", now, func(s *attributionv1.SessionEvidence) {
			s.PrNumber = 42
			s.PrRepository = "org/repo"
		}),
		sessionFor("broken", now, nil),
	}
	identities := &fakeIdentity{
		resolved: map[string]Identity{"good": {AuthorIDs: []string{"vendor-1"}}},
		errs:     map[string]error{"broken": errors.New("identity service unavailable")},
	}
	candidates := &fakeCandidates{authored: []*attributionv1.PRCandidate{
		testPRCandidate("org/repo#PR-42#merged", now, map[string]any{"repo": "org/repo"}),
	}}

	e := New(testParams(), candidates, identities)
	results, err := e.Attribute(context.Background(), "org-1", sessions)
	require.NoError(t, err)
	require.Len(t, results, 2)

	good := firstResultFor(t, results, "s-good")
	require.NotEmpty(t, good.Attribution.GetMatches())
	assert.Equal(t, "org/repo#PR-42#merged", good.Attribution.GetMatches()[0].GetPrKey())

	broken := firstResultFor(t, results, "s-broken")
	assert.Equal(t,
		attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_IDENTITY_ERROR,
		broken.Attribution.GetUnmatched().GetReason())
}

// CandidateCount and PossiblePRs are what let a persister tell a match the rules
// refused from one that was never examined. A result that evaluated candidates
// must carry both; one that had none must not claim it did.
func TestEngineAttributeCarriesCandidateProvenance(t *testing.T) {
	now := time.Now().UTC().Truncate(24 * time.Hour)
	session := sessionFor("p1", now, func(s *attributionv1.SessionEvidence) {
		s.RepoName = "org/repo"
		s.FilesEdited = []string{"nothing-in-common.go"}
	})
	candidates := &fakeCandidates{authored: []*attributionv1.PRCandidate{
		testPRCandidate("org/repo#PR-5#merged", now, map[string]any{
			"repo": "org/repo", "files_changed": []string{"other.go"},
		}),
	}}

	e := New(testParams(), candidates, linked("p1"))
	results, err := e.Attribute(context.Background(), "org-1", []*attributionv1.SessionEvidence{session})
	require.NoError(t, err)
	require.Len(t, results, 1)

	got := results[0]
	assert.Empty(t, got.Attribution.GetMatches(), "nothing should match")
	assert.Equal(t, 1, got.CandidateCount, "the candidate was evaluated and refused, not absent")
	assert.Contains(t, got.PossiblePRs, "org/repo#PR-5#merged")
}

func TestSummariseCountsBySignalAndReason(t *testing.T) {
	match := func(m attributionv1.SessionToPRMatchMethod, key string) *attributionv1.PRMatch {
		return &attributionv1.PRMatch{Method: m, PrKey: key}
	}
	unmatched := func(r attributionv1.SessionToPRUnmatchedReason) MatchResult {
		return MatchResult{Attribution: &attributionv1.PRAttribution{
			Unmatched: &attributionv1.PRUnmatched{Reason: r},
		}}
	}

	got := Summarise([]MatchResult{
		// One session, two pull requests: matches count per link, not per session.
		{Attribution: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{
			match(attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF, "a"),
			match(attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP, "b"),
		}}},
		{Attribution: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{
			match(attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH, "c"),
		}}},
		unmatched(attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR),
		unmatched(attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_VCS_NOT_LINKED),
		unmatched(attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_IDENTITY_ERROR),
	})

	assert.Equal(t, 1, got.MatchedDirectPRRef)
	assert.Equal(t, 1, got.MatchedBranch)
	assert.Equal(t, 1, got.MatchedFileOverlap)
	assert.Equal(t, 1, got.UnmatchedNoMatchingPR)
	assert.Equal(t, 1, got.UnmatchedVCSNotLinked)
	assert.Equal(t, 1, got.UnmatchedIdentityErr)
}

// The three abstention reasons must stay distinct in the summary. Collapsing
// them hides the difference between an answer and a missing answer: a rise in
// identity errors is an outage, a rise in no-matching-pull-request is not.
func TestSummariseKeepsTheAbstentionReasonsApart(t *testing.T) {
	reasons := []attributionv1.SessionToPRUnmatchedReason{
		attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR,
		attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_VCS_NOT_LINKED,
		attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_IDENTITY_ERROR,
	}
	for _, r := range reasons {
		t.Run(r.String(), func(t *testing.T) {
			got := Summarise([]MatchResult{{Attribution: &attributionv1.PRAttribution{
				Unmatched: &attributionv1.PRUnmatched{Reason: r},
			}}})
			total := got.UnmatchedNoMatchingPR + got.UnmatchedVCSNotLinked + got.UnmatchedIdentityErr
			assert.Equal(t, 1, total, "exactly one bucket should move")
		})
	}
}
