// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// scoreCodeL1UnderMetL4 scores code L1 (the given numerator over the given denominator)
// with code L4 met (30 of 30 low-turn by six people), and returns the scored code L1 cell
// and its published cell.
func scoreCodeL1UnderMetL4(num, den Reading) (string, *aidlcv1.MaturityCell) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, num)
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, den)
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(30))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, code, withPeople(resolved(30), 6))
	res := Score(rules.ForVersion("v1"), rd, OrgRead{}, nil)
	if got := vector(res)["code/L4"]; got != "met(established)" {
		panic("fixture: code/L4 = " + got)
	}
	for _, st := range res.Profile.GetStages() {
		if st.GetStage() == code {
			return vector(res)["code/L1"], st.GetCells()[0]
		}
	}
	return "", nil
}

// TestDisagreement_CheckedBeforeThePopulationSteps pins that a count above the
// population's counted value is a disagreement, published as coverage and never closed
// under a met L4: over an empty population, over one under its floor, over one whose
// remainder would make up the difference, and one past the boundary. At the boundary
// itself the share is decided.
func TestDisagreement_CheckedBeforeThePopulationSteps(t *testing.T) {
	cases := []struct {
		name     string
		num, den Reading
		want     string
	}{
		{"30 over 0", withPeople(resolved(30), 6), resolved(0), "abstain(coverage)"},
		{"30 over 10", withPeople(resolved(30), 6), resolved(10), "abstain(coverage)"},
		{"30 over 29 plus a remainder of 1", withPeople(resolved(30), 6), withNumRemainder(resolved(29), 1), "abstain(coverage)"},
		{"31 over 30", withPeople(resolved(31), 6), resolved(30), "abstain(coverage)"},
		{"30 over 30", withPeople(resolved(30), 6), resolved(30), "met(established)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, cell := scoreCodeL1UnderMetL4(tc.num, tc.den)
			if got != tc.want {
				t.Fatalf("code/L1 = %s, want %s", got, tc.want)
			}
			if tc.want == "abstain(coverage)" && cell.GetAbstainKind() != aidlcv1.AbstainKind_ABSTAIN_KIND_COVERAGE {
				t.Errorf("published abstain kind = %v, want COVERAGE", cell.GetAbstainKind())
			}
		})
	}
}

// TestMalformedCounts_AreNeverClosed pins that a count term that cannot be a count (a
// fraction however small, a value beyond 2^53) abstains as row-absent and is never
// awarded by closure under a met L4.
func TestMalformedCounts_AreNeverClosed(t *testing.T) {
	for _, den := range []float64{30.5, 20.0000001, 1e19} {
		got, cell := scoreCodeL1UnderMetL4(withPeople(resolved(10), 6), resolved(den))
		if got != "abstain(row_absent)" || cell.GetAbstainKind() != aidlcv1.AbstainKind_ABSTAIN_KIND_ROW_ABSENT {
			t.Errorf("denominator %v: code/L1 = %s (%v), want abstain(row_absent)", den, got, cell.GetAbstainKind())
		}
	}
}

// TestClosure_AwardsAnUndecidedCellBeneathAMetOne pins the over-crediting closure
// allows: code L3, which abstains because its control (the non-AI review rate) was never
// read, is met as an implied floor beneath a met L4.
func TestClosure_AwardsAnUndecidedCellBeneathAMetOne(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(30))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, code, withPeople(resolved(30), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, withPeople(resolved(5), 3))
	if got := vector(Score(rules.ForVersion("v1"), rd, OrgRead{}, nil))["code/L3"]; got != "met(floor)" {
		t.Errorf("code/L3 = %s, want met(floor): closure awards a cell that abstained for a missing control", got)
	}
}

// TestPeopleFloor_NilCountOnAMetShareAbstains pins that a share that would be met with
// no people count abstains (closable, as a gap) when nothing above it is met.
func TestPeopleFloor_NilCountOnAMetShareAbstains(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(30))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, resolved(30))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(0.8))
	if got := vector(Score(rules.ForVersion("v1"), rd, OrgRead{}, nil))["code/L3"]; got != "abstain(gap)" {
		t.Errorf("code/L3 = %s, want abstain(gap)", got)
	}
}

// scoreReviewL5 scores review L5 over the given ruleset, primary share and policy reads.
func scoreReviewL5(rs *rules.RuleSet, using, covered, met Reading) string {
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, unspec, withPeople(resolved(10), 10))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, review, resolved(10))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, review, using)
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS, unspec, covered)
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS, unspec, met)
	return vector(Score(rs, rd, OrgRead{}, nil))["review/L5"]
}

// TestSecondaryShare_ReadBeforeThePeopleFloor pins that the people floor runs after
// the secondary share: a primary share read by too few people, which the secondary
// denies, is not reached rather than an abstain.
func TestSecondaryShare_ReadBeforeThePeopleFloor(t *testing.T) {
	if got := scoreReviewL5(rules.ForVersion("v1"), withPeople(resolved(8), 1), resolved(40), withPeople(resolved(4), 2)); got != "not_reached" {
		t.Errorf("review/L5 = %s, want not_reached", got)
	}
}

// TestSecondaryShare_FractionalCountIsMalformed pins that the secondary share's count
// terms are checked as counts too.
func TestSecondaryShare_FractionalCountIsMalformed(t *testing.T) {
	if got := scoreReviewL5(rules.ForVersion("v1"), withPeople(resolved(8), 8), resolved(40.5), withPeople(resolved(30), 5)); got != "abstain(row_absent)" {
		t.Errorf("review/L5 = %s, want abstain(row_absent)", got)
	}
}

// TestSecondaryShare_PeopleGrainFloor pins the secondary share's floor by grain with a
// hand-built ruleset whose policy population is people: 10 people clear the 5-person
// floor that 10 merged pull requests would not.
func TestSecondaryShare_PeopleGrainFloor(t *testing.T) {
	rs := rules.ForVersion("v1")
	def := rs.Signals[aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS]
	def.Grain = aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE
	rs.Signals[aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS] = def
	if got := scoreReviewL5(rs, withPeople(resolved(8), 8), resolved(10), withPeople(resolved(8), 5)); got != "met(established)" {
		t.Errorf("review/L5 = %s, want met(established) over a people-grain policy population of 10", got)
	}
}
