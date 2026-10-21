// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package airoi

import "sort"

// classResult is one work type's band-weighted comparison, carrying both the row
// and the intermediates (delta, per-class hours) the headline needs but does not
// store.
type classResult struct {
	row *ClassRow
	// deltaHrs is baseline − ai band-weighted cycle time; positive means AI faster.
	deltaHrs float64
	// aiShareWeight is this class's full AI change count (row.AiChangeCount).
	aiShareWeight float64
	// included is false when the min-n floor drops the class from the headline.
	included bool
}

// buildClassResults band-weights every work type in the frame and applies the
// min-n floor. A class's baseline and AI cycle times are its band medians
// re-weighted by the AI cohort's band mix over the *matched* bands (where both
// cohorts have changes), holding work complexity fixed. Excluded classes are kept
// as rows but dropped from the headline. Sorted by work type for deterministic
// (audit-stable) output.
func buildClassResults(frame *SubjectFrame, cfg Config) []classResult {
	byClass := map[WorkType]map[Band]map[Cohort]BandStat{}
	for _, bs := range frame.BandStats {
		if _, ok := byClass[bs.WorkType]; !ok {
			byClass[bs.WorkType] = map[Band]map[Cohort]BandStat{}
		}
		if _, ok := byClass[bs.WorkType][bs.Band]; !ok {
			byClass[bs.WorkType][bs.Band] = map[Cohort]BandStat{}
		}
		byClass[bs.WorkType][bs.Band][bs.Cohort] = bs
	}

	results := make([]classResult, 0, len(byClass))
	for wt, bands := range byClass {
		results = append(results, buildOneClass(wt, bands, cfg))
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].row.WorkType < results[j].row.WorkType
	})
	return results
}

// buildOneClass computes one work type's band-weighted row. The band weight is
// the AI cohort's change count in that band; an unmatched band (missing either
// cohort, or BandUnknown) contributes no weight, so baseline and AI medians are
// compared over the same complexity mix.
//
// baseline_n / matched_n are the counts over the MATCHED bands (the compared
// population) — the min-n floor gates on those. ai_change_count is the full AI
// cohort over every band (BandUnknown excluded) — the volume the headline credits
// the measured per-change saving to.
func buildOneClass(wt WorkType, bands map[Band]map[Cohort]BandStat, cfg Config) classResult {
	var (
		weightSum        float64
		baselineCycleAcc float64
		aiCycleAcc       float64
		baselineRoundAcc float64
		aiRoundAcc       float64
		matchedN         int
		baselineN        int
		aiClassN         int
	)
	for band, cohorts := range bands {
		if band == BandUnknown {
			continue
		}
		base, hasBase := cohorts[CohortBaseline]
		ai, hasAI := cohorts[CohortAI]
		// ai_change_count spans every band, matched or not — accumulated before the
		// matched-band guard so unmatched bands still count.
		if hasAI {
			aiClassN += ai.N
		}
		if !hasBase || !hasAI {
			continue
		}
		w := float64(ai.N)
		weightSum += w
		baselineCycleAcc += w * base.MedianCycleHrs
		aiCycleAcc += w * ai.MedianCycleHrs
		baselineRoundAcc += w * base.MedianReviewRounds
		aiRoundAcc += w * ai.MedianReviewRounds
		matchedN += ai.N
		baselineN += base.N
	}

	row := &ClassRow{
		WorkType:      wt,
		BaselineN:     int32(baselineN),
		MatchedN:      int32(matchedN),
		AiChangeCount: int32(aiClassN),
	}
	// No matched band means no comparable population — a zero-median row, excluded.
	if weightSum == 0 {
		row.MinNExcluded = true
		return classResult{row: row, included: false}
	}

	baselineCycle := baselineCycleAcc / weightSum
	aiCycle := aiCycleAcc / weightSum
	row.BaselineMedianHrs = baselineCycle
	row.AiMedianHrs = aiCycle
	row.BaselineReviewRounds = baselineRoundAcc / weightSum
	row.AiReviewRounds = aiRoundAcc / weightSum
	// Attention: AI is both slower AND takes more review rounds.
	row.Attention = aiCycle > baselineCycle && row.AiReviewRounds > row.BaselineReviewRounds

	excluded := baselineN < cfg.MinBaselineN || matchedN < cfg.MinMatchedN
	row.MinNExcluded = excluded

	return classResult{
		row:           row,
		deltaHrs:      baselineCycle - aiCycle,
		aiShareWeight: float64(aiClassN),
		included:      !excluded,
	}
}
