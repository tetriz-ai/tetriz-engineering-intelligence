// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// These tests pin the coverage block: the scorer composes coverage_items from its own
// block-level copy (coverage_items.go), never from a cell's Reason, which may point
// at the cell it describes and name what would unlock it. checkBlockSentence below holds a
// block sentence to that: no level, no cell count, and none of the cell-reason-only
// vocabulary.

func testRuleSet(windowDays int) *rules.RuleSet { return &rules.RuleSet{WindowDays: windowDays} }

// abstainCell builds a Cell already decided as a non-gap abstain (its Gap
// stays UNSPECIFIED — use abstainGapCell for a GAP-kind cell that must carry one).
func abstainCell(stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel, kind abstainKind, reason string) *Cell {
	return &Cell{
		Stage: stage, Level: level, abstain: kind, Reason: reason,
		State: aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE,
	}
}

// abstainGapCell builds a GAP-kind abstain naming a specific instrumentation gap
// (or UNSPECIFIED for the no-resolver "unreadable number" shape). reason is
// deliberately allowed to be arbitrary cell-reason-only prose in the tests below — the
// point is that coverage_items.go must never echo it.
func abstainGapCell(stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel, gap aidlcv1.InstrumentationGap, reason string) *Cell {
	c := abstainCell(stage, level, abGap, reason)
	c.Gap = gap
	return c
}

// decidedCell is a MET/NOT_REACHED filler cell — used to give a stage verdict a
// non-abstaining cell where a test needs one (e.g. an L5 rung that IS decided, to
// prove the L5 disclosure does not fire).
func decidedCell(stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel) *Cell {
	return &Cell{Stage: stage, Level: level, State: aidlcv1.CellState_CELL_STATE_MET}
}

func verdict(stage aidlcv1.AidlcStage, cells ...*Cell) StageVerdict {
	return StageVerdict{Stage: stage, Cells: cells}
}

func oursSentences(items []*aidlcv1.MaturityCoverageItem) []string {
	var out []string
	for _, it := range items {
		if it.GetList() == aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS {
			out = append(out, it.GetSentence())
		}
	}
	return out
}

// findItem returns the (list, sentence) item covering stage, or nil.
func findItem(items []*aidlcv1.MaturityCoverageItem, list aidlcv1.MaturityCoverageList, sentence string, stage aidlcv1.AidlcStage) *aidlcv1.MaturityCoverageItem {
	for _, it := range items {
		if it.GetList() != list || it.GetSentence() != sentence {
			continue
		}
		for _, s := range it.GetStages() {
			if s == stage {
				return it
			}
		}
	}
	return nil
}

// ---- the cell-reason/block copy boundary ----------------------------------------

// checkBlockSentence holds a block sentence to its rule: it must never pair a stage title with
// a level label (which would name a cell), never carry a cell count, and never carry
// the cell-reason vocabulary that points at "this cell": a bare level reference
// ("this level", "a higher level"), a run reference ("this run"), a single-signal
// reference ("this signal"), an unlock or roadmap clause ("until we do", "this would
// let us"), or the "Ours:"/"Yours:" prefix a cell's reason uses (the block's own list
// already says whose it is).
func checkBlockSentence(t *testing.T, where, sentence string) {
	t.Helper()
	namesStage, namesLevel := false, false
	for _, title := range stageTitles {
		if strings.Contains(sentence, title) {
			namesStage = true
			break
		}
	}
	for _, title := range levelTitles {
		if strings.Contains(strings.ToLower(sentence), title) {
			namesLevel = true
			break
		}
	}
	if namesStage && namesLevel {
		t.Errorf("%s: sentence pairs a stage title with a level label %q", where, sentence)
	}
	if m := ofNCellsPhrase.FindString(sentence); m != "" {
		t.Errorf("%s: sentence carries a cell count %q in %q", where, m, sentence)
	}
	banned := []string{
		"this level", "this run", "this signal", "a higher level",
		"until we do", "this would let us", "Ours:", "Ours.", "Yours:",
	}
	for _, phrase := range banned {
		if strings.Contains(sentence, phrase) {
			t.Errorf("%s: sentence carries the cell-reason-only phrase %q in %q", where, phrase, sentence)
		}
	}
}

// TestCoverageItems_NeverEchoesCellReason pins that the block never echoes a cell reason: every
// ours-eligible kind is fed a Reason full of exactly the phrases checkBlockSentence bans, and
// the composed item must carry none of them — proving coverage_items.go composes its own
// text rather than passing the cell's Reason through.
func TestCoverageItems_NeverEchoesCellReason(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	l1 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L1
	poison := "Ours: this level cannot be read this run because this signal disagrees. Yours: until we do, this would let us tell a higher level apart."

	tests := []struct {
		name string
		cell *Cell
	}{
		{"gap_with_real_id", abstainGapCell(code, l1, aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G1, poison)},
		{"gap_no_resolver", abstainGapCell(code, l1, aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_UNSPECIFIED, poison)},
		{"row_absent", abstainCell(code, l1, abRowAbsent, poison)},
		{"instrumented_only", abstainCell(code, l1, abInstrumentedOnly, poison)},
		{"automated_only", abstainCell(code, l1, abAutomatedOnly, poison)},
		{"coverage", abstainCell(code, l1, abCoverage, poison)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := coverageItems(testRuleSet(90), []StageVerdict{verdict(code, tt.cell)})
			for _, it := range items {
				if it.GetSentence() == poison || strings.Contains(it.GetSentence(), "this run") {
					t.Fatalf("%s: item echoes the cell's Reason: %q", tt.name, it.GetSentence())
				}
				checkBlockSentence(t, tt.name, it.GetSentence())
			}
		})
	}
}

// TestCoverageItems_ImpliedFloorProducesNoItem pins that an unresolved
// implied-floor cell (the real scorer's shape: RuleKindImpliedFloor, no met rung
// above it yet, so it still carries reasonImpliedFloorPending) contributes no item
// at all — not even the unreadable-number bucket.
func TestCoverageItems_ImpliedFloorProducesNoItem(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	l2 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L2
	c := abstainGapCell(code, l2, aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G3, reasonImpliedFloorPending)
	items := coverageItems(testRuleSet(90), []StageVerdict{verdict(code, c)})
	if len(items) != 0 {
		t.Fatalf("implied-floor cell: got %d items, want none: %v", len(items), items)
	}
}

// TestCoverageItems_GapKeyedDedup drives real gap ids through the composer: three
// distinct gaps over five cells yield exactly three ours items, each passing
// checkBlockSentence.
func TestCoverageItems_GapKeyedDedup(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	ciCD := aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD
	deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY
	operate := aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE
	techSpec := aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC
	g1, g7, g8 := aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G1,
		aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G7, aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G8

	// Deliberately no cell at L5: a partial grid with an L5 cell would trip the L5
	// disclosure vacuously (see TestCoverageItems_L5Disclosure), which is a fact
	// about a partial fixture, not about dedup.
	stages := []StageVerdict{
		verdict(ciCD, abstainGapCell(ciCD, aidlcv1.AidlcLevel_AIDLC_LEVEL_L1, g1, "reason a")),
		verdict(techSpec, abstainGapCell(techSpec, aidlcv1.AidlcLevel_AIDLC_LEVEL_L4, g1, "reason b")), // dup of g1
		verdict(deploy, abstainGapCell(deploy, aidlcv1.AidlcLevel_AIDLC_LEVEL_L2, g7, "reason c")),
		verdict(operate, abstainGapCell(operate, aidlcv1.AidlcLevel_AIDLC_LEVEL_L3, g7, "reason d")), // dup of g7
		verdict(code, abstainGapCell(code, aidlcv1.AidlcLevel_AIDLC_LEVEL_L4, g8, "reason e")),
	}

	items := coverageItems(testRuleSet(90), stages)
	ours := oursSentences(items)
	if len(ours) != 3 {
		t.Fatalf("5 cells over 3 distinct gaps: got %d ours items, want 3: %v", len(ours), ours)
	}
	for _, s := range ours {
		checkBlockSentence(t, "gap dedup", s)
	}
	// The two duplicated gaps' union of stages is carried on one item each.
	g1Item := findItem(items, aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, mustOursGapSentence(t, g1), ciCD)
	if g1Item == nil || findItem(items, aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, mustOursGapSentence(t, g1), techSpec) == nil {
		t.Errorf("g1 item should cover both ci_cd and tech_spec, got %v", items)
	}
}

func mustOursGapSentence(t *testing.T, g aidlcv1.InstrumentationGap) string {
	t.Helper()
	s, ok := oursGapSentence(g)
	if !ok {
		t.Fatalf("gap %v has no ours sentence", g)
	}
	return s
}

// TestCoverageItems_G2FollowsTheBlocker pins that a G2 cell's block line follows what
// the scorer recorded as closing it: a rule that does not measure planning lists
// under ours with no route; an empty tracker window lists under theirs, one item
// naming the window with its stages unioned, routed to the stage row.
func TestCoverageItems_G2FollowsTheBlocker(t *testing.T) {
	plan := aidlcv1.AidlcStage_AIDLC_STAGE_PLAN
	techSpec := aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC
	g2 := aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G2
	g2Cell := func(stage aidlcv1.AidlcStage, b g2Blocker) *Cell {
		c := abstainGapCell(stage, aidlcv1.AidlcLevel_AIDLC_LEVEL_L2, g2, "reason")
		c.g2Blocker = b
		return c
	}
	noIssues := fmt.Sprintf(theirsG2NoIssuesSentenceFmt, 90)

	// Deliberately no cell at L5 (see TestCoverageItems_GapKeyedDedup's comment): a
	// partial grid with an L5 cell trips the L5 disclosure vacuously.
	tests := []struct {
		name      string
		blocker   g2Blocker
		list      aidlcv1.MaturityCoverageList
		sentence  string
		route     aidlcv1.MaturityCoverageRoute
		otherList aidlcv1.MaturityCoverageList
	}{
		{
			name: "not measured is ours", blocker: g2BlockerNotMeasured,
			list: aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, sentence: oursG2NotMeasuredSentence,
			route:     aidlcv1.MaturityCoverageRoute_MATURITY_COVERAGE_ROUTE_UNSPECIFIED,
			otherList: aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_THEIRS,
		},
		{
			name: "no issues in the window is theirs, routed to the stage row", blocker: g2BlockerNoIssues,
			list: aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_THEIRS, sentence: noIssues,
			route:     aidlcv1.MaturityCoverageRoute_MATURITY_COVERAGE_ROUTE_STAGE_ROW,
			otherList: aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := coverageItems(testRuleSet(90), []StageVerdict{
				verdict(plan, g2Cell(plan, tt.blocker)),
				verdict(techSpec, g2Cell(techSpec, tt.blocker)),
			})
			if len(items) != 1 {
				t.Fatalf("want one deduplicated item, got %v", items)
			}
			for _, stage := range []aidlcv1.AidlcStage{plan, techSpec} {
				item := findItem(items, tt.list, tt.sentence, stage)
				if item == nil {
					t.Fatalf("no %v item %q covering %v: %v", tt.list, tt.sentence, stage, items)
				}
				if item.GetRoute() != tt.route {
					t.Errorf("route = %v, want %v", item.GetRoute(), tt.route)
				}
			}
			checkBlockSentence(t, "g2 "+tt.name, tt.sentence)
		})
	}
}

// TestCoverageItems_G4FollowsTheBlocker pins that a G4 cell's block lines are the
// blockers the scorer recorded on it, one line per blocker, each deduplicated across
// stages: the declare line routes to org settings, the enforcement line carries no
// route and names every unenforced stage in one sentence, and the two ours lines
// carry no route. The end-to-end cases over the real scorer are in
// TestG4_CopyFollowsTheBlockingCheck.
func TestCoverageItems_G4FollowsTheBlocker(t *testing.T) {
	productSpec := aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC
	plan := aidlcv1.AidlcStage_AIDLC_STAGE_PLAN
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	l3 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L3
	g4 := aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G4
	theirs := aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_THEIRS
	ours := aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS

	// Deliberately no cell at L5 (see TestCoverageItems_GapKeyedDedup's comment): a
	// partial grid with an L5 cell trips the L5 disclosure vacuously.
	g4Cell := func(stage aidlcv1.AidlcStage, blockers ...g4Blocker) *Cell {
		c := abstainGapCell(stage, l3, g4, "reason")
		c.g4Blockers = blockers
		return c
	}
	type line struct {
		list     aidlcv1.MaturityCoverageList
		sentence string
		route    aidlcv1.MaturityCoverageRoute
		stages   []aidlcv1.AidlcStage
	}
	tests := []struct {
		name   string
		stages []StageVerdict
		want   []line
	}{
		{
			name: "undeclared on two stages is one declare line routed to settings",
			stages: []StageVerdict{
				verdict(productSpec, g4Cell(productSpec, g4BlockerUndeclared)),
				verdict(review, g4Cell(review, g4BlockerUndeclared)),
			},
			want: []line{{theirs, theirsG4UndeclaredSentence, aidlcv1.MaturityCoverageRoute_MATURITY_COVERAGE_ROUTE_ORG_SETTINGS, []aidlcv1.AidlcStage{productSpec, review}}},
		},
		{
			name: "unenforced on several stages is one enforcement line naming them, with no route",
			stages: []StageVerdict{
				verdict(plan, g4Cell(plan, g4BlockerUnenforced)),
				verdict(review, g4Cell(review, g4BlockerUnenforced)),
			},
			want: []line{{
				theirs, "Your organisation enforces no practice in its agent setup for Plan and Review yet; once one is enforced, we read how widely it is followed.",
				aidlcv1.MaturityCoverageRoute_MATURITY_COVERAGE_ROUTE_UNSPECIFIED,
				[]aidlcv1.AidlcStage{plan, review},
			}},
		},
		{
			name: "a cell missing enforcement and its declaration lists both lines",
			stages: []StageVerdict{
				verdict(plan, g4Cell(plan, g4BlockerUnenforced)),
				verdict(review, g4Cell(review, g4BlockerUnenforced, g4BlockerUndeclared)),
			},
			want: []line{
				{
					theirs, "Your organisation enforces no practice in its agent setup for Plan and Review yet; once one is enforced, we read how widely it is followed.",
					aidlcv1.MaturityCoverageRoute_MATURITY_COVERAGE_ROUTE_UNSPECIFIED,
					[]aidlcv1.AidlcStage{plan, review},
				},
				{theirs, theirsG4UndeclaredSentence, aidlcv1.MaturityCoverageRoute_MATURITY_COVERAGE_ROUTE_ORG_SETTINGS, []aidlcv1.AidlcStage{review}},
			},
		},
		{
			name: "not honoured is ours, with no route",
			stages: []StageVerdict{
				verdict(productSpec, g4Cell(productSpec, g4BlockerNotHonoured)),
				verdict(review, g4Cell(review, g4BlockerNotHonoured)),
			},
			want: []line{{ours, oursG4NotHonouredSentence, aidlcv1.MaturityCoverageRoute_MATURITY_COVERAGE_ROUTE_UNSPECIFIED, []aidlcv1.AidlcStage{productSpec, review}}},
		},
		{
			name:   "a rung that reads neither is ours, with no route",
			stages: []StageVerdict{verdict(productSpec, g4Cell(productSpec, g4BlockerUninstrumented))},
			want:   []line{{ours, oursG4UninstrumentedSentence, aidlcv1.MaturityCoverageRoute_MATURITY_COVERAGE_ROUTE_UNSPECIFIED, []aidlcv1.AidlcStage{productSpec}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := coverageItems(testRuleSet(90), tt.stages)
			var got []line
			for _, it := range items {
				got = append(got, line{it.GetList(), it.GetSentence(), it.GetRoute(), it.GetStages()})
				checkBlockSentence(t, tt.name, it.GetSentence())
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("items\n got %v\nwant %v", got, tt.want)
			}
		})
	}
}

// TestCoverageItems_UnreadableNumberDedup is the "one for the unreadable-number
// case": ROW_ABSENT and a GAP with no gap id (the no-resolver
// path) are the same fact to the reader and collapse into one ours item, whatever
// stage they came from.
func TestCoverageItems_UnreadableNumberDedup(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY
	l1 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L1
	stages := []StageVerdict{
		verdict(code, abstainCell(code, l1, abRowAbsent, "reason a")),
		verdict(deploy, abstainGapCell(deploy, l1, aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_UNSPECIFIED, "reason b")),
	}
	items := coverageItems(testRuleSet(90), stages)
	ours := oursSentences(items)
	if len(ours) != 1 || ours[0] != oursUnreadableNumberSentence {
		t.Fatalf("got ours items %v, want exactly [%q]", ours, oursUnreadableNumberSentence)
	}
	if findItem(items, aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, oursUnreadableNumberSentence, code) == nil ||
		findItem(items, aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, oursUnreadableNumberSentence, deploy) == nil {
		t.Errorf("the one item should cover both code and deploy: %v", items)
	}
}

// TestCoverageItems_InstrumentedAndAutomatedOnly pins their fixed block sentences,
// distinct from each other and from the unreadable-number bucket.
func TestCoverageItems_InstrumentedAndAutomatedOnly(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	l1 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L1
	items := coverageItems(testRuleSet(90), []StageVerdict{
		verdict(code, abstainCell(code, l1, abInstrumentedOnly, "reason"), abstainCell(code, aidlcv1.AidlcLevel_AIDLC_LEVEL_L2, abAutomatedOnly, "reason")),
	})
	ours := oursSentences(items)
	if len(ours) != 2 {
		t.Fatalf("got %d ours items, want 2: %v", len(ours), ours)
	}
	if findItem(items, aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, oursInstrumentedOnlySentence, code) == nil {
		t.Errorf("missing instrumented_only sentence: %v", items)
	}
	if findItem(items, aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, oursAutomatedOnlySentence, code) == nil {
		t.Errorf("missing automated_only sentence: %v", items)
	}
}

// TestCoverageItems_CoveragePerStage pins COVERAGE's block line: one line per
// stage naming that stage's work, deduplicated by stage (two COVERAGE cells on the
// same stage still yield one line; a different stage gets its own, different line).
func TestCoverageItems_CoveragePerStage(t *testing.T) {
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY
	items := coverageItems(testRuleSet(90), []StageVerdict{
		verdict(review,
			abstainCell(review, aidlcv1.AidlcLevel_AIDLC_LEVEL_L1, abCoverage, "reason a"),
			abstainCell(review, aidlcv1.AidlcLevel_AIDLC_LEVEL_L3, abCoverage, "reason b"),
		),
		verdict(deploy, abstainCell(deploy, aidlcv1.AidlcLevel_AIDLC_LEVEL_L1, abCoverage, "reason c")),
	})
	ours := oursSentences(items)
	if len(ours) != 2 {
		t.Fatalf("two stages, one COVERAGE line each: got %d, want 2: %v", len(ours), ours)
	}
	reviewSentence := oursCoverageSentence(review)
	deploySentence := oursCoverageSentence(deploy)
	if reviewSentence == deploySentence {
		t.Fatalf("review and deploy must name their own work distinctly, both got %q", reviewSentence)
	}
	if !strings.Contains(reviewSentence, "review") {
		t.Errorf("review's line does not name review's work: %q", reviewSentence)
	}
	if findItem(items, aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, reviewSentence, review) == nil {
		t.Errorf("missing review coverage line: %v", items)
	}
	if findItem(items, aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, deploySentence, deploy) == nil {
		t.Errorf("missing deploy coverage line: %v", items)
	}
	for _, s := range ours {
		checkBlockSentence(t, "coverage per stage", s)
	}
}

// TestCoverageItems_NoPopulationPerStage pins NO_POPULATION's theirs line: one per
// stage naming that stage's work in the window, so Plan, Deploy and Operate each read
// differently rather than all sharing one generic sentence.
func TestCoverageItems_NoPopulationPerStage(t *testing.T) {
	plan := aidlcv1.AidlcStage_AIDLC_STAGE_PLAN
	deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY
	operate := aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE
	items := coverageItems(testRuleSet(90), []StageVerdict{
		verdict(plan, abstainCell(plan, aidlcv1.AidlcLevel_AIDLC_LEVEL_L1, abNoPopulation, "reason a")),
		verdict(deploy, abstainCell(deploy, aidlcv1.AidlcLevel_AIDLC_LEVEL_L1, abNoPopulation, "reason b")),
		verdict(operate, abstainCell(operate, aidlcv1.AidlcLevel_AIDLC_LEVEL_L1, abNoPopulation, "reason c")),
	})
	planSentence := theirsNoPopulationSentence(plan, 90)
	deploySentence := theirsNoPopulationSentence(deploy, 90)
	operateSentence := theirsNoPopulationSentence(operate, 90)
	if planSentence == deploySentence || deploySentence == operateSentence || planSentence == operateSentence {
		t.Fatalf("plan/deploy/operate must each read distinctly: %q / %q / %q", planSentence, deploySentence, operateSentence)
	}
	if !strings.Contains(planSentence, "90") {
		t.Errorf("no_population line must name the window: %q", planSentence)
	}
	for stage, sentence := range map[aidlcv1.AidlcStage]string{plan: planSentence, deploy: deploySentence, operate: operateSentence} {
		item := findItem(items, aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_THEIRS, sentence, stage)
		if item == nil {
			t.Fatalf("missing theirs item for stage %v: %v", stage, items)
		}
		if item.GetRoute() != aidlcv1.MaturityCoverageRoute_MATURITY_COVERAGE_ROUTE_STAGE_ROW {
			t.Errorf("stage %v: route = %v, want STAGE_ROW", stage, item.GetRoute())
		}
	}
}

// TestCoverageItems_NeitherListKinds pins the "no item" kinds:
// POPULATION_FLOOR and JUDGMENT never surface, whatever their Reason says.
// TestCoverageItems_NoPopulationIsStageWide pins that the no-population line is a
// claim about the whole stage: it is emitted only when every level of the stage with
// a measure of its own abstained NO_POPULATION, so a stage with a met lower level
// never reads "no work was seen".
func TestCoverageItems_NoPopulationIsStageWide(t *testing.T) {
	operate := aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE
	noPop := func(l aidlcv1.AidlcLevel) *Cell { return abstainCell(operate, l, abNoPopulation, "reason") }
	floor := abstainCell(operate, aidlcv1.AidlcLevel_AIDLC_LEVEL_L2, abGap, reasonImpliedFloorPending)
	const (
		l1 = aidlcv1.AidlcLevel_AIDLC_LEVEL_L1
		l2 = aidlcv1.AidlcLevel_AIDLC_LEVEL_L2
		l3 = aidlcv1.AidlcLevel_AIDLC_LEVEL_L3
		l4 = aidlcv1.AidlcLevel_AIDLC_LEVEL_L4
	)
	tests := []struct {
		name  string
		cells []*Cell
		want  int
	}{
		{"L1 met, L2 and up no population: no item", []*Cell{decidedCell(operate, l1), noPop(l2), noPop(l3), noPop(l4)}, 0},
		{"every level no population: one item", []*Cell{noPop(l1), noPop(l2), noPop(l3), noPop(l4)}, 1},
		{"a pending implied floor neither qualifies nor disqualifies", []*Cell{noPop(l1), floor, noPop(l3), noPop(l4)}, 1},
	}
	sentence := theirsNoPopulationSentence(operate, 90)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := coverageItems(testRuleSet(90), []StageVerdict{verdict(operate, tt.cells...)})
			got := 0
			for _, it := range items {
				if it.GetSentence() == sentence {
					got++
				}
			}
			if got != tt.want {
				t.Errorf("no-population items = %d, want %d: %v", got, tt.want, items)
			}
		})
	}
	checkBlockSentence(t, "no population", sentence)
}

func TestCoverageItems_NeitherListKinds(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	l1 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L1
	for _, kind := range []abstainKind{abPopulationFloor, abJudgment} {
		items := coverageItems(testRuleSet(90), []StageVerdict{verdict(code, abstainCell(code, l1, kind, "reason"))})
		if len(items) != 0 {
			t.Errorf("kind %v: got %d items, want none: %v", kind, len(items), items)
		}
	}
}

// TestCoverageItems_DisputedYieldsNoItem is DISPUTED's own row: never produced by the
// internal scorer today (no abstainKind maps to it), but coverageItems reads the
// PROTO kind off c.abstain.proto(), so a cell already carrying the wire DISPUTED kind
// (e.g. a stored profile from an adjudication path outside this scorer) must still
// resolve to no item, not to ours by default.
func TestCoverageItems_DisputedYieldsNoItem(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	l1 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L1
	c := abstainCell(code, l1, abNone, "should never surface")
	// abNone.proto() is UNSPECIFIED, which is not a real shape (only a decided cell
	// carries it); DISPUTED itself has no internal abstainKind to construct from, so
	// this test instead pins that an internal kind with no case in the switch — the
	// same closed-switch shape a DISPUTED cell would hit — produces nothing.
	items := coverageItems(testRuleSet(90), []StageVerdict{verdict(code, c)})
	if len(items) != 0 {
		t.Fatalf("kind with no ours/theirs case: got %d items, want none: %v", len(items), items)
	}
}

// TestCoverageItems_L5Disclosure pins the L5 disclosure: the ours list gains one
// per-organisation line when every L5 cell across the map abstains, and none when
// even one L5 cell is decided or none is present.
func TestCoverageItems_L5Disclosure(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	l5 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L5

	t.Run("fires_when_l5_fully_abstains", func(t *testing.T) {
		stages := []StageVerdict{
			verdict(code, abstainCell(code, l5, abJudgment, reasonJudgment)),
			verdict(review, abstainCell(review, l5, abJudgment, reasonJudgment)),
		}
		items := coverageItems(testRuleSet(90), stages)
		found := false
		for _, it := range items {
			if it.GetSentence() == l5DisclosureSentence {
				found = true
			}
		}
		if !found {
			t.Fatalf("l5 fully abstaining: disclosure missing from %v", items)
		}
		checkBlockSentence(t, "l5 disclosure", l5DisclosureSentence)
	})

	t.Run("names_this_organisation_not_every_organisation", func(t *testing.T) {
		if strings.Contains(l5DisclosureSentence, "any organisation") || strings.Contains(l5DisclosureSentence, "every organisation") {
			t.Fatalf("l5 disclosure must not make a claim about every organisation: %q", l5DisclosureSentence)
		}
		if !strings.Contains(l5DisclosureSentence, "this organisation") {
			t.Fatalf("l5 disclosure must scope to this organisation: %q", l5DisclosureSentence)
		}
	})

	t.Run("absent_once_any_l5_cell_decides", func(t *testing.T) {
		stages := []StageVerdict{
			verdict(code, decidedCell(code, l5)),
			verdict(review, abstainCell(review, l5, abJudgment, reasonJudgment)),
		}
		items := coverageItems(testRuleSet(90), stages)
		for _, it := range items {
			if it.GetSentence() == l5DisclosureSentence {
				t.Fatalf("one l5 cell decided: disclosure should not fire, got %v", items)
			}
		}
	})

	t.Run("absent_with_no_l5_cells_at_all", func(t *testing.T) {
		l1 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L1
		stages := []StageVerdict{verdict(code, abstainGapCell(code, l1, aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G1, "reason"))}
		items := coverageItems(testRuleSet(90), stages)
		for _, it := range items {
			if it.GetSentence() == l5DisclosureSentence {
				t.Fatalf("no l5 cell present: disclosure should not fire vacuously, got %v", items)
			}
		}
	})
}

// TestCoverageItems_TotalityOnGoldenFixtures runs the real Score() pipeline (not
// hand-built cells) over a fixture built to hit every list-bearing kind at once —
// gap (with and without a real id), row_absent, instrumented_only, coverage,
// no_population — plus judgment and population_floor, which must produce nothing.
// It proves totality both ways using the SAME composer functions the unit tests
// above pin as ground truth: every cannot-see abstain the real scorer produces has
// a matching item, and every item traces back to a cell of a kind the table lists.
// automated_only has no live signal in the registry today (no SignalDef declares
// ObservesAutomatedOnly), so it is proven at the unit level above instead of here —
// noted rather than silently skipped.
func TestCoverageItems_TotalityOnGoldenFixtures(t *testing.T) {
	rs := rules.ForVersion("v1")
	rd := NewReadings()
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	ciCD := aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD

	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, unspec, withPeople(resolved(10), 10))

	// code L1: coverage straddle (FixtureB's shape) -> abstain(coverage).
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(withNumRemainder(resolved(48), 120), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(180))
	// code L3: numerator row absent -> abstain(row_absent).
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, Reading{Status: StatusRowAbsent})
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(180))
	// code L4/L5 stay unset -> abstain(gap) with no gap id (no_resolver), the baseline
	// every unset prevalence cell takes; plan L1 and test & QA L1 abstain(judgment).
	// plan L3's tracker gate is closed (no issue in the window) -> G2, theirs.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS_HUMAN_REVIEWED, aidlcv1.AidlcStage_AIDLC_STAGE_PLAN, gapBlockedReading())
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_MERGED_PRS, unspec, resolved(40))
	// review L5's declaration check is closed and declarations are not honoured -> G4,
	// ours (the not-honoured line).
	setEnforced(rd, review)
	setReviewPolicyGapBlocked(rd)

	// review L1: zero on an instrumented-only signal -> abstain(instrumented_only).
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS, review, withPeople(resolved(0), 0))
	// review L3: empty, fully observed denominator and no hit -> abstain(no_population). (A
	// hit over an empty population is a disagreement instead.)
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_BOT_PRESENT_PRS, review, resolved(0))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_RULE_APPLICABLE_PRS, review, resolved(0))

	// ci_cd L1: a people denominator under the census floor -> abstain(population_floor).
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, unspec, withPeople(resolved(4), 4))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CI_TOOL_SEAT_HOLDERS_BY_FUNCTION, ciCD, withPeople(resolved(3), 3))

	res := Score(rs, rd, OrgRead{}, nil)
	items := res.Profile.GetCoverageItems()

	for _, it := range items {
		if it.GetList() == aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS {
			checkBlockSentence(t, "totality fixture "+it.GetSentence(), it.GetSentence())
		}
	}

	sawPopulationFloor, sawJudgment := false, false
	sawGapReal, sawGapUnreadable, sawRowAbsent, sawInstrumented, sawCoverage, sawNoPop, sawG2, sawG4 := false, false, false, false, false, false, false, false
	wantKeys := map[string]bool{} // "list|sentence", the reverse-direction allow-list
	key := func(list aidlcv1.MaturityCoverageList, sentence string) string {
		return fmt.Sprintf("%d|%s", list, sentence)
	}

	for _, sv := range res.Stages {
		for _, c := range sv.Cells {
			if c.State != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE || c.Reason == reasonImpliedFloorPending {
				continue
			}
			switch c.abstain.proto() {
			case aidlcv1.AbstainKind_ABSTAIN_KIND_GAP:
				var list aidlcv1.MaturityCoverageList
				var sentence string
				// The fixture's G4 cell closes on a declaration that is not honoured:
				// ours. Its G2 cell closes on an empty tracker window: theirs.
				if c.Gap == aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G4 {
					list, sentence, sawG4 = aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, oursG4NotHonouredSentence, true
				} else if c.Gap == aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G2 {
					list, sentence, sawG2 = aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_THEIRS, fmt.Sprintf(theirsG2NoIssuesSentenceFmt, rs.WindowDays), true
				} else if s, ok := oursGapSentence(c.Gap); ok {
					list, sentence, sawGapReal = aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, s, true
				} else {
					list, sentence, sawGapUnreadable = aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, oursUnreadableNumberSentence, true
				}
				if findItem(items, list, sentence, sv.Stage) == nil {
					t.Errorf("gap cell %s/%s (gap %v): no matching item for %q", stageKey(sv.Stage), levelKey(c.Level), c.Gap, sentence)
				}
				wantKeys[key(list, sentence)] = true
			case aidlcv1.AbstainKind_ABSTAIN_KIND_ROW_ABSENT:
				sawRowAbsent = true
				if findItem(items, aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, oursUnreadableNumberSentence, sv.Stage) == nil {
					t.Errorf("row_absent cell %s/%s: no matching item", stageKey(sv.Stage), levelKey(c.Level))
				}
				wantKeys[key(aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, oursUnreadableNumberSentence)] = true
			case aidlcv1.AbstainKind_ABSTAIN_KIND_INSTRUMENTED_ONLY:
				sawInstrumented = true
				if findItem(items, aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, oursInstrumentedOnlySentence, sv.Stage) == nil {
					t.Errorf("instrumented_only cell %s/%s: no matching item", stageKey(sv.Stage), levelKey(c.Level))
				}
				wantKeys[key(aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, oursInstrumentedOnlySentence)] = true
			case aidlcv1.AbstainKind_ABSTAIN_KIND_COVERAGE:
				sawCoverage = true
				sentence := oursCoverageSentence(sv.Stage)
				if findItem(items, aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, sentence, sv.Stage) == nil {
					t.Errorf("coverage cell %s/%s: no matching item for %q", stageKey(sv.Stage), levelKey(c.Level), sentence)
				}
				wantKeys[key(aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, sentence)] = true
			case aidlcv1.AbstainKind_ABSTAIN_KIND_NO_POPULATION:
				// The line is stage-wide: only a stage whose every measured level
				// abstained this way carries one (TestCoverageItems_NoPopulationIsStageWide).
				sawNoPop = true
				sentence := theirsNoPopulationSentence(sv.Stage, rs.WindowDays)
				item := findItem(items, aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_THEIRS, sentence, sv.Stage)
				switch {
				case stageSawNoWork(sv) && item == nil:
					t.Errorf("no_population cell %s/%s: no matching item for %q", stageKey(sv.Stage), levelKey(c.Level), sentence)
				case !stageSawNoWork(sv) && item != nil:
					t.Errorf("no_population cell %s/%s: a stage with other readings carries the stage-wide line %q", stageKey(sv.Stage), levelKey(c.Level), sentence)
				}
				if stageSawNoWork(sv) {
					wantKeys[key(aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_THEIRS, sentence)] = true
				}
			case aidlcv1.AbstainKind_ABSTAIN_KIND_POPULATION_FLOOR:
				sawPopulationFloor = true
			case aidlcv1.AbstainKind_ABSTAIN_KIND_JUDGMENT:
				sawJudgment = true
			}
		}
	}
	if !sawPopulationFloor || !sawJudgment || !sawGapReal || !sawGapUnreadable || !sawRowAbsent || !sawInstrumented || !sawCoverage || !sawNoPop || !sawG2 || !sawG4 {
		t.Fatalf("fixture must exercise every kind: population_floor=%v judgment=%v gap_real=%v gap_unreadable=%v row_absent=%v instrumented=%v coverage=%v no_population=%v g2=%v g4=%v",
			sawPopulationFloor, sawJudgment, sawGapReal, sawGapUnreadable, sawRowAbsent, sawInstrumented, sawCoverage, sawNoPop, sawG2, sawG4)
	}

	// Reverse: every served item traces to a kind this pass accounted for, or is
	// the one legitimate extra (the l5 disclosure, which is not cell-sourced).
	for _, it := range items {
		k := key(it.GetList(), it.GetSentence())
		if it.GetSentence() == l5DisclosureSentence {
			continue
		}
		if !wantKeys[k] {
			t.Errorf("item %v traces to no accounted-for abstain on the fixture", it)
		}
	}
}
