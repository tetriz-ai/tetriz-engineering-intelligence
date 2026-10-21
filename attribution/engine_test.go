// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

func TestComputeMatches_FromRawRecords(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(24 * time.Hour)

	tests := []struct {
		name           string
		session        *attributionv1.SessionEvidence
		prs            []rawRecord
		wantMatched    bool
		wantMethod     attributionv1.SessionToPRMatchMethod
		wantConfidence float64
		wantReason     attributionv1.SessionToPRUnmatchedReason
	}{
		{
			name: "direct_pr_ref wins over later signals",
			session: &attributionv1.SessionEvidence{
				SessionId:    "s1",
				StartDate:    ts(now),
				IdeType:      "Claude Code",
				PrNumber:     42,
				PrRepository: "acme/widgets",
				FilesEdited:  []string{"src/handler.go"},
			},
			prs: []rawRecord{{
				RecordKey: "acme/widgets#PR-42#merged",
				DataDate:  now,
				Metadata: testMetadata(map[string]any{
					"pr_author_id":     "12345",
					"repo":             "acme/widgets",
					"files_changed":    []string{"src/handler.go", "README.md"},
					"files_line_stats": []map[string]any{{"path": "src/handler.go", "additions": 100, "deletions": 20}},
				}),
			}},
			wantMatched:    true,
			wantMethod:     attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF,
			wantConfidence: confidenceDirectPRRef,
		},
		{
			name: "no candidate PRs returns unmatched",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s2",
				StartDate:   ts(now),
				FilesEdited: []string{"a.go"},
			},
			prs:         nil,
			wantMatched: false,
			wantReason:  attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR,
		},
		{
			name: "file_overlap matched",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s3",
				StartDate:   ts(now),
				IdeType:     "Cursor",
				FilesEdited: []string{"src/handler.go", "src/handler_test.go"},
			},
			prs: []rawRecord{{
				RecordKey: "repo#PR-10#opened",
				DataDate:  now.AddDate(0, 0, 1),
				Metadata: testMetadata(map[string]any{
					"pr_author_id":     "12345",
					"files_changed":    []string{"src/handler.go", "src/handler_test.go", "README.md"},
					"files_line_stats": []map[string]any{{"path": "src/handler.go", "additions": 80, "deletions": 15}},
				}),
			}},
			wantMatched: true,
			wantMethod:  attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP,
		},
	}

	p := &Engine{params: testParams()}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			prCtxs := buildPRCandidates(tt.prs, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED)
			attr := p.computeMatches(tt.session, prCtxs)
			require.NotNil(t, attr)

			if tt.wantMatched {
				matches := attr.GetMatches()
				require.NotEmpty(t, matches, "expected match result")
				m := matches[0]
				assert.Equal(t, tt.wantMethod, m.GetMethod())
				if tt.wantConfidence > 0 {
					assert.InDelta(t, tt.wantConfidence, m.GetConfidence(), 0.001)
				}
				assert.NotEmpty(t, m.GetSignalsEvaluated(), "signals_evaluated should be populated")
			} else {
				u := attr.GetUnmatched()
				require.NotNil(t, u, "expected unmatched result")
				assert.Equal(t, tt.wantReason, u.GetReason())
				assert.NotEmpty(t, u.GetLastAttemptedAt())
			}
		})
	}
}

func TestComputeMatches_DifferentPRs(t *testing.T) {
	session := &attributionv1.SessionEvidence{
		StartDate:   ts(time.Now()),
		FilesEdited: []string{"server/a.go", "web/b.ts"},
	}
	prA := &attributionv1.PRCandidate{
		PrKey:             "org/repo#PR-1#merged",
		MergedAt:          ts(time.Now()),
		PrRepository:      "org/repo",
		PrNumber:          1,
		FilesChanged:      []string{"server/a.go", "server/other.go"},
		ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
	}
	prB := &attributionv1.PRCandidate{
		PrKey:             "org/repo#PR-2#merged",
		MergedAt:          ts(time.Now()),
		PrRepository:      "org/repo",
		PrNumber:          2,
		FilesChanged:      []string{"web/b.ts", "web/c.ts"},
		ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
	}

	p := &Engine{params: testParams()}
	attr := p.computeMatches(session, []*attributionv1.PRCandidate{prA, prB})

	assert.Len(t, attr.GetMatches(), 2)
	prKeys := []string{attr.GetMatches()[0].PrKey, attr.GetMatches()[1].PrKey}
	assert.ElementsMatch(t, []string{"org/repo#PR-1#merged", "org/repo#PR-2#merged"}, prKeys)
}

// TestComputeMatches_BranchDedup verifies that two PRs sharing the same branch
// collapse to the temporally-nearest one.
func TestComputeMatches_BranchDedup(t *testing.T) {
	now := time.Now().UTC().Truncate(24 * time.Hour)
	session := &attributionv1.SessionEvidence{
		StartDate:   ts(now),
		GitBranches: []string{"feat/thing"},
	}
	prNear := &attributionv1.PRCandidate{
		PrKey:             "org/repo#PR-1#merged",
		MergedAt:          ts(now.AddDate(0, 0, 1)),
		PrRepository:      "org/repo",
		HeadBranch:        "feat/thing",
		ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
	}
	prFar := &attributionv1.PRCandidate{
		PrKey:             "org/repo#PR-2#merged",
		MergedAt:          ts(now.AddDate(0, 0, 5)),
		PrRepository:      "org/repo",
		HeadBranch:        "feat/thing",
		ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
	}

	p := &Engine{params: testParams()}
	attr := p.computeMatches(session, []*attributionv1.PRCandidate{prNear, prFar})
	assert.Len(t, attr.GetMatches(), 1, "only nearest PR should match")
	assert.Equal(t, "org/repo#PR-1#merged", attr.GetMatches()[0].GetPrKey())
}

func TestComputeMatches_SamePRDedupBestTierWins(t *testing.T) {
	session := &attributionv1.SessionEvidence{
		StartDate:    ts(time.Now()),
		PrNumber:     42,
		PrRepository: "org/repo",
		GitBranch:    "feature-x",
		FilesEdited:  []string{"a.go"},
	}
	pr := &attributionv1.PRCandidate{
		PrKey:             "org/repo#PR-42#merged",
		MergedAt:          ts(time.Now()),
		PrRepository:      "org/repo",
		PrNumber:          42,
		HeadBranch:        "feature-x",
		FilesChanged:      []string{"a.go"},
		ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
	}

	p := &Engine{params: testParams()}
	attr := p.computeMatches(session, []*attributionv1.PRCandidate{pr})

	assert.Len(t, attr.GetMatches(), 1)
	m := attr.GetMatches()[0]
	assert.Equal(t, attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF, m.Method)
	assert.Contains(t, m.SignalsEvaluated, "SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH")
	assert.Contains(t, m.SignalsEvaluated, "SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP")
}

func TestComputeMatches_NoMatchEmitsUnmatched(t *testing.T) {
	session := &attributionv1.SessionEvidence{
		StartDate:   ts(time.Now()),
		FilesEdited: []string{"unrelated.go"},
	}
	pr := &attributionv1.PRCandidate{
		PrKey:             "org/repo#PR-1#merged",
		MergedAt:          ts(time.Now()),
		FilesChanged:      []string{"different.go"},
		ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
	}

	p := &Engine{params: testParams()}
	attr := p.computeMatches(session, []*attributionv1.PRCandidate{pr})

	assert.Empty(t, attr.GetMatches())
	assert.NotNil(t, attr.GetUnmatched())
	assert.Equal(t, attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR, attr.GetUnmatched().Reason)
}

// TestComputeMatches_ReviewerSide verifies the reviewer-side signal logic:
//   - DIRECT_PR_REF and BRANCH_MATCH fire for REVIEWED candidates, producing matches
//     with ParticipationKind = REVIEWED.
//   - FILE_OVERLAP is gated to AUTHORED candidates only — a REVIEWED candidate with
//     high file overlap must produce no match.
//   - An AUTHORED match carries explicit ParticipationKind = AUTHORED (no UNSPECIFIED).
//
// Self-review routing (PR in both AUTHORED and REVIEWED sets) is covered by
// TestComputeMatches_SelfCodeReviewRouting.
func TestComputeMatches_ReviewerSide(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(24 * time.Hour)

	tests := []struct {
		name        string
		session     *attributionv1.SessionEvidence
		prs         []*attributionv1.PRCandidate
		wantMatched bool
		wantMethod  attributionv1.SessionToPRMatchMethod
		wantKind    attributionv1.SessionParticipationKind
		wantReason  attributionv1.SessionToPRUnmatchedReason
	}{
		{
			name: "reviewer-side DIRECT_PR_REF produces REVIEWED match",
			session: &attributionv1.SessionEvidence{
				SessionId:    "s-reviewed-direct",
				StartDate:    ts(now),
				IdeType:      "Claude Code",
				PrNumber:     77,
				PrRepository: "org/repo",
				FilesEdited:  []string{"main.go"},
			},
			prs: []*attributionv1.PRCandidate{{
				PrKey:             "org/repo#PR-77#merged",
				MergedAt:          ts(now),
				PrRepository:      "org/repo",
				PrNumber:          77,
				FilesChanged:      []string{"other.go"},
				ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED,
			}},
			wantMatched: true,
			wantMethod:  attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF,
			wantKind:    attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED,
		},
		{
			name: "reviewer-side BRANCH_MATCH produces REVIEWED match",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s-reviewed-branch",
				StartDate:   ts(now),
				GitBranch:   "feat/new-feature",
				FilesEdited: []string{"impl.go"},
			},
			prs: []*attributionv1.PRCandidate{{
				PrKey:             "org/repo#PR-88#merged",
				MergedAt:          ts(now.AddDate(0, 0, 1)),
				PrRepository:      "org/repo",
				PrNumber:          88,
				HeadBranch:        "feat/new-feature",
				FilesChanged:      []string{"other.go"},
				ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED,
			}},
			wantMatched: true,
			wantMethod:  attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH,
			wantKind:    attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED,
		},
		{
			name: "reviewer-side high file overlap alone produces NO match (FILE_OVERLAP gated)",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s-reviewed-overlap",
				StartDate:   ts(now),
				FilesEdited: []string{"a.go", "b.go", "c.go"},
			},
			prs: []*attributionv1.PRCandidate{{
				PrKey:             "org/repo#PR-99#merged",
				MergedAt:          ts(now.AddDate(0, 0, 1)),
				PrRepository:      "org/repo",
				PrNumber:          99,
				FilesChanged:      []string{"a.go", "b.go", "c.go"},
				ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED,
			}},
			wantMatched: false,
			wantReason:  attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR,
		},
		{
			name: "author-side match produces explicit AUTHORED kind",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s-authored",
				StartDate:   ts(now),
				FilesEdited: []string{"handler.go", "handler_test.go"},
			},
			prs: []*attributionv1.PRCandidate{{
				PrKey:             "org/repo#PR-42#merged",
				MergedAt:          ts(now.AddDate(0, 0, 1)),
				PrRepository:      "org/repo",
				PrNumber:          42,
				FilesChanged:      []string{"handler.go", "handler_test.go", "README.md"},
				ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
			}},
			wantMatched: true,
			wantMethod:  attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP,
			wantKind:    attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
		},
	}

	p := &Engine{params: testParams()}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			attr := p.computeMatches(tt.session, tt.prs)
			require.NotNil(t, attr)

			if tt.wantMatched {
				matches := attr.GetMatches()
				require.NotEmpty(t, matches, "expected match result")
				m := matches[0]
				assert.Equal(t, tt.wantMethod, m.GetMethod(), "method mismatch")
				assert.Equal(t, tt.wantKind, m.GetParticipationKind(), "participation kind mismatch")
			} else {
				u := attr.GetUnmatched()
				require.NotNil(t, u, "expected unmatched result")
				assert.Equal(t, tt.wantReason, u.GetReason())
			}
		})
	}
}

// TestComputeMatches_SelfCodeReviewRouting verifies the task-type-aware routing
// for PRs where the session person is both author AND reviewer of the PR.
//
// Rule (per match emission):
//   - not author                                       -> REVIEWED
//   - author + self-review     + taskType=code-review  -> REVIEWED
//   - author + self-review     + taskType!=code-review -> AUTHORED
//   - author + no self-review  + any taskType          -> AUTHORED
//
// Self-review is represented in computeMatches input as a PR appearing in BOTH
// the AUTHORED and REVIEWED candidate sets (same RecordKey).
func TestComputeMatches_SelfCodeReviewRouting(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(24 * time.Hour)

	const recordKey = "org/repo#PR-42#merged"

	authorCtx := func() *attributionv1.PRCandidate {
		return &attributionv1.PRCandidate{
			PrKey:             recordKey,
			MergedAt:          ts(now),
			PrRepository:      "org/repo",
			PrNumber:          42,
			FilesChanged:      []string{"main.go"},
			ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
		}
	}
	reviewerCtx := func() *attributionv1.PRCandidate {
		return &attributionv1.PRCandidate{
			PrKey:             recordKey,
			MergedAt:          ts(now),
			PrRepository:      "org/repo",
			PrNumber:          42,
			FilesChanged:      []string{"main.go"},
			ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED,
		}
	}

	directRefSession := func(taskType string) *attributionv1.SessionEvidence {
		return &attributionv1.SessionEvidence{
			SessionId:    "s-" + taskType,
			StartDate:    ts(now),
			IdeType:      "Claude Code",
			PrNumber:     42,
			PrRepository: "org/repo",
			FilesEdited:  []string{"main.go"},
			TaskType:     taskType,
		}
	}

	tests := []struct {
		name      string
		session   *attributionv1.SessionEvidence
		prs       []*attributionv1.PRCandidate
		wantKind  attributionv1.SessionParticipationKind
		wantCount int
	}{
		{
			name:      "author + self-review + code-review routes to REVIEWED",
			session:   directRefSession("code-review"),
			prs:       []*attributionv1.PRCandidate{authorCtx(), reviewerCtx()},
			wantKind:  attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED,
			wantCount: 1,
		},
		{
			name:      "author + self-review + bug-fix routes to AUTHORED",
			session:   directRefSession("bug-fix"),
			prs:       []*attributionv1.PRCandidate{authorCtx(), reviewerCtx()},
			wantKind:  attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
			wantCount: 1,
		},
		{
			name:      "author + no self-review + code-review routes to AUTHORED",
			session:   directRefSession("code-review"),
			prs:       []*attributionv1.PRCandidate{authorCtx()},
			wantKind:  attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
			wantCount: 1,
		},
		{
			name:      "not author + reviewed + code-review stays REVIEWED",
			session:   directRefSession("code-review"),
			prs:       []*attributionv1.PRCandidate{reviewerCtx()},
			wantKind:  attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED,
			wantCount: 1,
		},
		{
			name:      "not author + reviewed + bug-fix stays REVIEWED",
			session:   directRefSession("bug-fix"),
			prs:       []*attributionv1.PRCandidate{reviewerCtx()},
			wantKind:  attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED,
			wantCount: 1,
		},
		{
			name:      "author + self-review + empty taskType routes to AUTHORED",
			session:   directRefSession(""),
			prs:       []*attributionv1.PRCandidate{authorCtx(), reviewerCtx()},
			wantKind:  attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
			wantCount: 1,
		},
	}

	p := &Engine{params: testParams()}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			attr := p.computeMatches(tt.session, tt.prs)
			require.NotNil(t, attr)
			matches := attr.GetMatches()
			require.Len(t, matches, tt.wantCount, "expected exactly %d match(es) per (session,PR)", tt.wantCount)
			assert.Equal(t, tt.wantKind, matches[0].GetParticipationKind(), "participation kind mismatch")
			assert.Equal(t, recordKey, matches[0].GetPrKey(), "pr_key mismatch")
		})
	}
}

// TestFilterByKind verifies that filterByKind returns only contexts of the requested kind.
func TestFilterByKind(t *testing.T) {
	t.Parallel()
	authored := &attributionv1.PRCandidate{ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED}
	reviewed := &attributionv1.PRCandidate{ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED}
	unspec := &attributionv1.PRCandidate{ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_UNSPECIFIED}

	prs := []*attributionv1.PRCandidate{authored, reviewed, unspec}
	got := filterByKind(prs, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED)
	assert.Equal(t, []*attributionv1.PRCandidate{authored}, got)

	got = filterByKind(prs, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED)
	assert.Equal(t, []*attributionv1.PRCandidate{reviewed}, got)
}

func TestPRMatchFromContext(t *testing.T) {
	now := time.Now().UTC().Truncate(24 * time.Hour)
	pr := testPRCandidate("repo#PR-42#merged", now, map[string]any{
		"repo":             "repo",
		"files_changed":    []string{"handler.go", "README.md"},
		"files_line_stats": []map[string]any{{"path": "handler.go", "additions": 100, "deletions": 20}},
	})

	m := prMatchFromContext(pr, 1.0, []string{"handler.go"},
		attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_FILE_OVERLAP)
	require.NotNil(t, m)
	assert.Equal(t, "repo#PR-42#merged", m.GetPrKey())
	assert.Equal(t, int32(42), m.GetPrNumber())
	assert.Equal(t, "repo", m.GetPrRepository())
	assert.InDelta(t, 1.0, m.GetConfidence(), 0.001)
	assert.Equal(t, []string{"handler.go"}, m.GetMatchedFiles())
	assert.Equal(t, int64(100), m.GetLinesAdded())
	assert.Equal(t, int64(20), m.GetLinesRemoved())
}

func TestBuildUnmatchedAttribution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		reason           attributionv1.SessionToPRUnmatchedReason
		signalsEvaluated []string
	}{
		{
			name:   "no_matching_pr with evaluated signals",
			reason: attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR,
			signalsEvaluated: []string{
				attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF.String(),
				attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH.String(),
				attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP.String(),
				attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_AI_SIGNATURE.String(),
			},
		},
		{
			name:             "vcs_not_linked with nil signals",
			reason:           attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_VCS_NOT_LINKED,
			signalsEvaluated: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			attr := buildUnmatchedAttribution(tt.reason, tt.signalsEvaluated)
			require.NotNil(t, attr)

			u := attr.GetUnmatched()
			require.NotNil(t, u)
			assert.Equal(t, tt.reason, u.GetReason())
			assert.NotEmpty(t, u.GetLastAttemptedAt(), "last_attempted_at should be set")

			parsed, parseErr := time.Parse(time.RFC3339, u.GetLastAttemptedAt())
			require.NoError(t, parseErr, "last_attempted_at should be valid RFC3339")
			assert.WithinDuration(t, time.Now().UTC(), parsed, 5*time.Second)
		})
	}
}

func TestEffectiveStrongSignalWindowDays(t *testing.T) {
	makeParams := func(strong, timeWindow int) Params {
		return Params{
			TimeWindowAfterDays:         timeWindow,
			StrongSignalWindowAfterDays: strong,
			MinConfidencePercent:        30,
			RetryCooldownHours:          12,
			MaxRetryDays:                5,
			IdentityResolveConcurrency:  10,
			ComputeMatchConcurrency:     10,
		}
	}

	tests := []struct {
		name   string
		strong int
		time   int
		want   int
	}{
		{name: "strong > 0 wins", strong: 14, time: 3, want: 14},
		{name: "strong == 0 falls back to time", strong: 0, time: 3, want: 3},
		{name: "strong < 0 falls back to time", strong: -1, time: 3, want: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := effectiveStrongSignalWindowDays(makeParams(tt.strong, tt.time))
			assert.Equal(t, tt.want, got)
		})
	}
}
