// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// This file renders every served string a reader meets (summary, per-stage headline,
// cell reasons, the coverage block, constraints and their moves) to plain text, one
// page per profile shape, under testdata/pages/. TestRenderedPages compares every
// rendered page with the committed one, so any change to the reader-facing copy shows
// up as a reviewable diff of whole pages; with AIDLC_UPDATE_PAGES=1 it rewrites them
// instead.

// levelLabelOrNone renders a frontier/emerging-edge level for a page: "none" for
// AIDLC_LEVEL_UNSPECIFIED (a real, common state — most stages have no frontier), else
// levelTitle's label.
func levelLabelOrNone(l aidlcv1.AidlcLevel) string {
	if l == aidlcv1.AidlcLevel_AIDLC_LEVEL_UNSPECIFIED {
		return "none"
	}
	return levelTitle(l)
}

// renderProfilePage renders one MaturityProfile's served strings as plain text, in the
// order a reader meets them: the summary, each stage's headline
// with its row or per-cell abstain reasons, the coverage block, then every constraint
// with the move (if any) that answers it. It renders exactly the fields the wire
// profile carries — nothing here is copy this function itself composes.
func renderProfilePage(title string, p *aidlcv1.MaturityProfile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n\n", title, strings.Repeat("=", len(title)))
	fmt.Fprintf(&b, "Summary: %s\n", p.GetSummary())

	b.WriteString("\nStages:\n")
	for _, st := range p.GetStages() {
		fmt.Fprintf(&b, "- %s (established: %s; emerging: %s)\n", stageTitle(st.GetStage()), levelLabelOrNone(st.GetFrontier()), levelLabelOrNone(st.GetEmergingEdge()))
		if r := st.GetAbstainReason(); r != "" {
			fmt.Fprintf(&b, "    Whole row: %s\n", r)
		}
		// A per-cell reason is served even when the row also carries the whole-row
		// sentence above, so the page shows every reason a cell can carry, not only
		// the row-level one.
		for _, c := range st.GetCells() {
			if c.GetReason() != "" {
				fmt.Fprintf(&b, "    %s: %s\n", levelTitle(c.GetLevel()), c.GetReason())
			}
		}
	}

	if items := p.GetCoverageItems(); len(items) > 0 {
		b.WriteString("\nWhat we cannot see:\n")
		for _, it := range items {
			side := "ours"
			if it.GetList() == aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_THEIRS {
				side = "yours"
			}
			fmt.Fprintf(&b, "- (%s) %s\n", side, it.GetSentence())
		}
	}

	if bns := p.GetBottlenecks(); len(bns) > 0 {
		b.WriteString("\nConstraints and the moves that clear them:\n")
		for _, bn := range bns {
			fmt.Fprintf(&b, "- %s\n  %s\n", bn.GetTitle(), bn.GetBody())
			for _, r := range p.GetRecommendations() {
				if r.GetAddresses() == bn.GetId() {
					fmt.Fprintf(&b, "  Move: %s\n  %s\n", r.GetTitle(), r.GetBody())
				}
			}
		}
	}
	return b.String()
}

// --- fixture builders ------------------------------------------------------------
//
// Each returns the rendered page for one profile shape. Numbers are chosen for
// realism; TestRungMissedCopy_WorkedExamples and
// TestBottleneckCopy_NamesThePracticeNotTheVerdict pin the copy these fixtures
// exercise sentence by sentence.

// fixtureHappyPath: a normally-rendering organisation, with several stages
// established, review's autonomous level not yet reached at 210 of 1,507, and the
// deploy ladder stalled. "Happy path" is a genuine mix of established stages and a
// couple of open constraints, not a zero-finding profile: a rubric that always clears
// could not also be informative.
func fixtureHappyPath() (string, *aidlcv1.MaturityProfile) {
	rs := rules.ForVersion("v1")
	rd := NewReadings()
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	productSpec := aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC
	techSpec := aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC
	ciCD := aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY

	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, unspec, withPeople(resolved(20), 20))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_HARNESS_SEAT_HOLDERS, productSpec, withPeople(resolved(15), 5))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_HARNESS_SEAT_HOLDERS, techSpec, withPeople(resolved(15), 5))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CI_TOOL_SEAT_HOLDERS_BY_FUNCTION, ciCD, withPeople(resolved(15), 5))

	// code: emerging through L3; L4 not reached.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(180), 8))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(520))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, withPeople(resolved(40), 8))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(180))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, code, withPeople(resolved(35), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(0.55))

	// review: L1/L3 established; L4 not reached at 210 of 1,507.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS, review, withPeople(resolved(5), 5))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_BOT_PRESENT_PRS, review, resolved(18))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_RULE_APPLICABLE_PRS, review, resolved(24))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_BOT_CLEARED_NO_HUMAN, review, withPeople(resolved(210), 30))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_BOT_SEEN_PRS, review, resolved(1507))

	// deploy: the ladder stalls (nothing met).
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_HARNESS_SEAT_HOLDERS, deploy, withPeople(resolved(1), 1))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_RELEASE_PRS_MERGED, deploy, withPeople(resolved(1), 1))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_SEAT_LINKED_ARTIFACTS, deploy, resolved(30))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_RELEASE_PRS_MERGED_AFTER_REVIEW, deploy, withPeople(resolved(0), 0))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_AGENT_UNIVERSE, deploy, resolved(25))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, deploy, resolved(0.6))

	res := Score(rs, rd, OrgRead{}, nil)
	return "Happy path: a normally-rendering organisation", res.Profile
}

// fixtureNoFrontier: nothing established or emerging anywhere — an organisation with no
// readable telemetry at all.
func fixtureNoFrontier() (string, *aidlcv1.MaturityProfile) {
	res := Score(rules.ForVersion("v1"), NewReadings(), OrgRead{}, nil)
	return "Fixture label, not shown to users: nothing established or emerging anywhere", res.Profile
}

// fixtureWholeRowAbstain: several stages (review, plan, test & QA, deploy) have no
// signal at all and abstain as whole rows, while product spec, tech spec, CI/CD and
// code are decided — the contrast a reader needs to see is "some of this map reads,
// some of it doesn't yet", not "this map is broken". No adoption cohort here (unlike
// fixtureEveryConstraintKind), so the page stays distinct: three constraint kinds, not
// all four.
func fixtureWholeRowAbstain() (string, *aidlcv1.MaturityProfile) {
	res := Score(rules.ForVersion("v1"), tokenSweepBaseReadings(), OrgRead{}, nil)
	return "Whole-row abstain: some stages have not been read yet", res.Profile
}

// fixtureAbstainWeCannotSeeThis: the "We cannot see this" treatment (GAP,
// COVERAGE, ROW_ABSENT, INSTRUMENTED_ONLY, AUTOMATED_ONLY) alongside a decided stage,
// so the page shows the contrast a reader needs — some of the map reads, the rest is
// ours to close — rather than a uniformly blank map (that state is fixtureNoFrontier).
func fixtureAbstainWeCannotSeeThis() (string, *aidlcv1.MaturityProfile) {
	rd := NewReadings()
	productSpec := aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(20), 20))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_HARNESS_SEAT_HOLDERS, productSpec, withPeople(resolved(15), 5))
	res := Score(rules.ForVersion("v1"), rd, OrgRead{DeclarationsHonoured: false}, nil)
	return "Fixture label, not shown to users: the abstain treatment We cannot see this", res.Profile
}

// fixtureAbstainNothingHappened: the "Nothing of this kind happened" treatment
// (NO_POPULATION) — code L1 with a confirmed, fully-observed zero population.
func fixtureAbstainNothingHappened() (string, *aidlcv1.MaturityProfile) {
	rd := NewReadings()
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(10), 10))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(0), 0))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(0))
	res := Score(rules.ForVersion("v1"), rd, OrgRead{}, nil)
	return "Fixture label, not shown to users: the abstain treatment Nothing of this kind happened", res.Profile
}

// fixtureAbstainTooLittleToReport: the "Too little to report" treatment
// (POPULATION_FLOOR) — code L1 with a real but too-small population.
func fixtureAbstainTooLittleToReport() (string, *aidlcv1.MaturityProfile) {
	rd := NewReadings()
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(10), 10))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(3), 2))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(8))
	res := Score(rules.ForVersion("v1"), rd, OrgRead{}, nil)
	return "Fixture label, not shown to users: the abstain treatment Too little to report", res.Profile
}

// fixtureAbstainNotSettled: the "Not settled" treatment (JUDGMENT): plan L1 is a
// judgment cell, alongside a decided stage so the page is not another uniformly blank
// map.
func fixtureAbstainNotSettled() (string, *aidlcv1.MaturityProfile) {
	rd := NewReadings()
	techSpec := aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(20), 20))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_HARNESS_SEAT_HOLDERS, techSpec, withPeople(resolved(15), 5))
	res := Score(rules.ForVersion("v1"), rd, OrgRead{}, nil)
	return "Fixture label, not shown to users: the abstain treatment Not settled", res.Profile
}

// fixtureEveryConstraintKind: an organisation whose profile carries all four
// constraint kinds at once — review reach, adoption reach, a missed rung and a
// stalled ladder, so every constraint's copy renders on one page. Two different G4
// blockers render side by side: nothing is enforced for Plan, Code or Test & QA (the
// enforcement line), while Product spec and Review have an enforced practice and
// close only on the declaration check. With the organisation's declarations honoured
// but nothing declared, that pair alone takes the "declare your review policy" line.
func fixtureEveryConstraintKind() (string, *aidlcv1.MaturityProfile) {
	rd := tokenSweepBaseReadings()
	planL5 := aidlcv1.AidlcStage_AIDLC_STAGE_PLAN
	codeL5 := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	testQA := aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA
	productSpec := aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED

	// Plan, Code and Test & QA L5: no admin-pushed practice artifact is enforced
	// for these stages yet — the enforcement-only case.
	for _, stage := range []aidlcv1.AidlcStage{planL5, codeL5, testQA} {
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, stage, gapBlockedReading())
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, stage, gapBlockedReading())
	}
	// Product spec and Review L5: enforcement resolves normally; the declaration
	// secondary is what closes the cell.
	for _, stage := range []aidlcv1.AidlcStage{productSpec, review} {
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, stage, withPeople(resolved(10), 10))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, stage, withPeople(resolved(5), 5))
	}
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS, unspec, gapBlockedReading())
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS, unspec, gapBlockedReading())

	res := Score(rules.ForVersion("v1"), rd, OrgRead{
		HasAdoptionCohort: true, AdoptionDenominatorPeople: 18, AdoptionPassivePeople: 11,
		DeclarationsHonoured: true,
	}, nil)
	return "An organisation with every constraint kind", res.Profile
}

// actionPhrasePage renders every entry of the action table (cellCopies and
// twoShareCellCopies) as a constraint title and the move title that answers it, in
// stage and level order, so every phrase the tables carry renders at least once.
// The fixtures above reach only the few cells their organisations fall short on.
func actionPhrasePage() string {
	title := "Action phrases: every cell a level can fall short on"
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n%s\n\nConstraints and the moves that clear them:\n", title, strings.Repeat("=", len(title)))
	entry := func(label string, cc cellCopy) {
		fmt.Fprintf(&b, "- %s\n  Constraint: %s.\n  Move: %s.\n", label, cc.finding, cc.action)
	}
	for _, stage := range stageOrder {
		for _, level := range ladderOrder {
			label := fmt.Sprintf("%s at %s", stageTitle(stage), levelTitle(level))
			if level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L5 {
				if ts, ok := twoShareCellCopies[stage]; ok {
					entry(label+" (enforced practice)", ts.primary)
					entry(label+" (declared policy)", ts.secondary)
					continue
				}
			}
			if cc, ok := cellCopies[stage][level]; ok {
				entry(label, cc)
			}
		}
	}
	return b.String()
}

// pagesDir is where the committed pages live.
var pagesDir = filepath.Join("testdata", "pages")

// updatePagesEnv, set to 1, makes TestRenderedPages rewrite the committed pages from
// the current copy instead of comparing against them.
const updatePagesEnv = "AIDLC_UPDATE_PAGES"

type renderedPage struct {
	file string
	text string
}

// renderedPages renders the nine pages, deterministically, in file order.
func renderedPages() []renderedPage {
	var pages []renderedPage
	add := func(file string, build func() (string, *aidlcv1.MaturityProfile)) {
		title, p := build()
		pages = append(pages, renderedPage{file, renderProfilePage(title, p)})
	}
	add("01-happy-path.txt", fixtureHappyPath)
	add("02-no-frontier.txt", fixtureNoFrontier)
	add("03-whole-row-abstain.txt", fixtureWholeRowAbstain)
	add("04-abstain-we-cannot-see-this.txt", fixtureAbstainWeCannotSeeThis)
	add("05-abstain-nothing-happened.txt", fixtureAbstainNothingHappened)
	add("06-abstain-too-little-to-report.txt", fixtureAbstainTooLittleToReport)
	add("07-abstain-not-settled.txt", fixtureAbstainNotSettled)
	add("08-every-constraint-kind.txt", fixtureEveryConstraintKind)
	pages = append(pages, renderedPage{"11-action-phrases.txt", actionPhrasePage()})
	return pages
}

// TestRenderedPages fails when a rendered page differs from its committed file, so a
// copy change cannot land without its whole-page diff. With AIDLC_UPDATE_PAGES=1 it
// rewrites the committed pages instead.
func TestRenderedPages(t *testing.T) {
	update := os.Getenv(updatePagesEnv) == "1"
	for _, pg := range renderedPages() {
		full := filepath.Join(pagesDir, pg.file)
		if update {
			if err := os.WriteFile(full, []byte(pg.text), 0o644); err != nil {
				t.Fatalf("write %s: %v", full, err)
			}
			continue
		}
		committed, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("read %s: %v", full, err)
		}
		if string(committed) != pg.text {
			t.Errorf("%s differs from the rendered copy. Review the change, then regenerate with %s=1 go test -run TestRenderedPages ./aidlc/", pg.file, updatePagesEnv)
		}
	}
}
