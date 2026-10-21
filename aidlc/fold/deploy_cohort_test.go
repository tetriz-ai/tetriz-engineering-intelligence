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

func shippedPR(key, author, branch string, seat bool) MergedPR {
	return MergedPR{PRKey: key, AuthorIdentityID: sptr(author), AuthorSeatLinked: seat, BaseBranch: branch,
		WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, Paths: []string{"src/app/handler.go"},
		HasFileList: true, SubmittedReview: true, ReviewersKnown: true, RepositoryID: sptr("R")}
}

func cellIn(res *aidlc.Result, stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel) *aidlc.Cell {
	for _, c := range res.Cells {
		if c.Stage == stage && c.Level == level {
			return c
		}
	}
	return nil
}

// TestFoldDeploy_AICountsAreSeatLinked pins that deploy's AI counts are over seat-linked
// authors, the population deploy L2 divides by: 15 AI pull requests by authors off the
// roster, beside 20 seat-linked non-AI ones, leave deploy L2 not reached.
func TestFoldDeploy_AICountsAreSeatLinked(t *testing.T) {
	var prs []MergedPR
	authored := map[string]struct{}{}
	for i := 0; i < 20; i++ {
		prs = append(prs, shippedPR(fmt.Sprintf("h-%d", i), fmt.Sprintf("h%d", i%5), "release/1", true))
	}
	for i := 0; i < 15; i++ {
		k := fmt.Sprintf("ai-%d", i)
		authored[k] = struct{}{}
		prs = append(prs, shippedPR(k, fmt.Sprintf("a%d", i%3), "release/1", false))
	}
	r := Fold(Facts{MergedPRs: prs, Attribution: Attribution{AuthoredPRKeys: authored},
		BranchRoles: fakeRoles{roles: map[string]BranchRole{"R/release/1": {Shipped: true}}, known: true}})
	deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY
	if v := valueOf(t, mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED, deploy)); v != 0 {
		t.Errorf("AI shipped = %v, want 0 seat-linked", v)
	}
	res := aidlc.Score(rules.ForVersion("v1"), r.Readings, r.Org, nil)
	if c := cellIn(res, deploy, aidlcv1.AidlcLevel_AIDLC_LEVEL_L2); c.State != aidlcv1.CellState_CELL_STATE_NOT_REACHED {
		t.Errorf("deploy/L2 = %v (%s), want NOT_REACHED", c.State, c.Reason)
	}
}

// TestFoldDeploy_ControlReadsShippedBranchPRs pins the deploy baseline control over
// non-AI pull requests into a shipped branch: with main shipped and no release-shaped
// name, 10 reviewed non-AI pull requests make it readable and deploy L2 is decided.
func TestFoldDeploy_ControlReadsShippedBranchPRs(t *testing.T) {
	var prs []MergedPR
	authored := map[string]struct{}{}
	for i := 0; i < 30; i++ {
		k := fmt.Sprintf("m-%d", i)
		if i < 20 {
			authored[k] = struct{}{}
		}
		prs = append(prs, shippedPR(k, fmt.Sprintf("p%d", i%6), "main", true))
	}
	r := Fold(Facts{MergedPRs: prs, Attribution: Attribution{AuthoredPRKeys: authored},
		BranchRoles: fakeRoles{roles: map[string]BranchRole{"R/main": {Shipped: true}}, known: true}})
	deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY
	if v := valueOf(t, mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, deploy)); v != 1 {
		t.Errorf("deploy control = %v, want 1 (10 of 10 reviewed)", v)
	}
	res := aidlc.Score(rules.ForVersion("v1"), r.Readings, r.Org, nil)
	if c := cellIn(res, deploy, aidlcv1.AidlcLevel_AIDLC_LEVEL_L2); c.State == aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE {
		t.Errorf("deploy/L2 abstains (%s), want it decided", c.Reason)
	}
}

// TestFold_UnreadAuthorsLeaveTheAdoptionCohort pins that an author whose AI authorship is
// unread (only pull requests with no PRKey) is in neither adoption count, so 30 keyless
// pull requests from 10 authors raise no adoption reach.
func TestFold_UnreadAuthorsLeaveTheAdoptionCohort(t *testing.T) {
	var prs []MergedPR
	for i := 0; i < 30; i++ {
		prs = append(prs, shippedPR("", fmt.Sprintf("p%d", i%10), "main", true))
	}
	r := Fold(Facts{MergedPRs: prs})
	if r.Org.AdoptionDenominatorPeople != 0 || r.Org.AdoptionPassivePeople != 0 || r.Org.HasAdoptionCohort {
		t.Errorf("org = %+v, want no cohort", r.Org)
	}
	for _, b := range aidlc.Score(rules.ForVersion("v1"), r.Readings, r.Org, nil).Bottlenecks {
		if b.ID == "adoption_reach" {
			t.Error("adoption_reach raised over authors whose AI authorship is unread")
		}
	}
}

// TestFold_KeylessPRsAreRemaindersEverywhere pins the declared remainder a keyless pull
// request leaves on each AI-defined count outside the code stage: deploy's AI-shipped
// count, the AI population plan and deploy L4 read, the review policy's covered
// population, and a path stage's seat-linked AI numerator and AI universe. Its review
// record filed under "" reaches no bot share.
func TestFold_KeylessPRsAreRemaindersEverywhere(t *testing.T) {
	var prs []MergedPR
	for i := 0; i < 4; i++ {
		p := shippedPR("", fmt.Sprintf("p%d", i), "release/1", true)
		p.Paths = []string{"infra/main.tf"}
		prs = append(prs, p)
	}
	r := Fold(Facts{MergedPRs: prs,
		ReviewFacts:      map[string]PRReviewFacts{"": {Captured: true, BotPresent: true}},
		BranchRoles:      fakeRoles{roles: map[string]BranchRole{"R/release/1": {Shipped: true}}, known: true},
		DeclarationState: DeclarationsRead,
		Declarations:     Declarations{ReviewPolicy: map[aidlcv1.PRWorkType]ApprovalKind{aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE: ApprovalHuman}},
	})
	deploy, ci, unspec := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY, aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	for _, tc := range []struct {
		name  string
		sig   aidlcv1.SignalKey
		stage aidlcv1.AidlcStage
	}{
		{"deploy AI shipped", aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED, deploy},
		{"AI merged PRs", aidlcv1.SignalKey_SIGNAL_KEY_AI_MERGED_PRS, unspec},
		{"policy covered", aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS, unspec},
		{"CI seat-linked AI", aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_CI_PRS_MERGED, ci},
		{"CI AI universe", aidlcv1.SignalKey_SIGNAL_KEY_CI_CD_AGENT_UNIVERSE, ci},
	} {
		rd := mustReading(t, r.Readings, tc.sig, tc.stage)
		if v := valueOf(t, rd); v != 0 || remainderOf(rd) != 4 {
			t.Errorf("%s = %v rem %d, want 0 rem 4", tc.name, v, remainderOf(rd))
		}
	}
	if v := valueOf(t, mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_BOT_PRESENT_PRS, review)); v != 0 {
		t.Errorf("bot present = %v, want 0: a review record under \"\" joins nothing", v)
	}
}
