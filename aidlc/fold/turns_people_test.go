// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"fmt"
	"testing"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
)

// thirtyAIDeliveryPRs is 30 AI-authored, seat-linked FEATURE pull requests by ten
// people, each with a file list that touches no test.
func thirtyAIDeliveryPRs() ([]MergedPR, Attribution, []string) {
	var prs []MergedPR
	authored := map[string]struct{}{}
	var roster []string
	for p := 0; p < 10; p++ {
		roster = append(roster, fmt.Sprintf("person-%d", p))
	}
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("pr-%d", i)
		author := roster[i%10]
		prs = append(prs, MergedPR{
			PRKey: key, AuthorIdentityID: &author, AuthorSeatLinked: true, BaseBranch: "main",
			WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE,
			Paths:    []string{"src/app/handler.go"}, HasFileList: true,
			SubmittedReview: true, ReviewersKnown: true,
		})
		authored[key] = struct{}{}
	}
	return prs, Attribution{AuthoredPRKeys: authored}, roster
}

// TestFoldMerged_MissingTurnCountIsUnread pins that an AI delivery pull request absent
// from HumanTurns is unread on the low-turn levels, never counted as high-turn: 3 of 30
// carry a count, so the other 27 are the code L4 numerator's remainder and the test & QA
// low-turn universe's, and code L4 abstains rather than reading not reached.
func TestFoldMerged_MissingTurnCountIsUnread(t *testing.T) {
	prs, attr, roster := thirtyAIDeliveryPRs()
	turns := map[string]int{"pr-0": 2, "pr-1": 2, "pr-2": 2, "pr-3": 0} // a zero is unread too
	r := Fold(Facts{Roster: roster, MergedPRs: prs, Attribution: attr, HumanTurns: turns})

	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	low := mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, code)
	if v := valueOf(t, low); v != 3 || remainderOf(low) != 27 {
		t.Errorf("low-turn = %v rem %d, want 3 rem 27", v, remainderOf(low))
	}
	uni := mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_TEST_QA_AGENT_LOW_TURN_UNIVERSE, aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA)
	if v := valueOf(t, uni); v != 3 || remainderOf(uni) != 27 {
		t.Errorf("test & QA low-turn universe = %v rem %d, want 3 rem 27", v, remainderOf(uni))
	}
	res := aidlc.Score(rules.ForVersion("v1"), r.Readings, r.Org, nil)
	for _, c := range res.Cells {
		if c.Stage == code && c.Level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L4 && c.State == aidlcv1.CellState_CELL_STATE_NOT_REACHED {
			t.Error("code/L4 not reached on 27 unread turn counts")
		}
	}
}

// TestFold_ZeroNumeratorHasZeroPeople pins that a numerator that counted nothing
// carries zero people, a measured zero, so a people-floored cell reads not reached:
// test & QA L2 with 0 of 30 AI delivery pull requests touching a test.
func TestFold_ZeroNumeratorHasZeroPeople(t *testing.T) {
	prs, attr, roster := thirtyAIDeliveryPRs()
	r := Fold(Facts{Roster: roster, MergedPRs: prs, Attribution: attr})
	testQA := aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA
	num := mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_TOUCHING_TESTS, testQA)
	if num.People == nil || *num.People != 0 {
		t.Fatalf("people behind a zero numerator = %v, want 0", num.People)
	}
	res := aidlc.Score(rules.ForVersion("v1"), r.Readings, r.Org, nil)
	for _, c := range res.Cells {
		if c.Stage == testQA && c.Level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L2 && c.State != aidlcv1.CellState_CELL_STATE_NOT_REACHED {
			t.Errorf("test_qa/L2 = %v (%s), want NOT_REACHED on 0 of 30", c.State, c.Reason)
		}
	}
}

// TestCoverageFor_Boundaries pins the coverage bands: zero is none, 0.99 and above is
// full, anything between is partial.
func TestCoverageFor_Boundaries(t *testing.T) {
	for f, want := range map[float64]aidlc.Coverage{
		0: aidlc.CoverageNone, 0.5: aidlc.CoveragePartial, 0.9899: aidlc.CoveragePartial,
		0.99: aidlc.CoverageFull, 1: aidlc.CoverageFull,
	} {
		if got := coverageFor(f); got != want {
			t.Errorf("coverageFor(%v) = %v, want %v", f, got, want)
		}
	}
}
