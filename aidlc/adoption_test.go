// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"testing"
	"time"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// TestResolveAdoptionState_FiveValues proves the resolution order: onboarding, then
// lapsed, then growing (including the empty-earlier-window case), then contracting,
// else steady.
func TestResolveAdoptionState_FiveValues(t *testing.T) {
	orgCreatedAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	asOf := orgCreatedAt.AddDate(0, 0, 200) // well past onboarding for every non-onboarding case

	tests := []struct {
		name            string
		last30, prior30 int
		want            aidlcv1.MaturityAdoptionState
	}{
		{"lapsed: no active engineer in the last thirty days", 0, 12, aidlcv1.MaturityAdoptionState_MATURITY_ADOPTION_STATE_LAPSED},
		{"growing: rose by more than twenty percent", 13, 10, aidlcv1.MaturityAdoptionState_MATURITY_ADOPTION_STATE_GROWING},
		{"steady: unchanged", 10, 10, aidlcv1.MaturityAdoptionState_MATURITY_ADOPTION_STATE_STEADY},
		{"contracting: fell by more than twenty percent", 7, 10, aidlcv1.MaturityAdoptionState_MATURITY_ADOPTION_STATE_CONTRACTING},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAdoptionState(orgCreatedAt, asOf, tt.last30, tt.prior30)
			if got != tt.want {
				t.Errorf("ResolveAdoptionState(last30=%d, prior30=%d) = %v, want %v", tt.last30, tt.prior30, got, tt.want)
			}
		})
	}
}

// TestResolveAdoptionState_OnboardingWinsOverEverything proves onboarding (under
// sixty days old) wins even when the counts alone would read lapsed or
// contracting.
func TestResolveAdoptionState_OnboardingWinsOverEverything(t *testing.T) {
	orgCreatedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		asOf            time.Time
		last30, prior30 int
	}{
		{"fifty-nine days old, would otherwise be lapsed", orgCreatedAt.AddDate(0, 0, 59), 0, 5},
		{"one day old, would otherwise be contracting", orgCreatedAt.AddDate(0, 0, 1), 2, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAdoptionState(orgCreatedAt, tt.asOf, tt.last30, tt.prior30)
			if got != aidlcv1.MaturityAdoptionState_MATURITY_ADOPTION_STATE_ONBOARDING {
				t.Errorf("ResolveAdoptionState() = %v, want ONBOARDING (under sixty days old wins)", got)
			}
		})
	}

	// Exactly sixty days old is NOT "under sixty days" — onboarding no longer wins.
	sixtyDaysOld := orgCreatedAt.AddDate(0, 0, 60)
	if got := ResolveAdoptionState(orgCreatedAt, sixtyDaysOld, 0, 5); got != aidlcv1.MaturityAdoptionState_MATURITY_ADOPTION_STATE_LAPSED {
		t.Errorf("ResolveAdoptionState() at exactly sixty days = %v, want LAPSED (sixty days old is not \"under sixty\")", got)
	}
}

// TestResolveAdoptionState_EmptyEarlierWindowIsGrowing proves an earlier window of
// zero with a non-empty later one reads growing, not an undefined or divide-by-zero
// state.
func TestResolveAdoptionState_EmptyEarlierWindowIsGrowing(t *testing.T) {
	orgCreatedAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	asOf := orgCreatedAt.AddDate(0, 0, 200)

	got := ResolveAdoptionState(orgCreatedAt, asOf, 4, 0)
	if got != aidlcv1.MaturityAdoptionState_MATURITY_ADOPTION_STATE_GROWING {
		t.Errorf("ResolveAdoptionState(last30=4, prior30=0) = %v, want GROWING", got)
	}
}

// TestResolveAdoptionState_TwentyPercentBoundaryIsSteady proves the exact ±20%
// bound reads STEADY, not GROWING/CONTRACTING: "rose by more than" and "fell by more
// than" are both strict.
func TestResolveAdoptionState_TwentyPercentBoundaryIsSteady(t *testing.T) {
	orgCreatedAt := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	asOf := orgCreatedAt.AddDate(0, 0, 200)

	tests := []struct {
		name            string
		last30, prior30 int
	}{
		{"a rise of exactly twenty percent", 12, 10},
		{"a fall of exactly twenty percent", 8, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAdoptionState(orgCreatedAt, asOf, tt.last30, tt.prior30)
			if got != aidlcv1.MaturityAdoptionState_MATURITY_ADOPTION_STATE_STEADY {
				t.Errorf("ResolveAdoptionState(last30=%d, prior30=%d) = %v, want STEADY (exact 20%% is not a cut side)", tt.last30, tt.prior30, got)
			}
		})
	}
}
