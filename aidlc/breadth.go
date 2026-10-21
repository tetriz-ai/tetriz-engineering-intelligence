// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"

// breadthFloorPeople is breadth's people floor: a numerator under this many
// people discloses nothing more specific than not-reported, however small the org.
const breadthFloorPeople = 3

// stageBreadth computes one stage's breadth: the presence fold's stage people set intersected with the organisation's active
// engineers is the numerator — the intersection is what bounds the ratio at one,
// since a person the presence fold saw who is not an active engineer this window
// cannot inflate it — and active engineers is the denominator. UNSPECIFIED (absent,
// not a floor no one crossed) when the stage's people set is empty: a stage nobody
// was seen working in discloses nothing about how widely it is. UNSPECIFIED too when
// activeUnresolved: a denominator that could not be read discloses nothing either.
func stageBreadth(stagePeople, active PeopleSet, activeUnresolved bool) aidlcv1.MaturityBreadth {
	if len(stagePeople) == 0 || activeUnresolved {
		return aidlcv1.MaturityBreadth_MATURITY_BREADTH_UNSPECIFIED
	}
	numerator := stagePeople.intersectCount(active)
	if numerator < breadthFloorPeople {
		return aidlcv1.MaturityBreadth_MATURITY_BREADTH_NOT_REPORTED
	}
	if numerator*2 >= len(active) {
		return aidlcv1.MaturityBreadth_MATURITY_BREADTH_ORG_WIDE
	}
	return aidlcv1.MaturityBreadth_MATURITY_BREADTH_TEAM
}
