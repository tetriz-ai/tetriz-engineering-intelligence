// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"strings"
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// scoreCodeUnderMetL4 scores the code row with L4 met (30 of 30 low-turn by six people)
// and the given code L3 numerator over 30 AI delivery pull requests.
func scoreCodeUnderMetL4(reviewed Reading) map[string]string {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(40))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(30), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(30))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, code, withPeople(resolved(30), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, reviewed)
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(0.8))
	return vector(Score(rules.ForVersion("v1"), rd, OrgRead{}, nil))
}

// TestClosure_NilPeopleMeasuredMissStaysAMiss pins that a measured miss is not reached
// whatever the people count: 0 of 30 reviewed with no people count is a miss, and
// closure under a met L4 does not award it.
func TestClosure_NilPeopleMeasuredMissStaysAMiss(t *testing.T) {
	got := scoreCodeUnderMetL4(resolved(0))
	if got["code/L4"] != "met(established)" {
		t.Fatalf("fixture: code/L4 = %s, want met(established)", got["code/L4"])
	}
	if got["code/L3"] != "not_reached" {
		t.Errorf("code/L3 = %s, want not_reached: a measured miss with no people count", got["code/L3"])
	}
}

// TestClosure_ContradictingReadingsAreNotAwarded pins that a count above its own
// population (90 reviewed of 30) abstains on coverage and is never awarded by closure.
func TestClosure_ContradictingReadingsAreNotAwarded(t *testing.T) {
	got := scoreCodeUnderMetL4(withPeople(resolved(90), 6))
	if got["code/L3"] != "abstain(coverage)" {
		t.Errorf("code/L3 = %s, want abstain(coverage), not closed to met", got["code/L3"])
	}
}

// TestShare_FractionalCountIsUnreadable pins that a count term that is not a whole
// number abstains row-absent instead of being truncated: 0.75 seat holders of 20.
func TestShare_FractionalCountIsUnreadable(t *testing.T) {
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(20), 20))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, withPeople(resolved(0.75), 1))
	if got := vector(Score(rules.ForVersion("v1"), rd, OrgRead{}, nil))["review/L1"]; got != "abstain(row_absent)" {
		t.Errorf("review/L1 = %s, want abstain(row_absent)", got)
	}
}

// TestReasonPopulationFloor_NamesTheGrain pins that a people-grain floor says people,
// and a merged-change floor says pieces of work.
func TestReasonPopulationFloor_NamesTheGrain(t *testing.T) {
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(4), 4))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, withPeople(resolved(2), 2))
	res := Score(rules.ForVersion("v1"), rd, OrgRead{}, nil)
	for _, c := range res.Cells {
		if c.Stage == aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW && c.Level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L1 {
			if !strings.Contains(c.Reason, "Fewer than 5 people") || strings.Contains(c.Reason, "pieces of this work") {
				t.Errorf("people-grain floor reason = %q, want it to say people", c.Reason)
			}
		}
	}
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd = NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(10), 5))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(19))
	for _, c := range Score(rules.ForVersion("v1"), rd, OrgRead{}, nil).Cells {
		if c.Stage == code && c.Level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L1 && !strings.Contains(c.Reason, "Fewer than 20 pieces of this work") {
			t.Errorf("merged-change floor reason = %q", c.Reason)
		}
	}
}

// TestSummarise_ClaimsOnlyAboutMeasuredStages pins that the summary never reads a stage
// nothing measured as a stage measured low: with one stage measured and eight with no
// decided cell, it scopes its claim to the measured stage and names the other eight.
func TestSummarise_ClaimsOnlyAboutMeasuredStages(t *testing.T) {
	var stages []StageVerdict
	for _, s := range stageOrder {
		stages = append(stages, StageVerdict{Stage: s, AllAbstained: s != aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW})
	}
	unmeasured := "Nothing could be measured in Product spec, Tech spec, Plan, Code, Test & QA, CI/CD, Deploy and Operate."
	if got, want := summarise(stages), "None of the stages that could be measured shows AI in a real share of its work over this window yet. "+unmeasured; got != want {
		t.Errorf("nothing met:\n got %q\nwant %q", got, want)
	}
	for i := range stages {
		if stages[i].Stage == aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW {
			stages[i].EmergingEdge = aidlcv1.AidlcLevel_AIDLC_LEVEL_L1
		}
	}
	want := "No stage that could be measured is established yet. Review is moving toward a way of working where a person does the work and an assistant helps on request. " + unmeasured
	if got := summarise(stages); got != want {
		t.Errorf("emerging only:\n got %q\nwant %q", got, want)
	}
}

// TestSummarise_UnmeasuredFromScore pins the same through Score: review L1 read on one
// person, every other stage unread.
func TestSummarise_UnmeasuredFromScore(t *testing.T) {
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(20), 20))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, withPeople(resolved(1), 1))
	got := Score(rules.ForVersion("v1"), rd, OrgRead{}, nil).Profile.GetSummary()
	if got == "None of your stages shows AI in a real share of its work over this window yet." ||
		!strings.Contains(got, "Nothing could be measured in Product spec") {
		t.Errorf("summary = %q, want it scoped to the measured stage and naming the rest", got)
	}
}

// TestForVersion_ReturnsAnIndependentCopy pins that changing a returned ruleset never
// changes another caller's: the next ForVersion still carries the pinned values, hash
// and scoring.
func TestForVersion_ReturnsAnIndependentCopy(t *testing.T) {
	rs := rules.ForVersion("v1")
	rs.BandCuts.Established = 0.1
	rs.PopulationFloors.People = 1
	codeL1 := rules.CellKey{Stage: aidlcv1.AidlcStage_AIDLC_STAGE_CODE, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L1}
	rs.Cells[codeL1].Checks[1].PeopleMin = 99 // the check slice itself, not a copy of the rule
	delete(rs.Signals, aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS)
	rs.Bottlenecks[0].HasMove = false
	rs.GapRemediation[aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G1] = rules.RemediationClassUnspecified

	fresh := rules.ForVersion("v1")
	if fresh.BandCuts.Established != 0.50 || fresh.PopulationFloors.People != 5 || len(fresh.Cells) != 45 {
		t.Errorf("a changed copy leaked into the registry: %+v %+v %d cells", fresh.BandCuts, fresh.PopulationFloors, len(fresh.Cells))
	}
	if _, ok := fresh.Signals[aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS]; !ok {
		t.Error("a deleted signal leaked into the registry")
	}
	if got := fresh.Cells[codeL1].Checks[1].PeopleMin; got != 2 {
		t.Errorf("code L1 people floor = %d, want 2: a changed check leaked into the registry", got)
	}
	if !fresh.Bottlenecks[0].HasMove {
		t.Error("a changed bottleneck rule leaked into the registry")
	}
	if fresh.GapRemediation[aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G1] == rules.RemediationClassUnspecified {
		t.Error("a changed gap remediation leaked into the registry")
	}
	if fresh.Hash() != "ab2ef21c68461214dcadf8d699cb95ad9cd56530b14c13980a321f931ccb1fa3" {
		t.Errorf("hash = %s", fresh.Hash())
	}
}

// BenchmarkForVersion measures the deep copy ForVersion makes per call.
func BenchmarkForVersion(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = rules.ForVersion("v1")
	}
}
