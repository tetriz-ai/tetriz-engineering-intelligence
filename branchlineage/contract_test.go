// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestMergePairs pins the detector's input: merged, non-fork pull requests with
// both branches set and different, counted per (head, base) with the earliest and
// latest merge, and sorted.
func TestMergePairs(t *testing.T) {
	fork := mpr("F", 5, "develop", "main", 50)
	fork.CrossRepository = true
	open := mpr("O", 6, "develop", "main", 60)
	open.MergedAt = time.Time{}
	got := MergePairs([]PR{
		mpr("B", 2, "develop", "main", 20),
		mpr("C", 3, "feature/x", "develop", 30),
		mpr("A", 1, "develop", "main", 10),
		mpr("SELF", 4, "main", "main", 40),
		mpr("NOHEAD", 7, "", "main", 70),
		fork, open,
	})
	assert.Equal(t, []BranchPair{
		{Head: "develop", Base: "main", Count: 2, FirstMergedAt: at(10), LastMergedAt: at(20)},
		{Head: "feature/x", Base: "develop", Count: 1, FirstMergedAt: at(30), LastMergedAt: at(30)},
	}, got)
}

// TestBranchMatches pins the tracked-branch entry contract: exact by equality,
// a glob by path.Match, a malformed glob matches nothing.
func TestBranchMatches(t *testing.T) {
	tests := []struct {
		entry, name string
		want        bool
	}{
		{entry: "main", name: "main", want: true},
		{entry: "main", name: "mainline", want: false},
		{entry: "release/*", name: "release/1.0", want: true},
		{entry: "release/*", name: "release/hotfix/1", want: false},
		{entry: "rc-?", name: "rc-1", want: true},
		{entry: "release/[", name: "release/[", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.entry+" "+tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, BranchMatches(tt.entry, tt.name))
		})
	}
}

// TestLabels pins the strings the Observer events and callers read.
func TestLabels(t *testing.T) {
	tests := []struct {
		got, want string
	}{
		{OutcomeDropped.String(), "dropped"},
		{OutcomeNonCarrier.String(), "non_carrier"},
		{OutcomeDerivedEmpty.String(), "derived_empty"},
		{OutcomeCarrier.String(), "carrier"},
		{OutcomeUnknown.String(), "unknown"},
		{ColdStartNone.String(), "none"},
		{ColdStartFirstCommit.String(), "first_commit"},
		{ColdStartLookback.String(), "lookback"},
		{ReasonCorrection.String(), "correction"},
		{ReasonLateLeaf.String(), "late_leaf"},
		{ReasonLatePromotion.String(), "late_promotion"},
		{ReasonUnknown.String(), "unknown"},
		{BandIn.String(), "in_band"},
		{BandOut.String(), "out_of_band"},
		{BandNoCarrierLines.String(), "no_carrier_lines"},
		{BandNoCarriedLines.String(), "no_carried_lines"},
		{BandZeroCarried.String(), "zero_carried"},
		{BandUnknown.String(), "unknown"},
		{OriginConfirmed.String(), "confirmed"},
		{OriginInferred.String(), "inferred"},
		{OriginNone.String(), "none"},
		{RefusedAmbiguousDirection.String(), "ambiguous_direction"},
		{RefusedFlowsFromTrackedBranch.String(), "flows_from_tracked_branch"},
		{RefusedReachesNoTrackedBranch.String(), "reaches_no_tracked_branch"},
		{RefusedUnknown.String(), "unknown"},
		{Hop{Source: "develop", Target: "main"}.String(), "develop->main"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) { assert.Equal(t, tt.want, tt.got) })
	}
}
