// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"fmt"
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// These are fold goldens: a fixture organisation's facts are folded by Fold, then
// scored. The oracle is the profile the readings produce, so this proves the folds map
// every fact to the reading the scorer expects, and that every live signal has a
// producer (a live cell with no reading would abstain as a gap, which the assertion
// below catches).

// path fragments that put a PR in each path/branch-defined stage bucket.
const (
	prdPath     = "prd/feature.md"
	designPath  = "docs/rfc/design.md"
	ciPath      = ".github/workflows/build.yml"
	monitorPath = "monitoring/alerts/latency.yml"
	testPath    = "internal/foo_test.go"
	releaseBr   = "release/1.4"
)

// buildFixture assembles a rich fixture whose populations clear the artifact floor (20)
// and people floor (5), so every live cell is decidable rather than floor-abstained.
func buildFixture() Facts {
	var (
		invs  []HarnessInvocation
		prs   []MergedPR
		attr  Attribution
		turns map[string]int
		facts map[string]PRReviewFacts
	)
	seatCount := 10
	person := func(i int) string { return fmt.Sprintf("p%02d", i%6) } // 6 distinct authors

	// ---- presence: 6 practice artifacts per stage, each invoked by 6 distinct people ----
	sm := map[string]aidlcv1.AidlcStage{}
	presenceStages := []aidlcv1.AidlcStage{
		aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC,
		aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC,
		aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW,
		aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD,
		aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY,
		aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE,
	}
	for si, stage := range presenceStages {
		artID := fmt.Sprintf("art-%d", si)
		sm[artID] = stage
		typ := aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL
		if stage == aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC {
			typ = aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_COMMAND // also feeds the command rung
		}
		for p := 0; p < 6; p++ {
			invs = append(invs, HarnessInvocation{PersonID: fmt.Sprintf("p%02d", p), ArtifactID: artID, Type: typ})
		}
	}
	// an ambient artifact invoked widely must NOT reach a rung.
	sm["art-hook"] = aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	for p := 0; p < 6; p++ {
		invs = append(invs, HarnessInvocation{PersonID: fmt.Sprintf("p%02d", p), ArtifactID: "art-hook", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_HOOK})
	}

	attr = Attribution{AuthoredPRKeys: map[string]struct{}{}, RoutedPRKeys: map[string]struct{}{}}
	turns = map[string]int{}
	facts = map[string]PRReviewFacts{}

	repo := "R"
	// helper to add a merged PR bucket of size n for a given path/work-type.
	add := func(prefix string, n int, wt aidlcv1.PRWorkType, paths []string, base, title string, aiFrac, reviewedFrac, lowTurnFrac float64, bot bool) {
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("%s#PR-%d", prefix, i)
			pid := person(i)
			pr := MergedPR{
				PRKey: id, AuthorIdentityID: &pid, AuthorSeatLinked: true, RepositoryID: &repo,
				WorkType: wt, Paths: paths, HasFileList: len(paths) > 0, BaseBranch: base, Title: title,
			}
			ai := float64(i) < aiFrac*float64(n)
			if ai {
				attr.AuthoredPRKeys[id] = struct{}{}
			}
			if float64(i) < reviewedFrac*float64(n) {
				pr.SubmittedReview = true
			}
			pr.ReviewersKnown = true
			if ai && float64(i) < lowTurnFrac*float64(n) {
				turns[id] = 2 // low turn
				c := 6.0
				pr.ComplexityScore = &c
			}
			prs = append(prs, pr)
			if bot {
				f := PRReviewFacts{Captured: true, BotPresent: true}
				if pr.SubmittedReview {
					f.HumanRows = 1
				}
				facts[id] = f
			}
		}
	}

	// code / test-QA delivery population: 80% AI-authored (20 of 25) clears the
	// AI-universe denominator's artifact floor, and all of those are low-turn so the
	// test-QA low-turn universe under it clears the floor too; the reviewed share
	// (88%) exceeds the AI share so the non-AI control population has a genuine
	// non-zero reviewed rate rather than a coincidental all-or-nothing overlap.
	add("code", 25, aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, []string{"internal/foo.go"}, "main", "feat: x", 0.8, 0.88, 1.0, false)
	// give the first 12 delivery PRs a test path too (test-QA numerator).
	for i := range prs {
		if prs[i].PRKey == fmt.Sprintf("code#PR-%d", i) && i < 12 {
			prs[i].Paths = append(prs[i].Paths, testPath)
		}
	}
	// tech/prd/ci/rel: same 80%-AI / 88%-reviewed shape as code, for the same two
	// reasons — clears the AI-attributed-universe artifact floor and gives the
	// non-AI baseline control a non-zero reviewed rate.
	add("tech", 25, aidlcv1.PRWorkType_PR_WORK_TYPE_DOCS, []string{designPath}, "main", "rfc", 0.8, 0.88, 0.0, false)
	add("prd", 25, aidlcv1.PRWorkType_PR_WORK_TYPE_DOCS, []string{prdPath}, "main", "prd", 0.8, 0.88, 0.0, false)
	add("ci", 25, aidlcv1.PRWorkType_PR_WORK_TYPE_CHORE_DEPS, []string{ciPath}, "main", "ci", 0.8, 0.88, 0.0, false)
	add("mon", 25, aidlcv1.PRWorkType_PR_WORK_TYPE_CHORE_DEPS, []string{monitorPath}, "main", "obs", 0.6, 0.0, 0.0, false)
	// rel carries a non-stage-matching file list so it places in deploy by branch alone
	// while not leaking into the path-defined stages' unplaceable-PR remainder (that
	// widening is exercised by TestFoldMerged_UnplaceablePathStagePRs).
	add("rel", 25, aidlcv1.PRWorkType_PR_WORK_TYPE_OTHER, []string{"cmd/main.go"}, releaseBr, "release 1.4", 0.8, 0.88, 0.0, false)
	// review-bot population: 25 merged PRs with captured bot reviews, half cleared with no human.
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("rev#PR-%d", i)
		pid := person(i)
		prs = append(prs, MergedPR{
			PRKey: id, AuthorIdentityID: &pid, AuthorSeatLinked: true,
			WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, Paths: []string{"internal/r.go"}, HasFileList: true, ReviewersKnown: true,
		})
		f := PRReviewFacts{Captured: true, BotPresent: true}
		if i >= 13 { // 12 of 25 cleared with no human -> ~0.48
			f.HumanRows = 1
		}
		facts[id] = f
	}
	roster := make([]string, seatCount)
	for i := range roster {
		roster[i] = fmt.Sprintf("p%02d", i)
	}
	sessions := make([]string, 0, 6)
	for p := 0; p < 6; p++ {
		sessions = append(sessions, fmt.Sprintf("p%02d", p))
	}
	attr.SessionPersonIDs = sessions
	// Author identities are person ids already, so the mapping is the identity.
	authors := map[string]string{}
	for p := 0; p < 6; p++ {
		authors[fmt.Sprintf("p%02d", p)] = fmt.Sprintf("p%02d", p)
	}
	return Facts{
		Roster: roster, Invocations: invs, StageMap: sm, MergedPRs: prs, Attribution: attr,
		HumanTurns: turns, ReviewFacts: facts,
		Observability:   ObservabilityRead{SeatHolders: 4, People: 4, ToolSessions: 40, Coverage: 0.5},
		BranchRoles:     fakeRoles{roles: map[string]BranchRole{"R/" + releaseBr: {Shipped: true}}, known: true},
		AuthorPersonIDs: authors,
	}
}

// TestFoldGolden_EveryLiveCellDecided proves the folds produce a reading for every
// live signal: a rich fixture makes the 21 cells below decidable, so any of them that
// abstained would mean a missing producer. It also scores the readings end to end and
// asserts a sane profile (established frontiers, closed floors).
func TestFoldGolden_EveryLiveCellDecided(t *testing.T) {
	folded := Fold(buildFixture())
	res := aidlc.Score(rules.ForVersion("v1"), folded.Readings, folded.Org, nil)
	got := cellStates(res)

	// These cells read signals the fixture fully supplies, so each must be decided
	// (met or not_reached), never an abstain: an abstain here would mean a signal with
	// no producer or a population the fixture failed to clear.
	liveCells := []string{
		"product_spec/L1", "product_spec/L3",
		"tech_spec/L1", "tech_spec/L2", "tech_spec/L3",
		"code/L1", "code/L3", "code/L4",
		"review/L1", "review/L3", "review/L4",
		"test_qa/L2", "test_qa/L3",
		"ci_cd/L1", "ci_cd/L2", "ci_cd/L3",
		"deploy/L1", "deploy/L2", "deploy/L3",
		"operate/L2", "operate/L3",
	}
	for _, cell := range liveCells {
		st := got[cell]
		if st == "" {
			t.Fatalf("cell %s missing from result", cell)
		}
		if isAbstain(st) {
			t.Errorf("live cell %s abstained (%s) — a live signal has no producer or a population floor was not cleared", cell, st)
		}
	}

	// A sane profile: at least one stage has an established frontier, and downward
	// closure has floored code L2 under a met code L3.
	if got["code/L3"] == "not_reached" {
		t.Errorf("code/L3 should be met given the fixture, got %s", got["code/L3"])
	}
	if s := got["code/L2"]; s != "met(floor)" && s != "met(established)" && s != "met(emerging)" {
		t.Errorf("code/L2 should be a floored/met cell under met L3, got %s", s)
	}
	anyEstablished := false
	for _, sv := range res.Stages {
		if sv.Frontier != aidlcv1.AidlcLevel_AIDLC_LEVEL_UNSPECIFIED {
			anyEstablished = true
		}
	}
	if !anyEstablished {
		t.Errorf("expected at least one established frontier in the fixture profile")
	}
}

// TestFoldGolden_AmbientExcludedAndAbsenceNotZero pins two guarantees: an ambient
// artifact (a hook) invoked by everyone never reaches a level, and a signal with no
// facts abstains rather than scoring zero.
func TestFoldGolden_AmbientExcludedAndAbsenceNotZero(t *testing.T) {
	// Only ambient (hook) usage and an empty PR set.
	invs := []HarnessInvocation{}
	sm := map[string]aidlcv1.AidlcStage{"h": aidlcv1.AidlcStage_AIDLC_STAGE_CODE}
	for p := 0; p < 8; p++ {
		invs = append(invs, HarnessInvocation{PersonID: fmt.Sprintf("p%d", p), ArtifactID: "h", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_HOOK})
	}
	roster := []string{"p0", "p1", "p2", "p3", "p4", "p5", "p6", "p7"}
	folded := Fold(Facts{Roster: roster, Invocations: invs, StageMap: sm})
	res := aidlc.Score(rules.ForVersion("v1"), folded.Readings, folded.Org, nil)
	got := cellStates(res)

	// review L1: the ambient hook is the only "review" usage; a zero presence
	// numerator that only ever sees an instrumented artifact abstains, never denies.
	if s := got["review/L1"]; !isAbstain(s) {
		t.Errorf("review/L1 should abstain on an all-ambient org, got %s", s)
	}
	// code L1: empty denominator, fully observed -> abstain(no_population), never met.
	if s := got["code/L1"]; s == "met(established)" || s == "met(emerging)" || s == "not_reached" {
		t.Errorf("code/L1 with no PRs should abstain, got %s", s)
	}
}

// ---- helpers -----------------------------------------------------------------

func cellStates(res *aidlc.Result) map[string]string {
	out := map[string]string{}
	for _, sv := range res.Stages {
		for _, c := range sv.Cells {
			out[stageLevelKey(sv.Stage, c.Level)] = compactState(c.State, c.DecidedBy, c.Band)
		}
	}
	return out
}

func isAbstain(s string) bool { return len(s) >= 7 && s[:7] == "abstain" }

func stageLevelKey(stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel) string {
	return stageName(stage) + "/" + levelName(level)
}

func compactState(state aidlcv1.CellState, by aidlcv1.DecidedBy, band aidlcv1.PrevalenceBand) string {
	switch state {
	case aidlcv1.CellState_CELL_STATE_MET:
		if by == aidlcv1.DecidedBy_DECIDED_BY_IMPLIED_FLOOR {
			return "met(floor)"
		}
		switch band {
		case aidlcv1.PrevalenceBand_PREVALENCE_BAND_ESTABLISHED:
			return "met(established)"
		case aidlcv1.PrevalenceBand_PREVALENCE_BAND_EMERGING:
			return "met(emerging)"
		}
		return "met"
	case aidlcv1.CellState_CELL_STATE_NOT_REACHED:
		return "not_reached"
	case aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE:
		return "abstain"
	}
	return "unspecified"
}

func stageName(s aidlcv1.AidlcStage) string {
	switch s {
	case aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC:
		return "product_spec"
	case aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC:
		return "tech_spec"
	case aidlcv1.AidlcStage_AIDLC_STAGE_PLAN:
		return "plan"
	case aidlcv1.AidlcStage_AIDLC_STAGE_CODE:
		return "code"
	case aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW:
		return "review"
	case aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA:
		return "test_qa"
	case aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD:
		return "ci_cd"
	case aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY:
		return "deploy"
	case aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE:
		return "operate"
	}
	return "unspecified"
}

func levelName(l aidlcv1.AidlcLevel) string {
	switch l {
	case aidlcv1.AidlcLevel_AIDLC_LEVEL_L1:
		return "L1"
	case aidlcv1.AidlcLevel_AIDLC_LEVEL_L2:
		return "L2"
	case aidlcv1.AidlcLevel_AIDLC_LEVEL_L3:
		return "L3"
	case aidlcv1.AidlcLevel_AIDLC_LEVEL_L4:
		return "L4"
	case aidlcv1.AidlcLevel_AIDLC_LEVEL_L5:
		return "L5"
	}
	return "L0"
}
