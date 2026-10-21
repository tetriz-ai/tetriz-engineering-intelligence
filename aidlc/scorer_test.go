// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"fmt"
	"sort"
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// These are scorer goldens: fixed readings authored at the scorer's own input
// granularity (a reading per signal), producing pinned verdict vectors. The verdicts
// are derived by hand from the cell rules for the chosen readings, and the basis is
// stated per fixture, so they prove the scorer applies the rules as written.

// ---- reading builders ---------------------------------------------------------

func resolved(v float64) Reading { return Reading{Status: StatusResolved, Value: f64(v)} }

func iptr(v int) *int { return &v }

func withPeople(r Reading, n int) Reading { r.People = iptr(n); return r }

func withCoverage(r Reading, c Coverage) Reading { r.Coverage = c; return r }

func withNumRemainder(r Reading, n int) Reading {
	r.RemainderDeclared = true
	r.RemainderValue = iptr(n)
	return r
}

func withBound(r Reading, b float64) Reading { r.BoundValue = f64(b); return r }

// vector renders one result as a compact per-cell verdict string ("met" /
// "not_reached" / "abstain(<kind>)"), keyed stage/level, so a moved verdict shows as
// one changed line.
func vector(res *Result) map[string]string {
	out := map[string]string{}
	for _, c := range res.Cells {
		out[fmt.Sprintf("%s/%s", stageKey(c.Stage), levelKey(c.Level))] = compact(c)
	}
	return out
}

func compact(c *Cell) string {
	switch c.State {
	case aidlcv1.CellState_CELL_STATE_MET:
		if c.DecidedBy == aidlcv1.DecidedBy_DECIDED_BY_IMPLIED_FLOOR {
			return "met(floor)"
		}
		switch c.Band {
		case aidlcv1.PrevalenceBand_PREVALENCE_BAND_ESTABLISHED:
			return "met(established)"
		case aidlcv1.PrevalenceBand_PREVALENCE_BAND_EMERGING:
			return "met(emerging)"
		default:
			return "met"
		}
	case aidlcv1.CellState_CELL_STATE_NOT_REACHED:
		return "not_reached"
	case aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE:
		return "abstain(" + abstainName(c.abstain) + ")"
	default:
		return "unspecified"
	}
}

func abstainName(a abstainKind) string {
	switch a {
	case abGap, abNoResolver:
		return "gap"
	case abRowAbsent, abMalformed:
		return "row_absent"
	case abNoPopulation:
		return "no_population"
	case abCoverage, abControlFailed, abReadingsDisagree:
		return "coverage"
	case abPopulationFloor:
		return "population"
	case abJudgment:
		return "judgment"
	case abInstrumentedOnly:
		return "instrumented_only"
	case abAutomatedOnly:
		return "automated_only"
	default:
		return "none"
	}
}

func bottleneckIDs(res *Result) []string {
	var ids []string
	for _, b := range res.Profile.Bottlenecks {
		ids = append(ids, b.Id)
	}
	return ids
}

func stageVerdict(res *Result, stage aidlcv1.AidlcStage) StageVerdict {
	for _, sv := range res.Stages {
		if sv.Stage == stage {
			return sv
		}
	}
	return StageVerdict{}
}

// ---- Fixture A: rich code + review, one stalled stage --------------------------

// TestFixtureA_RichCodeReviewStalledDeploy exercises every headline path in one
// organisation: emerging and established bands, the implied floor under a met rung, a
// measured miss (NOT_REACHED), the baseline control, a stage that stalls, and the
// review_reach / rung / ladder bottleneck derivations. The readings and their derived
// verdicts:
//
//	code:   L1 180/520 = 0.35 emerging; L3 40/180 = 0.22 emerging (control 0.55>0);
//	        L2 floored under L3; L4 35/180 = 0.19 not_reached; L5's enforced-practice
//	        signals unread -> abstain(gap).
//	review: L1 5/8 = 0.63 established; L3 18/24 = 0.75 established; L4 12/40 = 0.30
//	        emerging (authors 3); L2 floored under L3; L5 unread -> abstain(gap).
//	deploy: L1 1/8 = 0.13, L2 25/130 = 0.19, L3 0/25 = 0.00, all not_reached, nothing
//	        met, so the ladder stalls; L4 unread, L5 dark on G7.
//	every other stage: numerators unresolved -> abstain(gap); judgment cells abstain.
//	review_reach fires (code 40/180 = 0.22 < 0.25 -> HIGH); rung_code_L4 and
//	ladder_deploy follow.
func TestFixtureA_RichCodeReviewStalledDeploy(t *testing.T) {
	rs := rules.ForVersion("v1")
	rd := NewReadings()
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY

	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, unspec, withPeople(resolved(8), 8))

	// code
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(180), 8))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(520))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, withPeople(resolved(40), 8))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(180))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, code, withPeople(resolved(35), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(0.55))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_COMPLEXITY_SCORE, code, withCoverage(resolved(7.0), CoveragePartial))

	// review
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS, review, withPeople(resolved(5), 5))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_BOT_PRESENT_PRS, review, resolved(18))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_RULE_APPLICABLE_PRS, review, resolved(24))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_BOT_CLEARED_NO_HUMAN, review, withPeople(resolved(12), 3))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_BOT_SEEN_PRS, review, resolved(40))

	// deploy (stalls): a release is a merge into a shipped-role branch.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_HARNESS_SEAT_HOLDERS, deploy, withPeople(resolved(1), 1))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED, deploy, withPeople(resolved(25), 3))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SHIPPED_BRANCH_SEAT_LINKED_PRS_MERGED, deploy, resolved(130))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED_AFTER_REVIEW, deploy, withPeople(resolved(0), 0))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, deploy, resolved(0.6))

	res := Score(rs, rd, OrgRead{}, nil)
	got := vector(res)

	want := map[string]string{
		"code/L1": "met(emerging)", "code/L2": "met(floor)", "code/L3": "met(emerging)",
		"code/L4": "not_reached", "code/L5": "abstain(gap)",
		"review/L1": "met(established)", "review/L2": "met(floor)", "review/L3": "met(established)",
		"review/L4": "met(emerging)", "review/L5": "abstain(gap)",
		"deploy/L1": "not_reached", "deploy/L2": "not_reached", "deploy/L3": "not_reached",
		"deploy/L4": "abstain(gap)", "deploy/L5": "abstain(gap)",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("cell %s: got %s, want %s", k, got[k], w)
		}
	}

	// sparse stages abstain (gap or judgment), never met or not_reached.
	for k, v := range got {
		if len(k) >= 4 && (k[:4] == "prod" || k[:4] == "tech" || k[:4] == "plan" || k[:4] == "test" || k[:4] == "ci_c" || k[:4] == "oper") {
			if v == "met(established)" || v == "met(emerging)" || v == "not_reached" || v == "met(floor)" {
				t.Errorf("sparse stage cell %s unexpectedly decided: %s", k, v)
			}
		}
	}

	// frontiers.
	if fv := stageVerdict(res, code); fv.Frontier != aidlcv1.AidlcLevel_AIDLC_LEVEL_UNSPECIFIED || fv.EmergingEdge != aidlcv1.AidlcLevel_AIDLC_LEVEL_L3 {
		t.Errorf("code frontier: got frontier=%v edge=%v, want frontier=UNSPECIFIED edge=L3", fv.Frontier, fv.EmergingEdge)
	}
	if rv := stageVerdict(res, review); rv.Frontier != aidlcv1.AidlcLevel_AIDLC_LEVEL_L3 || rv.EmergingEdge != aidlcv1.AidlcLevel_AIDLC_LEVEL_L4 {
		t.Errorf("review frontier: got frontier=%v edge=%v, want frontier=L3 edge=L4", rv.Frontier, rv.EmergingEdge)
	}

	// bottlenecks, ranked: review_reach (HIGH) then the two MEDIUM_HIGH by weight
	// (rung_code_L4 weight 32 > ladder_deploy weight 30).
	gotIDs := bottleneckIDs(res)
	wantIDs := []string{"review_reach", "rung_code_L4", "ladder_deploy"}
	if fmt.Sprint(gotIDs) != fmt.Sprint(wantIDs) {
		t.Errorf("bottleneck ids: got %v, want %v", gotIDs, wantIDs)
	}
	if len(res.Profile.Bottlenecks) > 0 && res.Profile.Bottlenecks[0].Impact != aidlcv1.Impact_IMPACT_HIGH {
		t.Errorf("review_reach impact: got %v, want HIGH", res.Profile.Bottlenecks[0].Impact)
	}
}

// ---- Fixture B: the abstain paths ---------------------------------------------

// TestFixtureB_AbstainPaths pins the four abstains that are not gaps: the census
// floor (a people denominator under 5), the coverage straddle (a widened interval
// spanning the 0.50 cut), the numerator people floor (a banded share behind one
// person), and no_population (an empty denominator with nothing unobserved). Each is
// asserted on the cell whose readings force it.
func TestFixtureB_AbstainPaths(t *testing.T) {
	rs := rules.ForVersion("v1")

	// census floor: ci_cd L1 divides 3 seat-holders by a 4-seat roster (< the
	// people floor of 5) -> abstain(population).
	t.Run("census_floor", func(t *testing.T) {
		rd := NewReadings()
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(4), 4))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CI_TOOL_SEAT_HOLDERS_BY_FUNCTION, aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD, withPeople(resolved(3), 3))
		res := Score(rs, rd, OrgRead{}, nil)
		if v := vector(res)["ci_cd/L1"]; v != "abstain(population)" {
			t.Errorf("census floor: got %s, want abstain(population)", v)
		}
	})

	// coverage straddle: code L1 reads 48 of 180 with 120 unobserved -> interval
	// [0.27, 0.93] spans the 0.50 cut -> abstain(coverage).
	t.Run("coverage_straddle", func(t *testing.T) {
		rd := NewReadings()
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(10), 10))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, withPeople(withNumRemainder(resolved(48), 120), 6))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, resolved(180))
		res := Score(rs, rd, OrgRead{}, nil)
		if v := vector(res)["code/L1"]; v != "abstain(coverage)" {
			t.Errorf("coverage straddle: got %s, want abstain(coverage)", v)
		}
	})

	// numerator people floor: code L1 bands emerging on 60/180 = 0.33 but only one
	// person did it -> abstain(population).
	t.Run("people_floor", func(t *testing.T) {
		rd := NewReadings()
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(10), 10))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, withPeople(resolved(60), 1))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, resolved(180))
		res := Score(rs, rd, OrgRead{}, nil)
		if v := vector(res)["code/L1"]; v != "abstain(population)" {
			t.Errorf("people floor: got %s, want abstain(population)", v)
		}
	})

	// no_population: code L1 divides by an empty, fully-observed denominator ->
	// abstain(no_population).
	t.Run("no_population", func(t *testing.T) {
		rd := NewReadings()
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(10), 10))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, withPeople(resolved(0), 0))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, resolved(0))
		res := Score(rs, rd, OrgRead{}, nil)
		if v := vector(res)["code/L1"]; v != "abstain(no_population)" {
			t.Errorf("no_population: got %s, want abstain(no_population)", v)
		}
	})
}

// TestBandBoundaries pins the two cut points: a share just over 0.50 is established,
// one in (0.20, 0.50] is emerging, one at or under 0.20 is not reached. Full
// coverage so the interval is a point and only the cut decides.
func TestBandBoundaries(t *testing.T) {
	rs := rules.ForVersion("v1")
	cases := []struct {
		hits, pop float64
		want      string
	}{
		{102, 200, "met(established)"}, // 0.51
		{60, 200, "met(emerging)"},     // 0.30
		{40, 200, "not_reached"},       // 0.20 -> hi<=0.20 not_established
	}
	for _, tc := range cases {
		rd := NewReadings()
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(10), 10))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, withPeople(resolved(tc.hits), 4))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, resolved(tc.pop))
		res := Score(rs, rd, OrgRead{}, nil)
		if v := vector(res)["code/L1"]; v != tc.want {
			t.Errorf("hits=%v pop=%v: got %s, want %s", tc.hits, tc.pop, v, tc.want)
		}
	}
}

// TestControlAbstains pins the baseline control: an L3 rung whose non-agent review
// rate is zero abstains rather than denying, even on a high agent-review share, and
// an unresolvable control abstains too. Code L3 shares an established interval so only
// the control decides the abstain.
func TestControlAbstains(t *testing.T) {
	rs := rules.ForVersion("v1")
	base := func(ctrl *float64) *Result {
		rd := NewReadings()
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(10), 10))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, withPeople(resolved(150), 6))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, resolved(200))
		if ctrl != nil {
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, resolved(*ctrl))
		}
		return Score(rs, rd, OrgRead{}, nil)
	}
	if v := vector(base(f64(0)))["code/L3"]; v != "abstain(coverage)" { // control_failed -> COVERAGE
		t.Errorf("zero control: got %s, want abstain(coverage)", v)
	}
	if v := vector(base(nil))["code/L3"]; v != "abstain(gap)" { // unresolvable control -> no_resolver -> GAP
		t.Errorf("unresolved control: got %s, want abstain(gap)", v)
	}
	if v := vector(base(f64(0.4)))["code/L3"]; v != "met(established)" {
		t.Errorf("live control: got %s, want met(established)", v)
	}
}

// TestInstrumentedOnlyZero pins that a zero on a presence numerator that only sees an
// instrumented artifact abstains (instrumented_only), never denies.
func TestInstrumentedOnlyZero(t *testing.T) {
	rs := rules.ForVersion("v1")
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(10), 10))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, withPeople(resolved(0), 0))
	res := Score(rs, rd, OrgRead{}, nil)
	if v := vector(res)["review/L1"]; v != "abstain(instrumented_only)" {
		t.Errorf("instrumented-only zero: got %s, want abstain(instrumented_only)", v)
	}
}

// TestClosureBreaksOnMeasuredMiss pins that downward closure does not floor a rung
// beneath a NOT_REACHED cell, and does not fill a rung ABOVE the frontier: a met L3
// over a not_reached L2-equivalent stays put. Here code L1 is emerging-met, L3 is
// not reached, so L2's implied floor is NOT awarded (no met rung above it on the
// stage after L1 — L1 is below L2).
func TestClosureBreaksOnMeasuredMiss(t *testing.T) {
	rs := rules.ForVersion("v1")
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(10), 10))
	// code L1 emerging, L3/L4 not decided (numerators absent -> abstain gap), so
	// nothing above L2 is met and L2 stays an abstain, not a floor.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, withPeople(resolved(60), 4))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, resolved(180))
	res := Score(rs, rd, OrgRead{}, nil)
	got := vector(res)
	if got["code/L1"] != "met(emerging)" {
		t.Errorf("code/L1: got %s, want met(emerging)", got["code/L1"])
	}
	if got["code/L2"] != "abstain(gap)" {
		t.Errorf("code/L2 must stay an abstain with no met rung above it, got %s", got["code/L2"])
	}
}

// TestDeterministic pins that scoring the same readings twice yields the same
// vector — the scorer holds no order-dependent state.
func TestDeterministic(t *testing.T) {
	rs := rules.ForVersion("v1")
	build := func() *Readings {
		rd := NewReadings()
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(8), 8))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, withPeople(resolved(180), 8))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, resolved(520))
		return rd
	}
	a := keys(vector(Score(rs, build(), OrgRead{}, nil)))
	b := keys(vector(Score(rs, build(), OrgRead{}, nil)))
	if fmt.Sprint(a) != fmt.Sprint(b) {
		t.Errorf("non-deterministic scoring:\n%v\n%v", a, b)
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// TestShareCountsDescribeTheInterval pins the stored share triple to the interval it
// stands behind: population is the denominator reading plus its own unread remainder (the
// number lo and hi divide by), and hits + unobserved never exceeds it, so the stored
// counts always describe the stored interval even when a denominator carries a
// remainder and the unread mass is widened up to the whole gap.
func TestShareCountsDescribeTheInterval(t *testing.T) {
	rs := rules.ForVersion("v1")
	cases := []struct {
		name           string
		num            Reading
		den            Reading
		wantPop        int64
		wantUnobserved int64
		wantLo, wantHi float64
	}{
		{
			name:           "denominator remainder joins the population",
			num:            withPeople(resolved(60), 4),
			den:            withNumRemainder(resolved(200), 50),
			wantPop:        250,
			wantUnobserved: 50,
			wantLo:         0.24, wantHi: 0.44,
		},
		{
			name:           "partial numerator coverage widens the unread mass but never past the population",
			num:            withCoverage(withPeople(resolved(60), 4), CoveragePartial),
			den:            withNumRemainder(resolved(200), 50),
			wantPop:        250,
			wantUnobserved: 190, // clamped to population - hits, not 50 + (250 - 60) = 240
			wantLo:         0.24, wantHi: 1,
		},
		{
			name:           "a declared numerator remainder over the gap is clamped",
			num:            withNumRemainder(withPeople(resolved(60), 4), 500),
			den:            resolved(200),
			wantPop:        200,
			wantUnobserved: 140,
			wantLo:         0.3, wantHi: 1,
		},
		{
			name:           "a partial bound converts to a count and is clamped too",
			num:            withBound(withCoverage(withPeople(resolved(180), 4), CoveragePartial), 0.5),
			den:            withNumRemainder(resolved(200), 20),
			wantPop:        220,
			wantUnobserved: 40, // 20 + widened 110 = 130, clamped to 220 - 180
			wantLo:         0.8182, wantHi: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rd := NewReadings()
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(10), 10))
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, tc.num)
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, tc.den)
			res := Score(rs, rd, OrgRead{}, nil)
			var cell *Cell
			for _, c := range res.Cells {
				if c.Stage == aidlcv1.AidlcStage_AIDLC_STAGE_CODE && c.Level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L1 {
					cell = c
				}
			}
			if cell == nil || cell.Share == nil {
				t.Fatalf("code/L1 carries no share: %+v", cell)
			}
			s := cell.Share
			if s.Population != tc.wantPop {
				t.Errorf("population: got %d, want %d", s.Population, tc.wantPop)
			}
			if s.Unobserved != tc.wantUnobserved {
				t.Errorf("unobserved: got %d, want %d", s.Unobserved, tc.wantUnobserved)
			}
			if s.Hits+s.Unobserved > s.Population {
				t.Errorf("hits %d + unobserved %d exceed population %d", s.Hits, s.Unobserved, s.Population)
			}
			if s.Lo != tc.wantLo || s.Hi != tc.wantHi {
				t.Errorf("interval: got [%v, %v], want [%v, %v]", s.Lo, s.Hi, tc.wantLo, tc.wantHi)
			}
			if got := round4(float64(s.Hits) / float64(s.Population)); got != s.Lo {
				t.Errorf("hits/population %v does not reproduce lo %v", got, s.Lo)
			}
			if got := round4(float64(s.Hits+s.Unobserved) / float64(s.Population)); got != s.Hi {
				t.Errorf("(hits+unobserved)/population %v does not reproduce hi %v", got, s.Hi)
			}
		})
	}
}

// A numerator above its population is two readings disagreeing; the cell abstains on
// coverage instead of storing a share whose lower bound exceeds one.
func TestShareNumeratorAbovePopulationAbstains(t *testing.T) {
	rs := rules.ForVersion("v1")
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(10), 10))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, withPeople(resolved(250), 4))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, resolved(200))
	if v := vector(Score(rs, rd, OrgRead{}, nil))["code/L1"]; v != "abstain(coverage)" {
		t.Errorf("got %s, want abstain(coverage)", v)
	}
}
