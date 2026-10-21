// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalk(t *testing.T) {
	twoHop := []Hop{{"staging", "main"}, {"feature", "staging"}}
	tests := []struct {
		name  string
		edges edgeList
		seeds []Seed
		flow  []Hop
		want  []string
	}{
		{
			name:  "a two-hop chain expands fully",
			edges: edgeList{edge("P1", "C1", "staging", "main"), edge("C1", "LEAF", "feature", "staging")},
			seeds: []Seed{{Carrier: "P1", Target: "main"}},
			flow:  twoHop,
			want:  []string{"P1->C1", "C1->LEAF"},
		},
		{
			name:  "a routed back-merge edge is never admitted",
			edges: edgeList{edge("P1", "C1", "staging", "main"), edge("C1", "LEAF", "main", "staging")},
			seeds: []Seed{{Carrier: "P1", Target: "main"}},
			flow:  twoHop,
			want:  []string{"P1->C1"},
		},
		{
			name:  "a sync pull request's edge, off the flow, reaches nothing",
			edges: edgeList{edge("P1", "C1", "staging", "main"), edge("SYNC", "LEAF", "main", "feature")},
			seeds: []Seed{{Carrier: "P1", Target: "main"}, {Carrier: "SYNC", Target: "feature"}},
			flow:  twoHop,
			want:  []string{"P1->C1"},
		},
		{
			name: "a diamond keeps both carriers' edges to the shared leaf",
			edges: edgeList{
				edge("X", "A", "staging", "main"), edge("X", "B", "staging", "main"),
				edge("A", "L", "feature", "staging"), edge("B", "L", "feature", "stage"),
			},
			seeds: []Seed{{Carrier: "X", Target: "main"}},
			flow:  []Hop{{"staging", "main"}, {"feature", "staging"}, {"feature", "stage"}},
			want:  []string{"X->A", "X->B", "A->L", "B->L"},
		},
		{
			name: "a carrier reached twice under one seed emits its own edges once",
			edges: edgeList{
				edge("X", "A", "staging", "main"), edge("X", "B", "staging", "main"),
				edge("A", "L", "feature", "staging"), edge("B", "L", "feature", "stage"),
				edge("L", "M", "topic", "feature"),
			},
			seeds: []Seed{{Carrier: "X", Target: "main"}},
			flow:  []Hop{{"staging", "main"}, {"feature", "staging"}, {"feature", "stage"}, {"topic", "feature"}},
			want:  []string{"X->A", "X->B", "A->L", "B->L", "L->M"},
		},
		{
			name: "a cycle among untracked branches ends at the branch the route already passed",
			edges: edgeList{
				edge("A", "B", "cyc/a", "main"), edge("B", "C", "cyc/b", "cyc/a"),
				edge("C", "D", "cyc/c", "cyc/b"), edge("D", "B", "cyc/a", "cyc/c"),
			},
			seeds: []Seed{{Carrier: "A", Target: "main"}},
			flow:  []Hop{{"cyc/a", "main"}, {"cyc/b", "cyc/a"}, {"cyc/c", "cyc/b"}, {"cyc/a", "cyc/c"}},
			want:  []string{"A->B", "B->C", "C->D"},
		},
		{
			name:  "an empty flow admits nothing",
			edges: edgeList{edge("P1", "C1", "staging", "main")},
			seeds: []Seed{{Carrier: "P1", Target: "main"}},
			want:  nil,
		},
		{
			name:  "no seeds walk nothing",
			edges: edgeList{edge("P1", "C1", "staging", "main")},
			flow:  twoHop,
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Walk(context.Background(), tt.edges, tt.seeds, tt.flow, Limits{MaxDepth: 20})
			require.NoError(t, err)
			assert.Equal(t, tt.want, nilIfEmpty(pairs(got.Edges)))
			assert.Empty(t, got.DepthCapped)
			assert.False(t, got.RouteCapped)
			assert.Empty(t, got.Unexpanded, "a walk no cap stopped expands every node")
		})
	}
}

// TestWalk_IsDeterministic pins the order: seeds in the order given, each
// carrier's edges by carried key, whatever order the source returns them in.
func TestWalk_IsDeterministic(t *testing.T) {
	forward := edgeList{edge("S", "A", "staging", "main"), edge("S", "B", "staging", "main"), edge("S", "C", "staging", "main")}
	backward := edgeList{forward[2], forward[0], forward[1]}
	flow := []Hop{{"staging", "main"}}
	seeds := []Seed{{Carrier: "S", Target: "main"}}
	a, err := Walk(context.Background(), forward, seeds, flow, Limits{})
	require.NoError(t, err)
	b, err := Walk(context.Background(), backward, seeds, flow, Limits{})
	require.NoError(t, err)
	assert.Equal(t, a, b)
	assert.Equal(t, []string{"S->A", "S->B", "S->C"}, pairs(a.Edges))
}

// TestWalk_DepthCap: a chain deeper than the cap stops at the cap, and the result
// names the repository and the node whose edges were cut, never a silent
// under-count.
func TestWalk_DepthCap(t *testing.T) {
	edges := edgeList{
		edge("P1", "C1", "stage", "main"),
		edge("C1", "C2", "integration", "stage"),
		edge("C2", "LEAF", "feature", "integration"),
	}
	flow := []Hop{{"stage", "main"}, {"integration", "stage"}, {"feature", "integration"}}
	rec := newRecorder()
	got, err := Walk(context.Background(), edges, []Seed{{Carrier: "P1", Target: "main"}}, flow, Limits{MaxDepth: 2}, WithObserver(rec))
	require.NoError(t, err)
	assert.Equal(t, []string{"P1->C1", "C1->C2"}, pairs(got.Edges))
	assert.Equal(t, []string{testRepo}, got.DepthCapped)
	assert.Equal(t, []WalkNode{{Seed: "P1", Carrier: "C2"}}, got.Unexpanded)
	assert.False(t, got.RouteCapped)
	assert.Equal(t, 1, rec.counts["walk_depth_cap_hit"])
	assert.Equal(t, []map[string]string{{"repository": testRepo}}, rec.labels["walk_depth_cap_hit"])
	assert.Equal(t, []float64{2}, rec.samples["walk_depth_reached"])
}

// TestWalk_RouteCap builds a lattice where every branch at one level merged into
// both branches of the level above: two carriers per level, routes doubling per
// level. Only the route bound can stop it.
func TestWalk_RouteCap(t *testing.T) {
	const levels = 12
	pr := func(level, b int) string { return fmt.Sprintf("pr-%d-%d", level, b) }
	branch := func(level, b int) string { return fmt.Sprintf("lvl%d/b%d", level, b) }
	var edges edgeList
	var flow []Hop
	var seeds []Seed
	for b := 0; b < 2; b++ {
		edges = append(edges, edge(pr(0, b), pr(1, b), branch(0, b), "main"))
		flow = append(flow, Hop{branch(0, b), "main"})
		seeds = append(seeds, Seed{Carrier: pr(0, b), Target: "main"})
	}
	for lvl := 1; lvl < levels; lvl++ {
		for b := 0; b < 2; b++ {
			for parent := 0; parent < 2; parent++ {
				edges = append(edges, edge(pr(lvl, parent), pr(lvl+1, b), branch(lvl, b), branch(lvl-1, parent)))
				flow = append(flow, Hop{branch(lvl, b), branch(lvl-1, parent)})
			}
		}
	}
	rec := newRecorder()
	capped, err := Walk(context.Background(), edges, seeds, flow, Limits{MaxDepth: 20, MaxRoutes: 1000}, WithObserver(rec))
	require.NoError(t, err)
	assert.Equal(t, 1, rec.counts["walk_route_cap_hit"])
	assert.True(t, capped.RouteCapped)
	assert.NotEmpty(t, capped.Unexpanded, "the nodes whose descent was refused are named")
	expanded := map[WalkNode]bool{}
	for _, e := range capped.Edges {
		expanded[WalkNode{Seed: e.Seed, Carrier: e.Carrier}] = true
	}
	for _, n := range capped.Unexpanded {
		assert.False(t, expanded[n], "an unexpanded node has no edges of its own in the result: %v", n)
	}

	full, err := Walk(context.Background(), edges, seeds, flow, Limits{MaxDepth: 20, MaxRoutes: 1 << 20})
	require.NoError(t, err)
	assert.Greater(t, len(full.Edges), len(capped.Edges), "a larger route budget reaches strictly more")
	assert.False(t, full.RouteCapped)
}

// TestWalk_RouteCapBoundsThePeak: a level whose descent the budget refuses still
// emits every edge at that level, and never holds more routes than the budget.
func TestWalk_RouteCapBoundsThePeak(t *testing.T) {
	const fanOut = 1200
	var edges edgeList
	for i := 0; i < fanOut; i++ {
		edges = append(edges, edge("SEED", fmt.Sprintf("leaf-%04d", i), "release", "main"))
		edges = append(edges, edge(fmt.Sprintf("leaf-%04d", i), fmt.Sprintf("deep-%04d", i), "feature", "release"))
	}
	rec := newRecorder()
	got, err := Walk(context.Background(), edges, []Seed{{Carrier: "SEED", Target: "main"}},
		[]Hop{{"release", "main"}, {"feature", "release"}}, Limits{MaxDepth: 20, MaxRoutes: 1000}, WithObserver(rec))
	require.NoError(t, err)
	assert.Equal(t, 1, rec.counts["walk_route_cap_hit"])
	assert.True(t, got.RouteCapped)
	assert.Len(t, got.Unexpanded, fanOut, "no leaf at the refused level is expanded, so every one is named")
	depth1 := 0
	for _, e := range got.Edges {
		assert.LessOrEqual(t, e.Depth, 1, "the walk does not descend past the level that broke the budget")
		if e.Depth == 1 {
			depth1++
		}
	}
	assert.Equal(t, fanOut, depth1, "every edge at the refused level is still emitted")
	for _, peak := range rec.samples["walk_peak_routes"] {
		assert.LessOrEqual(t, peak, 1000.0, "the walk never held more routes than its budget")
	}
	assert.Equal(t, []float64{fanOut}, rec.samples["walk_route_demand"], "the demand past the budget is reported")
}

// TestWalk_LimitsAreTheCallers pins how a caller's limits are read: zero or
// negative takes the default, and any other value is used exactly as given.
func TestWalk_LimitsAreTheCallers(t *testing.T) {
	tests := []struct {
		name               string
		in                 Limits
		wantDepth, wantCap int
	}{
		{name: "zero takes the defaults", in: Limits{}, wantDepth: DefaultMaxDepth, wantCap: DefaultMaxRoutes},
		{name: "values are kept", in: Limits{MaxDepth: 3, MaxRoutes: 5000}, wantDepth: 3, wantCap: 5000},
		{name: "a small route budget is kept", in: Limits{MaxRoutes: 16}, wantDepth: DefaultMaxDepth, wantCap: 16},
		{name: "a large route budget is kept", in: Limits{MaxRoutes: 1 << 30}, wantDepth: DefaultMaxDepth, wantCap: 1 << 30},
		{name: "a large depth is kept", in: Limits{MaxDepth: 99}, wantDepth: 99, wantCap: DefaultMaxRoutes},
		{name: "negative takes the defaults", in: Limits{MaxDepth: -1, MaxRoutes: -1}, wantDepth: DefaultMaxDepth, wantCap: DefaultMaxRoutes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantDepth, tt.in.depth())
			assert.Equal(t, tt.wantCap, tt.in.routes())
		})
	}
}

// TestWalk_SmallRouteBudgetIsHonoured: a budget of 16 stops the walk at 16
// routes, and the result names every node it did not expand.
func TestWalk_SmallRouteBudgetIsHonoured(t *testing.T) {
	var edges edgeList
	for i := 0; i < 20; i++ {
		edges = append(edges, edge("SEED", fmt.Sprintf("leaf-%02d", i), "release", "main"))
		edges = append(edges, edge(fmt.Sprintf("leaf-%02d", i), fmt.Sprintf("deep-%02d", i), "feature", "release"))
	}
	got, err := Walk(context.Background(), edges, []Seed{{Carrier: "SEED", Target: "main"}},
		[]Hop{{"release", "main"}, {"feature", "release"}}, Limits{MaxRoutes: 16})
	require.NoError(t, err)
	assert.True(t, got.RouteCapped)
	assert.Len(t, got.Unexpanded, 20, "the budget runs out at the first level, so none of its 20 leaves is expanded")
	assert.Len(t, got.Edges, 20, "the refused level still emits every edge")
}

type failingSource struct{}

func (failingSource) EdgesFrom(context.Context, []string) ([]Edge, error) {
	return nil, errors.New("store offline")
}

// TestWalk_SourceErrorIsReturned: a failed edge read fails the walk, never a
// silent partial result.
func TestWalk_SourceErrorIsReturned(t *testing.T) {
	_, err := Walk(context.Background(), failingSource{}, []Seed{{Carrier: "P1", Target: "main"}}, []Hop{{"staging", "main"}}, Limits{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store offline")
}

// TestWalk_RouteCapNamesEveryCutNode: when the route budget runs out partway
// through a level, the nodes already queued for the next level are not expanded
// either, so they are named and never read as delivered work.
func TestWalk_RouteCapNamesEveryCutNode(t *testing.T) {
	edges := edgeList{
		edge("S", "A", "develop", "main"), edge("S", "B", "develop", "main"),
		edge("A", "X", "int", "develop"), edge("B", "Y", "int", "develop"),
	}
	l := Limits{MaxRoutes: 2}
	got, err := Walk(context.Background(), edges, []Seed{{Carrier: "S", Target: "main"}}, []Hop{{"develop", "main"}, {"int", "develop"}}, l)
	require.NoError(t, err)
	assert.True(t, got.RouteCapped)
	assert.Equal(t, []WalkNode{{Seed: "S", Carrier: "A"}, {Seed: "S", Carrier: "B"}}, got.Unexpanded)
	cw := CarriedBy(got, nil)
	assert.Empty(t, cw.Pairs, "neither A nor B was expanded, so neither is delivered work")
	assert.Equal(t, 2, cw.Unresolvable)
}

// TestWalk_DeepChainWithinTheCallersDepth: a chain deeper than the default is
// walked to the end when the caller's MaxDepth allows it.
func TestWalk_DeepChainWithinTheCallersDepth(t *testing.T) {
	var edges edgeList
	var flow []Hop
	for i := 0; i < 50; i++ {
		src, dst := fmt.Sprintf("b%02d", i+1), fmt.Sprintf("b%02d", i)
		if i == 0 {
			dst = "main"
		}
		edges = append(edges, edge(fmt.Sprintf("p%02d", i), fmt.Sprintf("p%02d", i+1), src, dst))
		flow = append(flow, Hop{src, dst})
	}
	got, err := Walk(context.Background(), edges, []Seed{{Carrier: "p00", Target: "main"}}, flow, Limits{MaxDepth: 60})
	require.NoError(t, err)
	assert.Len(t, got.Edges, 50)
	assert.Empty(t, got.DepthCapped)
}

// TestWalk_RouteCapBoundary: the routes held count the seed's, so a seed with one
// child fits a budget of 2, and a budget of 1 leaves the child unexpanded.
func TestWalk_RouteCapBoundary(t *testing.T) {
	edges := edgeList{edge("S", "A", "develop", "main")}
	flow := []Hop{{"develop", "main"}}
	seeds := []Seed{{Carrier: "S", Target: "main"}}
	fits, err := Walk(context.Background(), edges, seeds, flow, Limits{MaxRoutes: 2})
	require.NoError(t, err)
	assert.False(t, fits.RouteCapped)
	assert.Empty(t, fits.Unexpanded)
	capped, err := Walk(context.Background(), edges, seeds, flow, Limits{MaxRoutes: 1})
	require.NoError(t, err)
	assert.True(t, capped.RouteCapped)
	assert.Equal(t, []WalkNode{{Seed: "S", Carrier: "A"}}, capped.Unexpanded)
}
