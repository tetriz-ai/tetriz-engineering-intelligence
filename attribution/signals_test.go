// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"encoding/json"
	"testing"
	"time"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchDirectPRRef(t *testing.T) {
	now := time.Now().UTC().Truncate(24 * time.Hour)

	tests := []struct {
		name       string
		session    *attributionv1.SessionEvidence
		prs        []*attributionv1.PRCandidate
		wantPRKeys []string // expected set of matched PR keys, order-independent; empty/nil = no match
		wantConf   float64  // only checked when wantPRKeys has exactly one entry
	}{
		{
			name: "matches when pr_number and pr_repository present",
			session: &attributionv1.SessionEvidence{
				SessionId:    "s1",
				StartDate:    ts(now),
				PrNumber:     42,
				PrRepository: "acme/widgets",
				FilesEdited:  []string{"src/handler.go"},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("acme/widgets#PR-42#merged", now, map[string]any{
				"repo":             "acme/widgets",
				"files_changed":    []string{"src/handler.go", "README.md"},
				"files_line_stats": []map[string]any{{"path": "src/handler.go", "additions": 100, "deletions": 20}},
			})},
			wantPRKeys: []string{"acme/widgets#PR-42#merged"},
			wantConf:   confidenceDirectPRRef,
		},
		{
			// Producers disagree on repo casing; the direct-ref arm compares
			// case-insensitively like reviewerBranchEvidence, or it would drop a
			// reference naming this exact PR on the highest-confidence arm.
			name: "matches when repo differs only in case",
			session: &attributionv1.SessionEvidence{
				SessionId:    "s1c",
				StartDate:    ts(now),
				PrNumber:     42,
				PrRepository: "Acme/Widgets",
				FilesEdited:  []string{"src/handler.go"},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("acme/widgets#PR-42#merged", now, map[string]any{
				"repo":          "acme/widgets",
				"files_changed": []string{"src/handler.go"},
			})},
			wantPRKeys: []string{"acme/widgets#PR-42#merged"},
			wantConf:   confidenceDirectPRRef,
		},
		{
			// Candidates are fetched per person, so without a per-session ceiling this
			// session's matches would depend on how long some OTHER session in the
			// same batch ran.
			name: "empty when the referenced PR merged past this session's own window",
			session: &attributionv1.SessionEvidence{
				SessionId:    "s-window",
				StartDate:    ts(now),
				StartedAt:    tsPtr(&now),
				PrReferences: []*attributionv1.PRReference{{PrNumber: 42, PrRepository: "acme/widgets"}},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("acme/widgets#PR-42#merged", now.AddDate(0, 0, 30), map[string]any{
				"repo": "acme/widgets",
			})},
		},
		{
			// Same reference, but the session stayed active — its own anchor, not the
			// batch's, is what puts the PR in reach.
			name: "long-lived session still reaches its referenced PR",
			session: &attributionv1.SessionEvidence{
				SessionId:              "s-window-long",
				StartDate:              ts(now),
				StartedAt:              tsPtr(&now),
				PrReferences:           []*attributionv1.PRReference{{PrNumber: 42, PrRepository: "acme/widgets"}},
				SessionDurationSeconds: 28 * 24 * 3600,
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("acme/widgets#PR-42#merged", now.AddDate(0, 0, 30), map[string]any{
				"repo": "acme/widgets",
			})},
			wantPRKeys: []string{"acme/widgets#PR-42#merged"},
			wantConf:   confidenceDirectPRRef,
		},
		{
			// A session may legitimately name a PR that merged before it opened —
			// reviewing or building on already-merged work. Only the ceiling applies.
			name: "referenced PR that merged before the session started still matches",
			session: &attributionv1.SessionEvidence{
				SessionId:    "s-before-start",
				StartDate:    ts(now),
				StartedAt:    tsPtr(&now),
				PrReferences: []*attributionv1.PRReference{{PrNumber: 42, PrRepository: "acme/widgets"}},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("acme/widgets#PR-42#merged", now.AddDate(0, 0, -5), map[string]any{
				"repo": "acme/widgets",
			})},
			wantPRKeys: []string{"acme/widgets#PR-42#merged"},
			wantConf:   confidenceDirectPRRef,
		},
		{
			// A reviewer-side fetch can admit still-open PRs, which carry no merge
			// date for a window to place.
			name: "undated (still-open) candidate is exempt from the window",
			session: &attributionv1.SessionEvidence{
				SessionId:    "s-open-pr",
				StartDate:    ts(now),
				StartedAt:    tsPtr(&now),
				PrReferences: []*attributionv1.PRReference{{PrNumber: 42, PrRepository: "acme/widgets"}},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidateWithKind("acme/widgets#PR-42#merged", time.Time{}, map[string]any{
				"repo": "acme/widgets",
			}, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED)},
			wantPRKeys: []string{"acme/widgets#PR-42#merged"},
			wantConf:   confidenceDirectPRRef,
		},
		{
			name: "empty when pr_number is zero",
			session: &attributionv1.SessionEvidence{
				SessionId:    "s2",
				StartDate:    ts(now),
				PrRepository: "acme/widgets",
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("acme/widgets#PR-42#merged", now, map[string]any{"repo": "acme/widgets"})},
		},
		{
			name: "empty when pr_repository is empty",
			session: &attributionv1.SessionEvidence{
				SessionId: "s3",
				StartDate: ts(now),
				PrNumber:  42,
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("acme/widgets#PR-42#merged", now, nil)},
		},
		{
			name: "empty when no PR record matches prefix",
			session: &attributionv1.SessionEvidence{
				SessionId:    "s4",
				StartDate:    ts(now),
				PrNumber:     99,
				PrRepository: "acme/widgets",
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("acme/widgets#PR-42#merged", now, nil)},
		},
		{
			name: "empty when usage metadata is nil",
			session: &attributionv1.SessionEvidence{
				SessionId: "s5",
				StartDate: ts(now),
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("repo#PR-1#opened", now, nil)},
		},
		{
			name: "GitLab MR matches via repo metadata and MR IID",
			session: &attributionv1.SessionEvidence{
				SessionId:    "gl-1",
				StartDate:    ts(now),
				PrNumber:     42,
				PrRepository: "mygroup/myproject",
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("mr-100!42#merged", now, map[string]any{
				"repo": "mygroup/myproject",
			})},
			wantPRKeys: []string{"mr-100!42#merged"},
			wantConf:   confidenceDirectPRRef,
		},
		{
			name: "GitLab MR does not match when IID differs",
			session: &attributionv1.SessionEvidence{
				SessionId:    "gl-2",
				StartDate:    ts(now),
				PrNumber:     99,
				PrRepository: "mygroup/myproject",
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("mr-100!42#merged", now, map[string]any{
				"repo": "mygroup/myproject",
			})},
		},
		{
			// Only the single scalar PR matches; a second candidate PR sharing
			// the same repo but a different number must NOT match.
			name: "only the scalar-referenced PR matches, not other repo PRs",
			session: &attributionv1.SessionEvidence{
				SessionId:    "s-multi-candidate",
				StartDate:    ts(now),
				PrNumber:     42,
				PrRepository: "acme/widgets",
			},
			prs: []*attributionv1.PRCandidate{
				testPRCandidate("acme/widgets#PR-42#merged", now, map[string]any{"repo": "acme/widgets"}),
				testPRCandidate("acme/widgets#PR-99#merged", now, map[string]any{"repo": "acme/widgets"}),
			},
			wantPRKeys: []string{"acme/widgets#PR-42#merged"},
			wantConf:   confidenceDirectPRRef,
		},
		{
			// pr_references is the authoritative list; two entries → two matches.
			name: "pr_references with multiple entries matches all of them",
			session: &attributionv1.SessionEvidence{
				SessionId: "s-multi-refs",
				StartDate: ts(now),
				PrReferences: []*attributionv1.PRReference{
					{PrNumber: 42, PrRepository: "acme/widgets"},
					{PrNumber: 77, PrRepository: "acme/widgets"},
				},
			},
			prs: []*attributionv1.PRCandidate{
				testPRCandidate("acme/widgets#PR-42#merged", now, map[string]any{"repo": "acme/widgets"}),
				testPRCandidate("acme/widgets#PR-77#merged", now, map[string]any{"repo": "acme/widgets"}),
				testPRCandidate("acme/widgets#PR-99#merged", now, map[string]any{"repo": "acme/widgets"}),
			},
			wantPRKeys: []string{"acme/widgets#PR-42#merged", "acme/widgets#PR-77#merged"},
		},
		{
			// When pr_references is populated, the scalar
			// pr_number / pr_repository must not leak into the match set.
			name: "scalar is ignored when pr_references is populated",
			session: &attributionv1.SessionEvidence{
				SessionId:    "s-scalar-ignored",
				StartDate:    ts(now),
				PrNumber:     99,
				PrRepository: "acme/widgets",
				PrReferences: []*attributionv1.PRReference{
					{PrNumber: 42, PrRepository: "acme/widgets"},
				},
			},
			prs: []*attributionv1.PRCandidate{
				testPRCandidate("acme/widgets#PR-42#merged", now, map[string]any{"repo": "acme/widgets"}),
				testPRCandidate("acme/widgets#PR-99#merged", now, map[string]any{"repo": "acme/widgets"}),
			},
			wantPRKeys: []string{"acme/widgets#PR-42#merged"},
			wantConf:   confidenceDirectPRRef,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchDirectPRRef(tt.session, tt.prs, testParams())
			if len(tt.wantPRKeys) == 0 {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, len(tt.wantPRKeys))
			gotKeys := make([]string, 0, len(got))
			for _, m := range got {
				gotKeys = append(gotKeys, m.PrKey)
			}
			assert.ElementsMatch(t, tt.wantPRKeys, gotKeys)
			if len(tt.wantPRKeys) == 1 && tt.wantConf != 0 {
				assert.InDelta(t, tt.wantConf, got[0].Confidence, 0.001)
			}
		})
	}
}

// TestMatchDirectPRRefGated covers the admissibility gate on the highest-
// confidence method, where an ambient reference does the most damage: a PR URL
// read out of a nearby commit names a colleague's PR precisely, and this is the
// arm that would attribute on it at confidence 1.0.
//
// Every case holds the candidate and the reference fixed and varies only the
// source or the IDE, so a rejection proves the gate decided rather than that the
// fixture never reached it.
func TestMatchDirectPRRefGated(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(24 * time.Hour)

	session := func(ideType, repoName string, src attributionv1.SignalSource) *attributionv1.SessionEvidence {
		return &attributionv1.SessionEvidence{
			SessionId: "d1",
			StartDate: ts(now),
			IdeType:   ideType,
			RepoName:  repoName,
			PrReferences: []*attributionv1.PRReference{
				{PrNumber: 42, PrRepository: "org/gadgets", Source: src},
			},
		}
	}
	candidate := func() []*attributionv1.PRCandidate {
		return []*attributionv1.PRCandidate{testPRCandidate("org/gadgets#PR-42#merged", now, map[string]any{"repo": "org/gadgets"})}
	}
	const kiro = "IDE_TYPE_KIRO"

	tests := []struct {
		name      string
		session   *attributionv1.SessionEvidence
		wantMatch bool
	}{
		{
			// A URL scraped from a teammate's commit.
			name:    "gated: a local-git reference does not attribute",
			session: session(kiro, "org/gadgets", attributionv1.SignalSource_SIGNAL_SOURCE_LOCAL_GIT_WINDOWED),
		},
		{
			name:    "gated: an unclassified reference does not attribute",
			session: session(kiro, "org/gadgets", attributionv1.SignalSource_SIGNAL_SOURCE_UNSPECIFIED),
		},
		{
			name:      "gated: a command receipt still attributes",
			session:   session(kiro, "org/gadgets", attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND),
			wantMatch: true,
		},
		{
			name:      "gated: prose naming a PR in the session's own repo attributes",
			session:   session(kiro, "org/gadgets", attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_PROSE),
			wantMatch: true,
		},
		{
			name:    "gated: prose naming a PR outside the session's repo does not",
			session: session(kiro, "org/other", attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_PROSE),
		},
		{
			// The control that makes every rejection above meaningful: identical
			// fixture, ungated IDE, still attributes exactly as before.
			name:      "ungated IDE keeps attributing on a local-git reference",
			session:   session("IDE_TYPE_CLAUDE_CODE", "org/gadgets", attributionv1.SignalSource_SIGNAL_SOURCE_LOCAL_GIT_WINDOWED),
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchDirectPRRef(tt.session, candidate(), testParams())
			if !tt.wantMatch {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			assert.Equal(t, "org/gadgets#PR-42#merged", got[0].GetPrKey())
		})
	}
}

func TestMatchBranch(t *testing.T) {
	now := time.Now().UTC().Truncate(24 * time.Hour)

	tests := []struct {
		name       string
		session    *attributionv1.SessionEvidence
		prs        []*attributionv1.PRCandidate
		wantPRKeys []string // expected set of matched PR keys, order-independent; empty/nil = no match
		wantConf   float64  // only checked when wantPRKeys has exactly one entry
	}{
		{
			name: "matches single PR with same branch in window (scalar fallback)",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s1",
				StartDate:   ts(now),
				GitBranch:   "feat/add-signals",
				FilesEdited: []string{"signals.go"},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("repo#PR-10#opened", now.AddDate(0, 0, 1), map[string]any{
				"head_branch":      "feat/add-signals",
				"files_changed":    []string{"signals.go", "signals_test.go"},
				"files_line_stats": []map[string]any{{"path": "signals.go", "additions": 50, "deletions": 10}},
			})},
			wantPRKeys: []string{"repo#PR-10#opened"},
			wantConf:   confidenceBranchMatch * 0.95, // 1 day gap
		},
		{
			// A reused branch name across multiple PRs links the session only to
			// the PR whose merge is nearest in time, not all of them.
			name: "reused branch attributes to the temporally-closest PR",
			session: &attributionv1.SessionEvidence{
				SessionId: "s2",
				StartDate: ts(now),
				GitBranch: "feat/add-signals",
			},
			prs: []*attributionv1.PRCandidate{
				testPRCandidate("repo#PR-10#opened", now.AddDate(0, 0, 1), map[string]any{"head_branch": "feat/add-signals"}),
				testPRCandidate("repo#PR-11#opened", now.AddDate(0, 0, 5), map[string]any{"head_branch": "feat/add-signals"}),
			},
			wantPRKeys: []string{"repo#PR-10#opened"}, // PR-10 (1d) nearer than PR-11 (5d)
			wantConf:   confidenceBranchMatch * 0.95,
		},
		{
			// Same branch name reused across two different repos must NOT collapse:
			// candidates span a person's PRs across repositories, so each repo keeps
			// its own BRANCH_MATCH (key is (repo, branch), not branch alone).
			name: "same branch in different repos both match",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s-cross-repo",
				StartDate:   ts(now),
				GitBranches: []string{"fix-ci"},
			},
			prs: []*attributionv1.PRCandidate{
				testPRCandidate("org/api#PR-1#merged", now.AddDate(0, 0, 1), map[string]any{"repo": "org/api", "head_branch": "fix-ci"}),
				testPRCandidate("org/web#PR-2#merged", now.AddDate(0, 0, 1), map[string]any{"repo": "org/web", "head_branch": "fix-ci"}),
			},
			wantPRKeys: []string{"org/api#PR-1#merged", "org/web#PR-2#merged"},
		},
		{
			// Shared branch names like main/master/develop are not denied — orgs
			// vary in whether engineers push features directly to such branches, so
			// filtering them risks false-negatives in GitFlow-style workflows.
			name: "matches a shared default branch name like main",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s-generic",
				StartDate:   ts(now),
				GitBranches: []string{"main"},
			},
			prs:        []*attributionv1.PRCandidate{testPRCandidate("repo#PR-12#merged", now, map[string]any{"head_branch": "main"})},
			wantPRKeys: []string{"repo#PR-12#merged"},
			wantConf:   confidenceBranchMatch,
		},
		{
			name: "empty when head branch is a dependency-bot branch",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s-bot",
				StartDate:   ts(now),
				GitBranches: []string{"dependabot/go_modules/foo-1.2.3"},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("repo#PR-13#merged", now, map[string]any{"head_branch": "dependabot/go_modules/foo-1.2.3"})},
		},
		{
			// release/ is a real per-developer feature namespace, not bot-owned —
			// only dependabot/ and renovate/ prefixes are denied, so this matches.
			name: "matches a release/-prefixed feature branch",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s-release",
				StartDate:   ts(now),
				GitBranches: []string{"release/my-feature"},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("repo#PR-14#merged", now.AddDate(0, 0, 1), map[string]any{
				"head_branch":      "release/my-feature",
				"files_line_stats": []map[string]any{{"path": "x.go", "additions": 5, "deletions": 1}},
			})},
			wantPRKeys: []string{"repo#PR-14#merged"},
			wantConf:   confidenceBranchMatch * 0.95,
		},
		{
			// Two PRs on a reused branch merged the same day tie on time-gap; the
			// winner is chosen deterministically (smaller key), never by slice
			// order — PR-16 is listed first but PR-15 (smaller key) must win.
			name: "equal-gap reused branch resolves deterministically",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s-tie",
				StartDate:   ts(now),
				GitBranches: []string{"feat/tie"},
			},
			prs: []*attributionv1.PRCandidate{
				testPRCandidate("repo#PR-16#merged", now.AddDate(0, 0, 2), map[string]any{"head_branch": "feat/tie"}),
				testPRCandidate("repo#PR-15#merged", now.AddDate(0, 0, 2), map[string]any{"head_branch": "feat/tie"}),
			},
			wantPRKeys: []string{"repo#PR-15#merged"},
		},
		{
			name: "empty when branch PR is outside time window",
			session: &attributionv1.SessionEvidence{
				SessionId: "s3",
				StartDate: ts(now),
				GitBranch: "feat/add-signals",
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("repo#PR-10#opened", now.AddDate(0, 0, 7), map[string]any{"head_branch": "feat/add-signals"})},
		},
		{
			name: "empty when no branches and no pr_references",
			session: &attributionv1.SessionEvidence{
				SessionId: "s4",
				StartDate: ts(now),
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("repo#PR-10#opened", now, map[string]any{"head_branch": "feat/add-signals"})},
		},
		{
			name: "empty when no PRs have matching branch",
			session: &attributionv1.SessionEvidence{
				SessionId: "s5",
				StartDate: ts(now),
				GitBranch: "feat/add-signals",
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("repo#PR-10#opened", now, map[string]any{"head_branch": "feat/other-branch"})},
		},
		{
			// A session that works across two feature branches should match
			// PRs on either branch via the git_branches repeated field.
			name: "matches PRs on any branch in git_branches",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s-multi-branch",
				StartDate:   ts(now),
				GitBranches: []string{"alex/feat-1", "alex/feat-2"},
			},
			prs: []*attributionv1.PRCandidate{
				testPRCandidate("acme/widgets#PR-101#merged", now, map[string]any{
					"repo": "acme/widgets", "head_branch": "alex/feat-1",
				}),
				testPRCandidate("acme/widgets#PR-102#merged", now, map[string]any{
					"repo": "acme/widgets", "head_branch": "alex/feat-2",
				}),
				testPRCandidate("acme/widgets#PR-103#merged", now, map[string]any{
					"repo": "acme/widgets", "head_branch": "other/unrelated",
				}),
			},
			wantPRKeys: []string{"acme/widgets#PR-101#merged", "acme/widgets#PR-102#merged"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchBranch(tt.session, tt.prs, testParams())
			if len(tt.wantPRKeys) == 0 {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, len(tt.wantPRKeys))
			gotKeys := make([]string, 0, len(got))
			for _, m := range got {
				gotKeys = append(gotKeys, m.PrKey)
			}
			assert.ElementsMatch(t, tt.wantPRKeys, gotKeys)
			if len(tt.wantPRKeys) == 1 && tt.wantConf != 0 {
				assert.InDelta(t, tt.wantConf, got[0].Confidence, 0.001)
			}
			// Every case in this table is an ungated IDE, where branch membership
			// still attributes on its own. Such a match records no evidence —
			// not "checked and found absent" — so the field must stay unset.
			for _, m := range got {
				assert.Equal(t,
					attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_UNSPECIFIED,
					m.GetEvidence(), "ungated branch match must record no evidence")
			}
		})
	}
}

// TestMatchBranchGated covers the gated rule: a branch only scopes, and a
// candidate must carry PR-specific, session-produced evidence to be emitted.
//
// Every rejection case here is paired with a case that differs in exactly one
// fixture value and DOES match, so a rejection proves the gate decided rather
// than that the fixture never reached it.
func TestMatchBranchGated(t *testing.T) {
	now := time.Now().UTC().Truncate(24 * time.Hour)

	kiroSession := func(id string, um *usageMeta) *attributionv1.SessionEvidence {
		um.IdeType = "IDE_TYPE_KIRO"
		um.SessionId = id
		um.StartDate = ts(now)
		return um
	}
	ref := func(num int32, repo string, src attributionv1.SignalSource) *attributionv1.PRReference {
		return &attributionv1.PRReference{PrNumber: num, PrRepository: repo, Source: src}
	}
	reviewedPR := func(key, branch string) *attributionv1.PRCandidate {
		return testPRCandidateWithKind(key, now.AddDate(0, 0, 1), map[string]any{
			"repo": "org/gadgets", "head_branch": branch,
		}, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED)
	}

	tests := []struct {
		name         string
		session      *attributionv1.SessionEvidence
		prs          []*attributionv1.PRCandidate
		wantPRKeys   []string
		wantEvidence attributionv1.SessionToPRMatchEvidence
		wantFiles    []string // matched_files on the expected match; empty means assert empty
	}{
		{
			// The author arm: the session's edits land in the PR's diff at the
			// same bar FILE_OVERLAP applies, so the branch match is corroborated.
			name: "author arm matches when file overlap clears the bar",
			session: kiroSession("g-author-ok", &usageMeta{
				RepoName:    "org/gadgets",
				GitBranches: []string{"feat/gated"},
				FilesEdited: []string{"signals.go", "merge.go"},
			}),
			prs: []*attributionv1.PRCandidate{testPRCandidate("org/gadgets#PR-20#merged", now.AddDate(0, 0, 1), map[string]any{
				"repo": "org/gadgets", "head_branch": "feat/gated",
				"files_changed": []string{"signals.go", "unrelated.go"},
			})},
			wantPRKeys:   []string{"org/gadgets#PR-20#merged"},
			wantEvidence: attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_FILE_OVERLAP,
			wantFiles:    []string{"signals.go"},
		},
		{
			// Branch matches and nothing else does. This is the coverage this rule
			// deliberately removes.
			name: "author arm drops a branch match with no file overlap",
			session: kiroSession("g-author-none", &usageMeta{
				RepoName:    "org/gadgets",
				GitBranches: []string{"feat/gated"},
				FilesEdited: []string{"docs/notes.md"},
			}),
			prs: []*attributionv1.PRCandidate{testPRCandidate("org/gadgets#PR-20#merged", now.AddDate(0, 0, 1), map[string]any{
				"repo": "org/gadgets", "head_branch": "feat/gated",
				"files_changed": []string{"signals.go"},
			})},
		},
		{
			// Proves the bar is FILE_OVERLAP's own, not bare non-emptiness: the
			// overlap is a single ubiquitous basename, which scores 1.0 on
			// containment and is still refused by meetsMinIntersection. On a shared
			// feature branch this is exactly the coincidence that would leak.
			name: "author arm drops a lone common-basename overlap",
			session: kiroSession("g-author-readme", &usageMeta{
				RepoName:    "org/gadgets",
				GitBranches: []string{"feat/gated"},
				FilesEdited: []string{"README.md"},
			}),
			prs: []*attributionv1.PRCandidate{testPRCandidate("org/gadgets#PR-20#merged", now.AddDate(0, 0, 1), map[string]any{
				"repo": "org/gadgets", "head_branch": "feat/gated",
				"files_changed": []string{"README.md", "signals.go"},
			})},
		},
		{
			// The order-of-operations proof. The NEARER PR on the reused branch has
			// no overlap and the FARTHER one does. Evaluating evidence before the
			// nearest-merge dedup keeps the farther PR; deduping first would pick
			// PR-20, fail it, and emit nothing — so this case fails under the
			// inverted order rather than merely looking different.
			name: "evidence is evaluated before the nearest-merge dedup",
			session: kiroSession("g-order", &usageMeta{
				RepoName:    "org/gadgets",
				GitBranches: []string{"feat/shared"},
				FilesEdited: []string{"signals.go", "merge.go"},
			}),
			prs: []*attributionv1.PRCandidate{
				testPRCandidate("org/gadgets#PR-20#merged", now.AddDate(0, 0, 1), map[string]any{
					"repo": "org/gadgets", "head_branch": "feat/shared",
					"files_changed": []string{"totally/unrelated.go"},
				}),
				testPRCandidate("org/gadgets#PR-21#merged", now.AddDate(0, 0, 4), map[string]any{
					"repo": "org/gadgets", "head_branch": "feat/shared",
					"files_changed": []string{"signals.go", "other.go"},
				}),
			},
			wantPRKeys:   []string{"org/gadgets#PR-21#merged"},
			wantEvidence: attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_FILE_OVERLAP,
			wantFiles:    []string{"signals.go"},
		},
		{
			// The reviewer arm corroborates on a reference NAMING this PR that the
			// session itself produced. matched_files stays empty: a reviewer reads
			// rather than edits, so there is no file evidence to carry.
			name: "reviewer arm matches on a session-produced reference",
			session: kiroSession("g-rev-cmd", &usageMeta{
				RepoName:     "org/gadgets",
				GitBranches:  []string{"feat/reviewed"},
				PrReferences: []*attributionv1.PRReference{ref(30, "org/gadgets", attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND)},
			}),
			prs:          []*attributionv1.PRCandidate{reviewedPR("org/gadgets#PR-30#merged", "feat/reviewed")},
			wantPRKeys:   []string{"org/gadgets#PR-30#merged"},
			wantEvidence: attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_PR_REFERENCE,
		},
		{
			// Differs from the passing case above only in provenance, which is the
			// point: see isSessionProduced for why this source cannot attribute.
			name: "reviewer arm drops a local-git reference",
			session: kiroSession("g-rev-scrape", &usageMeta{
				RepoName:     "org/gadgets",
				GitBranches:  []string{"feat/reviewed"},
				PrReferences: []*attributionv1.PRReference{ref(30, "org/gadgets", attributionv1.SignalSource_SIGNAL_SOURCE_LOCAL_GIT_WINDOWED)},
			}),
			prs: []*attributionv1.PRCandidate{reviewedPR("org/gadgets#PR-30#merged", "feat/reviewed")},
		},
		{
			// Every stored reference is unclassified until re-extraction, and an
			// absent source cannot be told apart from an ambient scrape.
			name: "reviewer arm drops an unclassified reference",
			session: kiroSession("g-rev-unstamped", &usageMeta{
				RepoName:     "org/gadgets",
				GitBranches:  []string{"feat/reviewed"},
				PrReferences: []*attributionv1.PRReference{ref(30, "org/gadgets", attributionv1.SignalSource_SIGNAL_SOURCE_UNSPECIFIED)},
			}),
			prs: []*attributionv1.PRCandidate{reviewedPR("org/gadgets#PR-30#merged", "feat/reviewed")},
		},
		{
			// Producers do not agree on repo casing, and the reviewer arm has no
			// file evidence to fall back on — so a byte-exact compare here would
			// drop a reference naming this exact PR with the strongest source.
			name: "reviewer arm matches a reference whose repo differs only in case",
			session: kiroSession("g-rev-case", &usageMeta{
				RepoName:     "org/gadgets",
				GitBranches:  []string{"feat/reviewed"},
				PrReferences: []*attributionv1.PRReference{ref(30, "Org/Gadgets", attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND)},
			}),
			prs:          []*attributionv1.PRCandidate{reviewedPR("org/gadgets#PR-30#merged", "feat/reviewed")},
			wantPRKeys:   []string{"org/gadgets#PR-30#merged"},
			wantEvidence: attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_PR_REFERENCE,
		},
		{
			// A bare, owner-less repo must NOT corroborate an owner-qualified
			// candidate that merely shares the leaf name. Two connected repos named
			// "gadgets" under different owners, both with a PR 30, is the collision —
			// and dropping a match is the right failure here, since matching the
			// wrong repository would attribute someone else's PR.
			name: "reviewer arm drops an owner-less reference sharing only the repo leaf",
			session: kiroSession("g-rev-bare-repo", &usageMeta{
				RepoName:     "org/gadgets",
				GitBranches:  []string{"feat/reviewed"},
				PrReferences: []*attributionv1.PRReference{ref(30, "gadgets", attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND)},
			}),
			prs: []*attributionv1.PRCandidate{reviewedPR("org/gadgets#PR-30#merged", "feat/reviewed")},
		},
		{
			name: "reviewer arm drops when no reference names this PR",
			session: kiroSession("g-rev-other", &usageMeta{
				RepoName:     "org/gadgets",
				GitBranches:  []string{"feat/reviewed"},
				PrReferences: []*attributionv1.PRReference{ref(99, "org/gadgets", attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND)},
			}),
			prs: []*attributionv1.PRCandidate{reviewedPR("org/gadgets#PR-30#merged", "feat/reviewed")},
		},
		{
			name: "reviewer arm accepts prose naming a PR in the session's own repo",
			session: kiroSession("g-rev-prose-ok", &usageMeta{
				RepoName:     "org/gadgets",
				GitBranches:  []string{"feat/reviewed"},
				PrReferences: []*attributionv1.PRReference{ref(30, "org/gadgets", attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_PROSE)},
			}),
			prs:          []*attributionv1.PRCandidate{reviewedPR("org/gadgets#PR-30#merged", "feat/reviewed")},
			wantPRKeys:   []string{"org/gadgets#PR-30#merged"},
			wantEvidence: attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_PR_REFERENCE,
		},
		{
			// Prose routinely quotes a PR the session was only reading about, so the
			// reference's repo is compared against the SESSION's repo, not the
			// candidate's — which the selection above has already fixed.
			name: "reviewer arm drops prose naming a PR outside the session's repo",
			session: kiroSession("g-rev-prose-elsewhere", &usageMeta{
				RepoName:     "org/other",
				GitBranches:  []string{"feat/reviewed"},
				PrReferences: []*attributionv1.PRReference{ref(30, "org/gadgets", attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_PROSE)},
			}),
			prs: []*attributionv1.PRCandidate{reviewedPR("org/gadgets#PR-30#merged", "feat/reviewed")},
		},
		{
			// An unknown repo refuses rather than passes. Without this the prose gate would be vacuous for any producer
			// that omits repo_name.
			name: "reviewer arm refuses prose when the session repo is unknown",
			session: kiroSession("g-rev-prose-norepo", &usageMeta{
				GitBranches:  []string{"feat/reviewed"},
				PrReferences: []*attributionv1.PRReference{ref(30, "org/gadgets", attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_PROSE)},
			}),
			prs: []*attributionv1.PRCandidate{reviewedPR("org/gadgets#PR-30#merged", "feat/reviewed")},
		},
		{
			// A self-reviewed PR sits in both candidate sets under one record key.
			// The AUTHORED copy may clear the author arm even though the REVIEWED
			// copy carries no naming reference, and because unsupported candidates
			// never enter the dedup the outcome does not depend on slice order —
			// the REVIEWED copy is listed first here deliberately.
			name: "self-reviewed PR clears the gate on its authored copy",
			session: kiroSession("g-self-review", &usageMeta{
				RepoName:    "org/gadgets",
				GitBranches: []string{"feat/self"},
				FilesEdited: []string{"signals.go", "merge.go"},
			}),
			prs: []*attributionv1.PRCandidate{
				testPRCandidateWithKind("org/gadgets#PR-40#merged", now.AddDate(0, 0, 1), map[string]any{
					"repo": "org/gadgets", "head_branch": "feat/self",
					"files_changed": []string{"signals.go", "other.go"},
				}, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED),
				testPRCandidateWithKind("org/gadgets#PR-40#merged", now.AddDate(0, 0, 1), map[string]any{
					"repo": "org/gadgets", "head_branch": "feat/self",
					"files_changed": []string{"signals.go", "other.go"},
				}, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED),
			},
			wantPRKeys:   []string{"org/gadgets#PR-40#merged"},
			wantEvidence: attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_FILE_OVERLAP,
			wantFiles:    []string{"signals.go"},
		},
		{
			// A branch the session PUSHED (a session-produced source) corroborates on
			// its own, even below the file-overlap bar. This session edited one file
			// the PR does not touch, so FILE_OVERLAP scores zero — but it pushed the
			// exact head branch, which is the stronger evidence the branch source now
			// carries. Contrast "author arm drops a branch match with no file overlap"
			// above, whose branch arrives via GitBranches at UNSPECIFIED and so still
			// needs the file test.
			name: "author arm corroborates a session-pushed branch below the overlap bar",
			session: kiroSession("g-branch-prov", &usageMeta{
				RepoName:      "org/gadgets",
				BranchSignals: []*attributionv1.BranchSignal{{Name: "feat/pushed", Source: attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND}},
				FilesEdited:   []string{"docs/notes.md"},
			}),
			prs: []*attributionv1.PRCandidate{testPRCandidate("org/gadgets#PR-50#merged", now.AddDate(0, 0, 1), map[string]any{
				"repo": "org/gadgets", "head_branch": "feat/pushed",
				"files_changed": []string{"signals.go"},
			})},
			wantPRKeys:   []string{"org/gadgets#PR-50#merged"},
			wantEvidence: attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_BRANCH_PROVENANCE,
		},
		{
			// The reviewer arm gets the same lift: a session that pushed the PR's head
			// branch has proven its involvement without naming the PR, so it needs no
			// reference. matched_files stays empty — branch provenance, not
			// file evidence.
			name: "reviewer arm corroborates a session-pushed branch with no reference",
			session: kiroSession("g-rev-branch-prov", &usageMeta{
				RepoName:      "org/gadgets",
				BranchSignals: []*attributionv1.BranchSignal{{Name: "feat/reviewed", Source: attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND}},
			}),
			prs:          []*attributionv1.PRCandidate{reviewedPR("org/gadgets#PR-30#merged", "feat/reviewed")},
			wantPRKeys:   []string{"org/gadgets#PR-30#merged"},
			wantEvidence: attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_BRANCH_PROVENANCE,
		},
		{
			// A reflog-recovered branch (LOCAL_GIT_WINDOWED) is the WORKSPACE's account,
			// not the session's, so it does NOT self-corroborate and still needs file
			// overlap — which is absent here. This is what keeps the shared-branch
			// failure closed: a branch the workspace merely held cannot attribute.
			name: "reflog-sourced branch does not self-corroborate without overlap",
			session: kiroSession("g-branch-reflog", &usageMeta{
				RepoName:      "org/gadgets",
				BranchSignals: []*attributionv1.BranchSignal{{Name: "feat/pushed", Source: attributionv1.SignalSource_SIGNAL_SOURCE_LOCAL_GIT_WINDOWED}},
				FilesEdited:   []string{"docs/notes.md"},
			}),
			prs: []*attributionv1.PRCandidate{testPRCandidate("org/gadgets#PR-50#merged", now.AddDate(0, 0, 1), map[string]any{
				"repo": "org/gadgets", "head_branch": "feat/pushed",
				"files_changed": []string{"signals.go"},
			})},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchBranch(tt.session, tt.prs, testParams())
			if len(tt.wantPRKeys) == 0 {
				assert.Empty(t, got)
				return
			}
			require.Len(t, got, len(tt.wantPRKeys))
			gotKeys := make([]string, 0, len(got))
			for _, m := range got {
				gotKeys = append(gotKeys, m.GetPrKey())
			}
			assert.ElementsMatch(t, tt.wantPRKeys, gotKeys)
			assert.Equal(t, tt.wantEvidence, got[0].GetEvidence())
			assert.ElementsMatch(t, tt.wantFiles, got[0].GetMatchedFiles())
		})
	}
}

// TestMatchBranchGateIsIDEScoped pins the gate's reach. One fixture, one field
// changed: the same no-overlap branch match that a gated IDE drops must still be
// emitted for every other IDE. Without this pair, a passing rejection case above
// could mean the gate works or that the fixture never reached a candidate.
func TestMatchBranchGateIsIDEScoped(t *testing.T) {
	now := time.Now().UTC().Truncate(24 * time.Hour)

	session := func(ideType string) *attributionv1.SessionEvidence {
		return &attributionv1.SessionEvidence{
			SessionId:   "ide-scope",
			StartDate:   ts(now),
			IdeType:     ideType,
			RepoName:    "org/gadgets",
			GitBranches: []string{"feat/gated"},
			FilesEdited: []string{"docs/notes.md"}, // no overlap with the PR,
		}
	}
	prs := func() []*attributionv1.PRCandidate {
		return []*attributionv1.PRCandidate{testPRCandidate("org/gadgets#PR-20#merged", now.AddDate(0, 0, 1), map[string]any{
			"repo": "org/gadgets", "head_branch": "feat/gated",
			"files_changed": []string{"signals.go"},
		})}
	}

	assert.Empty(t, matchBranch(session("IDE_TYPE_KIRO"), prs(), testParams()),
		"gated IDE must drop an uncorroborated branch match")

	ungated := matchBranch(session("IDE_TYPE_CLAUDE_CODE"), prs(), testParams())
	require.Len(t, ungated, 1, "ungated IDE keeps its branch-only match")
	assert.Equal(t,
		attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_UNSPECIFIED,
		ungated[0].GetEvidence())
}

func TestMatchFileOverlap(t *testing.T) {
	now := time.Now().UTC().Truncate(24 * time.Hour)

	tests := []struct {
		name      string
		session   *attributionv1.SessionEvidence
		prs       []*attributionv1.PRCandidate
		wantNil   bool
		wantPRKey string
	}{
		{
			name: "matches best PR by file overlap",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s1",
				StartDate:   ts(now),
				FilesEdited: []string{"src/handler.go", "src/handler_test.go"},
			},
			prs: []*attributionv1.PRCandidate{
				testPRCandidate("repo#PR-10#opened", now, map[string]any{
					"files_changed":    []string{"src/handler.go", "src/handler_test.go", "README.md"},
					"files_line_stats": []map[string]any{{"path": "src/handler.go", "additions": 30, "deletions": 5}},
				}),
				testPRCandidate("repo#PR-20#opened", now, map[string]any{
					"files_changed": []string{"unrelated.go"},
				}),
			},
			wantNil:   false,
			wantPRKey: "repo#PR-10#opened",
		},
		{
			name: "nil when no files edited",
			session: &attributionv1.SessionEvidence{
				SessionId: "s2",
				StartDate: ts(now),
			},
			prs:     []*attributionv1.PRCandidate{testPRCandidate("repo#PR-10#opened", now, map[string]any{"files_changed": []string{"src/handler.go"}})},
			wantNil: true,
		},
		{
			// 1 of 4 session files in the PR → containment = 0.25, below the 0.3 threshold.
			name: "nil when overlap below threshold",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s3",
				StartDate:   ts(now),
				FilesEdited: []string{"a.go", "x.go", "y.go", "z.go"},
			},
			prs:     []*attributionv1.PRCandidate{testPRCandidate("repo#PR-10#opened", now, map[string]any{"files_changed": []string{"a.go", "b.go", "c.go", "d.go", "e.go"}})},
			wantNil: true,
		},
		{
			name: "nil when PR is outside time window",
			session: &attributionv1.SessionEvidence{
				SessionId:   "s4",
				StartDate:   ts(now),
				FilesEdited: []string{"src/handler.go"},
			},
			prs:     []*attributionv1.PRCandidate{testPRCandidate("repo#PR-10#opened", now.AddDate(0, 0, 7), map[string]any{"files_changed": []string{"src/handler.go"}})},
			wantNil: true,
		},
		{
			name: "Cursor session with repo-relative files matches via file overlap",
			session: &attributionv1.SessionEvidence{
				SessionId: "cursor-1",
				StartDate: ts(now),
				IdeType:   "IDE_TYPE_CURSOR",
				FilesEdited: []string{
					"billing/consumer/events_handler.go",
					"billing/workflow/plan_change_test.go",
					"billing/workflow/plan_change.go",
				},
			},
			prs: []*attributionv1.PRCandidate{
				testPRCandidate("org/repo#PR-55#merged", now.AddDate(0, 0, 1), map[string]any{
					"files_changed":    []string{"billing/consumer/events_handler.go", "billing/workflow/plan_change.go", "billing/workflow/plan_change_test.go", "README.md"},
					"files_line_stats": []map[string]any{{"path": "billing/consumer/events_handler.go", "additions": 40, "deletions": 10}},
				}),
			},
			wantNil:   false,
			wantPRKey: "org/repo#PR-55#merged",
		},
		{
			name: "Cursor session with Go module prefix files matches PR repo-relative paths",
			session: &attributionv1.SessionEvidence{
				SessionId: "cursor-2",
				StartDate: ts(now),
				IdeType:   "IDE_TYPE_CURSOR",
				FilesEdited: []string{
					"github.com/acme/widgets/services/billing/invoice/invoice_test.go",
					"github.com/acme/widgets/db/schema/reset-cursor.sql",
				},
			},
			prs: []*attributionv1.PRCandidate{
				testPRCandidate("acme/widgets#PR-1016#merged", now.AddDate(0, 0, 1), map[string]any{
					"files_changed":    []string{"services/billing/invoice/invoice_test.go", "db/schema/reset-cursor.sql"},
					"files_line_stats": []map[string]any{{"path": "services/billing/invoice/invoice_test.go", "additions": 13, "deletions": 4}},
				}),
			},
			wantNil:   false,
			wantPRKey: "acme/widgets#PR-1016#merged",
		},
		{
			name: "Cursor session with string-encoded connector metadata matches",
			session: &attributionv1.SessionEvidence{
				SessionId:   "cursor-encoded",
				StartDate:   ts(now),
				IdeType:     "IDE_TYPE_CURSOR",
				FilesEdited: []string{"billing/consumer/events_handler.go", "billing/workflow/plan_change.go"},
			},
			prs: func() []*attributionv1.PRCandidate {
				// A connector's map[string]string metadata, serialized via json.Marshal.
				connectorMeta, _ := json.Marshal(map[string]string{
					"pr_author_id":     "12345",
					"files_changed":    `["billing/consumer/events_handler.go","billing/workflow/plan_change.go","README.md"]`,
					"files_line_stats": `[{"path":"billing/consumer/events_handler.go","additions":40,"deletions":10}]`,
				})
				return buildPRCandidates([]rawRecord{{
					RecordKey: "org/repo#PR-60#merged",
					DataDate:  now.AddDate(0, 0, 1),
					Metadata:  connectorMeta,
				}}, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED)
			}(),
			wantNil:   false,
			wantPRKey: "org/repo#PR-60#merged",
		},
		{
			// A session in repo-a must not match a PR in repo-b even on a
			// strong multi-file overlap — the repo-equality guard blocks it.
			name: "nil when session repo differs from PR repo",
			session: &attributionv1.SessionEvidence{
				SessionId:   "f3-cross",
				StartDate:   ts(now),
				RepoName:    "org/repo-a",
				FilesEdited: []string{"src/handler.go", "src/util.go"},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("org/repo-b#PR-30#merged", now, map[string]any{
				"repo":          "org/repo-b",
				"files_changed": []string{"src/handler.go", "src/util.go"},
			})},
			wantNil: true,
		},
		{
			// Passthrough: older uploads without repo_name/project_name keep
			// matching — the guard is inert when the session repo is unknown.
			name: "matches when session repo unknown (guard inert)",
			session: &attributionv1.SessionEvidence{
				SessionId:   "f3-inert",
				StartDate:   ts(now),
				FilesEdited: []string{"src/handler.go", "src/util.go"},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("org/repo-b#PR-31#merged", now, map[string]any{
				"repo":             "org/repo-b",
				"files_changed":    []string{"src/handler.go", "src/util.go"},
				"files_line_stats": []map[string]any{{"path": "src/handler.go", "additions": 20, "deletions": 5}},
			})},
			wantNil:   false,
			wantPRKey: "org/repo-b#PR-31#merged",
		},
		{
			// Passthrough (other side): a PR whose metadata omits "repo"
			// (pr.GetPrRepository() == "") must still match a session that DOES carry
			// repo_name — the guard is inert when the PR repo is unknown, not a skip.
			// Without this the guard drops every record that has no repo.
			name: "matches when PR repo unknown (guard inert)",
			session: &attributionv1.SessionEvidence{
				SessionId:   "f3-pr-unknown",
				StartDate:   ts(now),
				RepoName:    "org/repo-a",
				FilesEdited: []string{"src/handler.go", "src/util.go"},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("repo#PR-33#merged", now, map[string]any{
				"files_changed":    []string{"src/handler.go", "src/util.go"},
				"files_line_stats": []map[string]any{{"path": "src/handler.go", "additions": 20, "deletions": 5}},
			})},
			wantNil:   false,
			wantPRKey: "repo#PR-33#merged",
		},
		{
			// Same repo still matches — the guard must not over-block.
			name: "matches when session repo equals PR repo",
			session: &attributionv1.SessionEvidence{
				SessionId:   "f3-same",
				StartDate:   ts(now),
				RepoName:    "org/repo-a",
				FilesEdited: []string{"src/handler.go", "src/util.go"},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("org/repo-a#PR-32#merged", now, map[string]any{
				"repo":             "org/repo-a",
				"files_changed":    []string{"src/handler.go", "src/util.go"},
				"files_line_stats": []map[string]any{{"path": "src/handler.go", "additions": 20, "deletions": 5}},
			})},
			wantNil:   false,
			wantPRKey: "org/repo-a#PR-32#merged",
		},
		{
			// GitLab nested subgroup: the PR repo carries the full path while the
			// session repo_name is the git-remote last-2 — the guard compares
			// last-2 on both sides, so they match.
			name: "matches GitLab subgroup PR via last-two-segment repo compare",
			session: &attributionv1.SessionEvidence{
				SessionId:   "f3-subgroup",
				StartDate:   ts(now),
				RepoName:    "sub/repo",
				FilesEdited: []string{"src/handler.go", "src/util.go"},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("group/sub/repo#PR-50#merged", now, map[string]any{
				"repo":             "group/sub/repo",
				"files_changed":    []string{"src/handler.go", "src/util.go"},
				"files_line_stats": []map[string]any{{"path": "src/handler.go", "additions": 20, "deletions": 5}},
			})},
			wantNil:   false,
			wantPRKey: "group/sub/repo#PR-50#merged",
		},
		{
			// A lone common-basename overlap (README.md) is not evidence
			// the session contributed to the PR.
			name: "nil when only overlap is a common basename",
			session: &attributionv1.SessionEvidence{
				SessionId:   "f4-common",
				StartDate:   ts(now),
				FilesEdited: []string{"README.md"},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("org/repo-c#PR-33#merged", now, map[string]any{
				"repo":          "org/repo-c",
				"files_changed": []string{"README.md", "src/foo.go"},
			})},
			wantNil: true,
		},
		{
			// Boundary: a single non-common source file still counts — the
			// min-intersection rule blocks only ubiquitous basenames.
			name: "matches on a single non-common file",
			session: &attributionv1.SessionEvidence{
				SessionId:   "f4-single",
				StartDate:   ts(now),
				FilesEdited: []string{"src/handler.go"},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("org/repo-d#PR-34#merged", now, map[string]any{
				"repo":             "org/repo-d",
				"files_changed":    []string{"src/handler.go", "src/other.go"},
				"files_line_stats": []map[string]any{{"path": "src/handler.go", "additions": 15, "deletions": 3}},
			})},
			wantNil:   false,
			wantPRKey: "org/repo-d#PR-34#merged",
		},
		{
			// Owner collision: two repos share the leaf "gadgets" under different
			// owners — the full-path guard must keep them distinct (basename-only
			// would have cross-matched).
			name: "nil when repos share a basename under different owners",
			session: &attributionv1.SessionEvidence{
				SessionId:   "f3-owner",
				StartDate:   ts(now),
				RepoName:    "orgA/gadgets",
				FilesEdited: []string{"src/handler.go", "src/util.go"},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("orgB/gadgets#PR-40#merged", now, map[string]any{
				"repo":          "orgB/gadgets",
				"files_changed": []string{"src/handler.go", "src/util.go"},
			})},
			wantNil: true,
		},
		{
			// Inert on project_name-only: no repo_name leaves the repo check to the
			// producer, and the folder basename
			// (gadgets-checkout) is unreliable — the guard must not drop this match.
			name: "matches project_name-only session (guard inert)",
			session: &attributionv1.SessionEvidence{
				SessionId:   "f3-proj",
				StartDate:   ts(now),
				ProjectName: "gadgets-checkout",
				FilesEdited: []string{"src/handler.go", "src/util.go"},
			},
			prs: []*attributionv1.PRCandidate{testPRCandidate("acme/gadgets#PR-41#merged", now, map[string]any{
				"repo":             "acme/gadgets",
				"files_changed":    []string{"src/handler.go", "src/util.go"},
				"files_line_stats": []map[string]any{{"path": "src/handler.go", "additions": 10, "deletions": 2}},
			})},
			wantNil:   false,
			wantPRKey: "acme/gadgets#PR-41#merged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchFileOverlap(tt.session, tt.prs, testParams())
			if tt.wantNil {
				assert.Empty(t, got)
				return
			}
			require.NotEmpty(t, got)
			// Verify the expected PR key appears in the results.
			var keys []string
			for _, m := range got {
				keys = append(keys, m.PrKey)
			}
			assert.Contains(t, keys, tt.wantPRKey)
			// Verify the first result has expected metadata populated.
			assert.Positive(t, got[0].GetLinesAdded(), "linesAdded should be populated")
			assert.NotNil(t, got[0].MatchedFiles, "matchedFiles should be populated")
		})
	}
}

// testPayload builds a json.RawMessage from a map for use as PR payload.
func testPayload(kvs map[string]any) json.RawMessage {
	b, _ := json.Marshal(kvs)
	return b
}

// TestMeetsMinIntersection pins the shared-file guard: an overlap must include at
// least one non-ubiquitous file. An overlap made up ENTIRELY of ubiquitous
// basenames (the measured false-positive class, e.g. index.ts + package.json) is
// refused however many files it has.
func TestMeetsMinIntersection(t *testing.T) {
	tests := []struct {
		name    string
		matched []string
		want    bool
	}{
		{name: "empty", matched: nil, want: false},
		{name: "single ubiquitous", matched: []string{"README.md"}, want: false},
		{name: "single specific", matched: []string{"src/handler.go"}, want: true},
		{name: "two ubiquitous only (FP class)", matched: []string{"src/index.ts", "package.json"}, want: false},
		{name: "ubiquitous plus specific", matched: []string{"package.json", "src/handler.go"}, want: true},
		{name: "two specific", matched: []string{"src/a.go", "src/b.go"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, meetsMinIntersection(tt.matched))
		})
	}
}

// TestMatchFileOverlap_OpenedOrMergedWindow verifies the candidate window admits a
// PR whose OPENED date is in window even when its MERGED date is not (raised near
// the session, merged later after slow review), and that both-out, opened-before-
// session, and payload-less-merged-out candidates stay excluded. The session date
// is midnight UTC, as a calendar-date start_date always is, and
// the opened payloads carry a real time-of-day, so the opened-arm's UTC-calendar-
// date normalisation is exercised rather than masked.
func TestMatchFileOverlap_OpenedOrMergedWindow(t *testing.T) {
	// windowTo = anchor + TimeWindowAfterDays(5) = 2026-08-06 00:00 UTC.
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	session := &attributionv1.SessionEvidence{
		SessionId:   "opened-window",
		StartDate:   ts(now),
		RepoName:    "org/repo",
		FilesEdited: []string{"src/handler.go", "src/util.go"},
	}
	meta := map[string]any{
		"repo":             "org/repo",
		"files_changed":    []string{"src/handler.go", "src/util.go"},
		"files_line_stats": []map[string]any{{"path": "src/handler.go", "additions": 20, "deletions": 5}},
	}
	mergedOut := now.AddDate(0, 0, 10) // beyond the 5-day window on the merged arm
	assertMatch := func(name string, pr *attributionv1.PRCandidate, want bool) {
		t.Run(name, func(t *testing.T) {
			got := matchFileOverlap(session, []*attributionv1.PRCandidate{pr}, testParams())
			if want {
				require.NotEmpty(t, got, "expected a file-overlap match")
				assert.Equal(t, pr.GetPrKey(), got[0].PrKey)
			} else {
				assert.Empty(t, got)
			}
		})
	}

	// Opened on the LAST window day, mid-afternoon (non-midnight) — must match: the
	// opened date normalises to its UTC calendar date (2026-08-06) == windowTo day.
	// A raw-timestamp compare would wrongly exclude it (15:04 > midnight windowTo).
	assertMatch("opened boundary-day non-midnight, merged out", testPRCandidateWithPayload(
		"org/repo#PR-70#merged", mergedOut, meta,
		testPayload(map[string]any{"createdAt": "2026-08-06T15:04:05Z"})), true)

	// Opened AND merged after the window → no match.
	assertMatch("both out", testPRCandidateWithPayload(
		"org/repo#PR-71#merged", mergedOut, meta,
		testPayload(map[string]any{"createdAt": "2026-08-12T09:00:00Z"})), false)

	// Opened BEFORE the session, merged out → no match: the opened arm requires
	// opened >= sessionDate, so a PR opened before the session is only reachable via the
	// merged arm (out of window here). Pins the opened-arm lower bound.
	assertMatch("opened before session, merged out", testPRCandidateWithPayload(
		"org/repo#PR-73#merged", mergedOut, meta,
		testPayload(map[string]any{"createdAt": "2026-07-22T09:00:00Z"})), false)

	// No payload → OpenedDate zero → merged-only; merged out of window → no match.
	assertMatch("payload-less, merged out", testPRCandidate(
		"org/repo#PR-72#merged", mergedOut, meta), false)
}

// TestPrsOpenedOrMergedOnOrBefore verifies the FILE_OVERLAP pre-filter keeps a PR
// whose opened date is on/before the cutoff even when its merged date is after,
// mirroring matchFileOverlap's opened-OR-merged window.
func TestPrsOpenedOrMergedOnOrBefore(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, 5)
	mergedIn := &attributionv1.PRCandidate{MergedAt: ts(now.AddDate(0, 0, 3))}
	mergedOutOpenedIn := &attributionv1.PRCandidate{MergedAt: ts(now.AddDate(0, 0, 10)), OpenedAt: ts(now.AddDate(0, 0, 2))}
	bothOut := &attributionv1.PRCandidate{MergedAt: ts(now.AddDate(0, 0, 10)), OpenedAt: ts(now.AddDate(0, 0, 10))}
	mergedOutNoOpened := &attributionv1.PRCandidate{MergedAt: ts(now.AddDate(0, 0, 10))}
	got := prsOpenedOrMergedOnOrBefore([]*attributionv1.PRCandidate{mergedIn, mergedOutOpenedIn, bothOut, mergedOutNoOpened}, cutoff)
	require.Len(t, got, 2, "keep merged-in-window and opened-in-window; drop both-out and merged-out-no-opened")
	assert.Same(t, mergedIn, got[0])
	assert.Same(t, mergedOutOpenedIn, got[1])
}

func TestIdeTypeToSource(t *testing.T) {
	tests := []struct {
		name    string
		ideType string
		want    string
	}{
		{name: "claude code", ideType: "Claude Code", want: "claude-code"},
		{name: "cursor", ideType: "Cursor", want: "cursor"},
		{name: "copilot", ideType: "Copilot", want: "copilot"},
		{name: "github copilot", ideType: "GitHub Copilot", want: "copilot"},
		{name: "windsurf", ideType: "Windsurf", want: "windsurf"},
		{name: "codeium", ideType: "Codeium", want: "codeium"},
		{name: "unknown", ideType: "unknown-ide", want: ""},
		{name: "empty", ideType: "", want: ""},
		// Proto enum string format, as producers send it.
		{name: "proto claude code", ideType: "IDE_TYPE_CLAUDE_CODE", want: "claude-code"},
		{name: "proto cursor", ideType: "IDE_TYPE_CURSOR", want: "cursor"},
		{name: "proto copilot", ideType: "IDE_TYPE_COPILOT", want: "copilot"},
		{name: "proto windsurf", ideType: "IDE_TYPE_WINDSURF", want: "windsurf"},
		{name: "proto codeium", ideType: "IDE_TYPE_CODEIUM", want: "codeium"},
		{name: "kiro", ideType: "Kiro", want: "kiro"},
		{name: "proto kiro", ideType: "IDE_TYPE_KIRO", want: "kiro"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ideTypeToSource(tt.ideType))
		})
	}
}

// TestBuildPRContexts_StringEncodedMetadata verifies that buildPRContexts
// correctly parses metadata stored by the connector as map[string]string,
// where array fields (files_changed, files_line_stats) are JSON strings
// rather than native JSON arrays.
func TestBuildPRContexts_StringEncodedMetadata(t *testing.T) {
	// A connector emits map[string]string and the caller marshals it via
	// json.Marshal(map[string]string{...}).
	connectorMeta := map[string]string{
		"pr_author_id":     "12345",
		"head_branch":      "feat/new-thing",
		"event_type":       "merged",
		"files_changed":    `["src/handler.go","src/handler_test.go","README.md"]`,
		"files_line_stats": `[{"path":"src/handler.go","additions":100,"deletions":20},{"path":"README.md","additions":5,"deletions":0}]`,
	}
	metaJSON, err := json.Marshal(connectorMeta)
	require.NoError(t, err)

	prs := buildPRCandidates([]rawRecord{{
		RecordKey: "repo#PR-42#merged",
		DataDate:  time.Now().UTC(),
		Metadata:  metaJSON,
	}}, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED)

	require.Len(t, prs, 1)
	pr := prs[0]
	assert.Equal(t, "feat/new-thing", pr.GetHeadBranch())
	assert.Equal(t, []string{"src/handler.go", "src/handler_test.go", "README.md"}, pr.GetFilesChanged())
	assert.Equal(t, int64(105), pr.GetLinesAdded())
	assert.Equal(t, int64(20), pr.GetLinesRemoved())
}

// TestBuildPRContexts_NativeArrayMetadata verifies the native-array format
// (map[string]any) parses as well as the string-encoded one.
func TestBuildPRContexts_NativeArrayMetadata(t *testing.T) {
	meta := testMetadata(map[string]any{
		"head_branch":      "feat/old-way",
		"files_changed":    []string{"a.go", "b.go"},
		"files_line_stats": []map[string]any{{"path": "a.go", "additions": 50, "deletions": 10}},
	})

	prs := buildPRCandidates([]rawRecord{{
		RecordKey: "repo#PR-1#merged",
		DataDate:  time.Now().UTC(),
		Metadata:  meta,
	}}, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED)

	require.Len(t, prs, 1)
	pr := prs[0]
	assert.Equal(t, "feat/old-way", pr.GetHeadBranch())
	assert.Equal(t, []string{"a.go", "b.go"}, pr.GetFilesChanged())
	assert.Equal(t, int64(50), pr.GetLinesAdded())
	assert.Equal(t, int64(10), pr.GetLinesRemoved())
}

// TestBuildPRContexts_RepoPathComposition verifies that Repo is always a
// canonical "owner/repo" path. A connector configured with bare repository
// names may store metadata as {"repo":"gadgets","owner":"acme"}; without
// composition, downstream URL builders produce broken links.
func TestBuildPRContexts_RepoPathComposition(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]any
		want string
	}{
		{
			name: "composes owner+repo when repo is bare",
			meta: map[string]any{"repo": "widgets", "owner": "acme"},
			want: "acme/widgets",
		},
		{
			name: "passes through fully-qualified repo unchanged",
			meta: map[string]any{"repo": "acme/widgets", "owner": "acme"},
			want: "acme/widgets",
		},
		{
			name: "leaves bare repo as-is when owner missing",
			meta: map[string]any{"repo": "gadgets"},
			want: "gadgets",
		},
		{
			name: "empty repo stays empty",
			meta: map[string]any{"owner": "acme"},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prs := buildPRCandidates([]rawRecord{{
				RecordKey: "repo#PR-1#merged",
				DataDate:  time.Now().UTC(),
				Metadata:  testMetadata(tt.meta),
			}}, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED)
			require.Len(t, prs, 1)
			assert.Equal(t, tt.want, prs[0].GetPrRepository())
		})
	}
}

// TestTimeDecay_NearerEndOfSession pins the interval semantics: a session is a
// span, so a PR merged close to EITHER end is closely related. Measuring from one
// end alone penalises the other, and for FILE_OVERLAP — the only signal with a
// confidence floor — that is the difference between a match and no match.
// TestMatchBranch_DedupKeepsTheHighestScoringCandidate pins the dedup metric to
// the scorer's. matchBranch keeps ONE PR per (repo, branch) and timeDecay then
// rates it, so if the two rank by different notions of "nearest" the dedup can
// discard the candidate that would have scored highest and store a strictly worse
// match. Both now measure to the nearer end of the session.
func TestMatchBranch_DedupKeepsTheHighestScoringCandidate(t *testing.T) {
	day := func(n int) time.Time {
		return time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)
	}
	start := day(0)
	session := &attributionv1.SessionEvidence{
		SessionId:              "s-reused-branch",
		StartDate:              ts(start),
		StartedAt:              tsPtr(&start),
		GitBranches:            []string{"feat/reused"},
		SessionDurationSeconds: 20 * 24 * 3600, // anchor lands on day 20,
	}
	// The same branch name reused across two PRs. PR-A merged next to the session
	// start, PR-B mid-window — nearer the anchor but further from either end.
	prs := []*attributionv1.PRCandidate{
		testPRCandidate("org/repo#PR-A#merged", day(1), map[string]any{
			"repo": "org/repo", "head_branch": "feat/reused",
		}),
		testPRCandidate("org/repo#PR-B#merged", day(10), map[string]any{
			"repo": "org/repo", "head_branch": "feat/reused",
		}),
	}

	got := matchBranch(session, prs, testParams())
	require.Len(t, got, 1, "a reused branch must collapse to one match")
	assert.Equal(t, "org/repo#PR-A#merged", got[0].PrKey,
		"the surviving candidate must be the one that scores highest, not the one nearest the anchor")
	assert.InDelta(t, confidenceBranchMatch*0.95, got[0].Confidence, 0.001)
}

func TestTimeDecay_NearerEndOfSession(t *testing.T) {
	start := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	anchor := start.AddDate(0, 0, 17) // a 17-day session, last active Jul 27

	tests := []struct {
		name   string
		prDate time.Time
		want   float64
	}{
		{
			// Near the START. Anchor-only would measure 15 days and floor at 0.75.
			name:   "PR merged early in a long session stays close to its start",
			prDate: start.AddDate(0, 0, 2),
			want:   0.90,
		},
		{
			// Near the ANCHOR. Start-only would measure 18 days and floor at 0.75.
			name:   "PR merged just after the work stopped stays close to its anchor",
			prDate: anchor.AddDate(0, 0, 1),
			want:   0.95,
		},
		{
			// Mid-session: far from both ends, so the floor is correct here.
			name:   "PR merged mid-session is far from either end",
			prDate: start.AddDate(0, 0, 9),
			want:   0.75,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := timeDecay(start, anchor, tt.prDate)
			assert.InDelta(t, tt.want, got, 0.001)
			assert.GreaterOrEqual(t, got, timeDecay(start, start, tt.prDate),
				"nearer-end decay must never score below a start-anchored one")
		})
	}
}

func TestTimeDecay(t *testing.T) {
	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		days int
		want float64
	}{
		{name: "same day", days: 0, want: 1.0},
		{name: "1 day", days: 1, want: 0.95},
		{name: "2 days", days: 2, want: 0.90},
		{name: "3 days", days: 3, want: 0.85},
		{name: "4 days", days: 4, want: 0.80},
		{name: "5 days", days: 5, want: 0.75},
		{name: "negative (session after PR)", days: -1, want: 0.95},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A same-day session: start and anchor coincide, so the min is trivial.
			got := timeDecay(base, base, base.AddDate(0, 0, tt.days))
			assert.InDelta(t, tt.want, got, 0.001)
		})
	}
}

// TestStrongSignalWindowBoundsBothStrongSignals pins the reach of
// StrongSignalWindowAfterDays on BOTH strong signals at once.
//
// The two are tested together deliberately: the window is enforced separately in
// each matcher, so a change that widens one and forgets the other would still
// pass a single-signal test while leaving a late-merging PR unreachable by the
// signal it happened to need.
//
// The ceiling is inclusive and it is real in both directions — a case at the
// bound matches and a case past it does not, so a future change cannot satisfy
// this test by removing the bound.
func TestStrongSignalWindowBoundsBothStrongSignals(t *testing.T) {
	t.Parallel()

	const (
		repo   = "org/gadgets"
		branch = "feat/late-merge"
		prKey  = "org/gadgets#PR-42#merged"
	)
	now := time.Now().UTC().Truncate(24 * time.Hour)

	// Zero duration, so the anchor lands on the start date and the gap under test
	// is purely the PR's own distance rather than an anchor extension.
	session := func() *attributionv1.SessionEvidence {
		return &attributionv1.SessionEvidence{
			SessionId:    "s-late-merge",
			StartDate:    ts(now),
			GitBranches:  []string{branch},
			PrReferences: []*attributionv1.PRReference{{PrNumber: 42, PrRepository: repo}},
		}
	}
	prMergedAfter := func(days int) []*attributionv1.PRCandidate {
		return []*attributionv1.PRCandidate{testPRCandidate(prKey, now.AddDate(0, 0, days), map[string]any{
			"repo": repo, "head_branch": branch,
		})}
	}

	tests := []struct {
		name       string
		windowDays int
		mergeGap   int
		wantMatch  bool
	}{
		// A merge 20 days after the session is rejected at a 14-day window and
		// admitted at 30.
		{name: "20d merge rejected at a 14d window", windowDays: 14, mergeGap: 20, wantMatch: false},
		{name: "20d merge admitted at a 30d window", windowDays: 30, mergeGap: 20, wantMatch: true},
		{name: "merge exactly on the bound is admitted", windowDays: 30, mergeGap: 30, wantMatch: true},
		{name: "merge past the bound is still rejected", windowDays: 30, mergeGap: 40, wantMatch: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			params := testParamsWithStrongWindow(tt.windowDays)
			prs := prMergedAfter(tt.mergeGap)

			branchMatches := matchBranch(session(), prs, params)
			refMatches := matchDirectPRRef(session(), prs, params)

			if !tt.wantMatch {
				assert.Empty(t, branchMatches, "BRANCH_MATCH should not reach past the window")
				assert.Empty(t, refMatches, "DIRECT_PR_REF should not reach past the window")
				return
			}
			require.Len(t, branchMatches, 1, "BRANCH_MATCH should reach within the window")
			assert.Equal(t, prKey, branchMatches[0].GetPrKey())
			require.Len(t, refMatches, 1, "DIRECT_PR_REF should reach within the window")
			assert.Equal(t, prKey, refMatches[0].GetPrKey())
		})
	}
}
