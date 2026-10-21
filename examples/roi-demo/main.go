// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Command roidemo shows the AI ROI engine end-to-end with an in-memory
// SourceReader — no database. It builds a small cohort, runs ComputeMeasurement,
// then derives the read-time dollar block.
//
//	go run ./examples/roi-demo
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/airoi"
)

// memReader is a trivial in-memory SourceReader: it returns a fixed set of band
// stats + counts, ignoring the window/keys. A real adopter implements this over
// their own PR store (this release ships the interface, not a SQL implementation).
type memReader struct {
	stats  []airoi.BandStat
	counts airoi.SubjectCounts
}

func (m memReader) LoadBandStats(_ context.Context, _ string, _, _ time.Time, _ []string, _ airoi.BandBounds) ([]airoi.BandStat, error) {
	return m.stats, nil
}
func (m memReader) LoadSubjectCounts(_ context.Context, _ string, _, _ time.Time, _ []string) (airoi.SubjectCounts, error) {
	return m.counts, nil
}

func main() {
	cfg := airoi.DefaultConfig()
	reader := memReader{
		stats: []airoi.BandStat{
			{WorkType: "feature", Band: airoi.BandM, Cohort: airoi.CohortBaseline, MedianCycleHrs: 40, MedianReviewRounds: 3, N: 30},
			{WorkType: "feature", Band: airoi.BandM, Cohort: airoi.CohortAI, MedianCycleHrs: 24, MedianReviewRounds: 2, N: 22},
			{WorkType: "bugfix", Band: airoi.BandS, Cohort: airoi.CohortBaseline, MedianCycleHrs: 12, MedianReviewRounds: 2, N: 25},
			{WorkType: "bugfix", Band: airoi.BandS, Cohort: airoi.CohortAI, MedianCycleHrs: 9, MedianReviewRounds: 2, N: 18},
		},
		counts: airoi.SubjectCounts{AIChangeCount: 40, TotalChangeCount: 98},
	}

	ctx := context.Background()
	from, to := time.Now().AddDate(0, 0, -30), time.Now()
	m, err := airoi.ComputeMeasurement(ctx, reader, "org-1", from, to, []string{"pr-1", "pr-2"}, cfg)
	if err != nil {
		panic(err)
	}

	fmt.Printf("AI adoption share : %.1f%%\n", m.AiAdoptionShare*100)
	fmt.Printf("Net lift          : %.3f  (gross %.3f, tax %.2f)\n", m.NetLift, m.GrossLift, m.AiTaxCoefficient)
	fmt.Printf("Hours recovered   : %.1f (window 30d)\n", m.HoursRecovered)
	fmt.Println("Per-class:")
	for _, r := range m.ClassRows {
		fmt.Printf("  %-8s baseline %.1fh  ai %.1fh  matched_n=%d  excluded=%v\n",
			r.WorkType, r.BaselineMedianHrs, r.AiMedianHrs, r.MatchedN, r.MinNExcluded)
	}

	spend := 12000.0
	cal := airoi.Calibration{FteRateUSDPerHr: 75, AnnualToolSpendUSD: &spend}
	d := airoi.DeriveDollars(m.HoursRecovered, cal, 30)
	fmt.Println("Dollars (FTE $75/h, $12k tool spend):")
	fmt.Printf("  annual value $%.0f", deref(d.AnnualValueUsd))
	fmt.Printf("  net gain $%.0f", deref(d.NetGainUsd))
	if d.RoiMultiple != nil {
		fmt.Printf("  ROI ×%.1f", *d.RoiMultiple)
	}
	if d.PaybackMonths != nil {
		fmt.Printf("  payback %.1f mo", *d.PaybackMonths)
	}
	fmt.Println()
}

func deref(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}
