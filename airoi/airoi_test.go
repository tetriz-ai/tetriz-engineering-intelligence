// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package airoi

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestMeasure_AIFaster_LiftAndHours(t *testing.T) {
	cfg := DefaultConfig()
	frame := &SubjectFrame{
		BandStats: []BandStat{
			{WorkType: "feature", Band: BandXS, Cohort: CohortBaseline, MedianCycleHrs: 10, MedianReviewRounds: 2, N: 20},
			{WorkType: "feature", Band: BandXS, Cohort: CohortAI, MedianCycleHrs: 5, MedianReviewRounds: 1, N: 15},
		},
		AIChangeCount: 15, TotalChangeCount: 35,
	}
	m := Measure(frame, cfg)
	// deltaHrs=5, baselineMedian=10 -> grossLift = 5/10 = 0.5
	if !approx(m.GrossLift, 0.5) {
		t.Fatalf("GrossLift = %v, want 0.5", m.GrossLift)
	}
	// effortPerChange = 5*0.5 = 2.5 (< cap), *15 = 37.5
	if !approx(m.GrossHours, 37.5) {
		t.Fatalf("GrossHours = %v, want 37.5", m.GrossHours)
	}
	// AI fewer rounds -> raw tax >1 -> capped to neutral 1.0 (no deflation of a good result)
	if !approx(m.AiTaxCoefficient, 1.0) {
		t.Fatalf("tax = %v, want 1.0", m.AiTaxCoefficient)
	}
	if !approx(m.NetLift, 0.5) || !approx(m.HoursRecovered, 37.5) {
		t.Fatalf("net lift/hours = %v/%v, want 0.5/37.5", m.NetLift, m.HoursRecovered)
	}
	if len(m.ClassRows) != 1 || m.ClassRows[0].MinNExcluded {
		t.Fatalf("expected 1 included class row, got %+v", m.ClassRows)
	}
}

func TestComputeTax_DeflatesWhenAINeedsMoreRounds(t *testing.T) {
	cfg := DefaultConfig()
	frame := &SubjectFrame{BandStats: []BandStat{
		{WorkType: "bugfix", Band: BandS, Cohort: CohortBaseline, MedianCycleHrs: 8, MedianReviewRounds: 1, N: 20},
		{WorkType: "bugfix", Band: BandS, Cohort: CohortAI, MedianCycleHrs: 6, MedianReviewRounds: 3, N: 15},
	}, AIChangeCount: 15, TotalChangeCount: 35}
	m := Measure(frame, cfg)
	// tax = (1+1)/(3+1) = 0.5
	if !approx(m.AiTaxCoefficient, 0.5) {
		t.Fatalf("tax = %v, want 0.5", m.AiTaxCoefficient)
	}
	if !approx(m.NetLift, m.GrossLift*0.5) {
		t.Fatalf("net lift not deflated: net=%v gross=%v", m.NetLift, m.GrossLift)
	}
}

func TestMeasure_MinNFloorExcludes(t *testing.T) {
	cfg := DefaultConfig()
	frame := &SubjectFrame{BandStats: []BandStat{
		{WorkType: "feature", Band: BandXS, Cohort: CohortBaseline, MedianCycleHrs: 10, MedianReviewRounds: 2, N: 5}, // < MinBaselineN
		{WorkType: "feature", Band: BandXS, Cohort: CohortAI, MedianCycleHrs: 5, MedianReviewRounds: 1, N: 3},        // < MinMatchedN
	}, AIChangeCount: 3, TotalChangeCount: 8}
	m := Measure(frame, cfg)
	if !m.ClassRows[0].MinNExcluded {
		t.Fatal("thin class should be min_n_excluded")
	}
	if m.GrossLift != 0 || m.HoursRecovered != 0 {
		t.Fatalf("excluded class must not contribute: lift=%v hours=%v", m.GrossLift, m.HoursRecovered)
	}
}

func usableFrame(wt WorkType, aiShareNum, total int) *SubjectFrame {
	return &SubjectFrame{
		BandStats: []BandStat{
			{WorkType: wt, Band: BandM, Cohort: CohortBaseline, MedianCycleHrs: 12, MedianReviewRounds: 2, N: 20},
			{WorkType: wt, Band: BandM, Cohort: CohortAI, MedianCycleHrs: 7, MedianReviewRounds: 2, N: 15},
		},
		AIChangeCount: aiShareNum, TotalChangeCount: total,
	}
}

func TestCascade_FromYourData(t *testing.T) {
	cfg := DefaultConfig()
	res := Cascade(usableFrame("feature", 15, 100), nil, nil, cfg) // AI share 0.15 < collapse
	if res.Basis != BasisFromYourData {
		t.Fatalf("basis = %v, want FROM_YOUR_DATA", res.Basis)
	}
}

func TestCascade_Insufficient(t *testing.T) {
	cfg := DefaultConfig()
	res := Cascade(&SubjectFrame{AIChangeCount: 2, TotalChangeCount: 3}, nil, nil, cfg)
	if res.Basis != BasisInsufficientData {
		t.Fatalf("basis = %v, want INSUFFICIENT_DATA", res.Basis)
	}
}

func TestCascade_LeverageWhenBaselineCollapsed(t *testing.T) {
	cfg := DefaultConfig()
	fyd := usableFrame("feature", 90, 100) // AI share 0.90 >= 0.75 collapse
	lev := usableFrame("feature", 90, 100)
	res := Cascade(fyd, lev, nil, cfg)
	if res.Basis != BasisWithinOrgLeverage {
		t.Fatalf("basis = %v, want WITHIN_ORG_LEVERAGE", res.Basis)
	}
}

func TestCascade_OneWayClampNoRiseFromLeverage(t *testing.T) {
	cfg := DefaultConfig()
	// prior settled at leverage; today derives FROM_YOUR_DATA — must clamp back to leverage.
	prior := &PriorRung{Basis: BasisWithinOrgLeverage}
	res := Cascade(usableFrame("feature", 15, 100), usableFrame("feature", 15, 100), prior, cfg)
	if res.Basis != BasisWithinOrgLeverage {
		t.Fatalf("basis = %v, want clamp to WITHIN_ORG_LEVERAGE", res.Basis)
	}
}

func TestDeriveDollars_Suppression(t *testing.T) {
	cal := func(spend *float64) Calibration {
		return Calibration{FteRateUSDPerHr: 20, AnnualToolSpendUSD: spend}
	}
	// spend never entered -> only annual_value, reason SPEND_NOT_ENTERED
	d := DeriveDollars(100, cal(nil), 30)
	if d.AnnualValueUsd == nil || d.NetGainUsd != nil || d.SuppressionReason != SuppressionSpendNotEntered {
		t.Fatalf("empty-spend suppression wrong: %+v", d)
	}
	// deliberate $0 -> net_gain shown, roi/payback withheld, reason NO_AI_SPEND
	zero := 0.0
	d = DeriveDollars(100, cal(&zero), 30)
	if d.NetGainUsd == nil || d.RoiMultiple != nil || d.SuppressionReason != SuppressionNoAISpend {
		t.Fatalf("$0-spend suppression wrong: %+v", d)
	}
	// positive spend -> full block
	spend := 5000.0
	d = DeriveDollars(100, cal(&spend), 30)
	if d.RoiMultiple == nil || d.PaybackMonths == nil {
		t.Fatalf("positive-spend should yield roi + payback: %+v", d)
	}
}

func TestBucketPRsByBand(t *testing.T) {
	scores := map[string]float64{"a": 1, "b": 2, "c": 3, "d": 8, "e": 9, "f": 10}
	low, high := BucketPRsByBand(scores)
	if len(low) == 0 || len(high) == 0 {
		t.Fatalf("expected a split, got low=%v high=%v", low, high)
	}
	// fewer than 3 -> no split
	if l, h := BucketPRsByBand(map[string]float64{"a": 1, "b": 2}); l != nil || h != nil {
		t.Fatalf("expected no split for <3 scores")
	}
}
