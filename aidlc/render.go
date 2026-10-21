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

// signalLabels is the reader-facing name of each signal, used in a cell's deciding
// terms and evidence. It is display copy kept apart from the rules registry, whose
// population text is part of the hash-frozen ruleset and cannot be reworded without a
// new version. Wording rules: plain words, no comparison operators, the same word for
// the same idea (AI-authored, never AI-attributed).
var signalLabels = map[aidlcv1.SignalKey]string{
	aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_HARNESS_SEAT_HOLDERS:                "people with a seat who used a product-spec artifact",
	aidlcv1.SignalKey_SIGNAL_KEY_SPEC_DRAFTING_COMMAND_SEAT_HOLDERS:               "people with a seat who typed a spec-drafting command",
	aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_HARNESS_SEAT_HOLDERS:                   "people with a seat who used a design artifact",
	aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS:                      "people with a seat who used a review artifact in a session",
	aidlcv1.SignalKey_SIGNAL_KEY_CI_TOOL_SEAT_HOLDERS_BY_FUNCTION:                 "people with a seat who used a CI artifact",
	aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_HARNESS_SEAT_HOLDERS:                      "people with a seat who used a deploy artifact",
	aidlcv1.SignalKey_SIGNAL_KEY_OBSERVABILITY_TOOL_SEAT_HOLDERS:                  "people with a seat who used an observability tool in a session",
	aidlcv1.SignalKey_SIGNAL_KEY_DEBUGGING_ARTIFACT_SEAT_HOLDERS:                  "people with a seat who used a debugging artifact",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_PRD_PRS_MERGED_AFTER_REVIEW:        "AI-authored merged requirement PRs a person reviewed",
	aidlcv1.SignalKey_SIGNAL_KEY_HARNESS_ROUTED_PRD_PRS_MERGED:                    "merged requirement PRs routed through a harness artifact",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_DESIGN_DOC_PRS_MERGED:              "AI-authored merged design-document PRs",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_DESIGN_DOC_PRS_MERGED_AFTER_REVIEW: "AI-authored merged design-document PRs a person reviewed",
	aidlcv1.SignalKey_SIGNAL_KEY_HARNESS_ROUTED_DESIGN_DOC_PRS_MERGED:             "merged design-document PRs routed through a harness artifact",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS:                         "AI-authored merged feature and bugfix PRs",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED:          "AI-authored merged feature and bugfix PRs a person reviewed",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN:                         "AI-authored merged feature and bugfix PRs finished with little back-and-forth",
	aidlcv1.SignalKey_SIGNAL_KEY_BOT_PRESENT_PRS:                                  "merged PRs a review bot reviewed",
	aidlcv1.SignalKey_SIGNAL_KEY_BOT_CLEARED_NO_HUMAN:                             "merged PRs a review bot cleared with no human review",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_TOUCHING_TESTS:                   "AI-authored merged feature and bugfix PRs that touched tests",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_LOW_TURN_PRS_TOUCHING_TESTS:                   "low back-and-forth AI-authored PRs that touched tests",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_CI_PRS_MERGED:                      "AI-authored merged CI-configuration PRs",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_CI_PRS_MERGED_AFTER_REVIEW:         "AI-authored merged CI-configuration PRs a person reviewed",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_RELEASE_PRS_MERGED:                 "AI-authored merged release PRs",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_RELEASE_PRS_MERGED_AFTER_REVIEW:    "AI-authored merged release PRs a person reviewed",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MONITORING_PRS_MERGED:              "AI-authored merged monitoring-as-code PRs",
	aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS:                                     "people with a seat",
	aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_AGENT_UNIVERSE:                      "AI-authored merged requirement PRs",
	aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_SEAT_LINKED_ARTIFACTS:                  "merged design-document PRs by people with a seat",
	aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_AGENT_UNIVERSE:                         "AI-authored merged design-document PRs",
	aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS:                       "merged feature and bugfix PRs by people with a seat",
	aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE:                              "AI-authored merged feature and bugfix PRs",
	aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_RULE_APPLICABLE_PRS:                       "merged PRs whose review record could be read",
	aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_BOT_SEEN_PRS:                              "merged PRs a review bot reviewed",
	aidlcv1.SignalKey_SIGNAL_KEY_TEST_QA_SEAT_LINKED_ARTIFACTS:                    "merged feature and bugfix PRs by people with a seat",
	aidlcv1.SignalKey_SIGNAL_KEY_TEST_QA_AGENT_LOW_TURN_UNIVERSE:                  "low back-and-forth AI-authored PRs with a readable file list",
	aidlcv1.SignalKey_SIGNAL_KEY_CI_CD_SEAT_LINKED_ARTIFACTS:                      "merged CI-configuration PRs by people with a seat",
	aidlcv1.SignalKey_SIGNAL_KEY_CI_CD_AGENT_UNIVERSE:                             "AI-authored merged CI-configuration PRs",
	aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_SEAT_LINKED_ARTIFACTS:                     "merged release PRs by people with a seat",
	aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_AGENT_UNIVERSE:                            "AI-authored merged release PRs",
	aidlcv1.SignalKey_SIGNAL_KEY_OPERATE_SEAT_LINKED_ARTIFACTS:                    "merged monitoring-as-code PRs by people with a seat",
	aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE:                      "share of this stage's non-AI work with a recorded review",
	aidlcv1.SignalKey_SIGNAL_KEY_COMPLEXITY_SCORE:                                 "change-complexity score of the low back-and-forth AI PRs",

	// The policy column, the Plan stage, Deploy over branch roles, declared commands.
	aidlcv1.SignalKey_SIGNAL_KEY_ENFORCED_PRACTICE_ARTIFACTS_BY_STAGE:                 "practices your admins enforce for this stage",
	aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE:              "people with a seat the enforced practice applies to",
	aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE:       "people with a seat who used the enforced practice",
	aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS:                         "AI-authored merged PRs your review policy covers",
	aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS:                             "AI-authored merged PRs approved the way your review policy asks",
	aidlcv1.SignalKey_SIGNAL_KEY_PLANNING_PHASE_SEAT_HOLDERS:                          "people with a seat who planned work in an AI session",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_MERGED_PRS:                                        "AI-authored merged PRs",
	aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS:                                "AI-authored merged PRs planned in an AI session",
	aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS_HUMAN_REVIEWED:                 "planned AI-authored merged PRs a person approved",
	aidlcv1.SignalKey_SIGNAL_KEY_UNATTENDED_PLANNED_AI_MERGED_PRS:                     "planned AI-authored merged PRs run without a per-step human gate",
	aidlcv1.SignalKey_SIGNAL_KEY_PM_ISSUES_IN_WINDOW:                                  "issues created in your tracker",
	aidlcv1.SignalKey_SIGNAL_KEY_SHIPPED_BRANCH_SEAT_LINKED_PRS_MERGED:                "merged PRs into a release branch by people with a seat",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED:              "AI-authored merged PRs into a release branch",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED_AFTER_REVIEW: "AI-authored merged PRs into a release branch a person reviewed",
	aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_PRODUCTION_BRANCH_PRS_DEPLOYED:         "AI-authored merges into the production branch confirmed deployed",
	aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_TEST_COMMAND_SESSIONS:                       "AI sessions where your declared test command could have run",
	aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_TEST_COMMAND_GREEN_SESSIONS:                 "AI sessions where your declared test command ran and passed",
	aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_CI_COMMAND_SESSIONS:                         "AI sessions where your declared CI command could have run",
	aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_CI_COMMAND_GREEN_SESSIONS:                   "AI sessions where your declared CI command ran and passed",
}

// levelNames are the ladder's rung names, used when a term names another rung.
var levelNames = map[aidlcv1.AidlcLevel]string{
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "Assisted",
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "Conversational",
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "Supervised agentic",
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "Autonomous",
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "Orchestrated",
}

// renderTerms writes a cell's deciding terms and evidence list from what the scorer
// recorded: the share interval and the checks it evaluated. Terms are display prose a
// reader renders as written; the evidence list carries the counts the terms rest on. A
// cell the scorer never read (a judgment, a named gap) has neither. Every sentence is
// true for the state it is written under; a term is never emitted where its claim would
// contradict the cell's abstain reason.
func renderTerms(rs *rules.RuleSet, c *Cell, siblings []*Cell) ([]string, []*aidlcv1.CellEvidence) {
	var terms []string
	var ev []*aidlcv1.CellEvidence

	if c.AwardedFloor {
		if above := metRuleAbove(c, siblings); above != aidlcv1.AidlcLevel_AIDLC_LEVEL_UNSPECIFIED {
			terms = append(terms, fmt.Sprintf("Counted because this stage reaches %s, which includes this level.", levelNames[above]))
		} else {
			terms = append(terms, "Counted because a higher level on this stage is met.")
		}
	}

	var shareCheck *rules.Check
	// hidden is set when the cell reads a people share over fewer than peopleCountFloor
	// people: its terms and evidence carry the band and the universe, never a count or
	// a percentage.
	hidden := c.Share != nil && !countsShown(MoveTerms{Grain: c.Share.Grain, Population: c.Share.Population})
	if rule, ok := rs.Cells[rules.CellKey{Stage: c.Stage, Level: c.Level}]; ok {
		shareCheck = findCheck(rule.Checks, rules.CheckKindPrevalenceShare)
	}
	if c.Share != nil && shareCheck != nil {
		sh := c.Share
		numLabel := signalLabel(rs, shareCheck.Signal)
		denLabel := signalLabel(rs, shareCheck.Denominator)
		numCov := checkCoverage(c, checkKindPrevalence)
		denCov := coverageProto(sh.DenCoverage)
		lo, hi := shareBounds(sh)
		var sentence string
		if hidden {
			sentence = fmt.Sprintf("%s: %s of the %s.", capitalise(numLabel), intervalBandText(rs, lo, hi), denLabel)
		} else {
			sentence = fmt.Sprintf("%s: %d of the %d %s (%s).", capitalise(numLabel), sh.Hits, sh.Population, denLabel, intervalText(rs, lo, hi))
			if sh.Unobserved > 0 {
				sentence += fmt.Sprintf(" For %d of those, we could not tell.", sh.Unobserved)
			}
		}
		if verdict := verdictSentence(rs, c); verdict != "" {
			sentence += " " + verdict
		}
		terms = append(terms, sentence)
		if hidden {
			ev = append(ev, &aidlcv1.CellEvidence{Label: numLabel, Value: intervalBandText(rs, lo, hi), Coverage: numCov})
		} else {
			ev = append(ev,
				&aidlcv1.CellEvidence{Label: numLabel, Value: fmt.Sprintf("%d", sh.Hits), Coverage: numCov},
				&aidlcv1.CellEvidence{Label: denLabel, Value: fmt.Sprintf("%d", sh.Population), Coverage: denCov},
			)
		}
		if sh.Unobserved > 0 && !hidden {
			ev = append(ev, &aidlcv1.CellEvidence{Label: "of which we could not tell", Value: fmt.Sprintf("%d", sh.Unobserved), Coverage: numCov})
		}
	}

	for _, ck := range c.Checks {
		label := signalLabel(rs, ck.GetKey())
		if hidden && hiddenCheck(ck, shareCheck) {
			switch ck.GetKind() {
			case checkKindNumerator:
				terms = append(terms, fmt.Sprintf("%s: none recorded in the window.", capitalise(label)))
			case checkKindPeopleFloor:
				if ck.GetMeasuredValue() >= ck.GetThreshold() {
					terms = append(terms, "Enough people did this to meet the floor this level asks for.")
				} else {
					terms = append(terms, fmt.Sprintf("Too few people did this: this level asks for at least %d.", int(ck.GetThreshold())))
				}
			default:
				terms = append(terms, fmt.Sprintf("%s: too few to show a count.", capitalise(label)))
			}
			continue
		}
		if smallPeopleCount(rs, ck) {
			switch ck.GetKind() {
			case checkKindPopulation:
				terms = append(terms, fmt.Sprintf("%s: too few in the window to show a count. Needs at least %d before a share can be read.", capitalise(label), int(ck.GetThreshold())))
			case checkKindUnmeasurable:
				terms = append(terms, fmt.Sprintf("%s: none could be read, so the population is unmeasurable rather than empty.", capitalise(label)))
			}
			continue
		}
		switch ck.GetKind() {
		case checkKindPopulation:
			n := int(ck.GetMeasuredValue())
			if n == 0 {
				terms = append(terms, fmt.Sprintf("%s: none in the window, so there is nothing to read.", capitalise(label)))
			} else {
				terms = append(terms, fmt.Sprintf("%s: %d in the window. Needs at least %d before a share can be read.", capitalise(label), n, int(ck.GetThreshold())))
			}
			ev = append(ev, &aidlcv1.CellEvidence{Label: label, Value: fmt.Sprintf("%d", n), Coverage: ck.GetCoverage()})
		case checkKindUnmeasurable:
			terms = append(terms, fmt.Sprintf("%s: none could be read, so the population is unmeasurable rather than empty.", capitalise(label)))
			ev = append(ev, &aidlcv1.CellEvidence{Label: label + " that could not be read", Value: fmt.Sprintf("%d", int(ck.GetMeasuredValue())), Coverage: ck.GetCoverage()})
		case checkKindNumerator:
			terms = append(terms, fmt.Sprintf("%s: none recorded in the window.", capitalise(label)))
			ev = append(ev, &aidlcv1.CellEvidence{Label: label, Value: "0", Coverage: ck.GetCoverage()})
		case checkKindPeopleFloor:
			people, need := int(ck.GetMeasuredValue()), int(ck.GetThreshold())
			if people >= need {
				terms = append(terms, fmt.Sprintf("%d people did this, at or above the %d this level asks for.", people, need))
			} else {
				terms = append(terms, fmt.Sprintf("Needs at least %d people; %d did this.", need, people))
			}
			ev = append(ev, &aidlcv1.CellEvidence{Label: "people who did this", Value: fmt.Sprintf("%d", people), Coverage: ck.GetCoverage()})
		case checkKindControl:
			rate := ck.GetMeasuredValue()
			if c.abstain == abControlFailed {
				terms = append(terms, "No reviews are recorded on this stage's non-AI work, so a review on an AI change proves nothing here.")
			} else {
				terms = append(terms, fmt.Sprintf("Reviews are recorded on %s of this stage's non-AI work, so a recorded review is a real signal here.", pctText(rs, rate)))
			}
			ev = append(ev, &aidlcv1.CellEvidence{Label: label, Value: pctText(rs, rate), Coverage: ck.GetCoverage()})
		case checkKindValidity:
			terms = append(terms, fmt.Sprintf("%s is %s; the verdict does not use it.", capitalise(label), numberText(ck.GetMeasuredValue())))
			ev = append(ev, &aidlcv1.CellEvidence{Label: label, Value: numberText(ck.GetMeasuredValue()), Coverage: ck.GetCoverage()})
		}
	}
	return terms, ev
}

// smallPeopleCount reports whether a population or unmeasurable check counts people over
// a group smaller than peopleCountFloor, whose size the cell must not print.
// It is decided on the check, not on the cell's share, because a cell below the population
// floor abstains before any share is read and still carries the population it measured.
// A zero population names nobody and keeps its own "none in the window" sentence.
func smallPeopleCount(rs *rules.RuleSet, ck *aidlcv1.CellCheck) bool {
	switch ck.GetKind() {
	case checkKindPopulation, checkKindUnmeasurable:
	default:
		return false
	}
	n := ck.GetMeasuredValue()
	return rs.Signals[ck.GetKey()].Grain == aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE && n > 0 && n < peopleCountFloor
}

// hiddenCheck reports whether a check's measured value is a people count that a hidden
// people share would expose: the people floor, a recorded-nothing numerator, or the
// population the share is read over.
func hiddenCheck(ck *aidlcv1.CellCheck, shareCheck *rules.Check) bool {
	switch ck.GetKind() {
	case checkKindPeopleFloor, checkKindNumerator:
		return true
	case checkKindPopulation, checkKindUnmeasurable:
		return shareCheck != nil && ck.GetKey() == shareCheck.Denominator
	}
	return false
}

// intervalBandText places a share interval against the band cuts in words, carrying no
// count and no percentage for the standard cuts.
func intervalBandText(rs *rules.RuleSet, lo, hi float64) string {
	switch {
	case hi <= rs.BandCuts.Emerging:
		return "no more than " + fractionWord(rs, rs.BandCuts.Emerging)
	case lo > rs.BandCuts.Established:
		return "more than " + fractionWord(rs, rs.BandCuts.Established)
	case lo > rs.BandCuts.Emerging && hi <= rs.BandCuts.Established:
		return "between " + fractionWord(rs, rs.BandCuts.Emerging) + " and " + fractionWord(rs, rs.BandCuts.Established)
	default:
		return "not settled either way"
	}
}

// verdictSentence is the share term's closing sentence: where the reading landed
// against the two cuts, in the reader's words. Empty when the share did not decide the
// cell and nothing true can be said about it in one line.
func verdictSentence(rs *rules.RuleSet, c *Cell) string {
	switch {
	case c.AwardedFloor:
		return "" // the floor sentence above already says why the cell counts
	case c.State == aidlcv1.CellState_CELL_STATE_MET && c.Band == aidlcv1.PrevalenceBand_PREVALENCE_BAND_ESTABLISHED:
		return fmt.Sprintf("Established: more than %s of what was read.", fractionWord(rs, rs.BandCuts.Established))
	case c.State == aidlcv1.CellState_CELL_STATE_MET && c.Band == aidlcv1.PrevalenceBand_PREVALENCE_BAND_EMERGING:
		return fmt.Sprintf("Emerging: more than %s of what was read, and no more than %s.", fractionWord(rs, rs.BandCuts.Emerging), fractionWord(rs, rs.BandCuts.Established))
	case c.State == aidlcv1.CellState_CELL_STATE_NOT_REACHED:
		return fmt.Sprintf("Not reached: even if everything that could not be read counted, this stays at or below %s.", fractionWord(rs, rs.BandCuts.Emerging))
	case c.State == aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE && c.abstain == abCoverage && c.Share != nil:
		lo, hi := shareBounds(c.Share)
		cut := rs.BandCuts.Established
		if lo <= rs.BandCuts.Emerging && hi > rs.BandCuts.Emerging {
			cut = rs.BandCuts.Emerging
		}
		return fmt.Sprintf("Too much could not be read: that range crosses %s, the line this level is decided on.", fractionWord(rs, cut))
	case c.State == aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE && c.abstain == abPopulationFloor:
		return "" // the people-floor sentence carries the reason
	default:
		return ""
	}
}

// metRuleAbove returns the lowest rung above c that a rule decided MET on the same
// stage — the rung an implied floor was awarded under — or UNSPECIFIED.
func metRuleAbove(c *Cell, siblings []*Cell) aidlcv1.AidlcLevel {
	for _, lv := range ladderOrder {
		if levelOrd(lv) <= levelOrd(c.Level) {
			continue
		}
		for _, s := range siblings {
			if s.Level == lv && s.State == aidlcv1.CellState_CELL_STATE_MET && !s.AwardedFloor {
				return lv
			}
		}
	}
	return aidlcv1.AidlcLevel_AIDLC_LEVEL_UNSPECIFIED
}

// signalLabel is the display label for a signal; a key with no label falls back to the
// registry's population text, and an unregistered key to its enum name so the term is
// never empty.
func signalLabel(rs *rules.RuleSet, key aidlcv1.SignalKey) string {
	if l, ok := signalLabels[key]; ok {
		return l
	}
	if def, ok := rs.Signals[key]; ok && def.Population != "" {
		return def.Population
	}
	return key.String()
}

// checkCoverage is the coverage the named check recorded, UNSPECIFIED when absent.
func checkCoverage(c *Cell, kind string) aidlcv1.SignalCoverage {
	for _, ck := range c.Checks {
		if ck.GetKind() == kind {
			return ck.GetCoverage()
		}
	}
	return aidlcv1.SignalCoverage_SIGNAL_COVERAGE_UNSPECIFIED
}

func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// shareBounds recomputes the interval from the stored counts rather than the stored
// four-decimal bounds, so a term at a cut boundary prints the figure the verdict was
// decided on: 20,004 of 100,000 is emerging and must not print as 20%.
func shareBounds(sh *ShareInfo) (lo, hi float64) {
	if sh.Population == 0 {
		return sh.Lo, sh.Hi
	}
	return float64(sh.Hits) / float64(sh.Population), float64(sh.Hits+sh.Unobserved) / float64(sh.Population)
}

// pctText renders a rate as a percentage with the fewest decimals that keep it apart
// from 0%, 100% and the ruleset's band cuts: a rate that is not the cut never prints as
// the cut, so a term cannot say "20%" beside "more than a fifth" for 20.0004%, nor "0%"
// for a rate that passed a greater-than-zero gate. Precision grows only as far as the
// rate needs; a rate that is exactly a cut prints as that cut.
func pctText(rs *rules.RuleSet, v float64) string {
	p := v * 100
	sensitive := []float64{0, 100, rs.BandCuts.Emerging * 100, rs.BandCuts.Established * 100}
	for decimals := 0; decimals <= 8; decimals++ {
		txt := pctDigits(p, decimals)
		collides := false
		for _, sv := range sensitive {
			if p != sv && txt == pctDigits(sv, decimals) {
				collides = true
				break
			}
		}
		if !collides {
			return txt + "%"
		}
	}
	return strconv.FormatFloat(p, 'f', -1, 64) + "%"
}

// pctDigits formats a percentage to a fixed number of decimals, rounding halves away
// from zero at every precision (12.5 prints as 13, the way a reader rounds).
func pctDigits(p float64, decimals int) string {
	scale := math.Pow(10, float64(decimals))
	return strconv.FormatFloat(math.Round(p*scale)/scale, 'f', decimals, 64)
}

// intervalText renders the share interval, collapsing to one number when both ends
// print the same.
func intervalText(rs *rules.RuleSet, lo, hi float64) string {
	a, b := pctText(rs, lo), pctText(rs, hi)
	if a == b {
		return a
	}
	return a + " to " + b
}

// fractionWord names a band cut in words where the cut is a familiar fraction, and
// otherwise as a percentage carried to four decimals (trailing zeros dropped). A cut is
// declared to at most four decimals of a percent (TestBandCuts_RenderExactly pins every
// registered version), so the threshold a term states is exactly the one the scorer
// compared against: a 20.5% cut prints "20.5%", never "21%".
func fractionWord(_ *rules.RuleSet, cut float64) string {
	switch cut {
	case 0.5:
		return "half"
	case 0.2:
		return "a fifth"
	case 0.25:
		return "a quarter"
	default:
		return strconv.FormatFloat(math.Round(cut*1_000_000)/10_000, 'f', -1, 64) + "%"
	}
}

func numberText(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}
