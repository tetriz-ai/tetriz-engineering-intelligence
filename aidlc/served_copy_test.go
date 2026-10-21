// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"strings"
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// TestServedCopy_IsCallerNeutral pins that the reasons and the coverage lines describe
// only what the measurement reads, and say nothing about schedule, data source or where
// to act.
func TestServedCopy_IsCallerNeutral(t *testing.T) {
	var sentences []string
	for g := range aidlcv1.InstrumentationGap_name {
		gap := aidlcv1.InstrumentationGap(g)
		for _, b := range []g4Blocker{g4BlockerNotHonoured, g4BlockerUndeclared, g4BlockerUnenforced, g4BlockerUninstrumented} {
			for _, also := range [][]g4Blocker{{b}, {b, g4BlockerUndeclared}, {b, g4BlockerNotHonoured}} {
				sentences = append(sentences, reasonForGap(gap, also, g2BlockerNotMeasured), reasonForGap(gap, also, g2BlockerNoIssues))
			}
		}
		if s, ok := oursGapSentence(gap); ok {
			sentences = append(sentences, s)
		}
	}
	rs := rules.ForVersion("v1")
	sentences = append(sentences,
		reasonTermUnreadable, reasonMalformed, reasonJudgment, reasonImpliedFloorPending, reasonInstrumentedOnly,
		reasonReadingsDisagree, reasonControlFailed, reasonControlUnreadable, reasonAutomatedOnly,
		reasonAllUnobserved, reasonStraddle, reasonPeopleUnknown, reasonRowLevel,
		l5DisclosureSentence, oursG2NotMeasuredSentence, oursG4NotHonouredSentence,
		oursG4UninstrumentedSentence, theirsG4UndeclaredSentence,
		theirsNoPopulationSentence(aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY, rs.WindowDays),
		theirsG4UnenforcedSentence([]aidlcv1.AidlcStage{aidlcv1.AidlcStage_AIDLC_STAGE_CODE}),
	)
	banned := []string{"schedule", "source", "settings", "not yet"}
	for _, s := range sentences {
		low := strings.ToLower(s)
		for _, phrase := range banned {
			if strings.Contains(low, phrase) {
				t.Errorf("served sentence carries %q: %q", phrase, s)
			}
		}
	}
}
