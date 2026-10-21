// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// This file is the reader-facing copy the scorer attaches to a cell it cannot decide
// and to the moves it recommends. The reader is an administrator outside engineering,
// so every sentence says three things where it can: what could not be read, whose it
// is to change (ours, meaning whoever runs the measurement, or yours, meaning the
// organisation), and what would change it. None of it is hashed into the ruleset, so
// wording is free to move. A number a sentence names (a floor, the window) is composed
// from the ruleset the cell was scored under, never written as a literal, so a later
// floor cannot leave a stale sentence behind a green test. Copy rules: plain words, digits for counts, no
// comparison operators, no em dash, no identifiers, nothing that expires at a version
// flip.

// reasonTermUnreadable is shared by the three sites where one of the share's own
// numbers could not be read (a missing reading, a declared remainder with no value,
// an unreadable count). They are one situation to the reader.
const reasonTermUnreadable = "Ours: one of the numbers this level needs was not supplied."

// reasonMalformed is a number that was supplied but cannot be a valid count: NaN,
// infinite, negative, fractional or beyond 2^53.
const reasonMalformed = "Ours: one of the numbers this level needs was supplied but is not a valid count."

// reasonJudgment is the abstain of a cell decided by a considered judgment nobody has
// made from telemetry.
const reasonJudgment = "Ours: your tools cannot show this level, so it stays open."

// reasonImpliedFloorPending is an implied-floor cell that no higher met rung has yet
// counted; once one is, closure awards it and the cell carries no reason.
const reasonImpliedFloorPending = "This level has no measure of its own; it is met once a higher level of this stage is met."

// reasonInstrumentedOnly is a zero on a signal that sees only the instrumented form.
const reasonInstrumentedOnly = "The tooling is present, but this signal alone cannot decide the level; work done in chat leaves no trace here."

// reasonReadingsDisagree is a count larger than the total it was read over.
const reasonReadingsDisagree = "Two of our readings disagree for this level; we do not award on a disagreement. Ours."

// reasonControlFailed is a review control that read zero on the stage's non-AI work.
const reasonControlFailed = "Review is rare on this stage's non-AI work too, so its absence on AI work says nothing on its own."

// reasonControlUnreadable is a review control that could not be read.
const reasonControlUnreadable = "Ours: we could not read how the same work looks without AI, which this level compares against."

// reasonAutomatedOnly is a zero on a signal that sees only the automated form.
const reasonAutomatedOnly = "Only automated tooling touched this stage; nobody chose a tool for it by hand."

// reasonAllUnobserved is a straddle in which nothing was observed: every item the
// level counts is unread mass, because the evidence it needs has no writer yet.
const reasonAllUnobserved = "Nothing this level counts has been observed yet: the evidence it needs is not recorded. Ours."

// reasonStraddle is a share whose readable range crosses the cut.
const reasonStraddle = "The share we can read sits on the line between two verdicts, so we do not call it either way yet."

// reasonPeopleUnknown is a share with no count of distinct people behind it.
const reasonPeopleUnknown = "We cannot tell how many different people did this, and one person's automation is not a team practice. Ours."

// reasonRowLevel is the stage's reason when every rung on it abstained.
const reasonRowLevel = "Nothing in this stage could be measured yet."

// reasonPopulationFloor is too few items to read a share over, with the floor and the
// window taken from the ruleset.
func reasonPopulationFloor(rs *rules.RuleSet, floor int, grain aidlcv1.ShareGrain) string {
	if grain == aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE {
		return fmt.Sprintf("Fewer than %d people to read this over, too few to call it a practice either way. This fills in as more people take part.", floor)
	}
	return fmt.Sprintf("Fewer than %d pieces of this work in the last %d days, too few to call it a practice either way. This fills in as more of this work happens.", floor, rs.WindowDays)
}

// reasonPeopleFloor is enough work by too few people.
func reasonPeopleFloor(peopleMin int) string {
	return fmt.Sprintf("Enough work, but fewer than %d people did it; a practice needs more than one contributor.", peopleMin)
}

// reasonNoPopulation is an empty, fully observed total.
func reasonNoPopulation(rs *rules.RuleSet, grain aidlcv1.ShareGrain) string {
	if grain == aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE {
		return "There is no one to read this level over, so there is nothing to measure yet. Yours: this fills in once people hold seats in the AI tool."
	}
	return fmt.Sprintf("No AI-assisted work of this kind was merged in the last %d days, so there is nothing to measure yet. Yours: this fills in once your team ships this kind of work with AI.", rs.WindowDays)
}

// reasonUnmeasurablePopulation is a total that could not be read rather than one
// that is empty; source is the reader-facing label of what was only partly captured.
func reasonUnmeasurablePopulation(source string) string {
	return fmt.Sprintf("We can see this work happened but cannot read enough of it to decide. Ours: %s are only partly captured.", source)
}

// reasonForGap is the per-gap sentence for a cell blocked on an instrumentation gap.
// G4 and G2 read their blocker (what holds the cell closed — see g4Blocker and
// g2Blocker); every other gap ignores both. No form names a screen or a setting: a
// reason must never send a reader somewhere the application showing it may not have.
func reasonForGap(g aidlcv1.InstrumentationGap, blockers []g4Blocker, g2 g2Blocker) string {
	switch g {
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G1:
		return "Ours: whether checks and builds passed is not read, so this level cannot be read."
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G2:
		if g2 == g2BlockerNoIssues {
			return "Yours: no issues were created in your issue tracker in this window; once planned work is tracked there, this level can be read."
		}
		return "Ours: planning work is not read from tracker issues and planning sessions, so this level cannot be read."
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G3:
		return "Ours: commits are not joined to their pull requests."
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G4:
		return reasonForG4(blockers)
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G5:
		return "Ours: runs that span repositories are not linked."
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G6:
		return "Ours: design documents are not compared with the code they describe."
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G7:
		return "Ours: deploy and rollback events are not read."
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G8:
		return "Ours: incident and alert events are not read."
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G9:
		return "Ours: the pull requests' file lists are only partly captured."
	default:
		return "Ours: this level is not measured."
	}
}

// reasonForG4 names the check that closed a G4 cell, and when a second check is
// also missing, says so in the same sentence: a cell missing both its enforcement
// and its declaration must never read as if declaring alone would open it.
func reasonForG4(blockers []g4Blocker) string {
	first := g4BlockerUninstrumented
	if len(blockers) > 0 {
		first = blockers[0]
	}
	also := func(b g4Blocker) bool {
		for _, have := range blockers[min(1, len(blockers)):] {
			if have == b {
				return true
			}
		}
		return false
	}
	switch first {
	case g4BlockerUnenforced:
		switch {
		case also(g4BlockerUndeclared):
			return "Yours: this level needs a practice your organisation enforces in its agent setup for this stage and a declared review policy; neither is in place yet."
		case also(g4BlockerNotHonoured):
			return "Yours and ours: there is no practice your organisation enforces in its agent setup for this stage yet, and approvals are not checked against your review policy."
		default:
			return "Yours: there is no practice your organisation enforces in its agent setup for this stage yet; once there is, we read how widely it is followed."
		}
	case g4BlockerUndeclared:
		return "Yours: your review policy is not declared yet; once it is, we check that approvals follow it."
	case g4BlockerNotHonoured:
		return "Ours: approvals are not checked against your review policy."
	default:
		return "Ours: the practices your organisation enforces and its review policy are not read, so this level cannot be read."
	}
}

// stageTitles are the stage names as a reader sees them, for prose that names a
// stage (a bottleneck title, a move).
var stageTitles = map[aidlcv1.AidlcStage]string{
	aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC: "Product spec",
	aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC:    "Tech spec",
	aidlcv1.AidlcStage_AIDLC_STAGE_PLAN:         "Plan",
	aidlcv1.AidlcStage_AIDLC_STAGE_CODE:         "Code",
	aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW:       "Review",
	aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA:      "Test & QA",
	aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD:        "CI/CD",
	aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY:       "Deploy",
	aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE:      "Operate",
}

func stageTitle(s aidlcv1.AidlcStage) string {
	if t, ok := stageTitles[s]; ok {
		return t
	}
	return stageKey(s)
}

// levelTitles are the five autonomy-level labels, lower-cased for prose.
// practiceText's fallback is the one prose site that names a level by it.
var levelTitles = map[aidlcv1.AidlcLevel]string{
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "assisted",
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "conversational",
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "supervised agentic",
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "autonomous",
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "orchestrated",
}

func levelTitle(l aidlcv1.AidlcLevel) string {
	return levelTitles[l]
}

// MoveTerms are the numbers a bottleneck's move is composed from: the count that did
// the thing, the total it was read over, the part that could not be read, and the
// grain those are counted in. They ride on the Bottleneck from the detector to the
// composer, which never sees the grid. Store them with the bottleneck so its move
// reads the same on every day it is served.
type MoveTerms struct {
	Hits       int64
	Population int64
	Unobserved int64
	Grain      aidlcv1.ShareGrain
}

// itemsToClear is how many more items would put the share strictly above cut. The
// answer depends on the grain. For merged changes every new item that does the thing
// also joins the total, so it is the smallest x with (hits + x) / (population + x)
// above the cut; a fixed-total answer would tell the reader to do something that does
// not clear the cut. For people the total is the roster and does not grow when one
// more person adopts, so it is the smallest count above the cut over today's roster.
// The comparison is the scorer's own (a float division, strictly above), so an exact
// multiple of the cut is not "cleared", as the band test says. Zero when the share is
// already above the cut, and zero for merged changes when the cut is one or more: no
// number of additional items lifts (hits + x) / (population + x) strictly above it, and
// the copy leaves the distance out rather than name one.
//
// It is computed in closed form and then settled against the scorer's own float
// comparison, so its cost does not grow with the population.
func itemsToClear(t MoveTerms, cut float64) int64 {
	if t.Population <= 0 || math.IsNaN(cut) {
		return 0
	}
	people := t.Grain == aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE
	if !people && cut >= 1 {
		return 0
	}
	above := func(h, p int64) bool { return float64(h)/float64(p) > cut }
	if above(t.Hits, t.Population) {
		return 0
	}
	if people {
		// The total is the roster; past the whole roster no count is above the cut, so
		// the answer stops at the people not yet counted (at least one).
		limit := max(1, t.Population-t.Hits)
		ok := func(x int64) bool { return above(t.Hits+x, t.Population) }
		x := settle(int64(math.Floor(cut*float64(t.Population)))-t.Hits+1, limit, ok)
		return min(x, limit)
	}
	// The smallest x with (hits + x) / (population + x) above the cut, cut below one.
	ok := func(x int64) bool { return above(t.Hits+x, t.Population+x) }
	guess := math.Floor((cut*float64(t.Population)-float64(t.Hits))/(1-cut)) + 1
	return settle(clampInt64(guess), max(1, math.MaxInt64/2-t.Population), ok)
}

// settle moves a closed-form guess to the smallest x in [1, limit] for which ok holds,
// ok being monotone; limit when none does. The guess is within a few steps of the
// answer, so the walks are short.
func settle(guess, limit int64, ok func(int64) bool) int64 {
	x := min(max(guess, 1), limit)
	for x > 1 && ok(x-1) {
		x--
	}
	for x < limit && !ok(x) {
		x++
	}
	return x
}

// clampInt64 converts a non-negative float guess to int64 without overflow.
func clampInt64(v float64) int64 {
	switch {
	case !(v > 1):
		return 1
	case v >= math.MaxInt64/2:
		return math.MaxInt64 / 2
	default:
		return int64(v)
	}
}

// itemWord names the grain's unit in a sentence about n of them.
func itemWord(g aidlcv1.ShareGrain, n int64) string {
	switch {
	case g == aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE && n == 1:
		return "person"
	case g == aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE:
		return "people"
	case n == 1:
		return "pull request"
	default:
		return "pull requests"
	}
}

// oneInWords names a band cut as an everyday ratio for constraint and move prose ("one
// in five", "half"), distinct from fractionWord's cell-term phrasing ("a fifth") so
// the two copy sites can change independently. Any other cut falls back to a
// percentage.
func oneInWords(rs *rules.RuleSet, cut float64) string {
	switch cut {
	case 0.5:
		return "half"
	case 0.2:
		return "one in five"
	case 0.25:
		return "one in four"
	default:
		return pctText(rs, cut)
	}
}

// practicePhrase is the explicit, hand-authored practice for every one of the 45
// (stage, level) cells. A phrase is authored, never derived by splitting a cell
// definition's prose: a clause cut from a shared sentence dangles a pronoun or loses its
// verb. Every entry is its own verb-led, self-contained clause — lower-case, no trailing
// punctuation (callers capitalise and punctuate) — so a title, body or move can quote it
// directly with no clause-selection logic and no risk of carrying over words this
// package bans (`read`, `missed`, `cut`, `level`, `rung`) or a pronoun with no
// antecedent in the clause itself. A constraint's own title and body name a cell through
// cellCopies instead, so this table grounds only the prose that quotes a whole practice
// (a constraint body with no bespoke measurement, a stalled stage's first step, the
// generic fallback action).
var practicePhrase = map[aidlcv1.AidlcStage]map[aidlcv1.AidlcLevel]string{
	aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "a person writes the requirement and an assistant helps phrase, tighten or critique the wording",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "a person and an assistant work out the requirement together in conversation, drafting sections and asking questions",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "an agent produces a complete spec from a brief and a person approves it before it becomes the record",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "an agent turns raw input into an accepted spec without supervision and a person checks the result afterward",
		// L5 is a two-share cell; see twoShareClauses.
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "a person writes the design and an assistant answers design questions on request",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "a person and an assistant draft the design document together, section by section",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "an agent drafts a full design document and a person reviews it and decides the open questions",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "an agent produces a design document that lands as the record with only a final review",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "coordinated agents keep design records in sync with the code under the organisation's policy",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_PLAN: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "a person keeps the plan and an assistant suggests next steps when asked",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "a person builds the plan in conversation with an assistant, using plan mode and a todo list",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "an agent produces a task breakdown and a person approves it before any of it runs",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "an agent breaks the work down and runs it without a per step gate while a person checks the results",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "people plan under a practice the organisation enforces",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_CODE: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "an assistant fills in completions and snippets while a person writes the change",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "a person applies changes suggested in chat, one edit at a time",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "an agent makes a complete multi file change and a person reviews the diff before it merges",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "an agent completes a piece of work into a merged change and a person reviews the result afterward",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "people write code under a practice the organisation enforces",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "a reviewer asks an assistant to explain a change while the review stays entirely human",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "a reviewer works through a change in conversation with an assistant while still writing the review",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "an automated reviewer posts findings and a person decides",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "an automated reviewer clears changes without a person re-reading them",
		// L5 is a two-share cell; see twoShareClauses.
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "a person writes tests and an assistant helps with a case or an assertion on request",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "an assistant writes tests alongside the change in conversation with a person",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "an agent produces tests as part of making the change and a person reviews them with the diff",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "an agent runs the test suite, diagnoses failures and fixes them without supervision while a person checks the result",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "people test under a practice the organisation enforces",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "a person maintains the pipeline and an assistant helps look into a build error",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "a person edits the pipeline configuration in conversation with an assistant",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "an agent changes the pipeline configuration or fixes a broken build and a person approves it",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "an agent responds to a failure and lands the fix without supervision while a person checks the result",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "the pipeline adapts itself under the organisation's policy while agents coordinate the work",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "a person runs the release and asks the assistant about it",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "a person walks through a release in conversation with an assistant, step by step",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "an agent runs a written release procedure and a person approves it at the gate",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "an agent performs the release without supervision and a person checks the outcome afterward",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "agents decide the release, verification and rollback under the organisation's policy",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "a person operates the system and an assistant helps look into a log or a stack trace",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "a person debugs and checks system health in conversation with an assistant",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "an agent investigates a problem and proposes a fix while a person decides and applies it",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "an agent detects, diagnoses and fixes a problem without supervision while a person reviews it afterward",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "detection, remediation and verification form a closed loop under the organisation's policy",
	},
}

// standInPhrases overrides practicePhrase for a cell whose rule measures a narrower
// stand-in in place of the level's definition (definitions.go's standIns): without
// it, Plan L3 would quote "a task breakdown approved before any of it runs" while the
// rule measures "planned in an AI session, approved on merge". A cell whose base
// phrase already matches its stand-in's meaning (Plan L5, Code L5, Test & QA L5 all
// describe an enforced practice) carries no line here;
// TestPracticePhrase_MatchesStandIn checks both kinds against the stand-in text.
var standInPhrases = map[aidlcv1.AidlcStage]map[aidlcv1.AidlcLevel]string{
	aidlcv1.AidlcStage_AIDLC_STAGE_PLAN: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "the work is planned in a session with an assistant and a person approves the result when it merges",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "the work is planned in a session with an assistant and runs without a person approving each step",
	},
}

// twoShareClause is the pair of practices a two-share L5 cell (review, product-spec)
// needs: primary describes the enforcement-usage share, secondary the
// declared-policy-approval share — two independently measured things, so one phrase
// cannot cover both. Keyed on stage, and consulted only when level is L5
// (practiceText checks the level before this map): a lookup on stage alone would win
// at every level and ground an L1 finding in L5's enforcement language.
type twoShareClause struct{ primary, secondary string }

var twoShareClauses = map[aidlcv1.AidlcStage]twoShareClause{
	aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC: {
		primary:   "people use a spec drafting practice the organisation enforces",
		secondary: "spec approvals follow the organisation's declared review policy",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW: {
		primary:   "a review practice the organisation enforces is the one people follow",
		secondary: "review approvals follow the organisation's declared policy",
	},
}

// practiceText is the practice a constraint, body or move grounds its copy in for a
// given (stage, level) finding, in mid-sentence case (so it can follow a colon):
// level L5 on the two stages that carry a secondary share takes the primary or
// secondary phrasing named by secondaryMiss; otherwise standInPhrases' entry if the
// cell has one, else practicePhrase's. Falls back to a stage and level phrase only for
// a (stage, level) pair no table carries, which opens with the stage's proper name,
// never a bare level key. No trailing punctuation; callers add their own.
func practiceText(stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel, secondaryMiss bool) string {
	if level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L5 {
		if ts, ok := twoShareClauses[stage]; ok {
			if secondaryMiss {
				return ts.secondary
			}
			return ts.primary
		}
	}
	if p, ok := standInPhrases[stage][level]; ok {
		return p
	}
	if p, ok := practicePhrase[stage][level]; ok {
		return p
	}
	return fmt.Sprintf("%s's %s practice", stageTitle(stage), levelTitle(level))
}

// stageNouns name a stage's work in running prose ("your testing work", "your coding
// work"), where stageTitle's proper-noun form
// ("Test & QA") reads as a label rather than a sentence.
var stageNouns = map[aidlcv1.AidlcStage]string{
	aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC: "product spec",
	aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC:    "tech spec",
	aidlcv1.AidlcStage_AIDLC_STAGE_PLAN:         "planning",
	aidlcv1.AidlcStage_AIDLC_STAGE_CODE:         "coding",
	aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW:       "review",
	aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA:      "testing",
	aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD:        "CI/CD",
	aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY:       "deployment",
	aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE:      "operations",
}

func stageNoun(s aidlcv1.AidlcStage) string {
	if n, ok := stageNouns[s]; ok {
		return n
	}
	return stageKey(s)
}

// cellCopy is the reader-facing text one (stage, level) cell contributes when it is
// the practice a level-fell-short constraint is about. action is the move's title: an
// imperative, at most 12 words, naming what to do and never the rubric's vocabulary.
// finding is the constraint's title: the same practice stated as what is scarce today.
// Neither carries a trailing full stop (the composers add it). measure and unit are
// optional overrides for a cell whose counted signal has its own words: measure is the
// constraint body over the counts, unit the noun that the move body's distance counts
// in (singular, plural).
//
// A phrase is hand-authored, never derived from practicePhrase or a cell definition:
// those are whole-practice sentences and too long to serve as a title. Plan L3 and L4
// are worded for the session-based stand-ins the rules measure
// (TestCellCopies_AgreeWithEveryStandIn checks them against definitions.go's
// standIns).
type cellCopy struct {
	action  string
	finding string
	measure func(t MoveTerms, windowDays int) string
	unit    [2]string
}

// cellCopies covers levels 1 to 5 of every stage. The two stages whose L5 cell reads two
// independent shares carry their pair in twoShareCellCopies instead.
var cellCopies = map[aidlcv1.AidlcStage]map[aidlcv1.AidlcLevel]cellCopy{
	aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: {action: "Have an assistant tighten and critique the requirements your people write", finding: "Few requirements get an assistant's help with wording or critique"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: {action: "Work out requirements in conversation with an assistant, drafting sections together", finding: "Few requirements are worked out in conversation with an assistant"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: {action: "Let agents draft full specs from a brief for approval", finding: "Agents draft few specs from a brief for a person to approve"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: {action: "Let agents turn raw tickets and threads into accepted specs", finding: "Agents turn few raw requests into accepted specs without supervision"},
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: {action: "Have an assistant answer design questions as your people write designs", finding: "Few designs are written with an assistant answering questions"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: {action: "Draft design documents with an assistant, section by section", finding: "Few design documents are drafted with an assistant, section by section"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: {action: "Let agents draft full design documents for a person to review", finding: "Agents draft few full design documents for a person to review"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: {action: "Let agents produce design documents that land after a final review", finding: "Few agent-produced design documents land with only a final review"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: {action: "Have coordinated agents keep design records in sync with the code", finding: "Few design records are kept in sync with the code by agents"},
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_PLAN: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: {action: "Ask an assistant for next steps when your people plan", finding: "Few plans get next-step suggestions from an assistant"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: {action: "Plan with an assistant in plan mode, using a todo list", finding: "Few tasks are planned with an assistant in plan mode"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: {action: "Plan work in an assistant session and approve it at merge", finding: "Few work items are planned with an assistant and approved at merge"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: {action: "Let agents run a plan without a person approving each step", finding: "Few plans run without a person approving each step"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: {action: "Get your people planning under a practice your admins enforce", finding: "Few people plan under a practice you enforce"},
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_CODE: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: {action: "Let an assistant fill in completions while your people write changes", finding: "Few changes are written with assistant completions and snippets"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: {action: "Have your people apply changes suggested in chat", finding: "Few changes come from suggestions applied in chat"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: {action: "Let agents make complete multi-file changes for a person to review", finding: "Agents make few complete changes for a person to review before merge"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: {
			action:  "Let agents take more pull requests to merge with little back-and-forth",
			finding: "Few AI-written pull requests merge with little back-and-forth",
			measure: func(t MoveTerms, windowDays int) string {
				return fmt.Sprintf("Of the %s AI-written pull requests merged in the last %d days, %s needed little back-and-forth before merging.",
					commaText(t.Population), windowDays, commaText(t.Hits))
			},
		},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: {action: "Get your people writing code under a practice your admins enforce", finding: "Few people write code under a practice you enforce"},
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: {action: "Have reviewers ask an assistant to explain the changes they review", finding: "Few reviewers ask an assistant to explain a change"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: {action: "Have reviewers work through changes in conversation with an assistant", finding: "Few reviews are worked through in conversation with an assistant"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: {action: "Run an automated reviewer on every change and let people decide", finding: "An automated reviewer posts findings on few of your changes"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: {
			action:  "Let your review bot approve low-risk pull requests on its own",
			finding: "A review bot clears few changes without a person re-reading them",
			measure: func(t MoveTerms, _ int) string {
				return fmt.Sprintf("Your review bot looked at %s merged pull requests. %s of them merged on the bot's review alone.",
					commaText(t.Population), commaText(t.Hits))
			},
			unit: [2]string{"bot-cleared pull request", "bot-cleared pull requests"},
		},
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: {action: "Have an assistant help with test cases and assertions on request", finding: "Few tests get an assistant's help with a case or assertion"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: {action: "Write tests alongside each change in conversation with an assistant", finding: "Few changes get tests written alongside them with an assistant"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: {action: "Let agents write tests as part of making each change", finding: "Agents write tests with few of the changes they make"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: {action: "Let agents run the test suite and fix failures on their own", finding: "Agents fix few failing test runs without supervision"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: {action: "Get your people testing under a practice your admins enforce", finding: "Few people test under a practice you enforce"},
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: {action: "Have an assistant help your people look into build errors", finding: "Few build errors are investigated with an assistant's help"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: {action: "Edit pipeline configuration in conversation with an assistant", finding: "Few pipeline changes are made in conversation with an assistant"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: {action: "Let agents fix broken builds and change the pipeline for approval", finding: "Agents fix few broken builds or pipeline problems for approval"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: {action: "Let agents respond to build failures and land fixes on their own", finding: "Agents land few fixes for build failures without supervision"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: {action: "Let coordinated agents keep your pipeline healthy under your policy", finding: "Few pipeline changes are made by coordinated agents under your policy"},
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: {action: "Have an assistant answer questions while your people run releases", finding: "Few of your people use an assistant while running releases"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: {action: "Walk through each release with an assistant, step by step", finding: "Few releases are walked through with an assistant, step by step"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: {action: "Let agents run releases from a written procedure you approve", finding: "Agents run few releases from a written procedure with approval"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: {action: "Let agents perform approved releases on their own", finding: "Agents perform few releases without supervision"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: {action: "Let agents decide release, verification and rollback under your policy", finding: "Few releases are decided and verified by agents under your policy"},
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: {action: "Have an assistant help your people look into logs and stack traces", finding: "Few problems are investigated with an assistant's help on logs"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: {action: "Debug and check system health in conversation with an assistant", finding: "Few debugging sessions happen in conversation with an assistant"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: {action: "Let agents investigate problems and propose fixes for a person to apply", finding: "Agents investigate few problems and propose fixes for a person"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: {action: "Let agents detect, diagnose and fix problems on their own", finding: "Agents fix few problems without supervision"},
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: {action: "Let agents detect, fix and verify problems under your policy", finding: "Agents detect, fix and verify few problems under your policy"},
	},
}

// twoShareCellCopies are the L5 cells that read two independent shares (see
// twoShareClauses): primary is the enforced-practice share, secondary the
// declared-policy-approval share. Which one a constraint is about follows
// Bottleneck.SecondaryMiss.
var twoShareCellCopies = map[aidlcv1.AidlcStage]struct{ primary, secondary cellCopy }{
	aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC: {
		primary:   cellCopy{action: "Get your people drafting specs under a practice your admins enforce", finding: "Few people draft specs under a practice you enforce"},
		secondary: cellCopy{action: "Have spec approvals follow your declared review policy", finding: "Few spec approvals follow your declared review policy"},
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW: {
		primary:   cellCopy{action: "Get reviewers following a review practice your admins enforce", finding: "Few reviewers follow a review practice you enforce"},
		secondary: cellCopy{action: "Have review approvals follow your declared review policy", finding: "Few review approvals follow your declared policy"},
	},
}

// cellCopyFor is the copy entry for a (stage, level) cell, reporting false for a cell no
// table carries so the composers fall back to the generic phrasing built from
// practiceText.
func cellCopyFor(stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel, secondaryMiss bool) (cellCopy, bool) {
	if level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L5 {
		if ts, ok := twoShareCellCopies[stage]; ok {
			if secondaryMiss {
				return ts.secondary, true
			}
			return ts.primary, true
		}
	}
	cc, ok := cellCopies[stage][level]
	return cc, ok
}

// commaText renders a count with thousands separators: constraint and move prose can
// carry populations in the low thousands (a merged-change count), and a bare four-digit
// number reads as an internal count rather than something a reader restates.
func commaText(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}
