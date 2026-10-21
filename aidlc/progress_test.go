// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"strings"
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// wantProgress is the progress record a constraint must carry.
type wantProgress struct {
	currentShare, targetShare float64
	counts                    *struct{ current, population, unobserved, itemsToTarget int64 }
}

// TestAssembleProfile_ConstraintStageLevelAndProgress holds every constraint kind to
// its stage, level and progress record: the line it is measured against, the distance
// to it, adoption reach's better-side direction, the rule that a small people roster
// gets no record,
// and a nil record for a constraint that rests on no share.
func TestAssembleProfile_ConstraintStageLevelAndProgress(t *testing.T) {
	rs := rules.ForVersion("v1")
	mc := aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES
	pp := aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE
	unspecStage := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	unspecLevel := aidlcv1.AidlcLevel_AIDLC_LEVEL_UNSPECIFIED
	code, review, plan, deploy := aidlcv1.AidlcStage_AIDLC_STAGE_CODE, aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW,
		aidlcv1.AidlcStage_AIDLC_STAGE_PLAN, aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY
	l1, l4, l5 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L1, aidlcv1.AidlcLevel_AIDLC_LEVEL_L4, aidlcv1.AidlcLevel_AIDLC_LEVEL_L5
	counts := func(current, population, unobserved, items int64) *struct{ current, population, unobserved, itemsToTarget int64 } {
		return &struct{ current, population, unobserved, itemsToTarget int64 }{current, population, unobserved, items}
	}

	tests := []struct {
		name      string
		b         Bottleneck
		wantStage aidlcv1.AidlcStage
		wantLevel aidlcv1.AidlcLevel
		want      *wantProgress // nil: no progress record
	}{
		{
			name:      "level fell short: Code at Autonomous, 28 of 172, nine more to pass one in five",
			b:         Bottleneck{ID: "rung_code_L4", Stage: code, Level: l4, Terms: &MoveTerms{Hits: 28, Population: 172, Grain: mc}},
			wantStage: code, wantLevel: l4,
			want: &wantProgress{currentShare: 28.0 / 172.0, targetShare: 0.2, counts: counts(28, 172, 0, 9)},
		},
		{
			name:      "level fell short: Review at Autonomous, 40 of 438, sixty more",
			b:         Bottleneck{ID: "rung_review_L4", Stage: review, Level: l4, Terms: &MoveTerms{Hits: 40, Population: 438, Grain: mc}},
			wantStage: review, wantLevel: l4,
			want: &wantProgress{currentShare: 40.0 / 438.0, targetShare: 0.2, counts: counts(40, 438, 0, 60)},
		},
		{
			name:      "level fell short over a people roster of 20 or more keeps its counts and a fixed roster",
			b:         Bottleneck{ID: "rung_plan_L5", Stage: plan, Level: l5, Terms: &MoveTerms{Hits: 3, Population: 40, Grain: pp}},
			wantStage: plan, wantLevel: l5,
			want: &wantProgress{currentShare: 3.0 / 40.0, targetShare: 0.2, counts: counts(3, 40, 0, 6)},
		},
		{
			name:      "level fell short over a people roster under 20 carries no progress record",
			b:         Bottleneck{ID: "rung_plan_L5", Stage: plan, Level: l5, Terms: &MoveTerms{Hits: 2, Population: 19, Grain: pp}},
			wantStage: plan, wantLevel: l5,
		},
		{
			name:      "the people floor is 20: a roster of exactly 20 keeps its counts",
			b:         Bottleneck{ID: "rung_plan_L5", Stage: plan, Level: l5, Terms: &MoveTerms{Hits: 2, Population: 20, Grain: pp}},
			wantStage: plan, wantLevel: l5,
			want: &wantProgress{currentShare: 2.0 / 20.0, targetShare: 0.2, counts: counts(2, 20, 0, 3)},
		},
		{
			name:      "stalled stage takes the emerging line from its lowest unreached level",
			b:         Bottleneck{ID: "ladder_deploy", Stage: deploy, Level: l1, Terms: &MoveTerms{Hits: 3, Population: 20, Grain: mc}},
			wantStage: deploy, wantLevel: l1,
			want: &wantProgress{currentShare: 3.0 / 20.0, targetShare: 0.2, counts: counts(3, 20, 0, 2)},
		},
		{
			name:      "review reach is tagged Code, takes the established line and counts from the recorded floor",
			b:         Bottleneck{ID: bottleneckIDReviewReach, Stage: code, Terms: &MoveTerms{Hits: 140, Population: 320, Unobserved: 3, Grain: mc}},
			wantStage: code, wantLevel: unspecLevel,
			want: &wantProgress{currentShare: 140.0 / 320.0, targetShare: 0.5, counts: counts(140, 320, 3, 41)},
		},
		{
			name:      "adoption reach fills toward better: stored hits count people without an AI-authored merge",
			b:         Bottleneck{ID: bottleneckIDAdoptionReach, Terms: &MoveTerms{Hits: 14, Population: 24, Grain: pp}},
			wantStage: unspecStage, wantLevel: unspecLevel,
			want: &wantProgress{currentShare: 10.0 / 24.0, targetShare: 0.5, counts: counts(10, 24, 0, 3)},
		},
		{
			name:      "adoption reach over a roster under 20 carries no progress record",
			b:         Bottleneck{ID: bottleneckIDAdoptionReach, Terms: &MoveTerms{Hits: 11, Population: 18, Grain: pp}},
			wantStage: unspecStage, wantLevel: unspecLevel,
		},
		{
			name:      "a level that fell short with no share carries stage and level but no progress",
			b:         Bottleneck{ID: "rung_code_L4", Stage: code, Level: l4},
			wantStage: code, wantLevel: l4,
		},
		{
			name:      "review reach with no share carries no progress",
			b:         Bottleneck{ID: bottleneckIDReviewReach, Stage: code},
			wantStage: code, wantLevel: unspecLevel,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := assembleProfile(rs, nil, []Bottleneck{tt.b}, nil)
			if len(p.GetBottlenecks()) != 1 {
				t.Fatalf("profile carries %d constraints, want 1", len(p.GetBottlenecks()))
			}
			got := p.GetBottlenecks()[0]
			if got.GetStage() != tt.wantStage || got.GetLevel() != tt.wantLevel {
				t.Errorf("stage/level = %s/%s, want %s/%s", got.GetStage(), got.GetLevel(), tt.wantStage, tt.wantLevel)
			}
			pr := got.GetProgress()
			if tt.want == nil {
				if pr != nil {
					t.Fatalf("progress = %v, want none", pr)
				}
				return
			}
			if pr == nil {
				t.Fatal("progress is nil")
			}
			if pr.GetCurrentShare() != tt.want.currentShare || pr.GetTargetShare() != tt.want.targetShare {
				t.Errorf("shares = %v to %v, want %v to %v", pr.GetCurrentShare(), pr.GetTargetShare(), tt.want.currentShare, tt.want.targetShare)
			}
			w := tt.want.counts
			if pr.Current == nil || pr.Population == nil || pr.Unobserved == nil || pr.ItemsToTarget == nil {
				t.Fatalf("counts must all be set, got %v", pr)
			}
			if *pr.Current != w.current || *pr.Population != w.population || *pr.Unobserved != w.unobserved || *pr.ItemsToTarget != w.itemsToTarget {
				t.Errorf("counts = current %d population %d unobserved %d items %d, want %d %d %d %d",
					*pr.Current, *pr.Population, *pr.Unobserved, *pr.ItemsToTarget, w.current, w.population, w.unobserved, w.itemsToTarget)
			}
		})
	}
}

// TestAssembleProfile_ProductConstraintIsNotServed pins that an internal (product
// subject) constraint reaches neither the org-facing list nor a progress record.
func TestAssembleProfile_ProductConstraintIsNotServed(t *testing.T) {
	rs := rules.ForVersion("v1")
	p := assembleProfile(rs, nil, []Bottleneck{{ID: "gap_G1", Subject: "product"}}, nil)
	if len(p.GetBottlenecks()) != 0 {
		t.Errorf("an internal constraint was served: %v", p.GetBottlenecks())
	}
}

// TestAssembleProfile_ConstraintKindAndAdoptionTitle pins the typed kind each served
// constraint carries and the adoption title's Most/Half wording at exactly half.
func TestAssembleProfile_ConstraintKindAndAdoptionTitle(t *testing.T) {
	rs := rules.ForVersion("v1")
	pp := aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE
	mc := aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES
	tests := []struct {
		b    Bottleneck
		want aidlcv1.MaturityConstraintKind
	}{
		{Bottleneck{ID: bottleneckIDReviewReach}, aidlcv1.MaturityConstraintKind_MATURITY_CONSTRAINT_KIND_REVIEW_REACH},
		{Bottleneck{ID: bottleneckIDAdoptionReach}, aidlcv1.MaturityConstraintKind_MATURITY_CONSTRAINT_KIND_ADOPTION_REACH},
		{Bottleneck{ID: "rung_code_L4"}, aidlcv1.MaturityConstraintKind_MATURITY_CONSTRAINT_KIND_LEVEL_FELL_SHORT},
		{Bottleneck{ID: "ladder_deploy"}, aidlcv1.MaturityConstraintKind_MATURITY_CONSTRAINT_KIND_STAGE_STALLED},
	}
	for _, tt := range tests {
		p := assembleProfile(rs, nil, []Bottleneck{tt.b}, nil)
		if got := p.GetBottlenecks()[0].GetKind(); got != tt.want {
			t.Errorf("%s: kind = %s, want %s", tt.b.ID, got, tt.want)
		}
	}

	half := bottleneckTitle(rs, Bottleneck{ID: bottleneckIDAdoptionReach, Terms: &MoveTerms{Hits: 10, Population: 20, Grain: pp}})
	most := bottleneckTitle(rs, Bottleneck{ID: bottleneckIDAdoptionReach, Terms: &MoveTerms{Hits: 11, Population: 20, Grain: pp}})
	if strings.HasPrefix(half, "Most") || !strings.HasPrefix(most, "Most") {
		t.Errorf("exactly half must not read as most: half = %q, most = %q", half, most)
	}

	// No further item needed leaves the distance unset rather than zero.
	pr := progressFor(rs, Bottleneck{ID: "rung_code_L4", Terms: &MoveTerms{Hits: 50, Population: 100, Grain: mc}})
	if pr == nil || pr.ItemsToTarget != nil {
		t.Errorf("a share already above its line must leave items_to_target unset, got %v", pr)
	}
}

// TestProgressFor_LabelsMatchTheProse pins the formatted labels to the precision
// the bodies print, with the floor prefix when some reviewer lists were unread.
func TestProgressFor_LabelsMatchTheProse(t *testing.T) {
	rs := rules.ForVersion("v1")
	mc := aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES
	b := Bottleneck{ID: bottleneckIDReviewReach, Terms: &MoveTerms{Hits: 140, Population: 320, Unobserved: 3, Grain: mc}}
	pr := progressFor(rs, b)
	if want := reachText(rs, *b.Terms); pr.GetCurrentLabel() != want || pr.GetCurrentLabel() != "at least 44%" {
		t.Errorf("current label = %q, want %q", pr.GetCurrentLabel(), want)
	}
	if pr.GetTargetLabel() != "50%" {
		t.Errorf("target label = %q, want 50%%", pr.GetTargetLabel())
	}
	r := progressFor(rs, Bottleneck{ID: "rung_code_L4", Terms: &MoveTerms{Hits: 28, Population: 172, Grain: mc}})
	if r.GetCurrentLabel() != "16%" || r.GetTargetLabel() != "20%" {
		t.Errorf("rung labels = %q to %q, want 16%% to 20%%", r.GetCurrentLabel(), r.GetTargetLabel())
	}
}
