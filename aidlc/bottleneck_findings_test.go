// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// TestReviewReachBottleneck_BoundedByUnreadReviewers proves that the review_reach
// bottleneck derives from the reviewed-share interval, not the raw ratio. When some AI
// PRs' reviewer lists were uncaptured, the reviewed count is only a lower bound, so the
// finding fires only when even the upper bound (crediting every unread PR as reviewed)
// stays below the threshold, so an org that merely lacks reviewer data does not read as
// an unreviewed one.
func TestReviewReachBottleneck_BoundedByUnreadReviewers(t *testing.T) {
	rs := rules.ForVersion("v1")
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE

	// a = 30 AI-attributed merged PRs (>= the artifact floor); r = 5 confirmed reviewed.
	fired := func(reviewed float64, unread int) bool {
		rd := NewReadings()
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(30), 6))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(60))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code,
			withPeople(withNumRemainder(resolved(reviewed), unread), 6))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(30))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(0.6))
		res := Score(rs, rd, OrgRead{}, nil)
		for _, b := range res.Bottlenecks {
			if b.ID == "review_reach" {
				return true
			}
		}
		return false
	}

	// raw ratio 5/30 = 0.167 < 0.5 in every row below; only the upper bound decides.
	if !fired(5, 0) { // upper bound 5/30 = 0.167 < 0.5 -> fires
		t.Errorf("review_reach should fire when the upper bound (0.167) is below the threshold")
	}
	if fired(5, 10) { // upper bound (5+10)/30 = 0.50, not < 0.5 -> no fire
		t.Errorf("review_reach should not fire at an upper bound of exactly 0.50")
	}
	if fired(5, 25) { // upper bound (5+25)/30 = 1.0 -> no fire, despite the raw 0.167
		t.Errorf("review_reach should not fire when unread reviewers lift the upper bound to 1.0")
	}
}
