// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"math"
	"strings"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// abstainKind is the scorer's internal abstain taxonomy. It is finer than the proto
// AbstainKind because downward closure reads it: a coverage abstain and a
// control-failed abstain both publish as ABSTAIN_KIND_COVERAGE but only the first is
// closable, and a no-resolver and a gap both publish as ABSTAIN_KIND_GAP and both are
// closable. Keeping them distinct here and mapping to the proto on assembly is what
// makes closure and the published kind both correct.
type abstainKind int

const (
	abNone abstainKind = iota
	abGap
	abNoResolver
	abRowAbsent
	abNoPopulation
	abCoverage
	// abReadingsDisagree is a count above its own population: two readings contradict
	// each other. It publishes as coverage but, unlike a coverage abstain, is an answer
	// that something is wrong, so closure never awards a rung over it.
	abReadingsDisagree
	// abMalformed is a reading whose numbers cannot describe a count (NaN, infinite,
	// negative, fractional, beyond 2^53). It publishes as row-absent but, unlike an
	// absent row, is an answer that the input is wrong, so closure never awards over it.
	abMalformed
	abControlFailed
	abPopulationFloor
	abJudgment
	abInstrumentedOnly
	abAutomatedOnly
)

// closable reports whether closure may award a rung carrying this abstain beneath a
// met one: gap, no_resolver, row_absent, no_population and
// coverage. A population-floor, judgment, control-failed, instrumented-only or
// automated-only abstain breaks the chain: it is a read-and-answered denial, not an
// unread cell a capability one rung up outranks.
func (a abstainKind) closable() bool {
	switch a {
	case abGap, abNoResolver, abRowAbsent, abNoPopulation, abCoverage:
		return true
	default:
		return false
	}
}

// proto maps the internal abstain to the published AbstainKind.
func (a abstainKind) proto() aidlcv1.AbstainKind {
	switch a {
	case abGap, abNoResolver:
		return aidlcv1.AbstainKind_ABSTAIN_KIND_GAP
	case abRowAbsent, abMalformed:
		return aidlcv1.AbstainKind_ABSTAIN_KIND_ROW_ABSENT
	case abNoPopulation:
		return aidlcv1.AbstainKind_ABSTAIN_KIND_NO_POPULATION
	case abCoverage, abControlFailed, abReadingsDisagree:
		return aidlcv1.AbstainKind_ABSTAIN_KIND_COVERAGE
	case abPopulationFloor:
		return aidlcv1.AbstainKind_ABSTAIN_KIND_POPULATION_FLOOR
	case abJudgment:
		return aidlcv1.AbstainKind_ABSTAIN_KIND_JUDGMENT
	case abInstrumentedOnly:
		return aidlcv1.AbstainKind_ABSTAIN_KIND_INSTRUMENTED_ONLY
	case abAutomatedOnly:
		return aidlcv1.AbstainKind_ABSTAIN_KIND_AUTOMATED_ONLY
	default:
		return aidlcv1.AbstainKind_ABSTAIN_KIND_UNSPECIFIED
	}
}

// ShareInfo is the banded share and both ends of its coverage interval behind a
// prevalence cell, carried structurally so a consumer never re-derives it from prose.
type ShareInfo struct {
	Lo, Hi          float64
	Hits            int64
	Population      int64
	Unobserved      int64
	Grain           aidlcv1.ShareGrain
	UncalibratedCut bool
	// DenCoverage is the population reading's coverage, kept for the rendered evidence
	// (the numerator's travels on the prevalence check). Not persisted.
	DenCoverage Coverage
}

// The check kinds a scored cell records (CellCheck.kind). The renderer switches on
// them, so they are named once here.
const (
	checkKindPrevalence   = "prevalence"
	checkKindPeopleFloor  = "people_floor"
	checkKindControl      = "control"
	checkKindValidity     = "validity"
	checkKindPopulation   = "population"
	checkKindUnmeasurable = "unmeasurable"
	checkKindNumerator    = "numerator"
)

// g4Blocker names the check that holds a G4-gated cell closed. G4 covers three
// different missing facts on the L5 cells, each with a different owner, plus a cell
// that reads neither: the cell's reason (reasonForG4) and the coverage block
// (coverageItems) both read this identity rather than re-deriving it from the rule,
// so the two surfaces always name the same blocker.
type g4Blocker int

const (
	// g4BlockerNotHonoured: the rule reads the review-policy declaration and the
	// organisation's declarations are not honoured. Ours.
	g4BlockerNotHonoured g4Blocker = iota
	// g4BlockerUndeclared: the rule reads the review-policy declaration, the
	// organisation's declarations are honoured, and none is declared. Theirs.
	g4BlockerUndeclared
	// g4BlockerUnenforced: the rule reads the stage's enforced practice and no
	// admin-pushed practice artifact is enforced for the stage. Theirs.
	g4BlockerUnenforced
	// g4BlockerUninstrumented: the rule reads neither the declaration nor
	// enforcement (a not-measurable or judgment cell naming G4); the level is not
	// measured at all. Ours.
	g4BlockerUninstrumented
)

// g2Blocker names what holds a G2-gated (Plan) cell closed, read by the cell's reason
// (reasonForGap) and the coverage block (coverageItems) alike.
type g2Blocker int

const (
	// g2BlockerNotMeasured: the rule does not measure planning at all (a
	// not-measurable or judgment cell naming G2). Ours.
	g2BlockerNotMeasured g2Blocker = iota
	// g2BlockerNoIssues: the rule reads the tracker and no issue was created in the
	// window. Theirs. Whether a tracker is connected at all is not a fact the scorer
	// reads, so an organisation with no tracker reads this blocker too.
	g2BlockerNoIssues
)

// enforcementSignals are the stage-scoped enforcement reads a G4-gated L5 rule's
// primary share is composed from; the fold gap-blocks all three together when
// nothing is enforced for the stage.
var enforcementSignals = map[aidlcv1.SignalKey]bool{
	aidlcv1.SignalKey_SIGNAL_KEY_ENFORCED_PRACTICE_ARTIFACTS_BY_STAGE:           true,
	aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE:        true,
	aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE: true,
}

// g4BlockerForSignal names the blocker a gap-blocked read of key stands for. A
// gap-blocked review-policy read means "not honoured" or, once honoured, "not
// declared" (foldReviewPolicy's only two gap-block causes).
func g4BlockerForSignal(key aidlcv1.SignalKey, declarationsHonoured bool) g4Blocker {
	switch {
	case rules.IsDeclarationSignal(rules.DeclarationReviewPolicy, key):
		if declarationsHonoured {
			return g4BlockerUndeclared
		}
		return g4BlockerNotHonoured
	case enforcementSignals[key]:
		return g4BlockerUnenforced
	default:
		return g4BlockerUninstrumented
	}
}

// gapBlockedSignal returns the first of a check's two signals whose reading is
// gap-blocked, in the order the scorer resolves them (count, then total).
func gapBlockedSignal(rd *Readings, ch *rules.Check, stage aidlcv1.AidlcStage) (aidlcv1.SignalKey, bool) {
	for _, key := range []aidlcv1.SignalKey{ch.Signal, ch.Denominator} {
		if rd.resolve(key, stage).Status == StatusGapBlocked {
			return key, true
		}
	}
	return aidlcv1.SignalKey_SIGNAL_KEY_UNSPECIFIED, false
}

// g4BlockersFor lists every G4 blocker holding a share rule's cell closed, the
// first being the check the scorer stopped at (the primary share before the
// secondary). The later checks are read too so a cell missing both its enforcement
// and its declaration says so, rather than implying either alone would open it.
func g4BlockersFor(rd *Readings, rule rules.CellRule, declarationsHonoured bool) []g4Blocker {
	var out []g4Blocker
	add := func(b g4Blocker) {
		for _, have := range out {
			if have == b {
				return
			}
		}
		out = append(out, b)
	}
	for i := range rule.Checks {
		ch := &rule.Checks[i]
		if ch.Kind != rules.CheckKindPrevalenceShare && ch.Kind != rules.CheckKindSecondaryShare {
			continue
		}
		if key, ok := gapBlockedSignal(rd, ch, rule.Stage); ok {
			add(g4BlockerForSignal(key, declarationsHonoured))
		}
	}
	return out
}

// Cell is one scored cell: the verdict, band, share and checks behind the profile's
// MaturityCell, with the fields a caller needs to store a day's cells (for
// DeriveMovement, say) that the profile's cell does not carry.
type Cell struct {
	Stage     aidlcv1.AidlcStage
	Level     aidlcv1.AidlcLevel
	State     aidlcv1.CellState
	DecidedBy aidlcv1.DecidedBy
	Band      aidlcv1.PrevalenceBand
	abstain   abstainKind
	Gap       aidlcv1.InstrumentationGap
	Reason    string
	Share     *ShareInfo
	// Share2 is the secondary share's reading (the risk-class rule review/L5 and
	// product-spec/L5 carry: a declared-policy-approval share composed with Share as
	// the lower band) — nil for a cell with no secondary check, or when the secondary
	// check itself abstained the cell. Set whenever the check ran and produced a
	// placeable share, regardless of which band it lands in.
	Share2 *ShareInfo
	// SecondaryMiss is true when the primary share (Share) read above the emerging cut
	// but the secondary share (Share2) is what pulled the cell to NOT_REACHED. A
	// composer describing this cell must use Share2's numbers, not Share's — Share
	// alone would state the opposite of the finding (it is the share that passed).
	SecondaryMiss bool
	Checks        []*aidlcv1.CellCheck
	AwardedFloor  bool
	// g4Blockers is set only on a cell abstaining on G4: every check holding it
	// closed, the first being the one the scorer stopped at (see g4Blocker).
	g4Blockers []g4Blocker
	// g2Blocker is meaningful only on a cell abstaining on G2.
	g2Blocker g2Blocker
}

// scoreCell dispatches on the rule kind.
func scoreCell(rs *rules.RuleSet, rd *Readings, rule rules.CellRule, declarationsHonoured bool) *Cell {
	c := &Cell{Stage: rule.Stage, Level: rule.Level, Gap: rule.Gap}
	switch rule.Kind {
	case rules.RuleKindNotMeasurable:
		c.State = aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE
		c.abstain = abGap
		c.setGapReason(rule.Gap, []g4Blocker{g4BlockerUninstrumented}, g2BlockerNotMeasured)
		return c
	case rules.RuleKindJudgment:
		c.State = aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE
		c.abstain = abJudgment
		// A judgment cell that names a gap is blocked by that gap first: what would
		// let a judgment be made is the missing instrumentation, so the reader is told
		// about the gap, not about judgment.
		if rule.Gap != aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_UNSPECIFIED {
			c.setGapReason(rule.Gap, []g4Blocker{g4BlockerUninstrumented}, g2BlockerNotMeasured)
		} else {
			c.Reason = reasonJudgment
		}
		return c
	case rules.RuleKindImpliedFloor:
		// Abstains on its gap until downward closure awards it under a met higher rung.
		c.State = aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE
		c.abstain = abGap
		c.Reason = reasonImpliedFloorPending
		return c
	case rules.RuleKindPrevalence:
		return scoreShare(rs, rd, rule, c, declarationsHonoured)
	default:
		c.State = aidlcv1.CellState_CELL_STATE_UNSPECIFIED
		return c
	}
}

// scoreShare is the prevalence rule's decision order. The comments name the guard
// each step is.
func scoreShare(rs *rules.RuleSet, rd *Readings, rule rules.CellRule, c *Cell, declarationsHonoured bool) *Cell {
	shareCheck := findCheck(rule.Checks, rules.CheckKindPrevalenceShare)
	if shareCheck == nil { // a prevalence rule always carries one
		c.State = aidlcv1.CellState_CELL_STATE_UNSPECIFIED
		return c
	}
	// A prevalence rule may name the gap that blocks it when its gate is closed (the
	// L5 and Plan cells); the gap is carried on the cell only when it actually blocks,
	// so a decided cell is never listed as gap-blocked.
	gate := rule.Gap
	c.Gap = aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_UNSPECIFIED
	numDef := rs.Signals[shareCheck.Signal]
	denDef := rs.Signals[shareCheck.Denominator]
	num := rd.resolveCount(shareCheck.Signal, rule.Stage)
	den := rd.resolveCount(shareCheck.Denominator, rule.Stage)

	// 1. numerator and denominator must both resolve. A gate the fold found
	// closed abstains on the rule's gap with the gap's own sentence.
	for _, r := range []Reading{num, den} {
		if r.Status == StatusGapBlocked {
			c.Gap = gate
			c.abstain = abGap
			c.State = aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE
			// A gap-blocked G2 read is the tracker's empty window (foldPlan's only
			// G2 gap-block cause).
			c.setGapReason(gate, g4BlockersFor(rd, rule, declarationsHonoured), g2BlockerNoIssues)
			return c
		}
	}
	for _, r := range []Reading{num, den} {
		if r.Status != StatusResolved || r.Value == nil {
			c.abstainOut(abstainFor(r), reasonFor(r))
			return c
		}
	}

	// 2. a declared denominator remainder the row does not carry is a stale run.
	if den.RemainderDeclared && den.RemainderValue == nil {
		c.abstainOut(abNoResolver, reasonTermUnreadable)
		return c
	}

	// 3. a zero on a signal that only ever sees an instrumented artifact abstains. The
	// zero is recorded so the cell can show what was read. A zero that comes with unread
	// hits (people whose stage could not be named) widens the share below first: it
	// abstains on coverage if the widened share straddles a cut, and instrumented-only
	// otherwise (step 11b). A zero on such a signal never denies.
	zeroInstrumented := numDef.Observes == rules.ObservesInstrumentedOnly && *num.Value == 0
	if zeroInstrumented && (num.RemainderValue == nil || *num.RemainderValue == 0) {
		c.Checks = append(c.Checks, &aidlcv1.CellCheck{
			Key: shareCheck.Signal, Kind: checkKindNumerator, MeasuredValue: 0, Comparator: "read", Coverage: coverageProto(num.Coverage),
		})
		c.abstainOut(abInstrumentedOnly, reasonInstrumentedOnly)
		return c
	}

	// 4. a declared numerator remainder the row does not carry is a stale run.
	if num.RemainderDeclared && num.RemainderValue == nil {
		c.abstainOut(abNoResolver, reasonTermUnreadable)
		return c
	}

	// 5. a count above the population's counted value is two readings disagreeing about
	// the same events (a hit the denominator did not count); no share can be placed on
	// it, and closure never awards over it. A declared remainder is unenumerated, so it
	// cannot make up the difference.
	hits := int(*num.Value)
	if hits > int(*den.Value) {
		c.abstainOut(abReadingsDisagree, reasonReadingsDisagree)
		return c
	}

	// 6. the two kinds of unread mass.
	denUnknown := 0
	if den.RemainderValue != nil {
		denUnknown = *den.RemainderValue
	}
	numUnreadable := 0
	if num.RemainderValue != nil {
		numUnreadable = *num.RemainderValue
	}
	population := int(*den.Value) + denUnknown

	// 7. partial coverage widens the numerator's unread mass. A bound converts to a
	// count; a declared-partial has no number, so the honest bound is the whole
	// remainder. The wider of remainder and bound wins.
	switch num.Coverage {
	case CoveragePartial:
		if num.BoundValue != nil {
			widened := int(math.Round(float64(population) * (1.0 - *num.BoundValue)))
			numUnreadable = maxInt(numUnreadable, widened)
		} else {
			numUnreadable = maxInt(numUnreadable, maxInt(0, population-int(*num.Value)))
		}
	case CoverageDeclaredPartial, CoverageNone, CoverageUnknown:
		// A declared subset, a reading that stands for nothing, and a coverage that
		// could not be established all leave every non-hit unread.
		numUnreadable = maxInt(numUnreadable, maxInt(0, population-int(*num.Value)))
	case CoverageFull:
		// No unread mass.
	}
	unobserved := denUnknown + numUnreadable

	// 8. the population floor, by grain. Below it a share is a small-sample artifact.
	floor := rs.PopulationFloors.Artifacts
	grain := aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES
	if denDef.Grain == aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE {
		floor = rs.PopulationFloors.People
		grain = aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE
	}
	if population > 0 && population < floor {
		c.Checks = append(c.Checks, populationCheck(shareCheck.Denominator, population, floor, den.Coverage))
		c.abstainOut(abPopulationFloor, reasonPopulationFloor(rs, floor, grain))
		return c
	}

	// 9. a zero denominator is not a zero share.
	if int(*den.Value) == 0 {
		if unobserved == 0 {
			c.Checks = append(c.Checks, populationCheck(shareCheck.Denominator, 0, floor, den.Coverage))
		} else {
			c.Checks = append(c.Checks, &aidlcv1.CellCheck{
				Key: shareCheck.Denominator, Kind: checkKindUnmeasurable, MeasuredValue: float64(unobserved),
				Comparator: "read", Coverage: coverageProto(den.Coverage),
			})
		}
		if unobserved > 0 {
			c.abstainOut(abCoverage, reasonUnmeasurablePopulation(signalLabel(rs, shareCheck.Denominator)))
		} else {
			c.abstainOut(abNoPopulation, reasonNoPopulation(rs, grain))
		}
		return c
	}

	// The interval treats every unobserved item as a miss, then as a hit, so the unread
	// mass that matters is bounded by what is not already a hit. Clamping here keeps the
	// stored counts (hits, unobserved, population) describing the stored interval — the
	// table holds hits + unobserved <= population as an invariant.
	unobserved = min(unobserved, population-hits)

	lo := float64(hits) / float64(population)
	hi := float64(hits+unobserved) / float64(population)

	// 10. the control is evaluated before the band; an unmet control abstains. A read
	// control is recorded as a check before the gate, so a cell the control failed can
	// still show the reading that failed it.
	if ctrl := findCheck(rule.Checks, rules.CheckKindBaselineControl); ctrl != nil {
		cr := rd.resolve(ctrl.Signal, rule.Stage)
		if cr.Status == StatusResolved && cr.Value != nil {
			c.Checks = append(c.Checks, &aidlcv1.CellCheck{
				Key: ctrl.Signal, Kind: checkKindControl, MeasuredValue: *cr.Value, Threshold: 0,
				Comparator: ">", Coverage: coverageProto(cr.Coverage),
			})
			if *cr.Value <= 0 {
				c.abstainOut(abControlFailed, reasonControlFailed)
				return c
			}
		} else {
			if cr.malformed {
				c.abstainOut(abMalformed, reasonMalformed)
				return c
			}
			c.abstainOut(abNoResolver, reasonControlUnreadable)
			return c
		}
	}

	// 11. band.
	var band aidlcv1.PrevalenceBand
	notEstablished := false
	switch {
	case lo > rs.BandCuts.Established:
		band = aidlcv1.PrevalenceBand_PREVALENCE_BAND_ESTABLISHED
	case lo > rs.BandCuts.Emerging && hi <= rs.BandCuts.Established:
		band = aidlcv1.PrevalenceBand_PREVALENCE_BAND_EMERGING
	case hi <= rs.BandCuts.Emerging:
		band = aidlcv1.PrevalenceBand_PREVALENCE_BAND_UNSPECIFIED
		notEstablished = true
	default:
		band = aidlcv1.PrevalenceBand_PREVALENCE_BAND_UNSPECIFIED // straddles a cut
	}

	// 11b. a zero on an instrumented-only signal whose unread mass stays at or under the
	// emerging cut: the same abstain as step 3, never a denial.
	if zeroInstrumented && notEstablished {
		c.Checks = append(c.Checks, &aidlcv1.CellCheck{
			Key: shareCheck.Signal, Kind: checkKindNumerator, MeasuredValue: 0, Comparator: "read", Coverage: coverageProto(num.Coverage),
		})
		c.abstainOut(abInstrumentedOnly, reasonInstrumentedOnly)
		return c
	}

	// 12. the agency guard, read only on the one band that denies. No v1 signal observes
	// automated-only; it stays for a hand-built RuleSet, which is exported.
	if notEstablished && numDef.Observes == rules.ObservesAutomatedOnly && *num.Value == 0 {
		c.Checks = append(c.Checks, &aidlcv1.CellCheck{
			Key: shareCheck.Signal, Kind: checkKindNumerator, MeasuredValue: 0, Comparator: "read", Coverage: coverageProto(num.Coverage),
		})
		c.abstainOut(abAutomatedOnly, reasonAutomatedOnly)
		return c
	}

	// record the share.
	// Population is the interval's denominator — the denominator reading plus its own
	// unread remainder — so hits/population reproduces lo.
	c.Share = &ShareInfo{
		Lo: round4(lo), Hi: round4(hi), Hits: int64(hits),
		Population: int64(population), Unobserved: int64(unobserved), Grain: grain,
		UncalibratedCut: band == aidlcv1.PrevalenceBand_PREVALENCE_BAND_EMERGING && rs.BandCuts.EmergingUncalibrated,
		DenCoverage:     den.Coverage,
	}
	c.Checks = append(c.Checks, &aidlcv1.CellCheck{
		Key: shareCheck.Signal, Kind: checkKindPrevalence, MeasuredValue: round4(lo), Threshold: rs.BandCuts.Emerging,
		Comparator: ">", Coverage: coverageProto(num.Coverage),
	})

	// 13. the interval straddles a cut — cannot place the band. A straddle with no
	// hit at all is the shape of an unwritten evidence column (every candidate is
	// unread), and says so rather than "sits on the line".
	if !notEstablished && band == aidlcv1.PrevalenceBand_PREVALENCE_BAND_UNSPECIFIED {
		if hits == 0 {
			c.abstainOut(abCoverage, reasonAllUnobserved)
		} else {
			c.abstainOut(abCoverage, reasonStraddle)
		}
		return c
	}

	// A secondary share (review L5, product-spec L5: the declared policy's approval
	// share beside the enforcement's usage share) composes as the lower band. Its gate
	// closing abstains the cell with that gate's sentence; its own straddle abstains;
	// a not-reached second share denies the cell whatever the first read.
	if sec := findCheck(rule.Checks, rules.CheckKindSecondaryShare); sec != nil {
		band2, share2, out := secondaryBand(rs, rd, rule, sec, c, gate, declarationsHonoured)
		if out != nil {
			return out
		}
		c.Share2 = share2
		if band2 == aidlcv1.PrevalenceBand_PREVALENCE_BAND_UNSPECIFIED {
			// The primary already read above the emerging cut (notEstablished false
			// going in) and the secondary is what denies the cell — a composer must
			// describe Share2, not Share, or it states the share that passed.
			if !notEstablished {
				c.SecondaryMiss = true
			}
			notEstablished = true
		} else if !notEstablished && bandRank(band2) < bandRank(band) {
			band = band2
		}
	}

	// 16. the numerator's distinct-people floor, after any secondary share, so a share
	// the secondary denies reads as not reached. A share below the emerging cut is a
	// measured miss whoever did the work, so only a share that would otherwise be met
	// needs the people count.
	if pf := findCheck(rule.Checks, rules.CheckKindPeopleFloor); pf != nil {
		switch {
		case num.People == nil && !notEstablished:
			c.abstainOut(abNoResolver, reasonPeopleUnknown)
			return c
		case num.People != nil:
			c.Checks = append(c.Checks, &aidlcv1.CellCheck{
				Key: shareCheck.Signal, Kind: checkKindPeopleFloor, MeasuredValue: float64(*num.People),
				Threshold: float64(pf.PeopleMin), Comparator: ">=", Coverage: coverageProto(num.Coverage),
			})
			if *num.People < pf.PeopleMin && !notEstablished {
				c.abstainOut(abPopulationFloor, reasonPeopleFloor(pf.PeopleMin))
				return c
			}
		}
	}

	// 17. validity check — attached, never a gate.
	if vc := findCheck(rule.Checks, rules.CheckKindContext); vc != nil {
		vr := rd.resolve(vc.Signal, rule.Stage)
		if vr.Status == StatusResolved && vr.Value != nil {
			c.Checks = append(c.Checks, &aidlcv1.CellCheck{
				Key: vc.Signal, Kind: checkKindValidity, MeasuredValue: *vr.Value,
				Comparator: "context", Coverage: coverageProto(vr.Coverage),
			})
		}
	}

	// 18. verdict.
	if notEstablished {
		c.State = aidlcv1.CellState_CELL_STATE_NOT_REACHED
		return c
	}
	c.State = aidlcv1.CellState_CELL_STATE_MET
	c.DecidedBy = aidlcv1.DecidedBy_DECIDED_BY_RULE
	c.Band = band
	return c
}

// secondaryBand evaluates a cell's secondary share and returns its band and the share
// itself (so a composer describing a secondary-share miss has real numbers to name,
// not the primary's — see Cell.SecondaryMiss), or a finished abstaining cell when
// the share cannot be placed. A nil share with UNSPECIFIED and a nil abstained cell
// means the share read below the emerging cut (not reached) but was still placeable.
// The widening rules are the primary share's, applied to a second (count, total) pair;
// the reading is recorded as a prevalence check on the secondary signal so the
// evidence shows both shares.
func secondaryBand(
	rs *rules.RuleSet, rd *Readings, rule rules.CellRule, sec *rules.Check, c *Cell,
	gate aidlcv1.InstrumentationGap, declarationsHonoured bool,
) (aidlcv1.PrevalenceBand, *ShareInfo, *Cell) {
	num := rd.resolveCount(sec.Signal, rule.Stage)
	den := rd.resolveCount(sec.Denominator, rule.Stage)
	for _, r := range []Reading{num, den} {
		if r.Status == StatusGapBlocked {
			c.Gap = gate
			c.abstain = abGap
			c.State = aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE
			c.setGapReason(gate, g4BlockersFor(rd, rule, declarationsHonoured), g2BlockerNoIssues)
			return 0, nil, c
		}
	}
	for _, r := range []Reading{num, den} {
		if r.Status != StatusResolved || r.Value == nil {
			c.abstainOut(abstainFor(r), reasonFor(r))
			return 0, nil, c
		}
		// A declared remainder the reading does not carry is a stale run, as on the
		// primary share.
		if r.RemainderDeclared && r.RemainderValue == nil {
			c.abstainOut(abNoResolver, reasonTermUnreadable)
			return 0, nil, c
		}
	}
	hits := int(*num.Value)
	if hits > int(*den.Value) {
		c.abstainOut(abReadingsDisagree, reasonReadingsDisagree)
		return 0, nil, c
	}
	denUnknown, numUnreadable := 0, 0
	if den.RemainderValue != nil {
		denUnknown = *den.RemainderValue
	}
	if num.RemainderValue != nil {
		numUnreadable = *num.RemainderValue
	}
	population := int(*den.Value) + denUnknown
	switch num.Coverage {
	case CoveragePartial:
		if num.BoundValue != nil {
			numUnreadable = maxInt(numUnreadable, int(math.Round(float64(population)*(1.0-*num.BoundValue))))
		} else {
			numUnreadable = maxInt(numUnreadable, maxInt(0, population-int(*num.Value)))
		}
	case CoverageDeclaredPartial, CoverageNone, CoverageUnknown:
		numUnreadable = maxInt(numUnreadable, maxInt(0, population-int(*num.Value)))
	case CoverageFull:
	}
	grain := aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES
	floor := rs.PopulationFloors.Artifacts
	if rs.Signals[sec.Denominator].Grain == aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE {
		floor = rs.PopulationFloors.People
		grain = aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE
	}
	if population == 0 {
		c.Checks = append(c.Checks, populationCheck(sec.Denominator, 0, floor, den.Coverage))
		c.abstainOut(abNoPopulation, reasonNoPopulation(rs, grain))
		return 0, nil, c
	}
	if population < floor {
		c.Checks = append(c.Checks, populationCheck(sec.Denominator, population, floor, den.Coverage))
		c.abstainOut(abPopulationFloor, reasonPopulationFloor(rs, floor, grain))
		return 0, nil, c
	}
	unobserved := min(denUnknown+numUnreadable, population-hits)
	lo := float64(hits) / float64(population)
	hi := float64(hits+unobserved) / float64(population)
	c.Checks = append(c.Checks, &aidlcv1.CellCheck{
		Key: sec.Signal, Kind: checkKindPrevalence, MeasuredValue: round4(lo), Threshold: rs.BandCuts.Emerging,
		Comparator: ">", Coverage: coverageProto(num.Coverage),
	})
	share := &ShareInfo{
		Lo: round4(lo), Hi: round4(hi), Hits: int64(hits), Population: int64(population),
		Unobserved: int64(unobserved), Grain: grain, DenCoverage: den.Coverage,
	}
	switch {
	case lo > rs.BandCuts.Established:
		return aidlcv1.PrevalenceBand_PREVALENCE_BAND_ESTABLISHED, share, nil
	case lo > rs.BandCuts.Emerging && hi <= rs.BandCuts.Established:
		return aidlcv1.PrevalenceBand_PREVALENCE_BAND_EMERGING, share, nil
	case hi <= rs.BandCuts.Emerging:
		return aidlcv1.PrevalenceBand_PREVALENCE_BAND_UNSPECIFIED, share, nil // not reached
	default:
		c.abstainOut(abCoverage, reasonStraddle)
		return 0, nil, c
	}
}

// bandRank orders the bands so the lower of two can be taken.
func bandRank(b aidlcv1.PrevalenceBand) int {
	switch b {
	case aidlcv1.PrevalenceBand_PREVALENCE_BAND_ESTABLISHED:
		return 2
	case aidlcv1.PrevalenceBand_PREVALENCE_BAND_EMERGING:
		return 1
	default:
		return 0
	}
}

// AbstainKind is the published kind of an abstaining cell; ABSTAIN_KIND_UNSPECIFIED
// for a cell that is not an abstain.
func (c *Cell) AbstainKind() aidlcv1.AbstainKind {
	if c.State != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE {
		return aidlcv1.AbstainKind_ABSTAIN_KIND_UNSPECIFIED
	}
	return c.abstain.proto()
}

func (c *Cell) abstainOut(kind abstainKind, reason string) {
	c.State = aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE
	c.abstain = kind
	c.Reason = reason
}

// setGapReason records what holds a gap-abstaining cell closed (g4 applies only to a
// G4 cell, g2 only to a G2 cell) and the reader-facing reason that names it, kept
// together so the cell's reason and the coverage block can never read different
// blockers off one cell.
func (c *Cell) setGapReason(g aidlcv1.InstrumentationGap, g4 []g4Blocker, g2 g2Blocker) {
	switch g {
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G4:
		c.g4Blockers = g4
	case aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G2:
		c.g2Blocker = g2
	default:
	}
	c.Reason = reasonForGap(g, g4, g2)
}

// reasonFor is the reason an unresolved count term gives: the malformed reason, the
// note of a reading nothing produced (as the caller's own words), or the generic one.
func reasonFor(r Reading) string {
	switch {
	case r.malformed:
		return reasonMalformed
	case r.Status == StatusNoRunRecord && noteText(r.Note) != "":
		return "Ours: " + noteText(r.Note) + "."
	default:
		return reasonTermUnreadable
	}
}

// noteText is a note as one clause: its whitespace (newlines included) collapsed to single
// spaces, and trailing spaces and periods trimmed, so the served sentence ends once.
func noteText(note string) string {
	return strings.TrimRight(strings.Join(strings.Fields(note), " "), " .")
}

// abstainFor is the abstain an unresolved reading gives: malformed, no run, or absent.
func abstainFor(r Reading) abstainKind {
	switch {
	case r.malformed:
		return abMalformed
	case r.Status == StatusNoRunRecord:
		return abNoResolver
	default:
		return abRowAbsent
	}
}

func findCheck(checks []rules.Check, kind rules.CheckKind) *rules.Check {
	for i := range checks {
		if checks[i].Kind == kind {
			return &checks[i]
		}
	}
	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

// populationCheck records the population a share could not be placed over — under its
// floor, or empty — so the abstain still shows the count that decided it.
func populationCheck(den aidlcv1.SignalKey, population, floor int, cov Coverage) *aidlcv1.CellCheck {
	return &aidlcv1.CellCheck{
		Key: den, Kind: checkKindPopulation, MeasuredValue: float64(population), Threshold: float64(floor),
		Comparator: ">=", Coverage: coverageProto(cov),
	}
}

func coverageProto(c Coverage) aidlcv1.SignalCoverage {
	switch c {
	case CoverageFull:
		return aidlcv1.SignalCoverage_SIGNAL_COVERAGE_FULL
	case CoveragePartial:
		return aidlcv1.SignalCoverage_SIGNAL_COVERAGE_PARTIAL
	case CoverageDeclaredPartial:
		return aidlcv1.SignalCoverage_SIGNAL_COVERAGE_DECLARED_PARTIAL
	case CoverageNone:
		return aidlcv1.SignalCoverage_SIGNAL_COVERAGE_NONE
	case CoverageUnknown:
		return aidlcv1.SignalCoverage_SIGNAL_COVERAGE_UNKNOWN
	default:
		return aidlcv1.SignalCoverage_SIGNAL_COVERAGE_UNSPECIFIED
	}
}
