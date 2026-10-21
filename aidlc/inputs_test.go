// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"math"
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// scoreReviewReach scores the code row with a fixed AI delivery population of the given
// size and the given reviewed-count reading, with code L3's review control readable.
func scoreReviewReach(population float64, reviewed Reading) *Result {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(population))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, reviewed)
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(0.9))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(population), 10))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(population+20))
	return Score(rules.ForVersion("v1"), rd, OrgRead{}, nil)
}

func hasBottleneck(res *Result, id string) bool {
	for _, b := range res.Bottlenecks {
		if b.ID == id {
			return true
		}
	}
	return false
}

// TestReviewReach_OnlyOnARecordedShare pins that review reach reads the share code L3
// recorded, by its widened upper bound: it fires on a decided share and on a straddle
// whose upper bound is under half, and not when partial coverage widens the bound to one
// or when L3's readings recorded no share at all.
func TestReviewReach_OnlyOnARecordedShare(t *testing.T) {
	cases := []struct {
		name     string
		reviewed Reading
		wantL3   string
		wantFire bool
	}{
		{"decided: 5 of 40 reviewed", withPeople(resolved(5), 4), "not_reached", true},
		{"partial coverage with a bound", withBound(withCoverage(withPeople(resolved(5), 4), CoveragePartial), 0.1), "abstain(coverage)", false},
		{"straddle with its upper bound under half", withNumRemainder(withPeople(resolved(5), 4), 10), "abstain(coverage)", true},
		{"row absent", Reading{Status: StatusRowAbsent, Value: f64(0)}, "abstain(row_absent)", false},
		{"gap blocked", Reading{Status: StatusGapBlocked, Value: f64(0)}, "abstain(gap)", false},
		{"declared remainder with no value", Reading{Status: StatusResolved, Value: f64(5), People: iptr(4), RemainderDeclared: true}, "abstain(gap)", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := scoreReviewReach(40, tc.reviewed)
			if got := vector(res)["code/L3"]; got != tc.wantL3 {
				t.Fatalf("code/L3 = %s, want %s", got, tc.wantL3)
			}
			if got := hasBottleneck(res, bottleneckIDReviewReach); got != tc.wantFire {
				t.Errorf("review_reach fired = %v, want %v", got, tc.wantFire)
			}
		})
	}
}

// TestReviewReach_FloorAtExactlyTwenty pins the population floor as inclusive: 20 AI
// delivery pull requests are enough to raise review reach.
func TestReviewReach_FloorAtExactlyTwenty(t *testing.T) {
	if !hasBottleneck(scoreReviewReach(20, withPeople(resolved(2), 2)), bottleneckIDReviewReach) {
		t.Error("review_reach did not fire over exactly 20 AI delivery pull requests")
	}
}

// TestCoverage_UnknownAndNoneWiden pins that a reading whose coverage is unknown, or
// that stands for nothing, widens by every non-hit: a zero under either is not a
// measured zero, so code L1 abstains on coverage rather than reading not reached.
func TestCoverage_UnknownAndNoneWiden(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	for _, cov := range []struct {
		name string
		c    Coverage
	}{{"unknown", CoverageUnknown}, {"none", CoverageNone}} {
		t.Run(cov.name, func(t *testing.T) {
			rd := NewReadings()
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(40))
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withCoverage(withPeople(resolved(0), 0), cov.c))
			if got := vector(Score(rules.ForVersion("v1"), rd, OrgRead{}, nil))["code/L1"]; got != "abstain(coverage)" {
				t.Errorf("code/L1 = %s, want abstain(coverage)", got)
			}
		})
	}
}

// TestInvalidReadings_AbstainRowAbsent pins that a reading whose numbers cannot describe
// a count abstains row-absent instead of being scored: a value that is NaN, infinite
// or negative, a negative people count or remainder, a coverage bound outside [0, 1].
func TestInvalidReadings_AbstainRowAbsent(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	good := withPeople(resolved(10), 5)
	cases := []struct {
		name     string
		num, den Reading
	}{
		{"NaN numerator", withPeople(resolved(math.NaN()), 5), resolved(40)},
		{"infinite numerator", withPeople(resolved(math.Inf(1)), 5), resolved(40)},
		{"negative numerator", withPeople(resolved(-5), 5), resolved(40)},
		{"NaN denominator", good, resolved(math.NaN())},
		{"infinite denominator", good, resolved(math.Inf(1))},
		{"negative people", withPeople(resolved(10), -1), resolved(40)},
		{"negative remainder", withNumRemainder(good, -100), resolved(40)},
		{"NaN bound", withBound(withCoverage(good, CoveragePartial), math.NaN()), resolved(40)},
		{"bound above one", withBound(withCoverage(good, CoveragePartial), 2), resolved(40)},
		{"negative bound", withBound(withCoverage(good, CoveragePartial), -0.1), resolved(40)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rd := NewReadings()
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, tc.num)
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, tc.den)
			if got := vector(Score(rules.ForVersion("v1"), rd, OrgRead{}, nil))["code/L1"]; got != "abstain(row_absent)" {
				t.Errorf("code/L1 = %s, want abstain(row_absent)", got)
			}
		})
	}
}

// TestPrimaryShare_StaleDenominatorRemainderAbstains pins step 2: a population that
// declares a remainder and carries none is a stale run, and the cell abstains.
func TestPrimaryShare_StaleDenominatorRemainderAbstains(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(30), 5))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, Reading{Status: StatusResolved, Value: f64(40), RemainderDeclared: true})
	if got := vector(Score(rules.ForVersion("v1"), rd, OrgRead{}, nil))["code/L1"]; got != "abstain(gap)" {
		t.Errorf("code/L1 = %s, want abstain(gap): a stale denominator remainder", got)
	}
}

// TestSecondaryShare_Guards pins the secondary share's guards on review L5, with the
// primary (enforced practice) share established: a stale remainder on either term
// abstains, a population under the 20 floor abstains, and a count above its population
// abstains.
func TestSecondaryShare_Guards(t *testing.T) {
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	stale := func(v float64, people int) Reading {
		r := withPeople(resolved(v), people)
		r.RemainderDeclared = true
		return r
	}
	cases := []struct {
		name         string
		covered, met Reading
		want         string
	}{
		{"control: both established", resolved(40), withPeople(resolved(30), 5), "met(established)"},
		{"stale covered remainder", stale(40, 0), withPeople(resolved(30), 5), "abstain(gap)"},
		{"stale met remainder", resolved(40), stale(30, 5), "abstain(gap)"},
		{"population under the floor", resolved(19), withPeople(resolved(15), 5), "abstain(population)"},
		{"count above its population", resolved(20), withPeople(resolved(25), 5), "abstain(coverage)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rd := NewReadings()
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, unspec, withPeople(resolved(10), 10))
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, review, resolved(10))
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, review, withPeople(resolved(8), 8))
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS, unspec, tc.covered)
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS, unspec, tc.met)
			if got := vector(Score(rules.ForVersion("v1"), rd, OrgRead{}, nil))["review/L5"]; got != tc.want {
				t.Errorf("review/L5 = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestLadder_KeepsTheTwoWithTheMostMisses pins that of the stalled stages (misses and
// nothing met) only the two with the most misses are raised.
func TestLadder_KeepsTheTwoWithTheMostMisses(t *testing.T) {
	rd := NewReadings()
	zero := withPeople(resolved(0), 0)
	pairs := []struct {
		stage    aidlcv1.AidlcStage
		num, den aidlcv1.SignalKey
	}{
		{aidlcv1.AidlcStage_AIDLC_STAGE_CODE, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS},
		{aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_CI_PRS_MERGED, aidlcv1.SignalKey_SIGNAL_KEY_CI_CD_SEAT_LINKED_ARTIFACTS},
		{aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MONITORING_PRS_MERGED, aidlcv1.SignalKey_SIGNAL_KEY_OPERATE_SEAT_LINKED_ARTIFACTS},
		{aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_DESIGN_DOC_PRS_MERGED, aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_SEAT_LINKED_ARTIFACTS},
	}
	for _, p := range pairs {
		rd.Set(p.num, p.stage, zero)
		rd.Set(p.den, p.stage, resolved(40))
	}
	// tech spec L2 reads the non-AI review control too; leave it readable.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC, resolved(0.5))
	res := Score(rules.ForVersion("v1"), rd, OrgRead{}, nil)

	stalled := 0
	for _, sv := range res.Stages {
		met, miss := 0, 0
		for _, c := range sv.Cells {
			switch c.State {
			case aidlcv1.CellState_CELL_STATE_MET:
				met++
			case aidlcv1.CellState_CELL_STATE_NOT_REACHED:
				miss++
			}
		}
		if met == 0 && miss > 0 {
			stalled++
		}
	}
	if stalled < 3 {
		t.Fatalf("fixture: %d stalled stages, want at least 3 so the cap is exercised", stalled)
	}
	ladders := 0
	for _, b := range res.Bottlenecks {
		if len(b.ID) > len(bottleneckIDPrefixLadder) && b.ID[:len(bottleneckIDPrefixLadder)] == bottleneckIDPrefixLadder {
			ladders++
		}
	}
	if ladders != 2 {
		t.Errorf("ladder bottlenecks = %d, want 2 of %d stalled stages", ladders, stalled)
	}
}

// TestCheckPreconditions_UnreadableCoverageWithholds pins that a path coverage that is
// not a share between 0 and 1 fails the path-coverage check.
func TestCheckPreconditions_UnreadableCoverageWithholds(t *testing.T) {
	for _, pc := range []float64{1.5, -0.1, math.NaN(), math.Inf(1)} {
		_, withheld := CheckPreconditions(rules.ForVersion("v1"), Preconditions{PathCoverage: pc, MergedPRCount: 50})
		if withheld != aidlcv1.PreconditionKey_PRECONDITION_KEY_PATH_COVERAGE {
			t.Errorf("PathCoverage %v: withheld by %v, want PATH_COVERAGE", pc, withheld)
		}
	}
}
