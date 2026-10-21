// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Command roi-on-a-thin-window shows why the ROI value is read off the basis
// cascade, not off a bare Measurement. The same thin window — one work type whose
// baseline and AI samples both sit under the min-n floor — is run two ways:
//
//   - ComputeMeasurement + DeriveDollars (the simple, basis-free path) prints a
//     net gain of -$12,000 with annual value $0. "AI cost us the tool spend and
//     returned nothing" and "we could not measure anything" look identical here,
//     because a Measurement carries no basis to tell them apart.
//
//   - ComputeSubjectCascade reports INSUFFICIENT_DATA, so an honest reader derives
//     no dollars at all.
//
//     go run ./examples/roi-on-a-thin-window
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/airoi"
)

// thinReader is an in-memory SourceReader whose one work type has too few changes
// in either cohort to clear DefaultConfig's min-n floor (MinBaselineN 15,
// MinMatchedN 10), so no class enters the headline.
type thinReader struct{}

func (thinReader) LoadBandStats(_ context.Context, _ string, _, _ time.Time, _ []string, _ airoi.BandBounds) ([]airoi.BandStat, error) {
	return []airoi.BandStat{
		{WorkType: "feature", Band: airoi.BandM, Cohort: airoi.CohortBaseline, MedianCycleHrs: 40, MedianReviewRounds: 3, N: 6},
		{WorkType: "feature", Band: airoi.BandM, Cohort: airoi.CohortAI, MedianCycleHrs: 24, MedianReviewRounds: 2, N: 5},
	}, nil
}

func (thinReader) LoadSubjectCounts(_ context.Context, _ string, _, _ time.Time, _ []string) (airoi.SubjectCounts, error) {
	return airoi.SubjectCounts{AIChangeCount: 5, TotalChangeCount: 11}, nil
}

func main() {
	ctx := context.Background()
	reader := thinReader{}
	from, to := time.Now().AddDate(0, 0, -30), time.Now()
	aiPRKeys := []string{"pr-1"}
	cfg := airoi.DefaultConfig()

	// The simple path: a bare Measurement, then dollars with no basis guard.
	m, err := airoi.ComputeMeasurement(ctx, reader, "org-1", from, to, aiPRKeys, cfg)
	if err != nil {
		panic(err)
	}
	spend := 12000.0
	cal := airoi.Calibration{FteRateUSDPerHr: 75, AnnualToolSpendUSD: &spend}
	d := airoi.DeriveDollars(m.HoursRecovered, cal, 30)

	fmt.Println("-- ComputeMeasurement + DeriveDollars (basis-free: cannot withhold) --")
	fmt.Printf("  hours recovered  %.1f\n", m.HoursRecovered)
	fmt.Printf("  annual value     $%.0f\n", deref(d.AnnualValueUsd))
	fmt.Printf("  net gain         $%.0f\n", deref(d.NetGainUsd))
	fmt.Printf("  payback          %s\n", paybackStr(d.PaybackMonths))

	// The honest path: let the cascade choose the basis first. quality and store
	// are nil (no leverage rung, no prior snapshot), so the only comparison is
	// AI-vs-non-AI, and it is unmeasurable on this window.
	res, err := airoi.ComputeSubjectCascade(ctx, reader, nil, nil, "org-1", from, to, aiPRKeys, to, cfg)
	if err != nil {
		panic(err)
	}
	fmt.Println("-- ComputeSubjectCascade (basis first) --")
	fmt.Printf("  basis            %s\n", basisName(res.Basis))
	if isNumericBasis(res.Basis) {
		fmt.Println("  dollars          derived (numeric basis)")
	} else {
		fmt.Println("  dollars          not derived (non-numeric basis)")
	}
}

// isNumericBasis reports whether a basis carries a real comparison and so may
// derive dollars — the one guard the basis-free path cannot apply.
func isNumericBasis(b airoi.BaselineBasis) bool {
	return b == airoi.BasisFromYourData ||
		b == airoi.BasisWithinOrgLeverage ||
		b == airoi.BasisBenchmarked
}

func deref(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func paybackStr(f *float64) string {
	if f == nil {
		return "WITHHELD"
	}
	return fmt.Sprintf("%.1f mo", *f)
}

func basisName(b airoi.BaselineBasis) string {
	switch b {
	case airoi.BasisFromYourData:
		return "FROM_YOUR_DATA"
	case airoi.BasisWithinOrgLeverage:
		return "WITHIN_ORG_LEVERAGE"
	case airoi.BasisInsufficientData:
		return "INSUFFICIENT_DATA"
	default:
		return "OTHER"
	}
}
