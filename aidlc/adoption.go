// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"time"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// onboardingWindowDays and adoptionChangeThreshold are the adoption-state bars:
// uniform across every organisation, and outside the hashed ruleset because they
// describe the deployment, not a level.
const (
	onboardingWindowDays    = 60
	adoptionChangeThreshold = 0.20
)

// ResolveAdoptionState reads whether the deployment is onboarding, growing, steady,
// contracting or lapsed, from the active-engineer counts in the last thirty days of
// the window (last30) and the thirty days before it (prior30), against the
// organisation's age as of asOf. Score leaves MaturityProfile.AdoptionState unset; set it from this.
//
// Resolution order: onboarding wins over every other reading, because a young
// organisation's ratio is not yet meaningful; then lapsed, because a zero last-thirty
// count has no meaningful ratio; then growing, including an empty earlier window
// (people now and none before is growth by any reading); then contracting; else
// steady. A change of exactly twenty percent reads steady: "more than" is strict in
// both directions.
func ResolveAdoptionState(orgCreatedAt, asOf time.Time, last30, prior30 int) aidlcv1.MaturityAdoptionState {
	if asOf.Sub(orgCreatedAt) < onboardingWindowDays*24*time.Hour {
		return aidlcv1.MaturityAdoptionState_MATURITY_ADOPTION_STATE_ONBOARDING
	}
	if last30 == 0 {
		return aidlcv1.MaturityAdoptionState_MATURITY_ADOPTION_STATE_LAPSED
	}
	if prior30 == 0 {
		return aidlcv1.MaturityAdoptionState_MATURITY_ADOPTION_STATE_GROWING
	}
	change := float64(last30-prior30) / float64(prior30)
	switch {
	case change > adoptionChangeThreshold:
		return aidlcv1.MaturityAdoptionState_MATURITY_ADOPTION_STATE_GROWING
	case change < -adoptionChangeThreshold:
		return aidlcv1.MaturityAdoptionState_MATURITY_ADOPTION_STATE_CONTRACTING
	default:
		return aidlcv1.MaturityAdoptionState_MATURITY_ADOPTION_STATE_STEADY
	}
}
