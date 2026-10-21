// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"testing"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// TestScore_TerminatesAtACutOfOne pins that a ruleset whose established cut is 1.0
// still scores: no number of additional merged pull requests lifts (h+x)/(p+x)
// strictly above one, so the review-reach distance must be "none", not a search that
// never ends.
func TestScore_TerminatesAtACutOfOne(t *testing.T) {
	rs := *rules.ForVersion("v1")
	rs.BandCuts.Established = 1.0
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(40), 5))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(40))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, withPeople(resolved(10), 5))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(0.5)) // code L3's control

	done := make(chan *Result, 1)
	go func() { done <- Score(&rs, rd, OrgRead{}, nil) }()
	select {
	case res := <-done:
		var found bool
		for _, b := range res.Profile.GetBottlenecks() {
			if b.GetId() != bottleneckIDReviewReach {
				continue
			}
			found = true
			if b.GetProgress().ItemsToTarget != nil {
				t.Errorf("items_to_target = %d, want unset: no count of items clears a line of one", b.GetProgress().GetItemsToTarget())
			}
		}
		if !found {
			t.Fatal("review reach did not fire on 10 reviewed of 40")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Score did not return within 5s with BandCuts.Established = 1.0")
	}
}

// TestItemsToClear_NoDistanceAtOrAboveOne pins the distance at a cut of one or more:
// merged changes can never rise strictly above it, so the answer is zero (no
// distance), which the move copy renders by leaving the distance sentence out.
func TestItemsToClear_NoDistanceAtOrAboveOne(t *testing.T) {
	mc := aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES
	for _, cut := range []float64{1.0, 1.5} {
		done := make(chan int64, 1)
		go func() { done <- itemsToClear(MoveTerms{Hits: 10, Population: 40, Grain: mc}, cut) }()
		select {
		case got := <-done:
			if got != 0 {
				t.Errorf("itemsToClear at cut %v = %d, want 0", cut, got)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("itemsToClear at cut %v did not return", cut)
		}
	}
}

// TestCell_AbstainKind pins the exported kind: the published abstain kind on an
// abstaining cell, and UNSPECIFIED on a decided one.
func TestCell_AbstainKind(t *testing.T) {
	res := Score(rules.ForVersion("v1"), NewReadings(), OrgRead{}, nil)
	for _, c := range res.Cells {
		if c.Stage == aidlcv1.AidlcStage_AIDLC_STAGE_PLAN && c.Level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L1 {
			if got := c.AbstainKind(); got != aidlcv1.AbstainKind_ABSTAIN_KIND_JUDGMENT {
				t.Errorf("plan/L1 abstain kind = %v, want JUDGMENT", got)
			}
		}
	}
	decided := &Cell{State: aidlcv1.CellState_CELL_STATE_MET, abstain: abCoverage}
	if got := decided.AbstainKind(); got != aidlcv1.AbstainKind_ABSTAIN_KIND_UNSPECIFIED {
		t.Errorf("a met cell's abstain kind = %v, want UNSPECIFIED", got)
	}
}
