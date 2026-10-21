// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"fmt"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// This file is the "What we cannot see" block's own copy, composed fresh, never by
// reusing a cell's Reason (copy.go). A cell's reason answers "why did this cell
// abstain" and may point at the cell it describes; the block is read as a whole-map
// summary, so a line here must never name a level, a run, or a specific signal, and
// must never carry the "Ours:"/"Yours:" prefix a cell's reason uses (the block's own
// list already says whose it is). Wording rules mirror copy.go's: plain words, no em
// dash, no identifiers.

// oursUnreadableNumberSentence covers every cell whose abstain is "one of our own
// numbers could not be read" — a stale remainder, an absent row, an
// unresolvable control, an unknown people count. The reader does not need these
// told apart; they are one fact (something on our side could not be read) told once.
const oursUnreadableNumberSentence = "Some of the numbers behind parts of the map could not be read."

// oursInstrumentedOnlySentence and oursAutomatedOnlySentence are the block-level
// forms of the instrumented-only and automated-only abstains: a zero that cannot be
// trusted to mean
// absence, because the signal only ever sees one form of the practice.
const (
	oursInstrumentedOnlySentence = "Some measures can only see the tool-based form of a practice, so a quiet one there does not mean the work is not happening."
	oursAutomatedOnlySentence    = "Some measures can only see automated activity, so a quiet one there does not mean nobody chose to do this by hand."
)

// l5DisclosureSentence is the ours list's one-line disclosure, fired when no stage has
// a reading at the top level for this organisation. It scopes the claim to "this
// organisation" rather than every organisation: an L5 rule can score for another
// organisation, so a general claim from one organisation's abstains would be false.
const l5DisclosureSentence = "Whether agents coordinate this organisation's work under its own policies, the highest level on the map, is not read."

// oursGapSentence is the block-level line for a real instrumentation gap — no
// identifier, no level, no "until we do" roadmap clause. G2 and G4 are excluded: each
// one's line depends on what closed the cell (see coverageItems).
func oursGapSentence(g aidlcv1.InstrumentationGap) (string, bool) {
	switch g {
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G1:
		return "Whether a check or build passed is not read.", true
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G3:
		return "Commits are not joined to the pull requests they belong to.", true
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G5:
		return "Runs that span more than one repository are not linked together.", true
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G6:
		return "Design documents are not compared with the code they describe.", true
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G7:
		return "Deploy and rollback events are not read.", true
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G8:
		return "Incident and alert events are not read.", true
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G9:
		return "Some pull requests' file lists are only partly captured.", true
	default:
		return "", false
	}
}

// The G2 block lines, one per g2Blocker: a rule that does not measure planning is
// the measurement's to close (ours); an empty tracker window is the organisation's
// (theirs),
// routed to the Plan row.
const (
	oursG2NotMeasuredSentence   = "Planning work is not read from tracker issues and planning sessions."
	theirsG2NoIssuesSentenceFmt = "No issues were created in your tracker in the last %d days."
)

// The G4 block lines, one per g4Blocker. The undeclared and unenforced lines are the
// organisation's own action (theirs); not-honoured and uninstrumented are the
// measurement's to close (ours). Only the declaration has a place the block routes to
// (MATURITY_COVERAGE_ROUTE_ORG_SETTINGS); the enforcement line carries no route.
const (
	oursG4NotHonouredSentence     = "Approvals are not checked against your review policy."
	oursG4UninstrumentedSentence  = "The practices your organisation enforces and the review policy it follows are not read."
	theirsG4UndeclaredSentence    = "Declare your review policy so approvals against it can be checked."
	theirsG4UnenforcedSentenceFmt = "Your organisation enforces no practice in its agent setup for %s yet; once one is enforced, we read how widely it is followed."
)

// theirsG4UnenforcedSentence names every stage with nothing enforced in one line,
// so the block carries the enforcement fact once however many stages it touches.
func theirsG4UnenforcedSentence(stages []aidlcv1.AidlcStage) string {
	list := joinWithAnd(stageTitlesFor(stages))
	return fmt.Sprintf(theirsG4UnenforcedSentenceFmt, list)
}

// stageWorkNoun names a stage's work the way a reader talks about it, for the two
// per-stage coverage sentences below. Distinct from copy.go's stageTitles (the grid's
// row label, capitalised, used in headings and the summary): this is the lowercase
// noun phrase that reads naturally inside "the ... work" and "No ... work was seen".
var stageWorkNoun = map[aidlcv1.AidlcStage]string{
	aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC: "product-spec",
	aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC:    "tech-spec",
	aidlcv1.AidlcStage_AIDLC_STAGE_PLAN:         "planning",
	aidlcv1.AidlcStage_AIDLC_STAGE_CODE:         "coding",
	aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW:       "review",
	aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA:      "test and QA",
	aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD:        "CI/CD",
	aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY:       "deploy",
	aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE:      "operate",
}

// oursCoverageSentence is COVERAGE's block line: which reading a stage's work could
// not be seen enough of, named by stage rather than by the generic straddle prose a
// cell's own reason carries (which never says what was being read).
func oursCoverageSentence(stage aidlcv1.AidlcStage) string {
	return fmt.Sprintf("We could not see enough of the %s work to read its share.", stageWorkNoun[stage])
}

// theirsNoPopulationSentence is a whole stage's NO_POPULATION theirs line: no work of
// the stage was seen in the window, and the reader's action is to connect what
// carries it. It is a claim about the stage, so it is emitted only when every level
// of the stage abstained that way (stageSawNoWork); a stage with any other reading
// leaves the reason to its cells' own reasons.
func theirsNoPopulationSentence(stage aidlcv1.AidlcStage, windowDays int) string {
	return fmt.Sprintf("No %s work was seen in the last %d days. Supply the records that carry it so this row can be read.", stageWorkNoun[stage], windowDays)
}

// stageSawNoWork reports whether every level of sv that has a measure of its own
// abstained NO_POPULATION — the only case in which "no work of this stage was seen"
// is true of the stage. An implied-floor cell still pending has no measure of its
// own, so it neither qualifies nor disqualifies the stage.
func stageSawNoWork(sv StageVerdict) bool {
	measured := 0
	for _, c := range sv.Cells {
		if c.Reason == reasonImpliedFloorPending {
			continue
		}
		if c.State != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE || c.abstain.proto() != aidlcv1.AbstainKind_ABSTAIN_KIND_NO_POPULATION {
			return false
		}
		measured++
	}
	return measured > 0
}

// coverageItems composes the "What we cannot see" block
// from the grid's abstains, using the block's OWN copy above rather than a cell's
// reason (see the file doc comment). The kind-to-list mapping, with the G2 and G4
// reroutes the organisation's own action answers and one exclusion:
//
//	An unresolved implied-floor cell (RuleKindImpliedFloor, no met rung above it yet)
//	    carries NO item: its only fact is "counts once a higher rung is met", which is
//	    exactly the cell reference and unlock clause the block must never state, and
//	    the underlying gap is already covered by the instrumented cell(s) it depends
//	    on (or, once one is met, the floor cell is no longer an abstain at all).
//	GAP naming G1/G3/G5-G9, ROW_ABSENT, a GAP with no gap id (the "unreadable number"
//	    case), INSTRUMENTED_ONLY, AUTOMATED_ONLY -> ours, deduplicated by sentence.
//	GAP naming G2 -> by the blocker the scorer recorded (Cell.g2Blocker): not
//	    measured -> ours; no tracker issues in the window -> theirs, one item with
//	    the stages it touches unioned, routed to the stage row.
//	GAP naming G4 -> one line per blocker the scorer recorded on the cell
//	    (Cell.g4Blockers), never re-derived here: not honoured -> ours;
//	    undeclared -> theirs, routed to org settings; unenforced -> theirs, one item
//	    naming every such stage, no route; uninstrumented -> ours.
//	COVERAGE -> ours, one line per stage.
//	NO_POPULATION -> theirs, one line per stage, only when every level of the
//	    stage abstained NO_POPULATION (stageSawNoWork); otherwise no item.
//	POPULATION_FLOOR, JUDGMENT, DISPUTED -> neither list; the cell's own reason is
//	    the only home for those.
//
// Order: ours items in first-seen order (the caller's stage/level traversal is
// already deterministic), the L5 disclosure last among ours, then theirs items —
// gap-remedy items in first-seen order, then no-population items in stage order.
func coverageItems(rs *rules.RuleSet, stages []StageVerdict) []*aidlcv1.MaturityCoverageItem {
	type oursEntry struct {
		item   *aidlcv1.MaturityCoverageItem
		stages map[aidlcv1.AidlcStage]bool
	}
	oursBySentence := map[string]*oursEntry{}
	var oursOrder []string
	addOurs := func(sentence string, stage aidlcv1.AidlcStage) {
		e, ok := oursBySentence[sentence]
		if !ok {
			e = &oursEntry{
				item:   &aidlcv1.MaturityCoverageItem{List: aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS, Sentence: sentence},
				stages: map[aidlcv1.AidlcStage]bool{},
			}
			oursBySentence[sentence] = e
			oursOrder = append(oursOrder, sentence)
		}
		if !e.stages[stage] {
			e.stages[stage] = true
			e.item.Stages = append(e.item.Stages, stage)
		}
	}

	// Theirs gap items are keyed by the action they name, not the sentence: the
	// unenforced line's sentence is composed from its stages once the walk is done.
	type theirsKey int
	const (
		theirsKeyG2NoIssues theirsKey = iota
		theirsKeyG4Undeclared
		theirsKeyG4Unenforced
	)
	theirsGapByGap := map[theirsKey]*oursEntry{}
	var theirsGapOrder []theirsKey
	addTheirsGap := func(g theirsKey, sentence string, route aidlcv1.MaturityCoverageRoute, stage aidlcv1.AidlcStage) {
		e, ok := theirsGapByGap[g]
		if !ok {
			e = &oursEntry{
				item: &aidlcv1.MaturityCoverageItem{
					List: aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_THEIRS, Sentence: sentence, Route: route,
				},
				stages: map[aidlcv1.AidlcStage]bool{},
			}
			theirsGapByGap[g] = e
			theirsGapOrder = append(theirsGapOrder, g)
		}
		if !e.stages[stage] {
			e.stages[stage] = true
			e.item.Stages = append(e.item.Stages, stage)
		}
	}

	theirsPopByStage := map[aidlcv1.AidlcStage]*aidlcv1.MaturityCoverageItem{}
	var theirsPopOrder []aidlcv1.AidlcStage

	coverageByStage := map[aidlcv1.AidlcStage]*aidlcv1.MaturityCoverageItem{}
	var coverageOrder []aidlcv1.AidlcStage

	l5CellsSeen, l5AnyDecided := 0, false

	for _, sv := range stages {
		sawNoWork := stageSawNoWork(sv)
		for _, c := range sv.Cells {
			if c.Level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L5 {
				l5CellsSeen++
				if c.State != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE {
					l5AnyDecided = true
				}
			}
			if c.State != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE {
				continue
			}
			// An implied-floor cell not yet awarded carries no item — see the func doc.
			if c.Reason == reasonImpliedFloorPending {
				continue
			}
			switch c.abstain.proto() {
			case aidlcv1.AbstainKind_ABSTAIN_KIND_GAP:
				switch c.Gap {
				case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G2:
					if c.g2Blocker == g2BlockerNoIssues {
						addTheirsGap(theirsKeyG2NoIssues, fmt.Sprintf(theirsG2NoIssuesSentenceFmt, rs.WindowDays),
							aidlcv1.MaturityCoverageRoute_MATURITY_COVERAGE_ROUTE_STAGE_ROW, sv.Stage)
					} else {
						addOurs(oursG2NotMeasuredSentence, sv.Stage)
					}
					continue
				case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G4:
					blockers := c.g4Blockers
					if len(blockers) == 0 {
						blockers = []g4Blocker{g4BlockerUninstrumented}
					}
					for _, b := range blockers {
						switch b {
						case g4BlockerUndeclared:
							addTheirsGap(theirsKeyG4Undeclared, theirsG4UndeclaredSentence, aidlcv1.MaturityCoverageRoute_MATURITY_COVERAGE_ROUTE_ORG_SETTINGS, sv.Stage)
						case g4BlockerUnenforced:
							addTheirsGap(theirsKeyG4Unenforced, "", aidlcv1.MaturityCoverageRoute_MATURITY_COVERAGE_ROUTE_UNSPECIFIED, sv.Stage)
						case g4BlockerNotHonoured:
							addOurs(oursG4NotHonouredSentence, sv.Stage)
						default:
							addOurs(oursG4UninstrumentedSentence, sv.Stage)
						}
					}
					continue
				default:
				}
				if sentence, ok := oursGapSentence(c.Gap); ok {
					addOurs(sentence, sv.Stage)
					continue
				}
				addOurs(oursUnreadableNumberSentence, sv.Stage) // no_resolver: no gap id at all
			case aidlcv1.AbstainKind_ABSTAIN_KIND_ROW_ABSENT:
				addOurs(oursUnreadableNumberSentence, sv.Stage)
			case aidlcv1.AbstainKind_ABSTAIN_KIND_INSTRUMENTED_ONLY:
				addOurs(oursInstrumentedOnlySentence, sv.Stage)
			case aidlcv1.AbstainKind_ABSTAIN_KIND_AUTOMATED_ONLY:
				addOurs(oursAutomatedOnlySentence, sv.Stage)
			case aidlcv1.AbstainKind_ABSTAIN_KIND_COVERAGE:
				if _, ok := coverageByStage[sv.Stage]; !ok {
					coverageByStage[sv.Stage] = &aidlcv1.MaturityCoverageItem{
						List:     aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS,
						Sentence: oursCoverageSentence(sv.Stage), Stages: []aidlcv1.AidlcStage{sv.Stage},
					}
					coverageOrder = append(coverageOrder, sv.Stage)
				}
			case aidlcv1.AbstainKind_ABSTAIN_KIND_NO_POPULATION:
				if !sawNoWork {
					continue
				}
				if _, ok := theirsPopByStage[sv.Stage]; !ok {
					theirsPopByStage[sv.Stage] = &aidlcv1.MaturityCoverageItem{
						List:     aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_THEIRS,
						Sentence: theirsNoPopulationSentence(sv.Stage, rs.WindowDays),
						Stages:   []aidlcv1.AidlcStage{sv.Stage},
						Route:    aidlcv1.MaturityCoverageRoute_MATURITY_COVERAGE_ROUTE_STAGE_ROW,
					}
					theirsPopOrder = append(theirsPopOrder, sv.Stage)
				}
			default:
				// POPULATION_FLOOR, JUDGMENT, DISPUTED and UNSPECIFIED carry no item —
				// the cell's own reason is their only home.
			}
		}
	}

	var items []*aidlcv1.MaturityCoverageItem
	for _, sentence := range oursOrder {
		items = append(items, oursBySentence[sentence].item)
	}
	for _, stage := range coverageOrder {
		items = append(items, coverageByStage[stage])
	}
	if l5CellsSeen > 0 && !l5AnyDecided {
		items = append(items, &aidlcv1.MaturityCoverageItem{
			List:     aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_OURS,
			Sentence: l5DisclosureSentence,
		})
	}
	for _, g := range theirsGapOrder {
		it := theirsGapByGap[g].item
		if g == theirsKeyG4Unenforced {
			it.Sentence = theirsG4UnenforcedSentence(it.GetStages())
		}
		items = append(items, it)
	}
	for _, stage := range theirsPopOrder {
		items = append(items, theirsPopByStage[stage])
	}
	return items
}
