// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"sort"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// peopleSet is a distinct-author accumulator; a numerator's people-floor reads its size.
type peopleSet map[string]bool

func (s peopleSet) add(id *string) {
	if id != nil && *id != "" {
		s[*id] = true
	}
}
func newPeopleSet() peopleSet { return peopleSet{} }

// count returns the set size as a *int for a Reading's People field, nil when the
// numerator carries no author identities (a people-floor on a nil count abstains rather
// than crediting an unknowable population).
func (s peopleSet) count() *int {
	if len(s) == 0 {
		return nil
	}
	n := len(s)
	return &n
}

// countExcluding returns how many members of s are not in other — the distinct
// remainder a reading carries once the people already credited in its numerator are
// removed.
func (s peopleSet) countExcluding(other peopleSet) int {
	n := 0
	for p := range s {
		if !other[p] {
			n++
		}
	}
	return n
}

// toScore converts the fold-local accumulator to the aidlc.PeopleSet the scorer's
// breadth read consumes.
func (s peopleSet) toScore() aidlc.PeopleSet {
	out := make(aidlc.PeopleSet, len(s))
	for p := range s {
		out[p] = struct{}{}
	}
	return out
}

// mcStageAcc accumulates one path- or branch-defined stage's merged-change counters
// over the PR pass. The AI-authored population counts every author; the seat-linked
// counts only the roster's.
type mcStageAcc struct {
	seatLinked         int
	seatLinkedPeople   peopleSet
	aiUniverse         int
	aiUniversePeople   peopleSet
	aiSeatLinked       int
	aiSeatLinkedPeople peopleSet
	aiReviewed         int
	aiReviewedPeople   peopleSet
	aiRevUnknown       int // AI PRs whose reviewer list was not captured — the after-review numerator cannot be read over them
	aiRouted           int
	aiRoutedPeople     peopleSet
	aiUnjoined         int // PRs with no PRKey: AI authorship unread, so the agent universe's remainder
	aiSeatUnjoined     int // the seat-linked ones: the seat-linked AI numerator's remainder
	nonAiReviewed      int // control numerator: non-AI PRs with a human review
	nonAiRevKnown      int // control denominator: non-AI PRs whose reviewer list was captured
}

func newMCStageAcc() *mcStageAcc {
	return &mcStageAcc{
		seatLinkedPeople: newPeopleSet(), aiUniversePeople: newPeopleSet(),
		aiSeatLinkedPeople: newPeopleSet(), aiReviewedPeople: newPeopleSet(),
		aiRoutedPeople: newPeopleSet(),
	}
}

// prFacts is one PR's derived per-PR facts, computed once and read by every stage.
type prFacts struct {
	pr      *MergedPR
	ai      bool
	routed  bool
	lowTurn bool
	// joinable is whether the PR has a PRKey; one with none joins nothing, so its AI
	// authorship, routing, turns and reviews are all unread.
	joinable bool
	// turnsKnown is whether HumanTurns carries a readable count for the PR (one or
	// more); a missing key or a zero is unread on the low-turn levels, never counted as
	// high-turn.
	turnsKnown bool
	delivery   bool
	wtKnown    bool
	tests      bool
	// reviewed is whether the PR carries a confirmed human review: a submitted review
	// that the captured review records do not show as bot-only. A bot-only review is
	// not human evidence.
	reviewed bool
}

// foldMergedChange emits every merged-change, control and validity reading, and returns
// the adoption cohort the adoption_reach bottleneck reads. pathCov is the org
// path-coverage fraction, the coverage bound the path-defined numerators widen by.
func foldMergedChange(rd *aidlc.Readings, prs []MergedPR, attr Attribution, turns map[string]int, facts map[string]PRReviewFacts, pathCov float64) aidlc.OrgRead {
	pathStageAccs := map[aidlcv1.AidlcStage]*mcStageAcc{
		aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC: newMCStageAcc(),
		aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC:    newMCStageAcc(),
		aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD:        newMCStageAcc(),
		aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY:       newMCStageAcc(),
		aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE:      newMCStageAcc(),
	}

	// code / test-QA counters (delivery population, work-type defined).
	var (
		codeSeatLinked         int
		codeSeatLinkedPeople   = newPeopleSet()
		codeWorkTypeUnknown    int // seat-linked PRs with an unknown work type — the code denominator's remainder
		codeAiSeatLinked       int
		codeAiSeatLinkedPeople = newPeopleSet()
		codeAiUniverse         int
		codeAiUniversePeople   = newPeopleSet()
		codeAiReviewed         int
		codeAiReviewedPeople   = newPeopleSet()
		codeAiRevUnknown       int
		codeAiLowTurn          int
		codeAiLowTurnPeople    = newPeopleSet()
		codeAiTurnsUnknown     int // AI delivery PRs with no readable turn count — the low-turn numerator's remainder
		codeUnjoined           int // delivery PRs with no PRKey — the AI delivery universe's remainder
		codeAiWorkTypeUnknown  int // AI PRs with no work type — could be AI delivery, the universe's remainder
		codeSeatUnjoined       int // the seat-linked ones — the code L1 numerator's remainder
		testTouchUnjoined      int // delivery PRs with no PRKey touching a test — the test L2 numerator's remainder
		codeNonAiReviewed      int
		codeNonAiRevKnown      int
		complexityValues       []float64
		complexityEligible     int // delivery ∧ AI ∧ low-turn PRs (complexity_coverage denominator)

		testAiTouching           int
		testAiTouchingPeople     = newPeopleSet()
		testAgentLowTurnUniverse int
		testLowTurnUnknown       int // AI PRs with a file list that could be low-turn delivery (turns or work type unread) — the universe's remainder
		testAiLowTurnTouching    int
		testAiLowTurnTouchPeople = newPeopleSet()

		// adoption cohort (org-level).
		mergers        = newPeopleSet()
		aiMergers      = newPeopleSet()
		unreadMergers  = newPeopleSet() // authors of a PR with no PRKey: AI authorship unread
		codeStageTotal int

		// seat-linked PRs with no file list: unplaceable into the path-defined stages
		// (product/tech/CI/operate), so they widen those stages' seat-linked
		// denominators as an unknown remainder rather than confidently overstating
		// adoption (branch/title-defined deploy is unaffected — it needs no file list).
		noFileListSeated int
	)

	for i := range prs {
		f := deriveFacts(&prs[i], attr, turns, facts)
		pr := f.pr
		mergers.add(pr.AuthorIdentityID)
		if f.ai {
			aiMergers.add(pr.AuthorIdentityID)
		}
		if !f.joinable {
			unreadMergers.add(pr.AuthorIdentityID)
		}
		if pr.AuthorSeatLinked && !pr.HasFileList {
			noFileListSeated++
		}

		// path/branch-defined stages.
		for stage, hit := range pathStages(pr.Paths, pr.BaseBranch, pr.Title) {
			if !hit {
				continue
			}
			accStage(pathStageAccs[stage], f)
		}

		// code stage: delivery work type.
		if f.delivery {
			codeStageTotal++
			if pr.AuthorSeatLinked {
				codeSeatLinked++
				codeSeatLinkedPeople.add(pr.AuthorIdentityID)
			}
			if !f.joinable {
				// No PRKey: whether AI authored it is unread, so it is a remainder on every
				// AI-defined count it could belong to, and never in the non-AI control.
				codeUnjoined++
				if pr.AuthorSeatLinked {
					codeSeatUnjoined++
				}
				if f.tests {
					testTouchUnjoined++
				}
				if pr.HasFileList {
					testLowTurnUnknown++
				}
			} else if f.ai {
				codeAiUniverse++
				codeAiUniversePeople.add(pr.AuthorIdentityID)
				if pr.AuthorSeatLinked {
					codeAiSeatLinked++
					codeAiSeatLinkedPeople.add(pr.AuthorIdentityID)
				}
				if f.reviewed {
					codeAiReviewed++
					codeAiReviewedPeople.add(pr.AuthorIdentityID)
				} else if !pr.ReviewersKnown {
					codeAiRevUnknown++
				}
				if !f.turnsKnown {
					codeAiTurnsUnknown++
					if pr.HasFileList {
						testLowTurnUnknown++
					}
				}
				if f.lowTurn {
					codeAiLowTurn++
					codeAiLowTurnPeople.add(pr.AuthorIdentityID)
					complexityEligible++
					if pr.ComplexityScore != nil {
						complexityValues = append(complexityValues, *pr.ComplexityScore)
					}
					if pr.HasFileList {
						testAgentLowTurnUniverse++
						if f.tests {
							testAiLowTurnTouching++
							testAiLowTurnTouchPeople.add(pr.AuthorIdentityID)
						}
					}
				}
				if f.tests {
					testAiTouching++
					testAiTouchingPeople.add(pr.AuthorIdentityID)
				}
			} else { // non-AI delivery: the code baseline control
				if pr.ReviewersKnown {
					codeNonAiRevKnown++
					if f.reviewed {
						codeNonAiReviewed++
					}
				}
			}
		} else if !f.wtKnown {
			// An unknown work type could be delivery: it widens every population delivery
			// defines, code's and test & QA's alike.
			if pr.AuthorSeatLinked {
				codeWorkTypeUnknown++
			}
			if f.ai {
				codeAiWorkTypeUnknown++ // could be AI delivery
				if pr.HasFileList {
					testLowTurnUnknown++ // could be low-turn AI delivery with a file list
				}
			}
		}
	}

	bp := &pathCov
	// ---- path/branch stages ----
	emitPathStage(rd, aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC, pathStageAccs, bp, noFileListSeated,
		signalPair{
			after:         aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_PRD_PRS_MERGED_AFTER_REVIEW,
			agentUniverse: aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_AGENT_UNIVERSE,
			routed:        aidlcv1.SignalKey_SIGNAL_KEY_HARNESS_ROUTED_PRD_PRS_MERGED,
		})
	emitPathStage(rd, aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC, pathStageAccs, bp, noFileListSeated,
		signalPair{
			seatLinkedNum: aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_DESIGN_DOC_PRS_MERGED,
			seatLinkedDen: aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_SEAT_LINKED_ARTIFACTS,
			after:         aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_DESIGN_DOC_PRS_MERGED_AFTER_REVIEW,
			agentUniverse: aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_AGENT_UNIVERSE,
			routed:        aidlcv1.SignalKey_SIGNAL_KEY_HARNESS_ROUTED_DESIGN_DOC_PRS_MERGED,
		})
	emitPathStage(rd, aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD, pathStageAccs, bp, noFileListSeated,
		signalPair{
			seatLinkedNum: aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_CI_PRS_MERGED,
			seatLinkedDen: aidlcv1.SignalKey_SIGNAL_KEY_CI_CD_SEAT_LINKED_ARTIFACTS,
			after:         aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_CI_PRS_MERGED_AFTER_REVIEW,
			agentUniverse: aidlcv1.SignalKey_SIGNAL_KEY_CI_CD_AGENT_UNIVERSE,
		})
	// deploy is branch/title defined — no path-coverage bound.
	emitPathStage(rd, aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY, pathStageAccs, nil, noFileListSeated,
		signalPair{
			seatLinkedNum: aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_RELEASE_PRS_MERGED,
			seatLinkedDen: aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_SEAT_LINKED_ARTIFACTS,
			after:         aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_RELEASE_PRS_MERGED_AFTER_REVIEW,
			agentUniverse: aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_AGENT_UNIVERSE,
		})
	emitPathStage(rd, aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE, pathStageAccs, bp, noFileListSeated,
		signalPair{
			seatLinkedNum: aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MONITORING_PRS_MERGED,
			seatLinkedDen: aidlcv1.SignalKey_SIGNAL_KEY_OPERATE_SEAT_LINKED_ARTIFACTS,
		})

	// ---- code stage ----
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code,
		withRemainder(numeratorReading(float64(codeAiSeatLinked), codeAiSeatLinkedPeople.count(), bp), codeSeatUnjoined))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code,
		denominatorReading(float64(codeSeatLinked), optRemainder(codeWorkTypeUnknown)))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code,
		denominatorReading(float64(codeAiUniverse), optRemainder(codeUnjoined+codeAiWorkTypeUnknown)))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code,
		withRemainder(numeratorReading(float64(codeAiReviewed), codeAiReviewedPeople.count(), nil), codeAiRevUnknown))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, code,
		withRemainder(numeratorReading(float64(codeAiLowTurn), codeAiLowTurnPeople.count(), nil), codeAiTurnsUnknown))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_COMPLEXITY_SCORE, code,
		complexityReading(complexityValues, complexityEligible))

	// ---- test & QA (delivery population, test-touch numerator) ----
	testQA := aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_TOUCHING_TESTS, testQA,
		withRemainder(numeratorReading(float64(testAiTouching), testAiTouchingPeople.count(), bp), testTouchUnjoined))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_TEST_QA_SEAT_LINKED_ARTIFACTS, testQA,
		denominatorReading(float64(codeSeatLinked), optRemainder(codeWorkTypeUnknown)))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_LOW_TURN_PRS_TOUCHING_TESTS, testQA,
		numeratorReading(float64(testAiLowTurnTouching), testAiLowTurnTouchPeople.count(), bp))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_TEST_QA_AGENT_LOW_TURN_UNIVERSE, testQA,
		denominatorReading(float64(testAgentLowTurnUniverse), optRemainder(testLowTurnUnknown)))

	// ---- baseline review control, per stage that reads it ----
	setControl(rd, aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC, pathStageAccs)
	setControl(rd, aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC, pathStageAccs)
	setControl(rd, aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD, pathStageAccs)
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code,
		controlReading(codeNonAiReviewed, codeNonAiRevKnown))

	// An author with an unread PR and no joined AI PR could be either side of the cohort,
	// so they are in neither count.
	unreadOnly := unreadMergers.countExcluding(aiMergers)
	cohort := len(mergers) - unreadOnly
	return aidlc.OrgRead{
		HasAdoptionCohort:         cohort > 0,
		AdoptionDenominatorPeople: cohort,
		AdoptionPassivePeople:     cohort - len(aiMergers),
	}
}

func deriveFacts(pr *MergedPR, attr Attribution, turns map[string]int, facts map[string]PRReviewFacts) prFacts {
	_, ai := attr.AuthoredPRKeys[pr.PRKey]
	_, routed := attr.RoutedPRKeys[pr.PRKey]
	t := turns[pr.PRKey]
	turnsKnown := t > 0 // a missing key and a zero are both "no readable count"
	return prFacts{
		pr: pr, ai: ai, routed: routed, joinable: pr.PRKey != "",
		lowTurn:    turnsKnown && t >= 1 && t <= 4,
		turnsKnown: turnsKnown,
		delivery:   isDelivery(pr.WorkType),
		wtKnown:    isWorkTypeKnown(pr.WorkType),
		tests:      touchesTests(pr.Paths),
		reviewed:   humanReviewed(pr, facts),
	}
}

// humanReviewed reports whether a PR carries a confirmed human review: a submitted
// review in the reviewer list that the captured review records do not show as
// bot-only. The records are the authoritative bot signal (the reviewer list carries
// none), so a captured set with no human review demotes the PR from human-reviewed
// even when the list shows a submitted review; with no captured records the list's
// submitted-review flag stands.
func humanReviewed(pr *MergedPR, facts map[string]PRReviewFacts) bool {
	if !pr.SubmittedReview {
		return false
	}
	if f, ok := facts[pr.PRKey]; ok && f.Captured && f.HumanRows == 0 {
		return false // captured review set is bot-only (or has no human row) — not human evidence
	}
	return true
}

func accStage(a *mcStageAcc, f prFacts) {
	pr := f.pr
	if pr.AuthorSeatLinked {
		a.seatLinked++
		a.seatLinkedPeople.add(pr.AuthorIdentityID)
	}
	if !f.joinable {
		a.aiUnjoined++
		if pr.AuthorSeatLinked {
			a.aiSeatUnjoined++
		}
		return
	}
	if f.ai {
		a.aiUniverse++
		a.aiUniversePeople.add(pr.AuthorIdentityID)
		if pr.AuthorSeatLinked {
			a.aiSeatLinked++
			a.aiSeatLinkedPeople.add(pr.AuthorIdentityID)
		}
		if f.reviewed {
			a.aiReviewed++
			a.aiReviewedPeople.add(pr.AuthorIdentityID)
		} else if !pr.ReviewersKnown {
			a.aiRevUnknown++
		}
		if f.routed {
			a.aiRouted++
			a.aiRoutedPeople.add(pr.AuthorIdentityID)
		}
	} else if pr.ReviewersKnown { // non-AI baseline control
		a.nonAiRevKnown++
		if f.reviewed {
			a.nonAiReviewed++
		}
	}
}

// signalPair names the signal keys a path/branch stage emits; a zero key means the
// stage does not carry that rung (product spec has no seat-linked L2, only an after-
// review L3 and a routed L4).
type signalPair struct {
	seatLinkedNum aidlcv1.SignalKey
	seatLinkedDen aidlcv1.SignalKey
	after         aidlcv1.SignalKey
	agentUniverse aidlcv1.SignalKey
	routed        aidlcv1.SignalKey
}

func emitPathStage(rd *aidlc.Readings, stage aidlcv1.AidlcStage, accs map[aidlcv1.AidlcStage]*mcStageAcc, bound *float64, noFileListSeated int, s signalPair) {
	a := accs[stage]
	if s.seatLinkedNum != aidlcv1.SignalKey_SIGNAL_KEY_UNSPECIFIED {
		rd.Set(s.seatLinkedNum, stage, withRemainder(numeratorReading(float64(a.aiSeatLinked), a.aiSeatLinkedPeople.count(), bound), a.aiSeatUnjoined))
	}
	if s.seatLinkedDen != aidlcv1.SignalKey_SIGNAL_KEY_UNSPECIFIED {
		// A path-defined stage (bound != nil) reads file paths, so a seat-linked PR with no
		// file list could belong to it but was never placed — carry it as the
		// denominator's unknown remainder. Deploy (bound == nil) is branch/title-defined
		// and needs no file list, so its denominator carries no such remainder.
		var denRemainder *int
		if bound != nil {
			denRemainder = optRemainder(noFileListSeated)
		}
		rd.Set(s.seatLinkedDen, stage, denominatorReading(float64(a.seatLinked), denRemainder))
	}
	if s.after != aidlcv1.SignalKey_SIGNAL_KEY_UNSPECIFIED {
		rd.Set(s.after, stage, withRemainder(numeratorReading(float64(a.aiReviewed), a.aiReviewedPeople.count(), bound), a.aiRevUnknown))
	}
	if s.agentUniverse != aidlcv1.SignalKey_SIGNAL_KEY_UNSPECIFIED {
		rd.Set(s.agentUniverse, stage, denominatorReading(float64(a.aiUniverse), optRemainder(a.aiUnjoined)))
	}
	if s.routed != aidlcv1.SignalKey_SIGNAL_KEY_UNSPECIFIED {
		rd.Set(s.routed, stage, declaredPartialReading(float64(a.aiRouted), a.aiRoutedPeople.count()))
	}
}

func setControl(rd *aidlc.Readings, stage aidlcv1.AidlcStage, accs map[aidlcv1.AidlcStage]*mcStageAcc) {
	a := accs[stage]
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, stage, controlReading(a.nonAiReviewed, a.nonAiRevKnown))
}

// controlReading is the non-agent baseline review rate. A zero known-population means
// the control could not be read (abstain as a gap); otherwise the rate, which a zero
// value turns into a control-failed abstain in the scorer (review is not a live
// practice on this stage's non-agent work, so its absence on the agent path is
// uninformative).
func controlReading(reviewed, known int) aidlc.Reading {
	if known == 0 {
		return noRun("no non-agent work with a captured reviewer list to baseline against")
	}
	return aidlc.Reading{Status: aidlc.StatusResolved, Value: f64(float64(reviewed) / float64(known)), Coverage: aidlc.CoverageFull}
}

// complexityReading is the code-L4 validity context: the median complexity of the
// low-turn AI delivery PRs, bounded by how many of them carried a complexity score.
// It is attached, never a gate; an empty eligible population is a no-run (nothing to
// validate).
func complexityReading(values []float64, eligible int) aidlc.Reading {
	if eligible == 0 || len(values) == 0 {
		return noRun("no low-turn AI delivery PRs to read complexity over")
	}
	cov := float64(len(values)) / float64(eligible)
	return aidlc.Reading{Status: aidlc.StatusResolved, Value: f64(median(values)), Coverage: coverageFor(cov)}
}

func median(v []float64) float64 {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// withRemainder attaches a declared numerator remainder (items the numerator predicate
// could not be read over — here, AI PRs whose reviewer list was uncaptured).
func withRemainder(r aidlc.Reading, remainder int) aidlc.Reading {
	if remainder > 0 {
		r.RemainderDeclared = true
		r.RemainderValue = iptr(remainder)
	}
	return r
}

func optRemainder(n int) *int {
	if n <= 0 {
		return nil
	}
	return iptr(n)
}
