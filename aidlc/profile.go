// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// stageOrder is the nine stages in the map's narrative order, the order stages are
// scored and rendered in.
var stageOrder = []aidlcv1.AidlcStage{
	aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC,
	aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC,
	aidlcv1.AidlcStage_AIDLC_STAGE_PLAN,
	aidlcv1.AidlcStage_AIDLC_STAGE_CODE,
	aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW,
	aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA,
	aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD,
	aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY,
	aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE,
}

// StageVerdict is one scored stage's headline.
type StageVerdict struct {
	Stage        aidlcv1.AidlcStage
	Frontier     aidlcv1.AidlcLevel
	EmergingEdge aidlcv1.AidlcLevel
	// Breadth is the breadth disclosure: how widely this
	// stage's reading is held, against the organisation's active engineers. It sets
	// none of Frontier or EmergingEdge above.
	Breadth      aidlcv1.MaturityBreadth
	Cells        []*Cell
	AllAbstained bool
}

// Result is one organisation's scored map: the assembled profile, plus the flat
// scored cells, stage verdicts and ranked bottlenecks behind it. The Bottlenecks slice
// is the full set (every subject); Profile carries the org-facing subset.
type Result struct {
	Profile     *aidlcv1.MaturityProfile
	Stages      []StageVerdict
	Cells       []*Cell
	Bottlenecks []Bottleneck
	GeneratedAt *timestamppb.Timestamp
	// rs is the ruleset the result was scored under, kept so the stored bottleneck
	// prose is composed with the same cuts the profile's was.
	rs *rules.RuleSet
}

// Score is the scorer's one entry point: it evaluates the ruleset's forty-five cells
// over the organisation's readings, closes each stage downward, derives the frontier,
// bottlenecks and summary, and assembles the MaturityProfile.
//
// rs and rd must not be nil: a nil ruleset (such as rules.ForVersion returns for an
// unknown version) or nil readings panic. Build readings with NewReadings, not as a
// zero Readings value.
func Score(rs *rules.RuleSet, rd *Readings, org OrgRead, generatedAt *timestamppb.Timestamp) *Result {
	grid := map[aidlcv1.AidlcStage]map[aidlcv1.AidlcLevel]*Cell{}
	var stages []StageVerdict
	var flat []*Cell

	for _, stage := range stageOrder {
		cells := map[aidlcv1.AidlcLevel]*Cell{}
		for _, lv := range ladderOrder {
			rule, ok := rs.Cells[rules.CellKey{Stage: stage, Level: lv}]
			if !ok {
				continue
			}
			cells[lv] = scoreCell(rs, rd, rule, org.DeclarationsHonoured)
		}
		closeDownward(cells)
		frontier, edge := stageFrontier(cells)
		sv := StageVerdict{
			Stage: stage, Frontier: frontier, EmergingEdge: edge,
			Breadth: stageBreadth(rd.StagePeople(stage), org.ActiveEngineers, org.ActiveEngineersUnresolved),
		}
		allAbstain := true
		for _, lv := range ladderOrder {
			if c := cells[lv]; c != nil {
				sv.Cells = append(sv.Cells, c)
				flat = append(flat, c)
				if c.State != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE {
					allAbstain = false
				}
			}
		}
		sv.AllAbstained = allAbstain
		stages = append(stages, sv)
		grid[stage] = cells
	}

	bottlenecks := deriveBottlenecks(rs, grid, org)

	return &Result{
		Profile:     assembleProfile(rs, stages, bottlenecks, generatedAt),
		Stages:      stages,
		Cells:       flat,
		Bottlenecks: bottlenecks,
		GeneratedAt: generatedAt,
		rs:          rs,
	}
}

// assembleProfile builds the org-facing MaturityProfile: the nine stage rows with
// their cells, the org-subject bottlenecks (a product-subject finding is internal
// only), and the recommendations answering the bottlenecks that carry a move.
func assembleProfile(
	rs *rules.RuleSet, stages []StageVerdict, bottlenecks []Bottleneck, generatedAt *timestamppb.Timestamp,
) *aidlcv1.MaturityProfile {
	p := &aidlcv1.MaturityProfile{
		WindowDays:  int32(rs.WindowDays),
		GeneratedAt: generatedAt,
	}
	for _, sv := range stages {
		ms := &aidlcv1.MaturityStage{
			Stage:           sv.Stage,
			WhatThisStageIs: stageWhat(sv.Stage),
			Frontier:        sv.Frontier,
			EmergingEdge:    sv.EmergingEdge,
			Breadth:         sv.Breadth,
		}
		if sv.AllAbstained {
			ms.AbstainReason = reasonRowLevel
		}
		for _, c := range sv.Cells {
			ms.Cells = append(ms.Cells, toMaturityCell(rs, c, sv.Cells))
		}
		p.Stages = append(p.Stages, ms)
	}
	for _, b := range bottlenecks {
		if b.Subject == AudienceProduct { // internal only, dropped for the org audience
			continue
		}
		p.Bottlenecks = append(p.Bottlenecks, &aidlcv1.MaturityBottleneck{
			Id: b.ID, Kind: constraintKind(b), Impact: b.Impact, Title: bottleneckTitle(rs, b), Body: bottleneckBody(rs, b),
			Stage: b.Stage, Level: b.Level, Progress: progressFor(rs, b),
		})
		if b.HasMove {
			p.Recommendations = append(p.Recommendations, &aidlcv1.MaturityRecommendation{
				Id: "recommendation_" + b.ID, Addresses: b.ID, Impact: b.Impact,
				Title: moveTitle(rs, b), Body: moveBody(rs, b),
			})
		}
	}
	p.Summary = summarise(stages)
	p.CoverageItems = coverageItems(rs, stages)
	return p
}

// constraintKind is the typed kind a served constraint carries so a reader can branch
// without parsing the id. A finding with no org-facing kind (a gap) maps to
// unspecified; none is served.
func constraintKind(b Bottleneck) aidlcv1.MaturityConstraintKind {
	switch {
	case b.ID == bottleneckIDReviewReach:
		return aidlcv1.MaturityConstraintKind_MATURITY_CONSTRAINT_KIND_REVIEW_REACH
	case b.ID == bottleneckIDAdoptionReach:
		return aidlcv1.MaturityConstraintKind_MATURITY_CONSTRAINT_KIND_ADOPTION_REACH
	case strings.HasPrefix(b.ID, bottleneckIDPrefixRung):
		return aidlcv1.MaturityConstraintKind_MATURITY_CONSTRAINT_KIND_LEVEL_FELL_SHORT
	case strings.HasPrefix(b.ID, bottleneckIDPrefixLadder):
		return aidlcv1.MaturityConstraintKind_MATURITY_CONSTRAINT_KIND_STAGE_STALLED
	default:
		return aidlcv1.MaturityConstraintKind_MATURITY_CONSTRAINT_KIND_UNSPECIFIED
	}
}

// progressFor is the constraint's progress toward the line its kind is measured
// against: a level that fell short or a stalled stage toward the emerging line, review
// reach toward the established line (its distance counted from the recorded floor, the
// hits), adoption reach toward half the roster. Adoption reach's stored hits count the
// people without an AI-authored merge, so its current count is the roster minus those
// hits and every bar fills toward better. Nil when the constraint rests on no share, and
// nil for a people share under peopleCountFloor: the exact share over a known roster
// would reveal the hidden count. ItemsToTarget is unset when no further item is
// needed.
func progressFor(rs *rules.RuleSet, b Bottleneck) *aidlcv1.MaturityProgress {
	if b.Terms == nil || b.Terms.Population <= 0 || !countsShown(*b.Terms) {
		return nil
	}
	t := *b.Terms
	current, target := t.Hits, rs.BandCuts.Emerging
	var more int64
	switch {
	case b.ID == bottleneckIDReviewReach:
		target = rs.BandCuts.Established
		more = itemsToClear(t, target)
	case b.ID == bottleneckIDAdoptionReach:
		current, target = t.Population-t.Hits, adoptionPassiveLine
		more = peopleToPassHalf(t)
	case strings.HasPrefix(b.ID, bottleneckIDPrefixRung), strings.HasPrefix(b.ID, bottleneckIDPrefixLadder):
		more = itemsToClear(t, target)
	default:
		return nil
	}
	population, unobserved := t.Population, t.Unobserved
	pr := &aidlcv1.MaturityProgress{
		CurrentShare: float64(current) / float64(t.Population),
		TargetShare:  target,
		Current:      &current, Population: &population, Unobserved: &unobserved,
		TargetLabel: pctText(rs, target),
	}
	pr.CurrentLabel = pctText(rs, pr.GetCurrentShare())
	if t.Unobserved > 0 {
		pr.CurrentLabel = "at least " + pr.GetCurrentLabel()
	}
	if more > 0 {
		pr.ItemsToTarget = &more
	}
	return pr
}

// toMaturityCell converts a scored cell to the profile's cell: the verdict, what the
// level means for this stage, the deciding terms and the readings behind them. Band is
// not a MaturityCell field (the profile cell carries state and decided-by; the band
// stays on the Result's scored cells), so it is dropped here.
// siblings are the stage's cells (this one included), read only to name the rung an
// implied floor was awarded under.
func toMaturityCell(rs *rules.RuleSet, c *Cell, siblings []*Cell) *aidlcv1.MaturityCell {
	mc := &aidlcv1.MaturityCell{
		Level:      c.Level,
		State:      c.State,
		DecidedBy:  c.DecidedBy,
		Definition: cellDefinitionFor(c.Stage, c.Level),
	}
	if c.State == aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE {
		mc.AbstainKind = c.abstain.proto()
		mc.Reason = c.Reason
	}
	mc.Terms, mc.Evidence = renderTerms(rs, c, siblings)
	return mc
}

// summarise composes the deterministic prose a reader sees for the whole map: every
// stage grouped by its frontier and emerging edge, one sentence per group, named by
// title and the practice that level looks like (never the level's own name, which
// names nothing a reader can picture on its own), with no number anywhere. Three shapes cover every profile: some stages
// established, nothing established but something emerging, or nothing established
// or emerging at all.
func summarise(stages []StageVerdict) string {
	var established, emergingOnly []StageVerdict
	var unmeasured []aidlcv1.AidlcStage
	for _, sv := range stages {
		if sv.AllAbstained {
			unmeasured = append(unmeasured, sv.Stage)
		}
		switch {
		case sv.Frontier != aidlcv1.AidlcLevel_AIDLC_LEVEL_UNSPECIFIED:
			established = append(established, sv)
		case sv.EmergingEdge != aidlcv1.AidlcLevel_AIDLC_LEVEL_UNSPECIFIED:
			emergingOnly = append(emergingOnly, sv)
		}
	}
	if len(established) == 0 && len(emergingOnly) == 0 {
		// Nothing decided anywhere is not a finding about the organisation: say so rather
		// than let "none" read as "AI is barely used".
		allAbstained := true
		for _, sv := range stages {
			if !sv.AllAbstained {
				allAbstained = false
				break
			}
		}
		if allAbstained {
			return "We could not measure any stage yet, so this map has no reading for your organisation."
		}
		if len(unmeasured) > 0 {
			return "None of the stages that could be measured shows AI in a real share of its work over this window yet. " + unmeasuredSentence(unmeasured)
		}
		return "None of your stages shows AI in a real share of its work over this window yet."
	}
	if len(established) == 0 {
		lead := "No stage is established yet."
		if len(unmeasured) > 0 {
			lead = "No stage that could be measured is established yet."
		}
		sentences := append([]string{lead}, frontierSentences(emergingOnly, shapeEmergingEverywhere)...)
		if len(unmeasured) > 0 {
			sentences = append(sentences, unmeasuredSentence(unmeasured))
		}
		return strings.Join(sentences, " ")
	}
	sentences := frontierSentences(established, shapeEstablished)
	sentences = append(sentences, frontierSentences(emergingOnly, shapeNothingEstablished)...)
	if len(unmeasured) > 0 {
		sentences = append(sentences, unmeasuredSentence(unmeasured))
	}
	return strings.Join(sentences, " ")
}

// unmeasuredSentence names the stages with no decided cell, so the summary never reads
// a stage nothing measured as one measured low.
func unmeasuredSentence(stages []aidlcv1.AidlcStage) string {
	return "Nothing could be measured in " + joinWithAnd(stageTitlesFor(stages)) + "."
}

// summaryLevelPractice names each level's shape as a practice a reader can picture,
// standing in for the level's own name ("assisted", "supervised agentic")
// wherever the summary names a level — the same person/assistant/agent vocabulary
// the practice-phrase table (copy.go) is authored in, generalised across stages here
// since one summary sentence can name several stages sharing a level at once.
var summaryLevelPractice = map[aidlcv1.AidlcLevel]string{
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "a person does the work and an assistant helps on request",
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "a person and an assistant work the task out together in conversation",
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "an agent produces a complete piece of work and a person checks and approves it",
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "an agent completes a piece of work unattended and a person checks the result afterward",
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "the organisation's own policy coordinates the work across agents",
}

// summaryShape selects a summary sentence's template.
type summaryShape int

const (
	// shapeEstablished: "In X and Y, PRACTICE[, and the work is moving toward a way
	// of working where PRACTICE]."
	shapeEstablished summaryShape = iota
	// shapeNothingEstablished: "X has nothing established yet and is moving toward a
	// way of working where PRACTICE." — an emerging-only stage beside established ones.
	shapeNothingEstablished
	// shapeEmergingEverywhere: "X is moving toward a way of working where PRACTICE."
	// — after the summary has already said no stage is established.
	shapeEmergingEverywhere
)

// frontierSentences groups stage verdicts by their (frontier, emerging edge) pair and
// renders one sentence per group, ordered from the highest frontier (or, for an
// emerging-only group, the highest edge) down, so the prose reads most-advanced
// first. Each sentence names a practice phrase at most once per level it names.
func frontierSentences(svs []StageVerdict, shape summaryShape) []string {
	type group struct {
		frontier, edge aidlcv1.AidlcLevel
		stages         []aidlcv1.AidlcStage
	}
	var groups []group
	index := map[[2]aidlcv1.AidlcLevel]int{}
	for _, sv := range svs {
		key := [2]aidlcv1.AidlcLevel{sv.Frontier, sv.EmergingEdge}
		if i, ok := index[key]; ok {
			groups[i].stages = append(groups[i].stages, sv.Stage)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, group{frontier: sv.Frontier, edge: sv.EmergingEdge, stages: []aidlcv1.AidlcStage{sv.Stage}})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].frontier != groups[j].frontier {
			return levelOrd(groups[i].frontier) > levelOrd(groups[j].frontier)
		}
		return levelOrd(groups[i].edge) > levelOrd(groups[j].edge)
	})

	sentences := make([]string, 0, len(groups))
	for _, g := range groups {
		names := joinWithAnd(stageTitlesFor(g.stages))
		plural := len(g.stages) > 1
		switch shape {
		case shapeEstablished:
			sentence := fmt.Sprintf("In %s, %s", names, summaryLevelPractice[g.frontier])
			if g.edge != aidlcv1.AidlcLevel_AIDLC_LEVEL_UNSPECIFIED {
				sentence += fmt.Sprintf(", and the work is moving toward a way of working where %s", summaryLevelPractice[g.edge])
			}
			sentences = append(sentences, sentence+".")
		case shapeNothingEstablished:
			has, is := "has", "is"
			if plural {
				has, is = "have", "are"
			}
			sentences = append(sentences, fmt.Sprintf("%s %s nothing established yet and %s moving toward a way of working where %s.", names, has, is, summaryLevelPractice[g.edge]))
		case shapeEmergingEverywhere:
			is := "is"
			if plural {
				is = "are"
			}
			sentences = append(sentences, fmt.Sprintf("%s %s moving toward a way of working where %s.", names, is, summaryLevelPractice[g.edge]))
		}
	}
	return sentences
}

func stageTitlesFor(stages []aidlcv1.AidlcStage) []string {
	titles := make([]string, len(stages))
	for i, s := range stages {
		titles[i] = stageTitle(s)
	}
	return titles
}

// joinWithAnd renders a name list the way the summary's prose does: no separator for
// one name, "and" for two, and a comma-separated list with "and" before the last
// name for three or more (no Oxford comma).
func joinWithAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
}

// Each constraint carries four texts with fixed roles: the constraint's title and body
// state the measurement, the move's title is the action, and the move's body is the
// impact and the distance to the line. No text uses the rubric's own vocabulary (level
// numbers, "read", "missed", "cut", "rung"); titles stay within 12 words and bodies
// within 60 (TestBottleneckCopy_ExhaustiveStageLevelSweep holds every cell to both).

func bottleneckTitle(rs *rules.RuleSet, b Bottleneck) string {
	switch b.ID {
	case bottleneckIDReviewReach:
		return fmt.Sprintf("Recorded review reaches under %s of AI-written pull requests.", oneInWords(rs, rs.BandCuts.Established))
	case bottleneckIDAdoptionReach:
		if b.Terms != nil && b.Terms.Hits*2 > b.Terms.Population {
			return "Most people who merge work have no AI-written merge yet."
		}
		return "At least half the people who merge work have no AI-written merge."
	default:
		if strings.HasPrefix(b.ID, bottleneckIDPrefixGap) {
			return "An instrumentation gap blocks a column."
		}
		return rungTitle(b)
	}
}

// rungTitle names the practice a level-fell-short constraint is about, as what is
// scarce today. A cell no table carries gets a generic title that points at the
// practice its body quotes.
func rungTitle(b Bottleneck) string {
	if cc, ok := cellCopyFor(b.Stage, b.Level, b.SecondaryMiss); ok {
		return cc.finding + "."
	}
	return fmt.Sprintf("Little of your %s work follows the practice below.", stageNoun(b.Stage))
}

// peopleCountFloor is the smallest people denominator whose counts may be shown: below
// it a numerator and denominator would name the individuals the reading protects. The
// same bar decides the copy's counts and the progress record's.
const peopleCountFloor = 20

// countsShown reports whether a share's counts may be rendered: every share except a
// people share over fewer than peopleCountFloor people.
func countsShown(t MoveTerms) bool {
	return t.Grain != aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE || t.Population >= peopleCountFloor
}

// shareOf renders a share as "<hits> of <population> <items>". A people share over
// fewer than peopleCountFloor people never reaches it: its callers print peopleBand.
func shareOf(t MoveTerms) string {
	return fmt.Sprintf("%s of %s %s", commaText(t.Hits), commaText(t.Population), itemWord(t.Grain, t.Population))
}

// peopleBand says where a hidden people share sits against the band cuts, as a noun
// phrase over the universe ("fewer than one in five of your people"). It carries no
// count and no percentage for the standard cuts.
func peopleBand(rs *rules.RuleSet, t MoveTerms) string {
	frac := float64(t.Hits) / float64(t.Population)
	switch {
	case frac < rs.BandCuts.Emerging:
		return "fewer than " + oneInWords(rs, rs.BandCuts.Emerging) + " of your people"
	case frac == rs.BandCuts.Emerging:
		return "no more than " + oneInWords(rs, rs.BandCuts.Emerging) + " of your people"
	case frac <= rs.BandCuts.Established:
		return "between " + oneInWords(rs, rs.BandCuts.Emerging) + " and " + oneInWords(rs, rs.BandCuts.Established) + " of your people"
	default:
		return "more than " + oneInWords(rs, rs.BandCuts.Established) + " of your people"
	}
}

func bottleneckBody(rs *rules.RuleSet, b Bottleneck) string {
	switch b.ID {
	case bottleneckIDReviewReach:
		if b.Terms != nil {
			return fmt.Sprintf("Recorded review reaches %s of AI-written pull requests (%s of %s merged in the last %d days). Review done before a pull request opens is never recorded, so treat this as a floor.",
				reachText(rs, *b.Terms), commaText(b.Terms.Hits), commaText(b.Terms.Population), rs.WindowDays)
		}
		return "Recorded review thins out exactly where AI-written pull requests concentrate. Review done before a pull request opens is never recorded, so treat this as a floor."
	case bottleneckIDAdoptionReach:
		// A hidden small-group share already has its band in the title, so the body falls
		// through to the sentence that carries no count rather than restating it.
		if b.Terms != nil && countsShown(*b.Terms) {
			t := *b.Terms
			return fmt.Sprintf("%s of %s people who merged work in the last %d days have no AI-written merge.", commaText(t.Hits), commaText(t.Population), rs.WindowDays)
		}
		return "Adoption is limited by the people AI tooling has not reached, not by how well it works for those it has."
	default:
		if strings.HasPrefix(b.ID, bottleneckIDPrefixLadder) {
			return ladderBody(rs, b)
		}
		if strings.HasPrefix(b.ID, bottleneckIDPrefixGap) {
			return "Several levels in this column stay blocked until a gap in what we capture is closed. Ours."
		}
		return rungBody(rs, b)
	}
}

// rungBody states what was measured for a level-fell-short constraint, in the reader's
// words: a cell with its own measurement sentence (cellCopy.measure) uses it, every
// other cell quotes its practice over the counts. A rung_ finding exists only for a
// cell that was measured and fell short, so the no-counts sentence says exactly that.
func rungBody(rs *rules.RuleSet, b Bottleneck) string {
	practice := practiceText(b.Stage, b.Level, b.SecondaryMiss)
	if b.Terms == nil {
		return fmt.Sprintf("We measured your %s work against this practice and found too little of it to count yet: %s.", stageNoun(b.Stage), practice)
	}
	t := *b.Terms
	if cc, ok := cellCopyFor(b.Stage, b.Level, b.SecondaryMiss); ok && cc.measure != nil && countsShown(t) {
		return cc.measure(t, rs.WindowDays)
	}
	if !countsShown(t) {
		return fmt.Sprintf("In the last %d days, %s followed this practice: %s.", rs.WindowDays, peopleBand(rs, t), practice)
	}
	return fmt.Sprintf("In the last %d days, %s followed this practice: %s.", rs.WindowDays, shareOf(t), practice)
}

// ladderBody names the first step of a stage with nothing established, in the same
// practice words the level it stopped at uses. It states only what the cell itself
// decided; a stage can also carry a same-row instrumentation gap, so it never offers a
// whole-stage cause.
func ladderBody(rs *rules.RuleSet, b Bottleneck) string {
	lead := fmt.Sprintf("Across your %s work, this practice is still rare: %s.",
		stageNoun(b.Stage), practiceText(b.Stage, b.Level, b.SecondaryMiss))
	if b.Terms == nil {
		return lead
	}
	if !countsShown(*b.Terms) {
		return fmt.Sprintf("%s In the last %d days, %s followed it.", lead, rs.WindowDays, peopleBand(rs, *b.Terms))
	}
	return fmt.Sprintf("%s In the last %d days, %s followed it.", lead, rs.WindowDays, shareOf(*b.Terms))
}

// reachText is the reviewed share as a floor when some reviewer lists were unread.
func reachText(rs *rules.RuleSet, t MoveTerms) string {
	txt := pctText(rs, float64(t.Hits)/float64(t.Population))
	if t.Unobserved > 0 {
		return "at least " + txt
	}
	return txt
}

// moveTitle is the action the move asks for, an imperative naming the practice to
// adopt. A level-fell-short or stalled-stage move takes its cell's action phrase; a cell
// no table carries gets the generic "Get more of your <stage> work done the way
// described below." (the body quotes the practice), so every headline is still a short
// true action.
func moveTitle(_ *rules.RuleSet, b Bottleneck) string {
	switch {
	case b.ID == bottleneckIDReviewReach:
		return "Make recorded review the gate for AI-written pull requests."
	case b.ID == bottleneckIDAdoptionReach:
		return "Help each person without an AI-written merge land a first one."
	case strings.HasPrefix(b.ID, bottleneckIDPrefixLadder), strings.HasPrefix(b.ID, bottleneckIDPrefixRung):
		if cc, ok := cellCopyFor(b.Stage, b.Level, b.SecondaryMiss); ok {
			return cc.action + "."
		}
		return fmt.Sprintf("Get more of your %s work done the way described below.", stageNoun(b.Stage))
	default:
		return ""
	}
}

// moveBody states what clearing the line unlocks and how far away it is: the line, the
// effect on the map, and the smallest number of additional items that clears it
// (itemsToClear owns the grain rule). The distance is omitted where the counts may not be
// shown, and where no counts were carried (a level whose cell recorded no share). Terms
// grounded by termsOf always sit below their line, so a non-positive distance is a
// broken upstream invariant and the sentence never claims "0 more".
func moveBody(rs *rules.RuleSet, b Bottleneck) string {
	switch {
	case b.ID == bottleneckIDReviewReach:
		lead := fmt.Sprintf("Once more than %s of your AI-written pull requests carry a recorded review, review becomes the usual gate for AI-written code.", oneInWords(rs, rs.BandCuts.Established))
		if b.Terms == nil {
			return lead
		}
		more := itemsToClear(*b.Terms, rs.BandCuts.Established)
		if more <= 0 {
			return lead
		}
		return fmt.Sprintf("%s That is about %s more reviewed %s at today's volume, since each one also adds to the total.", lead, commaText(more), itemWord(b.Terms.Grain, more))
	case b.ID == bottleneckIDAdoptionReach:
		lead := "Once more than half of your people have an AI-written merge, AI use is no longer limited to a few."
		if b.Terms == nil || !countsShown(*b.Terms) {
			return lead
		}
		more := peopleToPassHalf(*b.Terms)
		if more <= 0 {
			return lead
		}
		return fmt.Sprintf("%s That is about %s more %s.", lead, commaText(more), itemWord(b.Terms.Grain, more))
	case strings.HasPrefix(b.ID, bottleneckIDPrefixLadder), strings.HasPrefix(b.ID, bottleneckIDPrefixRung):
		if b.Terms == nil {
			return fmt.Sprintf("Once this is common in your %s work, your map counts it as a practice your team is adopting.", stageNoun(b.Stage))
		}
		lead := fmt.Sprintf("Once more than %s of these %s work this way, your map counts it as a practice your team is adopting.",
			oneInWords(rs, rs.BandCuts.Emerging), itemWord(b.Terms.Grain, b.Terms.Population))
		if !countsShown(*b.Terms) {
			return lead
		}
		more := itemsToClear(*b.Terms, rs.BandCuts.Emerging)
		if more <= 0 {
			return lead
		}
		return fmt.Sprintf("%s That is about %s more %s%s.", lead, commaText(more), distanceUnit(b, more), distanceTail(b.Terms.Grain))
	default:
		return ""
	}
}

// distanceUnit names what a constraint's distance counts: the cell's own noun where it
// has one, else the grain's item ("such pull requests" for changes, "people").
func distanceUnit(b Bottleneck, n int64) string {
	if cc, ok := cellCopyFor(b.Stage, b.Level, b.SecondaryMiss); ok && cc.unit[0] != "" {
		if n == 1 {
			return cc.unit[0]
		}
		return cc.unit[1]
	}
	if b.Terms.Grain == aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE {
		return itemWord(b.Terms.Grain, n)
	}
	return "such " + itemWord(b.Terms.Grain, n)
}

// distanceTail closes a distance sentence: a count of changes is tied to the volume the
// window showed, a count of people is not.
func distanceTail(g aidlcv1.ShareGrain) string {
	if g == aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE {
		return ""
	}
	return " at today's volume"
}

// peopleToPassHalf is how many passive people would need an AI-authored merge for the
// passive share to fall below adoptionPassiveLine (the finding fires at or above it).
func peopleToPassHalf(t MoveTerms) int64 {
	if t.Population <= 0 {
		return 0
	}
	// The largest passive count strictly under the line.
	allowed := int64(math.Ceil(adoptionPassiveLine*float64(t.Population))) - 1
	if allowed < 0 {
		allowed = 0
	}
	if t.Hits <= allowed {
		return 0
	}
	return t.Hits - allowed
}

// stageWhat is one line on what a stage covers, the profile's what_this_stage_is.
func stageWhat(s aidlcv1.AidlcStage) string {
	switch s {
	case aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC:
		return "turning a need into an agreed statement of what to build"
	case aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC:
		return "deciding how to build it, written down before building"
	case aidlcv1.AidlcStage_AIDLC_STAGE_PLAN:
		return "turning agreed work into an ordered, sized set of tasks"
	case aidlcv1.AidlcStage_AIDLC_STAGE_CODE:
		return "making the change"
	case aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW:
		return "deciding whether a change is fit to land"
	case aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA:
		return "deciding what to verify and verifying it"
	case aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD:
		return "turning a merged change into a build that can ship"
	case aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY:
		return "putting the build in front of users"
	case aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE:
		return "keeping it working once it is live"
	default:
		return ""
	}
}

// stageKey, levelKey, levelOrd and gapKey render an enum to the short spelling the
// bottleneck ids use ("rung_code_L4", "ladder_ci_cd", "gap_G1").
func stageKey(s aidlcv1.AidlcStage) string {
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
	default:
		return "unspecified"
	}
}

func levelKey(l aidlcv1.AidlcLevel) string {
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
	default:
		return "L0"
	}
}

func levelOrd(l aidlcv1.AidlcLevel) int { return int(l) }

func gapKey(g aidlcv1.InstrumentationGap) string {
	return fmt.Sprintf("G%d", int(g))
}
