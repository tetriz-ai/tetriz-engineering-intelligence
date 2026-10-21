// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func hop(s, d string) Hop { return Hop{Source: s, Target: d} }

func term(bs ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(bs))
	for _, b := range bs {
		m[b] = struct{}{}
	}
	return m
}

func hopStrs(hops []Hop) []string {
	out := make([]string, 0, len(hops))
	for _, h := range hops {
		out = append(out, h.String())
	}
	return out
}

// TestForwardFlow is the direction-safety guard: raw carry-edge pairs must reduce to
// a FORWARD flow toward the trunk, dropping the back-merge/sync directions the
// deriver also stores. Without this the walk's flow-membership check is tautological
// and sync edges over-credit attribution.
func TestForwardFlow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		pairs     []Hop
		terminals map[string]struct{}
		want      []string // nil ⇒ empty flow
	}{
		{
			name:      "forward chain toward the trunk is kept",
			pairs:     []Hop{hop("x", "develop"), hop("develop", "master")},
			terminals: term("master"),
			want:      []string{"x->develop", "develop->master"},
		},
		{
			name:      "a back-merge sourced from the trunk is dropped",
			pairs:     []Hop{hop("feature", "master"), hop("master", "feature")},
			terminals: term("master"),
			// master->feature: source is the trunk → a promotion never flows out of it.
			want: []string{"feature->master"},
		},
		{
			// Multi-trunk: a repo tracks BOTH staging and master, promoting develop->staging->master.
			// staging is a terminal AND the source of staging->master; that inter-trunk hop must be
			// KEPT (both endpoints are terminals) so the chain isn't broken — only a hop from
			// a terminal to a NON-terminal (a back-merge) is dropped.
			name:      "an inter-trunk hop between two tracked branches is kept",
			pairs:     []Hop{hop("develop", "staging"), hop("staging", "master")},
			terminals: term("staging", "master"),
			want:      []string{"develop->staging", "staging->master"},
		},
		{
			// Bidirectional inter-trunk edges: staging->master AND master->staging both stored. The
			// pair is ambiguous (no deterministic forward direction), so BOTH are dropped
			// (fail closed) — never admit the reverse. develop->staging has no reverse and
			// survives, still reaching the staging terminal. Safe under-credit (staging->master is
			// lost), never the over-credit of admitting master->staging as a promotion.
			name:      "a bidirectional inter-trunk pair is dropped (fail closed), the unambiguous hop kept",
			pairs:     []Hop{hop("develop", "staging"), hop("staging", "master"), hop("master", "staging")},
			terminals: term("staging", "master"),
			want:      []string{"develop->staging"},
		},
		{
			// Intermediate back-merge: develop->release and release->master are forward,
			// release->develop is a sync between two NON-terminals. Reachability alone would
			// keep release->develop (it reaches master), letting the walk credit
			// backward-synced work. The fail-closed reverse-drop removes the ambiguous
			// develop<->release pair entirely; the unambiguous release->master survives.
			name:      "an intermediate back-merge is dropped (fail closed)",
			pairs:     []Hop{hop("develop", "release"), hop("release", "master"), hop("release", "develop")},
			terminals: term("master"),
			want:      []string{"release->master"},
		},
		{
			// A multi-hop promotion (feat/x -> staging -> master), ad-hoc direct fixes to the
			// trunk (hotfix -> master), AND a trunk back-merge (master -> staging, trunk synced
			// down into the staging lane). With `master` the only tracked terminal, master->staging is
			// trunk-sourced and dropped by step 1 BEFORE the reverse-pair check, so staging->master
			// keeps its forward direction and the WHOLE promotion flow survives. A ubiquitous
			// back-merge does NOT collapse it.
			name: "a trunk back-merge does not drop the forward promotion flow",
			pairs: []Hop{
				hop("feat/x", "staging"), hop("staging", "master"), hop("hotfix", "master"), hop("master", "staging"),
			},
			terminals: term("master"),
			want:      []string{"feat/x->staging", "staging->master", "hotfix->master"},
		},
		{
			name:      "a sync into a non-terminal leaf (reaches no trunk) is dropped",
			pairs:     []Hop{hop("feature", "master"), hop("develop", "sidebranch")},
			terminals: term("master"),
			// develop->sidebranch: source not trunk, but sidebranch reaches no terminal.
			want: []string{"feature->master"},
		},
		{
			name:      "multi-hop chain kept, its reverse back-merge dropped",
			pairs:     []Hop{hop("feat", "develop"), hop("develop", "master"), hop("master", "feat")},
			terminals: term("master"),
			want:      []string{"feat->develop", "develop->master"},
		},
		{
			name:      "no terminal anchor yields no flow",
			pairs:     []Hop{hop("x", "develop"), hop("develop", "master")},
			terminals: term(),
			want:      nil,
		},
		{
			// A 2-cycle a<->b is ambiguous: the fail-closed reverse-drop removes BOTH a->b
			// and b->a (neither is a deterministic forward promotion), leaving the
			// unambiguous a->master. Reachability is still cycle-safe by construction (the
			// reverse BFS never re-adds a node), so the drop is what removes the cycle here,
			// not a guard.
			name:      "a 2-cycle is dropped (fail closed), the unambiguous hop kept",
			pairs:     []Hop{hop("a", "b"), hop("b", "a"), hop("a", "master")},
			terminals: term("master"),
			want:      []string{"a->master"},
		},
		{
			name:      "a zero hop and a blank endpoint are skipped",
			pairs:     []Hop{{}, hop("", "master"), hop("feature", ""), hop("feature", "master")},
			terminals: term("master"),
			want:      []string{"feature->master"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := forwardFlow(tt.pairs, tt.terminals)
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.ElementsMatch(t, tt.want, hopStrs(got))
		})
	}
}

// TestUnion pins the dedup contract every caller depends on. The overlapping case is
// the one that matters: a detected hop and a proved edge naming the same (source,
// target) must collapse to one, or the walk tests the same hop twice and a hop
// count double-counts.
func TestUnion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		lists [][]Hop
		want  []string
	}{
		{
			name:  "disjoint lists concatenate",
			lists: [][]Hop{{hop("a", "b")}, {hop("b", "c")}},
			want:  []string{"a->b", "b->c"},
		},
		{
			name: "an overlapping hop appears once",
			lists: [][]Hop{
				{hop("develop", "master"), hop("feature", "develop")},
				{hop("develop", "master"), hop("hotfix", "master")},
			},
			want: []string{"develop->master", "feature->develop", "hotfix->master"},
		},
		{
			// The empty-first-list case must still honour the dedup contract: a
			// caller with no detected hops passes one, and a short-circuit there
			// would return the second list unfiltered.
			name: "an empty first list still filters the second",
			lists: [][]Hop{
				{},
				{hop("a", "b"), hop("a", "b")},
			},
			want: []string{"a->b"},
		},
		{
			name:  "empty input yields nil",
			lists: [][]Hop{{}, {}},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := union(tt.lists...)
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.want, hopStrs(got))
		})
	}
}

// TestTerminals pins why terminals anchor on the tracked set: widening a flow must
// not silently retire a branch the organisation tracks.
func TestTerminals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		hops    []Hop
		tracked map[string]struct{}
		want    []string
	}{
		{
			// The repository tracks develop. With develop the only hop target, every
			// carrier landing there counts. Learning develop->main makes develop a
			// source, so terminality read from the flow's shape would retire it and
			// those carriers would stop counting with nobody having decided so.
			// Anchoring on the tracked set keeps develop terminal.
			name:    "a tracked intermediate stays a terminal after the flow widens",
			hops:    []Hop{hop("feature", "develop"), hop("develop", "main")},
			tracked: term("develop"),
			want:    []string{"develop"},
		},
		{
			name:    "both tracked branches on the flow are terminals",
			hops:    []Hop{hop("feature", "develop"), hop("develop", "main")},
			tracked: term("develop", "main"),
			want:    []string{"develop", "main"},
		},
		{
			name:    "an unresolvable tracked set falls back to the flow's sinks",
			hops:    []Hop{hop("feature", "develop"), hop("develop", "main")},
			tracked: nil,
			want:    []string{"main"},
		},
		{
			// A tracked branch this flow never reaches (a rename, or a policy pointing
			// elsewhere) cannot decide terminality, so shape decides rather than the repo
			// dropping to zero admitted carriers.
			name:    "a tracked set naming no branch on the flow falls back to the sinks",
			hops:    []Hop{hop("feature", "develop"), hop("develop", "main")},
			tracked: term("trunk"),
			want:    []string{"main"},
		},
		{
			name:    "a fan-in to one tracked branch yields it once",
			hops:    []Hop{hop("a", "main"), hop("b", "main")},
			tracked: term("main"),
			want:    []string{"main"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, terminalsOf(tt.hops, tt.tracked))
		})
	}
}

// TestResolveFlow pins the state table: who may widen a flow, who may narrow it,
// and what a confirmed decision closes.
func TestResolveFlow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                 string
		in                   FlowInput
		wantHops             []string
		wantTerminals        []string
		wantObserved         []string
		wantReachedNoTracked bool
	}{
		{
			// A confirmed path is a human decision. Proved edges outside it must not
			// widen it, but they are still reported as Observed, so a caller showing
			// the path can say what confirming it excludes.
			name: "a confirmed path is used verbatim; edges only inform Observed",
			in: FlowInput{
				Confirmed:     true,
				ConfirmedHops: []Hop{hop("develop", "main")},
				EdgePairs:     []Hop{hop("feature", "develop"), hop("develop", "main")},
				Tracked:       term("main"),
			},
			wantHops:      []string{"develop->main"},
			wantTerminals: []string{"main"},
			wantObserved:  []string{"feature->develop", "develop->main"},
		},
		{
			// Confirmed with no hops is an explicit off switch. Edge evidence must not
			// resurrect a flow for it.
			name: "a confirmed empty path attributes nothing",
			in: FlowInput{
				Confirmed: true,
				EdgePairs: []Hop{hop("feature", "main")},
				Tracked:   term("main"),
			},
			wantHops:     nil,
			wantObserved: []string{"feature->main"},
		},
		{
			// Detection only suggests a hop it saw repeatedly, so a one-off integration
			// branch is never in DetectedHops. The proved edge supplies it.
			name: "detected hops are widened by the proved edges",
			in: FlowInput{
				DetectedHops: []Hop{hop("develop", "main")},
				EdgePairs:    []Hop{hop("integration/one-off", "develop"), hop("develop", "main")},
				Tracked:      term("main"),
			},
			wantHops:      []string{"integration/one-off->develop", "develop->main"},
			wantTerminals: []string{"main"},
			wantObserved:  []string{"integration/one-off->develop", "develop->main"},
		},
		{
			// The reason the merged set is reduced as one set. Detection suggests
			// develop->release; the deriver proves the back-merge release->develop. Neither
			// list alone looks ambiguous, so reducing the edges first and unioning after
			// would put BOTH directions in the flow and credit the back-merge's carrier for
			// work flowing the other way. Reducing the union fails the pair closed and
			// keeps the unambiguous release->main.
			name: "an ambiguous pair split across detection and edges fails closed",
			in: FlowInput{
				DetectedHops: []Hop{hop("develop", "release")},
				EdgePairs:    []Hop{hop("release", "develop"), hop("release", "main")},
				Tracked:      term("main"),
			},
			wantHops:      []string{"release->main"},
			wantTerminals: []string{"main"},
			wantObserved:  []string{"release->main"},
		},
		{
			// No anchor means no forward direction to infer, so the edges are inert and
			// the detected hops stand alone. Inferring an anchor would manufacture credit.
			name: "with no tracked branches the edges are inert",
			in: FlowInput{
				DetectedHops: []Hop{hop("develop", "main")},
				EdgePairs:    []Hop{hop("feature", "develop")},
			},
			wantHops:      []string{"develop->main"},
			wantTerminals: []string{"main"},
		},
		{
			// Edges and tracked branches both present, yet nothing reaches them: the
			// tracked branch is named something else than the edges target. The caller logs it;
			// the detected hops still stand so the repo does not silently drop to zero.
			name: "edges reaching no tracked branch report it and leave the detected hops",
			in: FlowInput{
				DetectedHops: []Hop{hop("develop", "release")},
				EdgePairs:    []Hop{hop("feature", "release")},
				Tracked:      term("main"),
			},
			wantHops:             []string{"develop->release"},
			wantTerminals:        []string{"release"},
			wantReachedNoTracked: true,
		},
		{
			// The terminal set is anchored on the tracked branches whatever the
			// confirmation state, so a confirmed chain that passes THROUGH a tracked
			// branch admits landings there too. The hops stay verbatim — this widens what
			// counts as a landing, not what the flow contains — and it is the already
			// decided rule that each tracked branch counts its own landings.
			name: "a confirmed chain through a tracked intermediate admits both",
			in: FlowInput{
				Confirmed:     true,
				ConfirmedHops: []Hop{hop("feature", "release"), hop("release", "main")},
				Tracked:       term("release", "main"),
			},
			wantHops:      []string{"feature->release", "release->main"},
			wantTerminals: []string{"release", "main"},
		},
		{
			// A detected hop that reaches no tracked branch is dropped by the reduction
			// while the hops that do reach one survive. The direction is a safe
			// under-credit and it is the right answer — work that never reached a branch
			// the org counts is not carried to anywhere it counts — but it only happens
			// when the repo has edges at all, so pin it rather than leaving it incidental.
			name: "a detected hop reaching no tracked branch is dropped, the rest survive",
			in: FlowInput{
				DetectedHops: []Hop{hop("integration", "staging")},
				EdgePairs:    []Hop{hop("rc-001", "master")},
				Tracked:      term("master"),
			},
			wantHops:      []string{"rc-001->master"},
			wantTerminals: []string{"master"},
			wantObserved:  []string{"rc-001->master"},
		},
		{
			name:     "no evidence at all attributes nothing",
			in:       FlowInput{Tracked: term("main")},
			wantHops: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveFlow(tt.in)
			if tt.wantHops == nil {
				assert.Empty(t, got.Hops)
			} else {
				assert.ElementsMatch(t, tt.wantHops, hopStrs(got.Hops))
			}
			if tt.wantTerminals == nil {
				assert.Empty(t, got.Terminals)
			} else {
				assert.ElementsMatch(t, tt.wantTerminals, got.Terminals)
			}
			if tt.wantObserved == nil {
				assert.Empty(t, got.Observed)
			} else {
				assert.ElementsMatch(t, tt.wantObserved, hopStrs(got.Observed))
			}
			assert.Equal(t, tt.wantReachedNoTracked, got.EdgesReachNoTrackedBranch)
		})
	}
}

// TestResolveFlow_RefusedReasons pins the reason on each refused hop. A refused hop
// looks identical from outside, absent from the flow, but what a caller may offer
// differs by cause: an ambiguous hop is a tie only a person can break by confirming,
// while a back-merge is a hop whose direction is not in doubt and must never be
// offered for confirmation.
func TestResolveFlow_RefusedReasons(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   FlowInput
		want map[string]RefusalReason
	}{
		{
			// The deriver recorded both directions between develop and release, so there
			// is no evidence which way work is carried. A human may legitimately break it.
			name: "both directions recorded is ambiguous",
			in: FlowInput{
				DetectedHops: []Hop{hop("develop", "release")},
				EdgePairs:    []Hop{hop("release", "develop"), hop("release", "main")},
				Tracked:      term("main"),
			},
			want: map[string]RefusalReason{"develop->release": RefusedAmbiguousDirection},
		},
		{
			// main is tracked and hotfix is not, so this hop leaves the trunk. Its
			// direction is not in doubt, and confirming it would credit a back-merge.
			name: "leaving a tracked branch is a back-merge, not an ambiguity",
			in: FlowInput{
				DetectedHops: []Hop{hop("develop", "main"), hop("main", "hotfix")},
				EdgePairs:    []Hop{hop("develop", "main")},
				Tracked:      term("main"),
			},
			want: map[string]RefusalReason{"main->hotfix": RefusedFlowsFromTrackedBranch},
		},
		{
			// staging is not tracked and nothing carries on from it, so work promoted along
			// this hop lands nowhere the org counts. Confirming it would attribute nothing.
			name: "a hop that reaches no tracked branch is a dead end",
			in: FlowInput{
				DetectedHops: []Hop{hop("integration", "staging")},
				EdgePairs:    []Hop{hop("rc-001", "main")},
				Tracked:      term("main"),
			},
			want: map[string]RefusalReason{"integration->staging": RefusedReachesNoTrackedBranch},
		},
		{
			// A hop the reduction admitted is not refused at all.
			name: "an admitted hop is not reported",
			in: FlowInput{
				DetectedHops: []Hop{hop("develop", "main")},
				EdgePairs:    []Hop{hop("develop", "main")},
				Tracked:      term("main"),
			},
			want: map[string]RefusalReason{},
		},
		{
			// A refusal against a hop that exists only as a carry edge is not actionable:
			// nobody sees it, so nobody can confirm it. Only detected hops are reported.
			name: "a refusal on an edge-only hop is not reported",
			in: FlowInput{
				EdgePairs: []Hop{hop("main", "hotfix"), hop("develop", "main")},
				Tracked:   term("main"),
			},
			want: map[string]RefusalReason{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := map[string]RefusalReason{}
			for _, r := range ResolveFlow(tt.in).Refused {
				got[r.Hop.String()] = r.Reason
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestResolveFlow_FallbackKeepsAmbiguousRefusals: when the reduction keeps
// nothing, the detected hops stand as the flow, except the ones the edges
// recorded in both directions, which stay refused. The flag that the edges
// reached no tracked branch is set only when no edge pair touches one.
func TestResolveFlow_FallbackKeepsAmbiguousRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		in          FlowInput
		wantHops    []string
		wantRefused map[string]RefusalReason
		wantFlag    bool
	}{
		{
			name: "a detected hop recorded both ways stays refused",
			in: FlowInput{
				DetectedHops: []Hop{hop("release", "main")},
				EdgePairs:    []Hop{hop("main", "release"), hop("release", "main")},
				Tracked:      term("main", "release"),
			},
			wantRefused: map[string]RefusalReason{"release->main": RefusedAmbiguousDirection},
		},
		{
			name: "both trunks tracked, the routine back-merge makes the promotion ambiguous",
			in: FlowInput{
				DetectedHops: []Hop{hop("develop", "main")},
				EdgePairs:    []Hop{hop("develop", "main"), hop("main", "develop")},
				Tracked:      term("main", "develop"),
			},
			wantRefused: map[string]RefusalReason{"develop->main": RefusedAmbiguousDirection},
		},
		{
			name: "edges that touch no tracked branch leave the detected hops and set the flag",
			in: FlowInput{
				DetectedHops: []Hop{hop("develop", "release")},
				EdgePairs:    []Hop{hop("feature", "release")},
				Tracked:      term("main"),
			},
			wantHops:    []string{"develop->release"},
			wantRefused: map[string]RefusalReason{},
			wantFlag:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveFlow(tt.in)
			assert.Equal(t, tt.wantHops, nilIfEmpty(hopStrs(got.Hops)))
			refused := map[string]RefusalReason{}
			for _, r := range got.Refused {
				refused[r.Hop.String()] = r.Reason
			}
			assert.Equal(t, tt.wantRefused, refused)
			assert.Equal(t, tt.wantFlag, got.EdgesReachNoTrackedBranch)
		})
	}
}

// The same invariant with no edges at all: with nothing to reduce against, the
// reduction's output is never used, so the raw detected hops stand, refused ones
// included, and none of them may also be reported refused.
func TestResolveFlow_NoEdgesReportsNoRefusalForAnAttributedHop(t *testing.T) {
	t.Parallel()
	got := ResolveFlow(FlowInput{
		DetectedHops: []Hop{hop("develop", "main"), hop("feature", "develop")},
		Tracked:      term("main"),
	})

	assert.Equal(t, []string{"develop->main", "feature->develop"}, hopStrs(got.Hops),
		"with no edges to reduce against, the detected hops stand as the flow")
	assert.Empty(t, got.Refused, "and none of them is reported refused")
}

// Refused and Hops are disjoint on every path, so no hop reads as both attributed
// and left out.
func TestResolveFlow_RefusedIsDisjointFromHops(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   FlowInput
	}{
		{
			name: "back-merge, no edges",
			in: FlowInput{
				DetectedHops: []Hop{hop("develop", "main"), hop("main", "hotfix")},
				Tracked:      term("main"),
			},
		},
		{
			name: "back-merge, edges reduce to nothing",
			in: FlowInput{
				DetectedHops: []Hop{hop("main", "hotfix")},
				EdgePairs:    []Hop{hop("main", "hotfix"), hop("hotfix", "main")},
				Tracked:      term("main"),
			},
		},
		{
			name: "a confirmed hop that the reduction refuses",
			in: FlowInput{
				Confirmed:     true,
				ConfirmedHops: []Hop{hop("main", "hotfix")},
				DetectedHops:  []Hop{hop("main", "hotfix")},
				Tracked:       term("main"),
			},
		},
		{
			name: "a confirmed hop the reduction refuses as ambiguous",
			in: FlowInput{
				Confirmed:     true,
				ConfirmedHops: []Hop{hop("develop", "release")},
				DetectedHops:  []Hop{hop("develop", "release")},
				EdgePairs:     []Hop{hop("develop", "release"), hop("release", "develop")},
				Tracked:       term("main"),
			},
		},
		{
			name: "a confirmed hop the reduction refuses as reaching no tracked branch",
			in: FlowInput{
				Confirmed:     true,
				ConfirmedHops: []Hop{hop("integration", "staging")},
				DetectedHops:  []Hop{hop("integration", "staging")},
				EdgePairs:     []Hop{hop("rc", "main")},
				Tracked:       term("main"),
			},
		},
		{
			name: "reduction admits a subset",
			in: FlowInput{
				DetectedHops: []Hop{hop("develop", "main"), hop("main", "hotfix")},
				EdgePairs:    []Hop{hop("develop", "main")},
				Tracked:      term("main"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveFlow(tc.in)
			kept := make(map[string]struct{}, len(got.Hops))
			for _, h := range got.Hops {
				kept[h.String()] = struct{}{}
			}
			for _, r := range got.Refused {
				key := r.Hop.String()
				_, in := kept[key]
				assert.False(t, in, "%s is reported refused while it is in the flow", key)
			}
		})
	}
}

// TestResolveFlow_KeepsConfirmedAndInferredApart pins Origin: a confirmed flow
// and an inferred one stay distinct in the result, and Observed keeps what the
// edges prove beside a confirmed flow.
func TestResolveFlow_KeepsConfirmedAndInferredApart(t *testing.T) {
	t.Parallel()
	edges := []Hop{hop("feature", "develop"), hop("develop", "main")}
	confirmed := ResolveFlow(FlowInput{Confirmed: true, ConfirmedHops: []Hop{hop("develop", "main")}, EdgePairs: edges, Tracked: term("main")})
	inferred := ResolveFlow(FlowInput{EdgePairs: edges, Tracked: term("main")})
	off := ResolveFlow(FlowInput{Confirmed: true, EdgePairs: edges, Tracked: term("main")})
	// A confirmed hop the reduction would refuse is still used: confirming is how
	// a person overrides the refusal.
	overrides := []FlowInput{
		{
			Confirmed: true, ConfirmedHops: []Hop{hop("develop", "release"), hop("release", "main")},
			DetectedHops: []Hop{hop("develop", "release")},
			EdgePairs:    []Hop{hop("develop", "release"), hop("release", "develop")},
			Tracked:      term("main"),
		},
		{
			Confirmed: true, ConfirmedHops: []Hop{hop("develop", "main"), hop("main", "hotfix")},
			DetectedHops: []Hop{hop("main", "hotfix")},
			Tracked:      term("main"),
		},
	}
	for _, in := range overrides {
		got := ResolveFlow(in)
		assert.Equal(t, in.ConfirmedHops, got.Hops)
		assert.Equal(t, OriginConfirmed, got.Origin)
	}

	assert.Equal(t, OriginConfirmed, confirmed.Origin)
	assert.Equal(t, []string{"develop->main"}, hopStrs(confirmed.Hops))
	assert.ElementsMatch(t, []string{"feature->develop", "develop->main"}, hopStrs(confirmed.Observed))
	assert.Equal(t, OriginInferred, inferred.Origin)
	assert.ElementsMatch(t, []string{"feature->develop", "develop->main"}, hopStrs(inferred.Hops))
	assert.Equal(t, OriginNone, off.Origin, "a confirmed empty flow is off")
}

// TestEdgePairs pins the raw pairs read off stored edges: distinct and sorted,
// both directions kept.
func TestEdgePairs(t *testing.T) {
	t.Parallel()
	got := EdgePairs([]Edge{
		edge("C2", "C1", "release", "main"),
		edge("C1", "L1", "develop", "release"),
		edge("C1", "L2", "develop", "release"),
		edge("SYNC", "C2", "main", "develop"),
	})
	assert.Equal(t, []string{"develop->release", "main->develop", "release->main"}, hopStrs(got))
}

// TestResolveFlow_InferredFlowNeverLeavesATrackedBranch: a hop from a tracked
// branch into an untracked one is a back-merge, refused in an inferred flow
// whether the reduction kept nothing or there were no edges to reduce.
func TestResolveFlow_InferredFlowNeverLeavesATrackedBranch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		in       FlowInput
		wantHops []string
		refused  []string
	}{
		{
			name:    "the reduction keeps nothing",
			in:      FlowInput{DetectedHops: []Hop{hop("main", "c")}, EdgePairs: []Hop{hop("main", "c")}, Tracked: term("main")},
			refused: []string{"main->c"},
		},
		{
			name:     "no edge pairs to reduce",
			in:       FlowInput{DetectedHops: []Hop{hop("develop", "main"), hop("main", "hotfix")}, Tracked: term("main")},
			wantHops: []string{"develop->main"},
			refused:  []string{"main->hotfix"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolveFlow(tt.in)
			assert.Equal(t, tt.wantHops, nilIfEmpty(hopStrs(got.Hops)))
			var refused []string
			for _, r := range got.Refused {
				assert.Equal(t, RefusedFlowsFromTrackedBranch, r.Reason)
				refused = append(refused, r.Hop.String())
			}
			assert.Equal(t, tt.refused, refused)
		})
	}
}

// TestResolveFlow_ConfirmedAndOffSwitchStillReportRefusals: a confirmed flow,
// and the off switch, still list the detected hops left out, with their reason.
func TestResolveFlow_ConfirmedAndOffSwitchStillReportRefusals(t *testing.T) {
	t.Parallel()
	base := FlowInput{
		Confirmed:    true,
		DetectedHops: []Hop{hop("develop", "main"), hop("main", "hotfix")},
		EdgePairs:    []Hop{hop("develop", "main")},
		Tracked:      term("main"),
	}
	confirmed := base
	confirmed.ConfirmedHops = []Hop{hop("develop", "main")}
	want := []RefusedHop{{Hop: hop("main", "hotfix"), Reason: RefusedFlowsFromTrackedBranch}}
	for name, in := range map[string]FlowInput{"confirmed": confirmed, "off switch": base} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, ResolveFlow(in).Refused)
		})
	}
}
