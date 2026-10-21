// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

func peopleSet(ids ...string) PeopleSet {
	out := make(PeopleSet, len(ids))
	for _, id := range ids {
		out[id] = struct{}{}
	}
	return out
}

// TestStageBreadth_ThreeValuesAndAbsent pins the cut points: under
// three people is not_reported, three or more and under half of active
// engineers is team, three or more and half or more is org_wide, and an empty stage
// people set is absent (UNSPECIFIED) rather than a floor no one crossed.
func TestStageBreadth_ThreeValuesAndAbsent(t *testing.T) {
	tests := []struct {
		name        string
		stagePeople PeopleSet
		active      PeopleSet
		unresolved  bool
		want        aidlcv1.MaturityBreadth
	}{
		{
			name:        "two of ten is not reported",
			stagePeople: peopleSet("a", "b"),
			active:      peopleSet("a", "b", "c", "d", "e", "f", "g", "h", "i", "j"),
			want:        aidlcv1.MaturityBreadth_MATURITY_BREADTH_NOT_REPORTED,
		},
		{
			name:        "three of ten is team",
			stagePeople: peopleSet("a", "b", "c"),
			active:      peopleSet("a", "b", "c", "d", "e", "f", "g", "h", "i", "j"),
			want:        aidlcv1.MaturityBreadth_MATURITY_BREADTH_TEAM,
		},
		{
			name:        "five of ten is org wide",
			stagePeople: peopleSet("a", "b", "c", "d", "e"),
			active:      peopleSet("a", "b", "c", "d", "e", "f", "g", "h", "i", "j"),
			want:        aidlcv1.MaturityBreadth_MATURITY_BREADTH_ORG_WIDE,
		},
		{
			name:        "three of six is org wide (exactly half)",
			stagePeople: peopleSet("a", "b", "c"),
			active:      peopleSet("a", "b", "c", "d", "e", "f"),
			want:        aidlcv1.MaturityBreadth_MATURITY_BREADTH_ORG_WIDE,
		},
		{
			name:        "empty stage people set is absent",
			stagePeople: PeopleSet{},
			active:      peopleSet("a", "b", "c"),
			want:        aidlcv1.MaturityBreadth_MATURITY_BREADTH_UNSPECIFIED,
		},
		{
			name:        "nil stage people set is absent",
			stagePeople: nil,
			active:      peopleSet("a", "b", "c"),
			want:        aidlcv1.MaturityBreadth_MATURITY_BREADTH_UNSPECIFIED,
		},
		{
			name:        "an unresolved active-engineer set is absent, however many stage people",
			stagePeople: peopleSet("a", "b", "c", "d", "e"),
			active:      nil,
			unresolved:  true,
			want:        aidlcv1.MaturityBreadth_MATURITY_BREADTH_UNSPECIFIED,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stageBreadth(tt.stagePeople, tt.active, tt.unresolved); got != tt.want {
				t.Errorf("stageBreadth() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestStageBreadth_IntersectionBoundsTheRatio proves the numerator is the stage
// people set intersected with active engineers, not the raw stage set: a stage set
// carrying people outside the active set must never push the disclosed ratio above
// one, however large the stage set itself is.
func TestStageBreadth_IntersectionBoundsTheRatio(t *testing.T) {
	// Five stage people, only two of them active engineers this window: without the
	// intersection the numerator (5) would exceed the denominator (2).
	stagePeople := peopleSet("a", "b", "outsider1", "outsider2", "outsider3")
	active := peopleSet("a", "b")
	if got := stageBreadth(stagePeople, active, false); got != aidlcv1.MaturityBreadth_MATURITY_BREADTH_NOT_REPORTED {
		t.Errorf("stageBreadth() = %v, want not_reported (intersection is 2, under the floor of 3)", got)
	}

	// Six stage people, three of them active engineers out of six active engineers
	// total: the ratio reads exactly half (team's upper bound / org_wide's lower
	// bound), never inflated by the three non-active stage people.
	stagePeople2 := peopleSet("a", "b", "c", "outsider1", "outsider2", "outsider3")
	active2 := peopleSet("a", "b", "c", "d", "e", "f")
	if got := stageBreadth(stagePeople2, active2, false); got != aidlcv1.MaturityBreadth_MATURITY_BREADTH_ORG_WIDE {
		t.Errorf("stageBreadth() = %v, want org_wide (intersection is 3 of 6 active, exactly half)", got)
	}
}

// TestScore_BreadthOnStage proves Score wires StagePeople and ActiveEngineers through
// to the assembled profile's MaturityStage.breadth, and that an unset stage carries
// UNSPECIFIED.
func TestScore_BreadthOnStage(t *testing.T) {
	rs := rules.ForVersion("v1")
	rd := NewReadings()
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd.SetStagePeople(code, peopleSet("a", "b", "c", "d"))
	org := OrgRead{ActiveEngineers: peopleSet("a", "b", "c", "d", "e", "f", "g", "h")}

	res := Score(rs, rd, org, nil)

	var codeStage, techStage *aidlcv1.MaturityStage
	for _, s := range res.Profile.GetStages() {
		switch s.GetStage() {
		case code:
			codeStage = s
		case aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC:
			techStage = s
		}
	}
	if codeStage == nil || techStage == nil {
		t.Fatal("expected both code and tech_spec stages in the profile")
	}
	// 4 of 8 active engineers is exactly half -> org_wide.
	if got := codeStage.GetBreadth(); got != aidlcv1.MaturityBreadth_MATURITY_BREADTH_ORG_WIDE {
		t.Errorf("code breadth = %v, want org_wide", got)
	}
	// tech_spec never had SetStagePeople called -> absent.
	if got := techStage.GetBreadth(); got != aidlcv1.MaturityBreadth_MATURITY_BREADTH_UNSPECIFIED {
		t.Errorf("tech_spec breadth = %v, want UNSPECIFIED (absent)", got)
	}
}
