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

// TestFold_ZeroOverUnreadPeopleNeverDenies pins that a presence level with no counted
// hits never reads not reached, whatever its unread people: a roster of 20, 1, 4 or 10
// invokers of a skill the empty stage map does not name, and review L3 met over 30
// bot-reviewed pull requests. No presence cell is not reached, and no rung or ladder
// bottleneck is raised on a presence stage.
func TestFold_ZeroOverUnreadPeopleNeverDenies(t *testing.T) {
	presence := map[aidlcv1.AidlcStage]aidlcv1.AidlcLevel{
		aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC: aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
		aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC:    aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
		aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW:       aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
		aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD:        aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
		aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY:       aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
		aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE:      aidlcv1.AidlcLevel_AIDLC_LEVEL_L2,
	}
	banned := map[string]bool{"rung_review_L1": true, "ladder_product_spec": true, "ladder_tech_spec": true}
	for _, unread := range []int{1, 4, 10} {
		t.Run(fmt.Sprintf("%d unread", unread), func(t *testing.T) {
			var roster []string
			var invs []HarnessInvocation
			for i := 0; i < 20; i++ {
				p := fmt.Sprintf("p%d", i)
				roster = append(roster, p)
				if i < unread {
					invs = append(invs, HarnessInvocation{PersonID: p, ArtifactID: "unmapped", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL})
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
			for stage, level := range presence {
				if c := cellIn(res, stage, level); c.State == aidlcv1.CellState_CELL_STATE_NOT_REACHED {
					t.Errorf("%v/%v not reached on zero counted hits", stage, level)
				}
			}
			for _, b := range res.Bottlenecks {
				if banned[b.ID] {
					t.Errorf("bottleneck %s raised on zero counted hits", b.ID)
				}
			}
		})
	}
}

// TestFold_UnreadPlannedSetOverridesAndGates pins the planned-set flag's other
// outcomes: plan L4 is a no-run with the fold's note as its reason; with no tracker
// issue plan L3 still gap-blocks on G2; and a non-empty planned set is ignored when the
// flag is set.
func TestFold_UnreadPlannedSetOverridesAndGates(t *testing.T) {
	var last Result
	build := func(issues int, planned map[string]HitlState) *aidlc.Result {
		var prs []MergedPR
		authored := map[string]struct{}{}
		for i := 0; i < 30; i++ {
			key := fmt.Sprintf("pr-%d", i)
			authored[key] = struct{}{}
			prs = append(prs, MergedPR{PRKey: key, AuthorIdentityID: sptr(fmt.Sprintf("p%d", i%5)), AuthorSeatLinked: true,
				WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, HasFileList: true, ReviewersKnown: true, SubmittedReview: true})
		}
		r := Fold(Facts{MergedPRs: prs, Attribution: Attribution{AuthoredPRKeys: authored},
			Planning: PlanningRead{IssuesInWindow: issues, PlannedPRKeys: planned, PlannedPRKeysUnread: true}})
		last = r
		return aidlc.Score(rules.ForVersion("v1"), r.Readings, r.Org, nil)
	}
	plan := aidlcv1.AidlcStage_AIDLC_STAGE_PLAN
	l4 := cellIn(build(5, nil), plan, aidlcv1.AidlcLevel_AIDLC_LEVEL_L4)
	if l4.AbstainKind() != aidlcv1.AbstainKind_ABSTAIN_KIND_GAP || l4.Reason != "Ours: the planned pull requests could not be read." {
		t.Errorf("plan/L4 = %v %q, want a gap abstain with the fold's note", l4.AbstainKind(), l4.Reason)
	}
	for _, sig := range []aidlcv1.SignalKey{aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS, aidlcv1.SignalKey_SIGNAL_KEY_UNATTENDED_PLANNED_AI_MERGED_PRS} {
		if r := mustReading(t, last.Readings, sig, plan); r.Status != aidlc.StatusNoRunRecord {
			t.Errorf("%v status = %v, want a no-run: both plan L4 terms are unread", sig, r.Status)
		}
	}
	if l3 := cellIn(build(0, nil), plan, aidlcv1.AidlcLevel_AIDLC_LEVEL_L3); l3.Gap != aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G2 {
		t.Errorf("plan/L3 gap = %v, want G2 with no tracker issue", l3.Gap)
	}
	planned := map[string]HitlState{}
	for i := 0; i < 30; i++ {
		planned[fmt.Sprintf("pr-%d", i)] = HitlAttended
	}
	if l3 := cellIn(build(5, planned), plan, aidlcv1.AidlcLevel_AIDLC_LEVEL_L3); l3.State != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE {
		t.Errorf("plan/L3 = %v, want an abstain: the flag overrides a read planned set", l3.State)
	}
	for _, sig := range []aidlcv1.SignalKey{aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS, aidlcv1.SignalKey_SIGNAL_KEY_UNATTENDED_PLANNED_AI_MERGED_PRS} {
		if r := mustReading(t, last.Readings, sig, plan); r.Status != aidlc.StatusNoRunRecord {
			t.Errorf("%v status = %v with a read planned set, want a no-run: the flag overrides it", sig, r.Status)
		}
	}
}

// zeroHitsReview folds a roster of 20 with no counted review-harness invocation and
// unread people invoking a skill the empty stage map does not name. With bot set, 30
// bot-reviewed pull requests meet review L3. It returns review L1 and L3.
func zeroHitsReview(unread int, bot bool) (l1, l3 *aidlc.Cell) {
	var roster []string
	var invs []HarnessInvocation
	for i := 0; i < 20; i++ {
		p := fmt.Sprintf("p%d", i)
		roster = append(roster, p)
		if i < unread {
			invs = append(invs, HarnessInvocation{PersonID: p, ArtifactID: "unmapped", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL})
		}
	}
	var prs []MergedPR
	facts := map[string]PRReviewFacts{}
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("pr-%d", i)
		prs = append(prs, MergedPR{PRKey: key, AuthorIdentityID: sptr(roster[i%20]), AuthorSeatLinked: true,
			WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, HasFileList: true})
		if bot {
			facts[key] = PRReviewFacts{Captured: true, BotPresent: true, BotRows: 1}
		}
	}
	r := Fold(Facts{Roster: roster, Invocations: invs, MergedPRs: prs, ReviewFacts: facts})
	res := aidlc.Score(rules.ForVersion("v1"), r.Readings, r.Org, nil)
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	return cellIn(res, review, aidlcv1.AidlcLevel_AIDLC_LEVEL_L1), cellIn(res, review, aidlcv1.AidlcLevel_AIDLC_LEVEL_L3)
}

// TestFold_ZeroOverUnreadAbstainKinds pins review L1's abstain at 0 counted hits of 20,
// with nothing met above it: instrumented-only at 3 and 4 unread, whose widened share
// stays at or under the emerging cut, and coverage at 5 and 10, whose share straddles
// it. The instrumented-only cell records the numerator it read: 0.
func TestFold_ZeroOverUnreadAbstainKinds(t *testing.T) {
	io, cov := aidlcv1.AbstainKind_ABSTAIN_KIND_INSTRUMENTED_ONLY, aidlcv1.AbstainKind_ABSTAIN_KIND_COVERAGE
	for _, tc := range []struct {
		unread int
		want   aidlcv1.AbstainKind
	}{{3, io}, {4, io}, {5, cov}, {10, cov}} {
		l1, _ := zeroHitsReview(tc.unread, false)
		if got := l1.AbstainKind(); got != tc.want {
			t.Errorf("%d unread: review/L1 = %v %v, want an abstain of kind %v", tc.unread, l1.State, got, tc.want)
		}
		if tc.want != io {
			continue
		}
		read := false
		for _, ch := range l1.Checks {
			if ch.GetKind() == "numerator" && ch.GetMeasuredValue() == 0 && ch.GetComparator() == "read" {
				read = true
			}
		}
		if !read {
			t.Errorf("%d unread: review/L1 checks %v, want the numerator read as 0", tc.unread, l1.Checks)
		}
	}
}

// TestFold_InstrumentedOnlyNeverCloses pins that closure never awards an
// instrumented-only abstain beneath a met rung: at 0 counted hits, review L1 stays
// instrumented-only under a met review L3, at 0 and at 4 unread of 20.
func TestFold_InstrumentedOnlyNeverCloses(t *testing.T) {
	for _, unread := range []int{0, 4} {
		l1, l3 := zeroHitsReview(unread, true)
		if l3.State != aidlcv1.CellState_CELL_STATE_MET {
			t.Fatalf("fixture: review/L3 = %v, want met", l3.State)
		}
		if got := l1.AbstainKind(); got != aidlcv1.AbstainKind_ABSTAIN_KIND_INSTRUMENTED_ONLY {
			t.Errorf("%d unread: review/L1 = %v %v (floor %v), want instrumented-only under a met L3", unread, l1.State, got, l1.AwardedFloor)
		}
	}
}

// TestFold_UnknownWorkTypeGuards pins the three guards on a pull request with no work
// type, one pull request at a time: the AI-delivery population's remainder needs AI
// authorship, test & QA's needs a seat-linked author, and the low-turn universe's needs
// AI authorship and a file list.
func TestFold_UnknownWorkTypeGuards(t *testing.T) {
	cases := []struct {
		name               string
		ai, seat, files    bool
		codeAI, testQA, lt int
	}{
		{"AI, seat-linked, no file list", true, true, false, 1, 1, 0},
		{"AI, not seat-linked, file list", true, false, true, 1, 0, 1},
		{"not AI, seat-linked, file list", false, true, true, 0, 1, 0},
	}
	code, testQA := aidlcv1.AidlcStage_AIDLC_STAGE_CODE, aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pr := MergedPR{PRKey: "k", AuthorIdentityID: sptr("p1"), AuthorSeatLinked: tc.seat, HasFileList: tc.files}
			if tc.files {
				pr.Paths = []string{"src/x.go"}
			}
			authored := map[string]struct{}{}
			if tc.ai {
				authored["k"] = struct{}{}
			}
			r := Fold(Facts{MergedPRs: []MergedPR{pr}, Attribution: Attribution{AuthoredPRKeys: authored}})
			got := []int{
				remainderOf(mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code)),
				remainderOf(mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_TEST_QA_SEAT_LINKED_ARTIFACTS, testQA)),
				remainderOf(mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_TEST_QA_AGENT_LOW_TURN_UNIVERSE, testQA)),
			}
			if got[0] != tc.codeAI || got[1] != tc.testQA || got[2] != tc.lt {
				t.Errorf("remainders = %v, want [%d %d %d]", got, tc.codeAI, tc.testQA, tc.lt)
			}
		})
	}
}

// TestFold_UnknownWorkTypeQuietsReviewReach pins review reach over the widened
// AI-delivery population: 5 of 30 AI delivery pull requests reviewed raises it, and 30
// more AI pull requests with no work type widen the reviewed share to [5/60, 35/60],
// whose upper bound clears one half, so it stays silent.
func TestFold_UnknownWorkTypeQuietsReviewReach(t *testing.T) {
	build := func(unknown int) []aidlc.Bottleneck {
		var prs []MergedPR
		authored := map[string]struct{}{}
		add := func(key string, ai bool, wt aidlcv1.PRWorkType, reviewed bool) {
			if ai {
				authored[key] = struct{}{}
			}
			prs = append(prs, MergedPR{PRKey: key, AuthorIdentityID: sptr(fmt.Sprintf("p%d", len(prs)%6)), AuthorSeatLinked: true,
				WorkType: wt, Paths: []string{"src/x.go"}, HasFileList: true, ReviewersKnown: true, SubmittedReview: reviewed})
		}
		for i := 0; i < 30; i++ {
			add(fmt.Sprintf("ai-%d", i), true, aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, i < 5)
		}
		for i := 0; i < 10; i++ {
			add(fmt.Sprintf("h-%d", i), false, aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, true) // the control
		}
		for i := 0; i < unknown; i++ {
			add(fmt.Sprintf("u-%d", i), true, aidlcv1.PRWorkType_PR_WORK_TYPE_UNSPECIFIED, false)
		}
		r := Fold(Facts{MergedPRs: prs, Attribution: Attribution{AuthoredPRKeys: authored}})
		return aidlc.Score(rules.ForVersion("v1"), r.Readings, r.Org, nil).Bottlenecks
	}
	has := func(bs []aidlc.Bottleneck) bool {
		for _, b := range bs {
			if b.ID == "review_reach" {
				return true
			}
		}
		return false
	}
	if !has(build(0)) {
		t.Fatal("fixture: review_reach does not fire at 5 of 30")
	}
	if has(build(30)) {
		t.Error("review_reach fired over 30 AI pull requests whose work type is unread")
	}
}
