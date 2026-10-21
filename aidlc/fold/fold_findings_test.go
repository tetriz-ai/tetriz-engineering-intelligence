// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"fmt"
	"math"
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// readingFor returns the reading a fold Set for (signal, stage), or ok=false.
func readingFor(rd *aidlc.Readings, sig aidlcv1.SignalKey, stage aidlcv1.AidlcStage) (aidlc.Reading, bool) {
	for _, e := range rd.All() {
		if e.Signal == sig && e.Stage == stage {
			return e.Reading, true
		}
	}
	return aidlc.Reading{}, false
}

// cellShare returns the scored share of one cell, or nil when the cell recorded none.
func cellShare(res *aidlc.Result, stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel) *aidlc.ShareInfo {
	for _, c := range res.Cells {
		if c.Stage == stage && c.Level == level {
			return c.Share
		}
	}
	return nil
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// TestFoldPresence_UnreadableHoldersWidenBound proves that unreadable holders
// (people who invoked a practice artifact with no readable stage) are carried as a
// stage-specific remainder, so 3 classified + 5 unreadable over 10 seats scores as the
// interval [0.30, 0.80] rather than a confident 0.30 point.
func TestFoldPresence_UnreadableHoldersWidenBound(t *testing.T) {
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	skill := aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL

	var invs []HarnessInvocation
	for p := 0; p < 3; p++ { // 3 people invoke a REVIEW-classified artifact
		invs = append(invs, HarnessInvocation{PersonID: fmt.Sprintf("p%d", p), ArtifactID: "art-review", Type: skill})
	}
	for p := 3; p < 8; p++ { // 5 other people invoke an unclassified artifact
		invs = append(invs, HarnessInvocation{PersonID: fmt.Sprintf("p%d", p), ArtifactID: "art-unknown", Type: skill})
	}
	for p := 8; p < 10; p++ { // 2 more invoke an artifact mapped as serving no stage
		invs = append(invs, HarnessInvocation{PersonID: fmt.Sprintf("p%d", p), ArtifactID: "art-none", Type: skill})
	}
	// A judged no-stage artifact is present in the map as UNSPECIFIED: a read answer, so
	// its invokers are neither a hit nor an unknown remainder. This pins that a map-present
	// UNSPECIFIED is not counted in the unknown remainder (folding p8/p9 into it would read
	// 7, not 5); it does not pin where the guard sits, since no rung is keyed on
	// UNSPECIFIED.
	stageMap := map[string]aidlcv1.AidlcStage{
		"art-review": review,
		"art-none":   aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED,
	}

	rd := aidlc.NewReadings()
	foldPresence(rd, 10, invs, stageMap)

	got, ok := readingFor(rd, aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS, review)
	if !ok {
		t.Fatalf("no REVIEW_HARNESS_SEAT_HOLDERS reading")
	}
	if got.Value == nil || *got.Value != 3 {
		t.Fatalf("numerator = %v, want 3", got.Value)
	}
	if !got.RemainderDeclared || got.RemainderValue == nil || *got.RemainderValue != 5 {
		t.Fatalf("remainder = %v (declared %v), want 5", got.RemainderValue, got.RemainderDeclared)
	}

	res := aidlc.Score(rules.ForVersion("v1"), rd, aidlc.OrgRead{}, nil)
	sh := cellShare(res, review, aidlcv1.AidlcLevel_AIDLC_LEVEL_L1)
	if sh == nil {
		t.Fatalf("review/L1 recorded no share")
	}
	if !approx(sh.Lo, 0.30) || !approx(sh.Hi, 0.80) {
		t.Fatalf("review/L1 share = [%v, %v], want [0.30, 0.80]", sh.Lo, sh.Hi)
	}
}

// TestFoldReviewBots_UncapturedWidenBound proves that rule-applicable PRs whose
// review row was not captured bound the bot share. 20 captured (all bot) over 80
// uncaptured scores L3 as [0.20, 1.00] rather than a confident 100%.
func TestFoldReviewBots_UncapturedWidenBound(t *testing.T) {
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW

	var prs []MergedPR
	facts := map[string]PRReviewFacts{}
	for i := 0; i < 20; i++ { // captured, bot-reviewed
		id := fmt.Sprintf("cap#PR-%d", i)
		pid := fmt.Sprintf("p%d", i)
		prs = append(prs, MergedPR{PRKey: id, AuthorIdentityID: &pid, AuthorSeatLinked: true})
		facts[id] = PRReviewFacts{Captured: true, BotPresent: true}
	}
	for i := 0; i < 80; i++ { // rule-applicable but uncaptured
		id := fmt.Sprintf("unc#PR-%d", i)
		pid := fmt.Sprintf("q%d", i)
		prs = append(prs, MergedPR{PRKey: id, AuthorIdentityID: &pid, AuthorSeatLinked: true})
	}

	rd := aidlc.NewReadings()
	foldReviewBots(rd, prs, facts)

	den, ok := readingFor(rd, aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_RULE_APPLICABLE_PRS, review)
	if !ok {
		t.Fatalf("no REVIEW_RULE_APPLICABLE_PRS reading")
	}
	if den.Value == nil || *den.Value != 20 {
		t.Fatalf("captured denominator = %v, want 20", den.Value)
	}
	if !den.RemainderDeclared || den.RemainderValue == nil || *den.RemainderValue != 80 {
		t.Fatalf("uncaptured remainder = %v, want 80", den.RemainderValue)
	}

	res := aidlc.Score(rules.ForVersion("v1"), rd, aidlc.OrgRead{}, nil)
	sh := cellShare(res, review, aidlcv1.AidlcLevel_AIDLC_LEVEL_L3)
	if sh == nil {
		t.Fatalf("review/L3 recorded no share")
	}
	if !approx(sh.Lo, 0.20) || !approx(sh.Hi, 1.00) {
		t.Fatalf("review/L3 share = [%v, %v], want [0.20, 1.00]", sh.Lo, sh.Hi)
	}
}

// TestFoldMerged_UnplaceablePathStagePRs proves that seated PRs with no file list
// widen a path-defined stage's seat-linked denominator (they could belong to it), while
// the branch/title-defined deploy denominator carries no such remainder.
func TestFoldMerged_UnplaceablePathStagePRs(t *testing.T) {
	techSpec := aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC
	deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY

	var prs []MergedPR
	attr := Attribution{AuthoredPRKeys: map[string]struct{}{}, RoutedPRKeys: map[string]struct{}{}}
	add := func(id string, paths []string, base, title string) {
		pid := id
		prs = append(prs, MergedPR{
			PRKey: id, AuthorIdentityID: &pid, AuthorSeatLinked: true,
			WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_OTHER, Paths: paths, HasFileList: len(paths) > 0,
			BaseBranch: base, Title: title,
		})
	}
	for i := 0; i < 25; i++ { // placed in tech_spec by a design-doc path
		add(fmt.Sprintf("tech-%d", i), []string{designPath}, "main", "rfc")
	}
	for i := 0; i < 5; i++ { // placed in deploy by branch, with a file list
		add(fmt.Sprintf("rel-%d", i), []string{"cmd/main.go"}, releaseBr, "release")
	}
	for i := 0; i < 15; i++ { // seated, no file list — unplaceable into path stages
		add(fmt.Sprintf("nolist-%d", i), nil, "main", "misc")
	}

	rd := aidlc.NewReadings()
	foldMergedChange(rd, prs, attr, nil, nil, 1.0)

	techDen, ok := readingFor(rd, aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_SEAT_LINKED_ARTIFACTS, techSpec)
	if !ok {
		t.Fatalf("no TECH_SPEC_SEAT_LINKED_ARTIFACTS reading")
	}
	if techDen.Value == nil || *techDen.Value != 25 {
		t.Fatalf("tech_spec seat-linked = %v, want 25", techDen.Value)
	}
	if !techDen.RemainderDeclared || techDen.RemainderValue == nil || *techDen.RemainderValue != 15 {
		t.Fatalf("tech_spec unplaceable remainder = %v, want 15", techDen.RemainderValue)
	}

	depDen, ok := readingFor(rd, aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_SEAT_LINKED_ARTIFACTS, deploy)
	if !ok {
		t.Fatalf("no DEPLOY_SEAT_LINKED_ARTIFACTS reading")
	}
	if depDen.RemainderDeclared {
		t.Fatalf("deploy seat-linked should carry no unplaceable remainder (it is branch/title-defined), got %v", depDen.RemainderValue)
	}
}

// TestFoldMerged_BotOnlyReviewNotHuman proves that a submitted review that
// the captured review records show as bot-only is not human evidence; a confirmed human
// review is; a submitted review with no captured records is trusted; and an uncaptured
// reviewer
// list is carried as the after-review numerator's unknown remainder.
func TestFoldMerged_BotOnlyReviewNotHuman(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE

	mk := func(id string, submitted, listKnown bool) MergedPR {
		pid := id
		return MergedPR{
			PRKey: id, AuthorIdentityID: &pid, AuthorSeatLinked: true,
			WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE,
			Paths:    []string{"internal/x.go"}, HasFileList: true,
			SubmittedReview: submitted, ReviewersKnown: listKnown,
		}
	}
	prs := []MergedPR{
		mk("bot", true, true),       // submitted review, records say bot-only -> NOT human
		mk("human", true, true),     // submitted review, records confirm a human -> human
		mk("listonly", true, true),  // submitted review, no captured records -> trusted human
		mk("unknown", false, false), // reviewer list uncaptured -> unknown remainder
	}
	attr := Attribution{AuthoredPRKeys: map[string]struct{}{
		"bot": {}, "human": {}, "listonly": {}, "unknown": {},
	}, RoutedPRKeys: map[string]struct{}{}}
	facts := map[string]PRReviewFacts{
		"bot":   {Captured: true, BotPresent: true, BotRows: 1, HumanRows: 0},
		"human": {Captured: true, HumanRows: 1},
		// "listonly" and "unknown" have no captured review record.
	}

	rd := aidlc.NewReadings()
	foldMergedChange(rd, prs, attr, nil, facts, 1.0)

	num, ok := readingFor(rd, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code)
	if !ok {
		t.Fatalf("no AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED reading")
	}
	if num.Value == nil || *num.Value != 2 {
		t.Fatalf("human-reviewed numerator = %v, want 2 (human + listonly; bot excluded)", num.Value)
	}
	if num.RemainderValue == nil || *num.RemainderValue != 1 {
		t.Fatalf("after-review unknown remainder = %v, want 1 (the uncaptured list)", num.RemainderValue)
	}
}
