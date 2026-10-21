// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"fmt"
	"strings"
	"testing"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
)

// TestFold_UnknownWorkTypeWidensTestQA pins that a seat-linked pull request with no work
// type could be delivery for test and QA as it is for code: 12 of 20 delivery pull
// requests touch a test, and 10 more have no work type, so test and QA L2's share is
// [12/30, 22/30], which straddles one half and abstains instead of reading established.
func TestFold_UnknownWorkTypeWidensTestQA(t *testing.T) {
	var prs []MergedPR
	authored := map[string]struct{}{}
	turns := map[string]int{}
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("pr-%d", i)
		p := MergedPR{PRKey: key, AuthorIdentityID: sptr(fmt.Sprintf("p%d", i%5)), AuthorSeatLinked: true,
			WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, Paths: []string{"src/app/handler.go"}, HasFileList: true}
		switch {
		case i < 12:
			p.Paths = []string{"src/app/handler_test.go"}
			authored[key] = struct{}{}
			turns[key] = 6 // not low-turn, so test & QA L3 cannot floor L2
		case i >= 20:
			p.WorkType = aidlcv1.PRWorkType_PR_WORK_TYPE_UNSPECIFIED
			authored[key] = struct{}{}
		}
		prs = append(prs, p)
	}
	r := Fold(Facts{MergedPRs: prs, Attribution: Attribution{AuthoredPRKeys: authored}, HumanTurns: turns})
	testQA := aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA
	den := mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_TEST_QA_SEAT_LINKED_ARTIFACTS, testQA)
	if v := valueOf(t, den); v != 20 || remainderOf(den) != 10 {
		t.Errorf("test & QA population = %v rem %d, want 20 rem 10", v, remainderOf(den))
	}
	uni := mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_TEST_QA_AGENT_LOW_TURN_UNIVERSE, testQA)
	if v := valueOf(t, uni); v != 0 || remainderOf(uni) != 10 {
		t.Errorf("low-turn universe = %v rem %d, want 0 rem 10 (the AI pull requests with no work type)", v, remainderOf(uni))
	}
	res := aidlc.Score(rules.ForVersion("v1"), r.Readings, r.Org, nil)
	if c := cellIn(res, testQA, aidlcv1.AidlcLevel_AIDLC_LEVEL_L2); c.State != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE {
		t.Errorf("test_qa/L2 = %v %v, want an abstain: the share straddles one half", c.State, c.Band)
	}
}

// TestFold_UnreadPlannedSetIsUnread pins PlanningRead.PlannedPRKeysUnread: when the
// planned set could not be read, plan L3 abstains instead of reading not reached, and
// no plan bottleneck is raised.
func TestFold_UnreadPlannedSetIsUnread(t *testing.T) {
	var prs []MergedPR
	authored := map[string]struct{}{}
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("pr-%d", i)
		authored[key] = struct{}{}
		prs = append(prs, MergedPR{PRKey: key, AuthorIdentityID: sptr(fmt.Sprintf("p%d", i%5)), AuthorSeatLinked: true,
			WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, HasFileList: true, ReviewersKnown: true, SubmittedReview: true})
	}
	r := Fold(Facts{MergedPRs: prs, Attribution: Attribution{AuthoredPRKeys: authored},
		Planning: PlanningRead{IssuesInWindow: 5, PlannedPRKeysUnread: true}})
	res := aidlc.Score(rules.ForVersion("v1"), r.Readings, r.Org, nil)
	plan := aidlcv1.AidlcStage_AIDLC_STAGE_PLAN
	if c := cellIn(res, plan, aidlcv1.AidlcLevel_AIDLC_LEVEL_L3); c.State != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE {
		t.Errorf("plan/L3 = %v, want an abstain: the planned set is unread", c.State)
	}
	for _, b := range res.Bottlenecks {
		if strings.HasSuffix(b.ID, "_plan") || strings.HasPrefix(b.ID, "rung_plan") {
			t.Errorf("bottleneck %s raised over an unread planned set", b.ID)
		}
	}
}

// TestFold_UnreadApprovalsWiden pins PRReviewFacts.ApprovalsUnread: a captured review
// record whose approvals were not captured leaves the declared policy's approval unread
// (the numerator's remainder), not counted as unapproved.
func TestFold_UnreadApprovalsWiden(t *testing.T) {
	var prs []MergedPR
	authored := map[string]struct{}{}
	facts := map[string]PRReviewFacts{}
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("pr-%d", i)
		authored[key] = struct{}{}
		prs = append(prs, MergedPR{PRKey: key, AuthorIdentityID: sptr(fmt.Sprintf("p%d", i%5)), AuthorSeatLinked: true,
			WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, HasFileList: true, ReviewersKnown: true, SubmittedReview: true})
		facts[key] = PRReviewFacts{Captured: true, HumanRows: 1, ApprovalsUnread: true}
	}
	r := Fold(Facts{MergedPRs: prs, Attribution: Attribution{AuthoredPRKeys: authored}, ReviewFacts: facts,
		DeclarationState: DeclarationsRead,
		Declarations:     Declarations{ReviewPolicy: map[aidlcv1.PRWorkType]ApprovalKind{aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE: ApprovalHuman}}})
	met := mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED)
	if v := valueOf(t, met); v != 0 || remainderOf(met) != 30 {
		t.Errorf("policy met = %v rem %d, want 0 rem 30: the approvals are unread", v, remainderOf(met))
	}
}

// TestFold_EmptyStageMapIsUnreadNotInstrumentedOnly pins that practice use whose stage
// is unknown (an empty stage map) is unread mass: review L1 abstains on coverage, which
// closure awards beneath a met review L3, rather than the instrumented-only abstain.
func TestFold_EmptyStageMapIsUnreadNotInstrumentedOnly(t *testing.T) {
	var roster []string
	var invs []HarnessInvocation
	for i := 0; i < 20; i++ {
		p := fmt.Sprintf("p%d", i)
		roster = append(roster, p)
		if i < 10 {
			invs = append(invs, HarnessInvocation{PersonID: p, ArtifactID: "a-review", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL})
		}
	}
	var prs []MergedPR
	facts := map[string]PRReviewFacts{}
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("pr-%d", i)
		prs = append(prs, MergedPR{PRKey: key, AuthorIdentityID: sptr(roster[i%20]), AuthorSeatLinked: true,
			WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, HasFileList: true})
		facts[key] = PRReviewFacts{Captured: true, BotPresent: true, BotRows: 1}
	}
	r := Fold(Facts{Roster: roster, Invocations: invs, MergedPRs: prs, ReviewFacts: facts})
	res := aidlc.Score(rules.ForVersion("v1"), r.Readings, r.Org, nil)
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	if c := cellIn(res, review, aidlcv1.AidlcLevel_AIDLC_LEVEL_L3); c.State != aidlcv1.CellState_CELL_STATE_MET {
		t.Fatalf("fixture: review/L3 = %v, want met", c.State)
	}
	l1 := cellIn(res, review, aidlcv1.AidlcLevel_AIDLC_LEVEL_L1)
	if l1.State != aidlcv1.CellState_CELL_STATE_MET || l1.DecidedBy != aidlcv1.DecidedBy_DECIDED_BY_IMPLIED_FLOOR {
		t.Errorf("review/L1 = %v by %v (%s), want met as an implied floor", l1.State, l1.DecidedBy, l1.Reason)
	}
}

// TestFold_NoShippedRoleReasonIsServed pins that the fold's own note on a no-run reading
// reaches the reader: deploy with no shipped branch says so.
func TestFold_NoShippedRoleReasonIsServed(t *testing.T) {
	a := "p1"
	r := Fold(Facts{MergedPRs: []MergedPR{{PRKey: "k", AuthorIdentityID: &a, AuthorSeatLinked: true}}})
	res := aidlc.Score(rules.ForVersion("v1"), r.Readings, r.Org, nil)
	c := cellIn(res, aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY, aidlcv1.AidlcLevel_AIDLC_LEVEL_L2)
	if c.Reason != "Ours: no branch in this organisation is marked as one that ships, so its releases cannot be read." {
		t.Errorf("deploy/L2 reason = %q", c.Reason)
	}
}

// TestFold_UnknownWorkTypeWidensCodeAIDelivery pins that an AI pull request with no work
// type could be AI delivery, so it is a remainder on code's AI-delivery population: 30 AI
// delivery pull requests all low-turn beside 30 AI ones with no work type read code L4 as
// [30/60, 60/60], which abstains instead of reading established.
func TestFold_UnknownWorkTypeWidensCodeAIDelivery(t *testing.T) {
	var prs []MergedPR
	authored := map[string]struct{}{}
	turns := map[string]int{}
	for i := 0; i < 60; i++ {
		key := fmt.Sprintf("pr-%d", i)
		authored[key] = struct{}{}
		p := MergedPR{PRKey: key, AuthorIdentityID: sptr(fmt.Sprintf("p%d", i%6)), AuthorSeatLinked: true,
			WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, Paths: []string{"src/app/handler.go"}, HasFileList: true}
		if i < 30 {
			turns[key] = 2
		} else {
			p.WorkType = aidlcv1.PRWorkType_PR_WORK_TYPE_UNSPECIFIED
		}
		prs = append(prs, p)
	}
	r := Fold(Facts{MergedPRs: prs, Attribution: Attribution{AuthoredPRKeys: authored}, HumanTurns: turns})
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	uni := mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code)
	if v := valueOf(t, uni); v != 30 || remainderOf(uni) != 30 {
		t.Errorf("code AI-delivery population = %v rem %d, want 30 rem 30", v, remainderOf(uni))
	}
	res := aidlc.Score(rules.ForVersion("v1"), r.Readings, r.Org, nil)
	if c := cellIn(res, code, aidlcv1.AidlcLevel_AIDLC_LEVEL_L4); c.State != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE {
		t.Errorf("code/L4 = %v %v, want an abstain: the share widens to [0.5, 1]", c.State, c.Band)
	}
}
