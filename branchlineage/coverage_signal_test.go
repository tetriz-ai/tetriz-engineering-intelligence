// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lines(n int64) *int64 { return &n }

// carriersOf builds, for each distinct carrier in edges, the merged pull request
// ComputeCoverage reads: its hop and its own line count.
func carriersOf(edges []Edge) []PR {
	seen := map[string]bool{}
	var out []PR
	for i, e := range edges {
		if seen[e.Carrier] {
			continue
		}
		seen[e.Carrier] = true
		p := PR{Key: e.Carrier, Repository: e.Repository, Number: i + 1, HeadBranch: e.Source, BaseBranch: e.Target, MergedAt: at(i)}
		if e.CarrierLines != nil {
			p.LinesAdded = *e.CarrierLines
		}
		out = append(out, p)
	}
	return out
}

func linedEdge(carrier, carriedKey, source, target string, carrierLines, carriedLines *int64) Edge {
	e := edge(carrier, carriedKey, source, target)
	e.CarrierLines, e.CarriedLines = carrierLines, carriedLines
	return e
}

// TestComputeCoverage_Bands: each carrier on the flow lands in exactly one band,
// and a carrier off the flow (a sync pull request) is not a carrier at all.
func TestComputeCoverage_Bands(t *testing.T) {
	edges := []Edge{
		linedEdge("IN", "a", "staging", "main", lines(100), lines(80)),
		linedEdge("OUT", "b", "staging", "main", lines(100), lines(10)),
		linedEdge("NO-CARRIER", "c", "staging", "main", nil, lines(50)),
		linedEdge("NO-CARRIED", "d", "staging", "main", lines(70), nil),
		linedEdge("ZERO", "e", "staging", "main", lines(70), lines(0)),
		linedEdge("SYNC", "f", "main", "feature", lines(100), lines(90)),
	}
	cov := ComputeCoverage(carriersOf(edges), edges, []Hop{{"staging", "main"}}, []string{"main"}, 100)
	assert.Equal(t, 5, cov.CarrierCount, "five carriers on the flow; the sync carrier is not counted")
	assert.Equal(t, 1, cov.ExplainedInBand)
	assert.Equal(t, 1, cov.ExcludedNoCarrierLines)
	assert.Equal(t, 1, cov.ExcludedNoCarriedLines)
	assert.Equal(t, 1, cov.ExcludedZeroCarried)
	got := map[string]Band{}
	for _, c := range cov.Carriers {
		got[c.Carrier] = c.Band
	}
	assert.Equal(t, map[string]Band{
		"IN": BandIn, "OUT": BandOut, "NO-CARRIER": BandNoCarrierLines, "NO-CARRIED": BandNoCarriedLines, "ZERO": BandZeroCarried,
	}, got)
	var unexplained []string
	for _, c := range cov.Unexplained() {
		unexplained = append(unexplained, c.Carrier)
	}
	assert.Equal(t, []string{"NO-CARRIED", "NO-CARRIER", "OUT", "ZERO"}, unexplained, "the coverage warning lists every carrier the band does not explain")
}

// TestComputeCoverage_BandEdges pins the band's two ends as inside.
func TestComputeCoverage_BandEdges(t *testing.T) {
	tests := []struct {
		name    string
		carrier int64
		want    Band
	}{
		{name: "half the carried sum is in", carrier: 50, want: BandIn},
		{name: "twice the carried sum is in", carrier: 200, want: BandIn},
		{name: "just under half is out", carrier: 49, want: BandOut},
		{name: "just over twice is out", carrier: 201, want: BandOut},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := []Edge{linedEdge("C", "a", "staging", "main", lines(tt.carrier), lines(100))}
			cov := ComputeCoverage(carriersOf(edges), edges, []Hop{{"staging", "main"}}, []string{"main"}, 1)
			require.Len(t, cov.Carriers, 1)
			assert.Equal(t, tt.want, cov.Carriers[0].Band)
		})
	}
}

// TestComputeCoverage_TerminalRestriction: on develop -> release -> main only the
// carriers landing on a terminal count, so the carrier share stays a share of
// the tracked-branch merges.
func TestComputeCoverage_TerminalRestriction(t *testing.T) {
	edges := []Edge{
		linedEdge("INTERMEDIATE", "a", "develop", "release", lines(100), lines(80)),
		linedEdge("TERMINAL", "b", "release", "main", lines(100), lines(80)),
	}
	flow := []Hop{{"develop", "release"}, {"release", "main"}}
	assert.Equal(t, 1, ComputeCoverage(carriersOf(edges), edges, flow, []string{"main"}, 4).CarrierCount)
	assert.Equal(t, 2, ComputeCoverage(carriersOf(edges), edges, flow, []string{"release", "main"}, 4).CarrierCount)
	assert.Equal(t, 0, ComputeCoverage(carriersOf(edges), edges, flow, nil, 4).CarrierCount, "no terminal admits no carriers")
}

// TestComputeCoverage_Shares pins the share arithmetic: excluded carriers leave
// the explained share's denominator, and no carriers reads as "no carriers",
// never 0% explained.
func TestComputeCoverage_Shares(t *testing.T) {
	tests := []struct {
		name                                        string
		inBand, outBand, noCarrier, noCarried, zero int
		total                                       int
		wantHas                                     bool
		wantCarrierShare, wantExplained             float64
	}{
		{name: "excluded carriers lower the denominator", inBand: 8, noCarrier: 2, noCarried: 1, zero: 1, total: 100, wantHas: true, wantCarrierShare: 0.12, wantExplained: 1},
		{name: "one of two eligible explained", inBand: 1, outBand: 1, noCarrier: 1, noCarried: 1, zero: 1, total: 50, wantHas: true, wantCarrierShare: 0.1, wantExplained: 0.5},
		{name: "every carrier excluded", noCarrier: 1, noCarried: 1, zero: 1, total: 30, wantHas: true, wantCarrierShare: 0.1},
		{name: "no carriers", total: 50},
		{name: "a zero total does not divide by zero"},
		{name: "every carrier explained", inBand: 5, total: 20, wantHas: true, wantCarrierShare: 0.25, wantExplained: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var edges []Edge
			n := 0
			add := func(count int, carrierLines, carriedLines *int64) {
				for i := 0; i < count; i++ {
					n++
					edges = append(edges, linedEdge(string(rune('A'+n)), "x", "staging", "main", carrierLines, carriedLines))
				}
			}
			add(tt.inBand, lines(100), lines(100))
			add(tt.outBand, lines(100), lines(10))
			add(tt.noCarrier, nil, lines(10))
			add(tt.noCarried, lines(10), nil)
			add(tt.zero, lines(10), lines(0))
			cov := ComputeCoverage(carriersOf(edges), edges, []Hop{{"staging", "main"}}, []string{"main"}, tt.total)
			assert.Equal(t, tt.wantHas, cov.HasCarriers)
			assert.InDelta(t, tt.wantCarrierShare, cov.CarrierShare, 1e-9)
			assert.InDelta(t, tt.wantExplained, cov.ExplainedShare, 1e-9)
			assert.Equal(t, tt.total, cov.TrackedMergedTotal)
		})
	}
}

// TestComputeCoverage_GitFlowReleaseCut is the missing-history warning: a
// release branch cut with its content already on it promotes far more lines
// than the fixes merged into it by pull request, so its carrier sits above the
// band and is listed as unexplained.
func TestComputeCoverage_GitFlowReleaseCut(t *testing.T) {
	cut := mpr("RELEASE", 200, "release/2.0", "main", 900)
	cut.LinesAdded, cut.LinesDeleted = 4000, 200
	g, err := Derive([]PR{mpr("FIX", 70, "hotfix/x", "release/2.0", 400), cut})
	require.NoError(t, err)
	cov := ComputeCoverage([]PR{mpr("FIX", 70, "hotfix/x", "release/2.0", 400), cut}, g.Edges(), []Hop{{"release/2.0", "main"}}, []string{"main"}, 1)
	require.Len(t, cov.Unexplained(), 1)
	assert.Equal(t, "RELEASE", cov.Unexplained()[0].Carrier)
	assert.Equal(t, BandOut, cov.Unexplained()[0].Band)
	assert.Zero(t, cov.ExplainedShare)
}

// TestComputeCoverage_ReleaseCutWithNoHotfix: a promotion that carried nothing,
// such as a release branch created with its content already on it and no pull
// request merged into it, is still a promotion on the flow. It is listed as
// unexplained, never invisible.
func TestComputeCoverage_ReleaseCutWithNoHotfix(t *testing.T) {
	feature := mpr("PR-1", 1, "feature/x", "develop", 60)
	feature.LinesAdded = 300
	cut := mpr("PR-2", 2, "release/2.0", "main", 300)
	cut.LinesAdded, cut.LinesDeleted = 1400, 0
	prs := []PR{feature, cut}
	g, err := Derive(prs)
	require.NoError(t, err)
	require.Empty(t, g.Edges(), "no pull request merged into release/2.0, so nothing is carried")

	cov := ComputeCoverage(prs, g.Edges(), []Hop{{"release/2.0", "main"}}, []string{"main"}, 1)
	assert.True(t, cov.HasCarriers)
	assert.Equal(t, 1, cov.CarrierCount)
	require.Len(t, cov.Unexplained(), 1)
	assert.Equal(t, "PR-2", cov.Unexplained()[0].Carrier)
	assert.Equal(t, BandZeroCarried, cov.Unexplained()[0].Band)
}

// TestComputeCoverage_DerivedEmptyPromotionCounts: a promotion whose window held
// nothing is a carrier with nothing carried, not a missing one.
func TestComputeCoverage_DerivedEmptyPromotionCounts(t *testing.T) {
	prs := []PR{
		mpr("L0", 1, "feature/a", "develop", 10),
		mpr("P1", 2, "develop", "main", 20),
		mpr("P2", 3, "develop", "main", 30),
	}
	g, err := Derive(prs)
	require.NoError(t, err)
	cov := ComputeCoverage(prs, g.Edges(), []Hop{{"develop", "main"}}, []string{"main"}, 2)
	assert.Equal(t, 2, cov.CarrierCount)
	got := map[string]Band{}
	for _, c := range cov.Carriers {
		got[c.Carrier] = c.Band
	}
	assert.Equal(t, map[string]Band{"P1": BandIn, "P2": BandZeroCarried}, got)
}
