// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func wedge(seed, carrier, carriedKey string) WalkEdge {
	return WalkEdge{Seed: seed, Carrier: carrier, Carried: carriedKey}
}

func only(keys ...string) func(string) bool {
	set := map[string]bool{}
	for _, k := range keys {
		set[k] = true
	}
	return func(k string) bool { return set[k] }
}

func walkOf(edges ...WalkEdge) WalkResult { return WalkResult{Edges: edges} }

func landed(w CarriedWork) map[string][]string {
	out := map[string][]string{}
	for _, p := range w.Pairs {
		out[p.Carried] = p.LandedBy
	}
	return out
}

// TestCarriedBy pins terminality per (seed, carried) against the visible
// children only.
func TestCarriedBy(t *testing.T) {
	tests := []struct {
		name             string
		edges            []WalkEdge
		visible          func(string) bool
		want             map[string][]string
		wantUnresolvable int
	}{
		{
			name: "an intermediate carrier is not a pair of its own",
			edges: []WalkEdge{
				wedge("C2", "C2", "C1"), wedge("C2", "C1", "L1"), wedge("C2", "C1", "L2"), wedge("C2", "C1", "L4"),
				wedge("C3", "C3", "L3"),
			},
			want: map[string][]string{"L1": {"C2"}, "L2": {"C2"}, "L4": {"C2"}, "L3": {"C3"}},
		},
		{
			name:             "a carrier whose only child is not visible is terminal itself",
			edges:            []WalkEdge{wedge("S", "S", "P"), wedge("S", "P", "Q")},
			visible:          only("S", "P"),
			want:             map[string][]string{"P": {"S"}},
			wantUnresolvable: 1,
		},
		{
			name: "a leaf carried through two carriers under one seed is one pair",
			edges: []WalkEdge{
				wedge("S", "S", "C1"), wedge("S", "S", "C2"), wedge("S", "C1", "L"), wedge("S", "C2", "L"),
			},
			want: map[string][]string{"L": {"S"}},
		},
		{
			name:  "terminal under one seed, intermediate under another",
			edges: []WalkEdge{wedge("A", "A", "X"), wedge("B", "B", "X"), wedge("B", "X", "Y")},
			want:  map[string][]string{"X": {"A"}, "Y": {"B"}},
		},
		{
			name:  "one leaf landed by two seeds lists both",
			edges: []WalkEdge{wedge("A", "A", "L"), wedge("B", "B", "L")},
			want:  map[string][]string{"L": {"A", "B"}},
		},
		{
			name:    "a pull request landed by a visible and an invisible seed lists the visible one",
			edges:   []WalkEdge{wedge("A", "A", "L"), wedge("B", "B", "L")},
			visible: only("A", "L"),
			want:    map[string][]string{"L": {"A"}},
		},
		{
			name: "an invisible leaf reached from two seeds is counted once",
			edges: []WalkEdge{
				wedge("A", "A", "GHOST"), wedge("B", "B", "GHOST"),
			},
			visible:          only("A", "B"),
			want:             map[string][]string{},
			wantUnresolvable: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CarriedBy(walkOf(tt.edges...), tt.visible)
			assert.Equal(t, tt.want, landed(got))
			assert.Equal(t, tt.wantUnresolvable, got.Unresolvable)
		})
	}
}

// TestCarriedBy_CapStoppedNodesAreNotWork: a carried pull request whose own edges
// a cap stopped the walk from following is not listed as delivered work; it is
// counted as unresolved, since what it carried is unknown.
func TestCarriedBy_CapStoppedNodesAreNotWork(t *testing.T) {
	w := WalkResult{
		Edges:      []WalkEdge{wedge("S", "S", "P"), wedge("S", "P", "CUT"), wedge("S", "S", "LEAF")},
		Unexpanded: []WalkNode{{Seed: "S", Carrier: "CUT"}},
	}
	got := CarriedBy(w, nil)
	assert.Equal(t, map[string][]string{"LEAF": {"S"}}, landed(got))
	assert.Equal(t, 1, got.Unresolvable)
}

// TestCarriedBy_InvisibleLandingsAreUnresolvable: a carried pull request whose
// landing seeds are all outside the visible population is counted unresolved,
// never shown as a pair with no landing.
func TestCarriedBy_InvisibleLandingsAreUnresolvable(t *testing.T) {
	got := CarriedBy(walkOf(wedge("S", "S", "L")), func(k string) bool { return k != "S" })
	assert.Empty(t, got.Pairs)
	assert.Equal(t, 1, got.Unresolvable)
}

// TestCarriedBy_APairIsNeverAlsoUnresolvable: a pull request one seed delivered
// and another seed's capped walk left unexpanded is a pair, and not counted as
// unresolved too.
func TestCarriedBy_APairIsNeverAlsoUnresolvable(t *testing.T) {
	got := CarriedBy(WalkResult{
		Edges:      []WalkEdge{wedge("A", "A", "X"), wedge("B", "B", "X")},
		Unexpanded: []WalkNode{{Seed: "B", Carrier: "X"}},
	}, nil)
	assert.Equal(t, map[string][]string{"X": {"A"}}, landed(got))
	assert.Equal(t, 0, got.Unresolvable)
}

// TestCarriedBy_PairAndCutVariants pins that a key delivered under some seed is a
// pair and not unresolved, with a visibility function given and with two seeds
// delivering it.
func TestCarriedBy_PairAndCutVariants(t *testing.T) {
	everything := func(string) bool { return true }
	tests := []struct {
		name    string
		w       WalkResult
		visible func(string) bool
		want    map[string][]string
	}{
		{
			name:    "a visibility function that admits everything",
			w:       WalkResult{Edges: []WalkEdge{wedge("A", "A", "X"), wedge("B", "B", "X")}, Unexpanded: []WalkNode{{Seed: "B", Carrier: "X"}}},
			visible: everything,
			want:    map[string][]string{"X": {"A"}},
		},
		{
			name: "two seeds deliver it and a third is cut",
			w: WalkResult{
				Edges:      []WalkEdge{wedge("A", "A", "X"), wedge("B", "B", "X"), wedge("C", "C", "X")},
				Unexpanded: []WalkNode{{Seed: "C", Carrier: "X"}},
			},
			want: map[string][]string{"X": {"A", "B"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CarriedBy(tt.w, tt.visible)
			assert.Equal(t, tt.want, landed(got))
			assert.Equal(t, 0, got.Unresolvable)
		})
	}
}
