// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"math"
	"strings"
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// TestMalformedReadings_UnderAMetL4 pins that every malformed reading abstains as
// row-absent with the malformed reason, and is never closed under a met L4: a NaN,
// infinite or negative term, a negative people count or remainder, a bound out of range,
// a remainder or people count beyond 2^53, and a count of 2^53 + 2. A count of exactly
// 2^53 is a valid count.
func TestMalformedReadings_UnderAMetL4(t *testing.T) {
	good := withPeople(resolved(10), 6)
	cases := []struct {
		name     string
		num, den Reading
	}{
		{"NaN numerator", withPeople(resolved(math.NaN()), 6), resolved(30)},
		{"infinite denominator", good, resolved(math.Inf(1))},
		{"negative numerator", withPeople(resolved(-5), 6), resolved(30)},
		{"negative people", withPeople(resolved(10), -1), resolved(30)},
		{"negative remainder", good, withNumRemainder(resolved(30), -3)},
		{"NaN bound", withBound(withCoverage(good, CoveragePartial), math.NaN()), resolved(30)},
		{"remainder MaxInt-29", good, withNumRemainder(resolved(30), math.MaxInt-29)},
		{"remainder 1<<62", good, withNumRemainder(resolved(30), 1<<62)},
		{"people beyond 2^53", withPeople(resolved(10), 1<<53+1), resolved(30)},
		{"count 2^53 + 2", good, resolved(1<<53 + 2)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, cell := scoreCodeL1UnderMetL4(tc.num, tc.den)
			if got != "abstain(row_absent)" || cell.GetAbstainKind() != aidlcv1.AbstainKind_ABSTAIN_KIND_ROW_ABSENT {
				t.Fatalf("code/L1 = %s (%v), want abstain(row_absent)", got, cell.GetAbstainKind())
			}
			if cell.GetReason() != reasonMalformed {
				t.Errorf("reason = %q, want the malformed reason", cell.GetReason())
			}
		})
	}
	if got, _ := scoreCodeL1UnderMetL4(good, resolved(1<<53)); got == "abstain(row_absent)" {
		t.Error("a count of exactly 2^53 was rejected")
	}
}

// TestMalformedControl_IsNotClosed pins that a malformed control abstains with the
// malformed reason and is never closed under a met L4.
func TestMalformedControl_IsNotClosed(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(30))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, code, withPeople(resolved(30), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, withPeople(resolved(5), 3))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(math.NaN()))
	if got := vector(Score(rules.ForVersion("v1"), rd, OrgRead{}, nil))["code/L3"]; got != "abstain(row_absent)" {
		t.Errorf("code/L3 = %s, want abstain(row_absent), not closed", got)
	}
}

// TestSecondaryShare_DisagreementBeforeThePopulationSteps pins the secondary share's
// disagreement check: before its zero-population and floor steps, and against the
// counted value only.
func TestSecondaryShare_DisagreementBeforeThePopulationSteps(t *testing.T) {
	for _, tc := range []struct {
		name         string
		covered, met Reading
	}{
		{"5 over 0", resolved(0), withPeople(resolved(5), 5)},
		{"25 over 19", resolved(19), withPeople(resolved(25), 5)},
		{"30 over 29 plus a remainder of 1", withNumRemainder(resolved(29), 1), withPeople(resolved(30), 5)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := scoreReviewL5(rules.ForVersion("v1"), withPeople(resolved(8), 8), tc.covered, tc.met); got != "abstain(coverage)" {
				t.Errorf("review/L5 = %s, want abstain(coverage)", got)
			}
		})
	}
}

// TestNoPopulation_PeopleGrainAtTheCell pins the people-grain wording on a scored cell:
// plan L2 over a roster of none says no one, not merged work.
func TestNoPopulation_PeopleGrainAtTheCell(t *testing.T) {
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(0), 0))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PLANNING_PHASE_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_PLAN, withPeople(resolved(0), 0))
	for _, c := range Score(rules.ForVersion("v1"), rd, OrgRead{}, nil).Cells {
		if c.Stage == aidlcv1.AidlcStage_AIDLC_STAGE_PLAN && c.Level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L2 {
			if !strings.Contains(c.Reason, "no one") || strings.Contains(c.Reason, "merged") {
				t.Errorf("plan/L2 reason = %q, want the people-grain wording", c.Reason)
			}
		}
	}
}
