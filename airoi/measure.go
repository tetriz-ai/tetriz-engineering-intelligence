// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package airoi

// minBaselineMedianHrs is a numerical-stability floor (6 minutes), not a
// methodology parameter: deltaHrs/baseline is unbounded as the baseline median
// approaches zero, so a class whose baseline is below any plausible human cycle
// time (near-instant bot auto-merges) is excluded from the headline.
const minBaselineMedianHrs = 0.1

// Measure turns a subject's aggregate frame into the measured facts of a
// Measurement: the class-corrected, tax-adjusted headline lift, recovered effort
// hours, adoption, the per-class breakdown, and the pre-tax gross figures. It
// emits only stored facts — dollars and per-class delta are derived at read.
// MixShift is set by Cascade, not here.
func Measure(frame *SubjectFrame, cfg Config) *Measurement {
	classes := buildClassResults(frame, cfg)
	tax := computeTax(frame, cfg)

	rows := make([]*ClassRow, 0, len(classes))
	var (
		aiShareTotal float64
		grossLift    float64
		grossHours   float64
	)
	for _, c := range classes {
		rows = append(rows, c.row)
		if c.included {
			aiShareTotal += c.aiShareWeight
		}
	}
	// Headline lift and effort sum only over classes that cleared the min-n floor,
	// each weighted by its share of the AI cohort. A slower class (negative delta)
	// legitimately pulls both figures down.
	if aiShareTotal > 0 {
		for _, c := range classes {
			// Baseline-median floor is a numerical-stability guard, not methodology.
			if !c.included || c.row.BaselineMedianHrs < minBaselineMedianHrs {
				continue
			}
			wClass := c.aiShareWeight / aiShareTotal
			grossLift += wClass * (c.deltaHrs / c.row.BaselineMedianHrs)
			// Symmetric per-change effort cap: one long-tail change can't dominate
			// the headline hours in either direction.
			effortPerChange := c.deltaHrs * cfg.EffortRatio
			if effortPerChange > cfg.EffortCapHrs {
				effortPerChange = cfg.EffortCapHrs
			} else if effortPerChange < -cfg.EffortCapHrs {
				effortPerChange = -cfg.EffortCapHrs
			}
			// Saving measured on matched bands, credited to the full AI cohort.
			grossHours += effortPerChange * float64(c.row.AiChangeCount)
		}
	}

	// The quality tax is a deflator (≤ 1.0) that may only reduce a POSITIVE result.
	// lift and hours can diverge in sign, so each applies the tax on its own sign;
	// gross figures are stored so the pre-tax numbers need no reconstruction.
	liftTax := 1.0
	if grossLift > 0 {
		liftTax = tax
	}
	hoursTax := 1.0
	if grossHours > 0 {
		hoursTax = tax
	}
	return &Measurement{
		NetLift:          grossLift * liftTax,
		GrossLift:        grossLift,
		AiTaxCoefficient: tax,
		HoursRecovered:   grossHours * hoursTax,
		GrossHours:       grossHours,
		AiChangeCount:    int32(frame.AIChangeCount),
		AiAdoptionShare:  ratio(frame.AIChangeCount, frame.TotalChangeCount),
		ClassRows:        rows,
	}
}

// computeTax is the org-level, band-matched quality deflator: each cohort's review
// rounds aggregated over the matched bands (AI-count weighted), then the smoothed
// ratio (baselineRounds+1)/(aiRounds+1), capped at 1.0 (deflate only). Neutral 1.0
// below the min-n floor.
func computeTax(frame *SubjectFrame, cfg Config) float64 {
	var weightSum, baselineAcc, aiAcc float64
	var baselineN, matchedN int
	byCell := map[workTypeBand]map[Cohort]BandStat{}
	for _, bs := range frame.BandStats {
		if bs.Band == BandUnknown {
			continue
		}
		k := workTypeBand{bs.WorkType, bs.Band}
		if _, ok := byCell[k]; !ok {
			byCell[k] = map[Cohort]BandStat{}
		}
		byCell[k][bs.Cohort] = bs
	}
	for _, cohorts := range byCell {
		base, hasBase := cohorts[CohortBaseline]
		ai, hasAI := cohorts[CohortAI]
		if !hasBase || !hasAI {
			continue
		}
		w := float64(ai.N)
		weightSum += w
		baselineAcc += w * base.MedianReviewRounds
		aiAcc += w * ai.MedianReviewRounds
		baselineN += base.N
		matchedN += ai.N
	}
	if weightSum == 0 || baselineN < cfg.MinBaselineN || matchedN < cfg.MinMatchedN {
		return 1.0
	}
	baselineRounds := baselineAcc / weightSum
	aiRounds := aiAcc / weightSum
	tax := (baselineRounds + 1.0) / (aiRounds + 1.0)
	if tax > 1.0 {
		return 1.0
	}
	return tax
}

// workTypeBand keys a (work-type, band) cell for the org-level tax aggregation.
type workTypeBand struct {
	wt   WorkType
	band Band
}

// ratio is num/den as a float, or 0 when the denominator is zero.
func ratio(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}
