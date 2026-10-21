// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// presenceStageSignal maps each presence stage to the seat-holder numerator it emits.
// Only these six stages carry a presence level: code and test & QA read merged changes,
// and plan reads planning sessions (foldPlan).
var presenceStageSignal = map[aidlcv1.AidlcStage]aidlcv1.SignalKey{
	aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC: aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_HARNESS_SEAT_HOLDERS,
	aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC:    aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_HARNESS_SEAT_HOLDERS,
	aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW:       aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS,
	aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD:        aidlcv1.SignalKey_SIGNAL_KEY_CI_TOOL_SEAT_HOLDERS_BY_FUNCTION,
	aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY:       aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_HARNESS_SEAT_HOLDERS,
	aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE:      aidlcv1.SignalKey_SIGNAL_KEY_DEBUGGING_ARTIFACT_SEAT_HOLDERS,
}

// foldPresence emits the seat-roster denominator and the presence-level numerators:
// the distinct people who invoked an authored-practice artifact serving each stage.
// seatCount is the roster size (the SEAT_HOLDERS denominator). A zero numerator is not
// a denial: the scorer abstains on a presence numerator that only ever sees an
// instrumented artifact.
func foldPresence(rd *aidlc.Readings, seatCount int, invs []HarnessInvocation, stageMap map[string]aidlcv1.AidlcStage) {
	// SEAT_HOLDERS is org-level (UNSPECIFIED stage), people grain.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED,
		numeratorReading(float64(seatCount), iptr(seatCount), nil))

	// distinct people per stage for authored-practice artifacts, plus the
	// command-typed subset for the spec-drafting-command level. unreadableHolders is the
	// distinct set of people who invoked a practice artifact whose stage is unknown
	// (absent from the map): they belong to some stage we cannot name, so each presence
	// level carries them as an unknown remainder over the same seat denominator rather
	// than dropping them, which widens the interval instead of reading as a confident
	// share. An artifact mapped to UNSPECIFIED (serves no stage) is a read answer, so
	// its invokers are neither hits nor unknowns.
	byStage := map[aidlcv1.AidlcStage]peopleSet{}
	commandProductSpec := newPeopleSet()
	unreadableHolders := newPeopleSet()
	for _, inv := range invs {
		if !isPractice(inv.Type) {
			continue // ambient installed configuration never reaches a rung
		}
		stage, ok := stageMap[inv.ArtifactID]
		if !ok {
			unreadableHolders[inv.PersonID] = true // no readable stage — a bounded unknown, not a drop
			continue
		}
		if stage == aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED {
			continue // judged as serving no stage: neither a hit nor an unknown
		}
		if byStage[stage] == nil {
			byStage[stage] = newPeopleSet()
		}
		p := inv.PersonID
		byStage[stage][p] = true
		if stage == aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC &&
			inv.Type == aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_COMMAND {
			commandProductSpec[p] = true
		}
	}

	for stage, signal := range presenceStageSignal {
		n := len(byStage[stage])
		// The stage-specific remainder is the unreadable holders not already counted in
		// this stage's numerator (a person credited here is not also an unknown here).
		remainder := unreadableHolders.countExcluding(byStage[stage])
		rd.Set(signal, stage, withRemainder(numeratorReading(float64(n), iptr(n), nil), remainder))
		// Breadth's raw material: the stage's distinct people set, kept beside the count
		// above rather than collapsed to it.
		rd.SetStagePeople(stage, byStage[stage].toScore())
	}

	// The spec-drafting-command level: command telemetry is a declared subset, so it is
	// a declared-partial read that abstains rather than deciding.
	nc := len(commandProductSpec)
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SPEC_DRAFTING_COMMAND_SEAT_HOLDERS,
		aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC, declaredPartialReading(float64(nc), iptr(nc)))
}
