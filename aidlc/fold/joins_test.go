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

// TestFold_EmptyPRKeyJoinsNothing pins that a pull request with an empty PRKey joins no
// attribution, turn or review entry filed under "": 30 such seat-linked feature pull
// requests count in the code population, and their AI authorship is unread (the
// numerator's remainder and the AI universe's), so code L1 to L4 are not met.
func TestFold_EmptyPRKeyJoinsNothing(t *testing.T) {
	var prs []MergedPR
	for i := 0; i < 30; i++ {
		a := fmt.Sprintf("p%d", i%6)
		prs = append(prs, MergedPR{AuthorIdentityID: &a, AuthorSeatLinked: true, BaseBranch: "main",
			WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, Paths: []string{"src/app/handler.go"},
			HasFileList: true, SubmittedReview: true, ReviewersKnown: true})
	}
	r := Fold(Facts{MergedPRs: prs, Attribution: Attribution{AuthoredPRKeys: map[string]struct{}{"": {}}},
		HumanTurns: map[string]int{"": 2}, ReviewFacts: map[string]PRReviewFacts{"": {Captured: true, HumanRows: 1, BotPresent: true}}})
	if r.Preconditions.MergedPRCount != 30 {
		t.Errorf("merged pull requests = %d, want 30: each still counts", r.Preconditions.MergedPRCount)
	}
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	num := mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code)
	if v := valueOf(t, num); v != 0 || remainderOf(num) != 30 {
		t.Errorf("code AI-authored = %v rem %d, want 0 rem 30", v, remainderOf(num))
	}
	uni := mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code)
	if v := valueOf(t, uni); v != 0 || remainderOf(uni) != 30 {
		t.Errorf("code AI universe = %v rem %d, want 0 rem 30", v, remainderOf(uni))
	}
	if r.Org.AdoptionDenominatorPeople != 0 {
		t.Errorf("adoption cohort = %d, want 0: the authors' AI authorship is unread", r.Org.AdoptionDenominatorPeople)
	}
	res := aidlc.Score(rules.ForVersion("v1"), r.Readings, r.Org, nil)
	for _, c := range res.Cells {
		if c.Stage == aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW && c.Level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L3 && c.State == aidlcv1.CellState_CELL_STATE_MET {
			t.Error("review/L3 met on a review record filed under an empty PRKey")
		}
		if c.Stage == code && c.Level <= aidlcv1.AidlcLevel_AIDLC_LEVEL_L4 && c.State == aidlcv1.CellState_CELL_STATE_MET {
			t.Errorf("code/%v met on pull requests with no PRKey", c.Level)
		}
	}
}

// TestFold_DuplicatePRKeyFirstWins pins which copy of a repeated PRKey is read: the
// first, and pull requests with an empty PRKey are each kept.
func TestFold_DuplicatePRKeyFirstWins(t *testing.T) {
	a := "p1"
	prs := []MergedPR{
		{PRKey: "k", AuthorIdentityID: &a, AuthorSeatLinked: true, WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, HasFileList: true},
		{PRKey: "k", AuthorIdentityID: &a, AuthorSeatLinked: true, WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_DOCS},
		{AuthorIdentityID: &a, HasFileList: true},
		{AuthorIdentityID: &a, HasFileList: true},
	}
	r := Fold(Facts{MergedPRs: prs})
	if r.Preconditions.MergedPRCount != 3 || r.Preconditions.PathCoverage != 1 {
		t.Errorf("preconditions = %+v, want 3 pull requests, all with a file list (the first k and both unkeyed)", r.Preconditions)
	}
	if v := valueOf(t, mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, aidlcv1.AidlcStage_AIDLC_STAGE_CODE)); v != 1 {
		t.Errorf("code seat-linked = %v, want 1: the first k is a feature", v)
	}
}

// TestFold_EmptyPlannerIDIsNotAPerson pins that an empty planner id is never counted.
func TestFold_EmptyPlannerIDIsNotAPerson(t *testing.T) {
	r := Fold(Facts{Planning: PlanningRead{PlannerPersonIDs: map[string]struct{}{"": {}, "p1": {}}}})
	if v := valueOf(t, mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_PLANNING_PHASE_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_PLAN)); v != 1 {
		t.Errorf("planners = %v, want 1", v)
	}
}

// TestIsRelease_IgnoresCase pins that a release branch or title is read whatever its
// case: "Release/2.0" and "Release v2.0" give the same release readings as lower case.
func TestIsRelease_IgnoresCase(t *testing.T) {
	readings := func(branch, title string) (float64, float64) {
		var prs []MergedPR
		authored := map[string]struct{}{}
		for i := 0; i < 25; i++ {
			key := fmt.Sprintf("pr-%d", i)
			a := fmt.Sprintf("p%d", i%5)
			authored[key] = struct{}{}
			prs = append(prs, MergedPR{PRKey: key, AuthorIdentityID: &a, AuthorSeatLinked: true, BaseBranch: branch,
				Title: title, WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_OTHER, Paths: []string{"src/x.go"}, HasFileList: true})
		}
		r := Fold(Facts{MergedPRs: prs, Attribution: Attribution{AuthoredPRKeys: authored}})
		deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY
		return valueOf(t, mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_SEAT_LINKED_ARTIFACTS, deploy)),
			valueOf(t, mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_RELEASE_PRS_MERGED, deploy))
	}
	upDen, upNum := readings("Release/2.0", "Release v2.0")
	loDen, loNum := readings("release/2.0", "release v2.0")
	if upDen != loDen || upNum != loNum || loDen != 25 {
		t.Errorf("release readings: %v/%v mixed case, %v/%v lower case, want 25/25 both", upNum, upDen, loNum, loDen)
	}
	if !isRelease("main", "Release v2.0") || !isRelease("Release/2.0", "") {
		t.Error("isRelease is case-sensitive")
	}
}
