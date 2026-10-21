// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// TestBandBoundaries_ExactlyOnTheCuts pins both cuts as strict: a share of exactly
// one half is emerging, not established, and a people count exactly at a level's
// floor meets it.
func TestBandBoundaries_ExactlyOnTheCuts(t *testing.T) {
	rs := rules.ForVersion("v1")
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	cases := []struct {
		name      string
		hits, pop float64
		people    int
		want      string
	}{
		{"exactly half is emerging", 100, 200, 4, "met(emerging)"},
		{"exactly the people floor (2) meets it", 60, 180, 2, "met(emerging)"},
		{"one under the people floor abstains", 60, 180, 1, "abstain(population)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rd := NewReadings()
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(tc.hits), tc.people))
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(tc.pop))
			if got := vector(Score(rs, rd, OrgRead{}, nil))["code/L1"]; got != tc.want {
				t.Errorf("code/L1 = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestClosure_StopsAtAMeasuredMiss pins that downward closure never floors a level
// beneath a measured miss: code L4 is met, L3 is not reached, so L2 (an implied floor
// with no measure of its own) stays an abstain rather than being awarded under L4.
func TestClosure_StopsAtAMeasuredMiss(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(180))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, code, withPeople(resolved(150), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, withPeople(resolved(10), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(0.5))
	got := vector(Score(rules.ForVersion("v1"), rd, OrgRead{}, nil))
	if got["code/L4"] != "met(established)" || got["code/L3"] != "not_reached" {
		t.Fatalf("fixture: code/L4 = %s, code/L3 = %s; want met(established) over not_reached", got["code/L4"], got["code/L3"])
	}
	if got["code/L2"] != "abstain(gap)" {
		t.Errorf("code/L2 = %s, want abstain(gap): a measured miss above it breaks the chain", got["code/L2"])
	}
}
