// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// supportStr renders the support map with "src->dst" keys.
func supportStr(f DetectedFlow) map[string]int {
	out := make(map[string]int, len(f.Support))
	for h, n := range f.Support {
		out[fmt.Sprint(h)] = n
	}
	return out
}

// hopStr renders the detected hops as "src->dst" strings for order-sensitive
// assertions.
func hopStr(f DetectedFlow) []string {
	out := make([]string, 0, len(f.Hops))
	for _, h := range f.Hops {
		out = append(out, h.String())
	}
	return out
}

func TestDetectFlow(t *testing.T) {
	pair := func(head, base string, n int) BranchPair {
		return BranchPair{Head: head, Base: base, Count: n}
	}

	tests := []struct {
		name         string
		pairs        []BranchPair
		wantHops     []string
		wantEvidence map[string]int
	}{
		{
			name:         "linear promotion chain is ordered source-to-target",
			pairs:        []BranchPair{pair("develop", "main", 4), pair("feature", "develop", 6)},
			wantHops:     []string{"feature->develop", "develop->main"},
			wantEvidence: map[string]int{"feature->develop": 6, "develop->main": 4},
		},
		{
			name:         "back-merge is never suggested (dominant direction wins)",
			pairs:        []BranchPair{pair("feature", "develop", 1), pair("develop", "main", 7), pair("main", "develop", 3)},
			wantHops:     []string{"develop->main"},
			wantEvidence: map[string]int{"develop->main": 7},
		},
		{
			name:         "an equal split is ambiguous and dropped",
			pairs:        []BranchPair{pair("develop", "main", 5), pair("main", "develop", 5)},
			wantHops:     []string{},
			wantEvidence: map[string]int{},
		},
		{
			name:         "below-threshold pairs are dropped",
			pairs:        []BranchPair{pair("feature", "develop", 1)},
			wantHops:     []string{},
			wantEvidence: map[string]int{},
		},
		{
			name:         "empty input yields no flow",
			pairs:        nil,
			wantHops:     []string{},
			wantEvidence: map[string]int{},
		},
		{
			name: "a cycle across three branches is broken at its weakest edge",
			pairs: []BranchPair{
				pair("a", "b", 9), pair("b", "c", 8), pair("c", "a", 2),
			},
			// c->a (weakest, 2) is dropped, leaving an acyclic a->b->c.
			wantHops:     []string{"a->b", "b->c"},
			wantEvidence: map[string]int{"a->b": 9, "b->c": 8},
		},
		{
			name: "disconnected leaf flows are dropped — neither source is a lane",
			pairs: []BranchPair{
				pair("feature", "main", 6), pair("staging", "prod", 3),
			},
			// The integration-lane gate drops both: nothing merges into feature or
			// staging, so each is a leaf, not a promotion lane. Two leaves straight to
			// a trunk are noise.
			wantHops:     []string{},
			wantEvidence: map[string]int{},
		},
		{
			name: "a fan-out from a non-lane source is dropped",
			pairs: []BranchPair{
				pair("develop", "main", 5), pair("develop", "release", 3),
			},
			// Nothing merges into develop here, so it is not a lane and both of its
			// terminal hops are dropped. Lane shape is proven by data, not by name.
			wantHops:     []string{},
			wantEvidence: map[string]int{},
		},
		{
			name: "a used-once integration lane into the terminal is kept",
			pairs: []BranchPair{
				// deploy receives one merge (below the ≥2 hop threshold, so x->deploy
				// is not itself a suggested hop) — but that single merge-in proves
				// deploy is a lane, so the deploy->main terminal hop survives the gate.
				pair("x", "deploy", 1), pair("deploy", "main", 3),
			},
			wantHops:     []string{"deploy->main"},
			wantEvidence: map[string]int{"deploy->main": 3},
		},
		{
			name: "a feature straight to the terminal is dropped (not a lane)",
			pairs: []BranchPair{
				pair("feature", "main", 3),
			},
			// Nothing merges into feature — the direct-to-trunk over-fire the gate kills.
			wantHops:     []string{},
			wantEvidence: map[string]int{},
		},
		{
			name: "a dominant noisy leaf does not suppress a quieter genuine chain",
			pairs: []BranchPair{
				// feature->prod is noise (feature is no lane) with the HIGHEST support;
				// x->develop->main is a genuine, quieter promotion chain into a different
				// terminal. Gating before sink selection drops feature->prod and lets the
				// reducer keep the real chain, instead of picking prod and returning empty.
				pair("feature", "prod", 10), pair("x", "develop", 2), pair("develop", "main", 3),
			},
			wantHops:     []string{"x->develop", "develop->main"},
			wantEvidence: map[string]int{"x->develop": 2, "develop->main": 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectFlow(tt.pairs, nil)
			require.Equal(t, tt.wantHops, hopStr(got), "hops (order matters)")
			if len(tt.wantEvidence) == 0 {
				require.Empty(t, got.Support)
				return
			}
			require.Equal(t, tt.wantEvidence, supportStr(got))
		})
	}
}

// TestDetectFlow_Priors proves each detection prior changes the suggestion
// versus the nil-facts baseline: recency and the declared flow each break an
// otherwise-dropped equal split, and the merged set biases the terminal choice.
// Every case pairs the same input run through nil facts (the baseline) so the delta
// is attributable to the prior alone.
func TestDetectFlow_Priors(t *testing.T) {
	pair := func(head, base string, n int) BranchPair {
		return BranchPair{Head: head, Base: base, Count: n}
	}
	early := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		pairs         []BranchPair
		facts         *FlowFacts
		wantBaseline  []string // nil-facts output (the merge-history-only suggestion)
		wantWithFacts []string // output once the prior applies
	}{
		{
			name:  "recency breaks an equal-count tie toward the more recently updated branch",
			pairs: []BranchPair{pair("feature", "develop", 1), pair("develop", "main", 5), pair("main", "develop", 5)},
			// main's head is newer than develop's → promotion runs into main.
			facts:         &FlowFacts{Recency: map[string]time.Time{"develop": early, "main": late}},
			wantBaseline:  []string{}, // equal split is ambiguous and dropped without a prior
			wantWithFacts: []string{"develop->main"},
		},
		{
			name:          "declared flow seeds an otherwise-ambiguous repo",
			pairs:         []BranchPair{pair("feature", "develop", 1), pair("develop", "main", 5), pair("main", "develop", 5)},
			facts:         &FlowFacts{Declared: &DeclaredFlow{Development: "develop", Production: "main"}},
			wantBaseline:  []string{},
			wantWithFacts: []string{"develop->main"},
		},
		{
			name: "a merged terminal is not chosen as sink",
			// Two disconnected single-hop flows → two terminals (main, prod). Without a
			// prior the higher-support terminal (main, 6) wins; marking main merged makes
			// the non-merged terminal (prod) win despite lower support. The used-once
			// merges into feature and staging make both a proven lane, so the
			// integration-lane gate keeps whichever terminal the prior selects (without
			// them the gate would drop both, hiding the prior's effect).
			pairs: []BranchPair{
				pair("feature", "main", 6), pair("staging", "prod", 3),
				pair("x", "feature", 1), pair("y", "staging", 1),
			},
			facts:         &FlowFacts{Merged: map[string]bool{"main": true}},
			wantBaseline:  []string{"feature->main"},
			wantWithFacts: []string{"staging->prod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseline := hopStr(DetectFlow(tt.pairs, nil))
			require.Equal(t, tt.wantBaseline, baseline, "nil-facts baseline (merge history only)")

			withFacts := hopStr(DetectFlow(tt.pairs, tt.facts))
			require.Equal(t, tt.wantWithFacts, withFacts, "output with the prior applied")
		})
	}
}

// TestDetectFlow_NilFactsMatchesEmpty is the regression guard for the central
// invariant: priors sharpen, they never gate. A nil *FlowFacts and a
// non-nil facts whose every dimension is empty must both reproduce the
// merge-history-only suggestion byte-for-byte — so a caller with no priors
// sees exactly the merge-history-only output.
func TestDetectFlow_NilFactsMatchesEmpty(t *testing.T) {
	pairs := []BranchPair{
		{Head: "feature", Base: "develop", Count: 6},
		{Head: "develop", Base: "main", Count: 4},
		// An equal split that the baseline drops as ambiguous — empty facts must also drop it.
		{Head: "hotfix", Base: "release", Count: 3},
		{Head: "release", Base: "hotfix", Count: 3},
	}
	nilOut := DetectFlow(pairs, nil)
	emptyOut := DetectFlow(pairs, &FlowFacts{Recency: map[string]time.Time{}, Merged: map[string]bool{}})

	require.Equal(t, hopStr(nilOut), hopStr(emptyOut), "empty facts must match nil facts")
	require.Equal(t, nilOut.Support, emptyOut.Support, "evidence counts must match too")
	// And the concrete merge-history-only shape, so this pins the actual output, not just equality.
	require.Equal(t, []string{"feature->develop", "develop->main"}, hopStr(nilOut))
}

// TestDetectFlow_Deterministic guards against map-iteration order leaking
// into the cycle-break and single-sink choice: the same input must yield the
// identical suggested path on every run.
func TestDetectFlow_Deterministic(t *testing.T) {
	pairs := []BranchPair{
		{Head: "a", Base: "x", Count: 5},
		{Head: "b", Base: "x", Count: 5},
		{Head: "c", Base: "y", Count: 5},
		{Head: "x", Base: "y", Count: 4},
	}
	first := hopStr(DetectFlow(pairs, nil))
	require.NotEmpty(t, first, "the input has a flow to suggest")
	for i := 0; i < 20; i++ {
		require.Equal(t, first, hopStr(DetectFlow(pairs, nil)), "detector output must be identical across runs")
	}
}

// TestDetectFlow_NeverContainsCycle is a property guard: for any dominant
// edge set the detector emits, the hops must form an acyclic chain (no branch is
// both upstream and downstream of another in the suggested path).
func TestDetectFlow_NeverContainsCycle(t *testing.T) {
	pairs := []BranchPair{
		{Head: "a", Base: "b", Count: 5},
		{Head: "b", Base: "c", Count: 5},
		{Head: "c", Base: "a", Count: 5},
	}
	got := DetectFlow(pairs, nil)
	require.NotEmpty(t, got.Hops, "breaking the cycle leaves a flow")

	seenTargets := map[string]int{}
	for i, h := range got.Hops {
		seenTargets[h.Target] = i
	}
	for i, h := range got.Hops {
		if prev, ok := seenTargets[h.Source]; ok {
			require.Less(t, prev, i, "hop %d sources a branch that was already a target — a cycle", i)
		}
	}
}

// TestDetectFlow_BreaksOnlyTheCycle: a residual cycle is broken at its weakest
// edge on the cycle; a hop leaving the cycle is never the one dropped, however
// weak.
func TestDetectFlow_BreaksOnlyTheCycle(t *testing.T) {
	pair := func(head, base string, n int) BranchPair { return BranchPair{Head: head, Base: base, Count: n} }
	tests := []struct {
		name  string
		pairs []BranchPair
		want  []string
	}{
		{
			name:  "a weak hop leaving the cycle survives",
			pairs: []BranchPair{pair("a", "b", 9), pair("b", "c", 9), pair("c", "a", 3), pair("c", "d", 2)},
			want:  []string{"a->b", "b->c", "c->d"},
		},
		{
			name:  "a weak hop entering the cycle survives",
			pairs: []BranchPair{pair("x", "a", 2), pair("a", "b", 9), pair("b", "c", 9), pair("c", "a", 3), pair("c", "main", 5)},
			want:  []string{"x->a", "a->b", "b->c", "c->main"},
		},
		{
			name:  "a lone cycle loses its weakest edge",
			pairs: []BranchPair{pair("a", "b", 9), pair("b", "c", 8), pair("c", "a", 2)},
			want:  []string{"a->b", "b->c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, hopStr(DetectFlow(tt.pairs, nil)))
		})
	}
}

// TestDetectFlow_SupportPerHop: each hop keeps its own support, even when branch
// names joined with "->" would read the same.
func TestDetectFlow_SupportPerHop(t *testing.T) {
	got := DetectFlow([]BranchPair{
		{Head: "z", Base: "p->q", Count: 1},
		{Head: "p->q", Base: "r", Count: 3},
		{Head: "p", Base: "q->r", Count: 5},
		{Head: "q->r", Base: "r", Count: 4},
	}, nil)
	require.Len(t, got.Hops, 3)
	require.Len(t, got.Support, 3, "one support entry per hop")
	total := 0
	for _, n := range got.Support {
		total += n
	}
	require.Equal(t, 12, total, "no hop's count is lost")
}

// TestDetectFlow_OutputAlwaysValidates is the property the detector promises: a
// suggestion is always a flow a person can confirm as it stands.
func TestDetectFlow_OutputAlwaysValidates(t *testing.T) {
	branches := []string{"a", "b", "c", "d", "e", "f"}
	// Blank names are in the mix: the detector must never suggest a hop ValidateFlow
	// would reject.
	blanks := []string{" ", "\t", "\u00a0"}
	nonEmpty := 0
	for seed := int64(1); seed <= 5000; seed++ {
		r := rand.New(rand.NewSource(seed))
		var ps []BranchPair
		for i := 0; i < 2+r.Intn(8); i++ {
			h, b := branches[r.Intn(len(branches))], branches[r.Intn(len(branches))]
			if r.Intn(10) == 0 {
				h = blanks[r.Intn(len(blanks))]
			}
			if r.Intn(10) == 0 {
				b = blanks[r.Intn(len(blanks))]
			}
			if h != b {
				ps = append(ps, BranchPair{Head: h, Base: b, Count: 1 + r.Intn(6)})
			}
		}
		got := DetectFlow(ps, nil)
		require.NoError(t, ValidateFlow(got.Hops), "seed %d: %v", seed, ps)
		if len(got.Hops) > 0 {
			nonEmpty++
		}
	}
	// The property is only worth something if the detector suggests a flow on a
	// good share of these inputs: 2,492 of 5,000 seeds today.
	require.GreaterOrEqual(t, nonEmpty, 2000, "suggestions on %d of 5000 seeds", nonEmpty)
}

// TestDetectFlow_ASyncFromTheTargetIsNoInboundMerge: a merge of the trunk into a
// feature branch does not make that branch a promotion source, so its merges into
// the trunk are not suggested; a merge from any other branch does.
func TestDetectFlow_ASyncFromTheTargetIsNoInboundMerge(t *testing.T) {
	synced := DetectFlow([]BranchPair{
		{Head: "main", Base: "feature/x", Count: 1},
		{Head: "feature/x", Base: "main", Count: 2},
	}, nil)
	require.Empty(t, hopStr(synced))
	for _, notAMerge := range []BranchPair{
		{Head: "feature/x", Base: "feature/x", Count: 1},
		{Head: "", Base: "feature/x", Count: 1},
		{Head: "topic", Base: "feature/x", Count: 0},
		{Head: "topic", Base: "feature/x", Count: -1},
	} {
		t.Run(fmt.Sprintf("%+v", notAMerge), func(t *testing.T) {
			got := DetectFlow([]BranchPair{notAMerge, {Head: "feature/x", Base: "main", Count: 2}}, nil)
			require.Empty(t, hopStr(got), "no merge from another branch")
		})
	}
	fed := DetectFlow([]BranchPair{
		{Head: "topic", Base: "feature/x", Count: 1},
		{Head: "feature/x", Base: "main", Count: 2},
	}, nil)
	require.Equal(t, []string{"feature/x->main"}, hopStr(fed))
}

// TestDetectFlow_AnInboundMergeCountsOnlyBeforeAnOutboundOne: a merge into a
// branch proves it a promotion source only if it landed no later than the
// branch's last merge into the terminal; a merge after that was promoted by none
// of them. Pairs with no merge times keep the rule without the order.
func TestDetectFlow_AnInboundMergeCountsOnlyBeforeAnOutboundOne(t *testing.T) {
	hour := func(h float64) time.Time { return testBase.Add(time.Duration(h * float64(time.Hour))) }
	pr := func(n int, head, base string, h float64) PR {
		return PR{Key: fmt.Sprint("PR-", n), Repository: "r", Number: n, HeadBranch: head, BaseBranch: base, MergedAt: hour(h)}
	}
	tests := []struct {
		name  string
		pairs []BranchPair
		want  []string
	}{
		{
			name: "a feature branch merged twice, then stacked onto, is no lane",
			pairs: MergePairs([]PR{
				pr(1, "feat/x", "main", 1), pr(2, "feat/x", "main", 2),
				pr(3, "feat/y", "feat/x", 3), // stacked onto feat/x after both merges
			}),
			want: []string{},
		},
		{
			name: "a merge into it before its last merge out proves a lane",
			pairs: MergePairs([]PR{
				pr(1, "feat/x", "main", 1), pr(3, "feat/y", "feat/x", 1.5),
				pr(2, "feat/x", "main", 2),
			}),
			want: []string{"feat/x->main"},
		},
		{
			name: "a merge in at the instant of the last merge out counts",
			pairs: MergePairs([]PR{
				pr(1, "feat/x", "main", 1), pr(2, "feat/x", "main", 2),
				pr(3, "feat/y", "feat/x", 2),
			}),
			want: []string{"feat/x->main"},
		},
		{
			name: "a merge out with no merge time keeps the rule without the order",
			pairs: []BranchPair{
				{Head: "feat/x", Base: "main", Count: 2},
				{Head: "feat/y", Base: "feat/x", Count: 1, FirstMergedAt: hour(3), LastMergedAt: hour(3)},
			},
			want: []string{"feat/x->main"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectFlow(tt.pairs, nil)
			require.Equal(t, tt.want, hopStr(got))
			require.NoError(t, ValidateFlow(got.Hops))
		})
	}
}
