// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package airoi

// Calibration is the adopter-supplied input a dollar derivation needs: the
// blended FTE hourly rate and the annual AI tool spend, each with provenance.
// FteRateUSDPerHr must be positive (an entered rate or a product default —
// RateProvenance says which). A nil AnnualToolSpendUSD means spend was never
// entered (EMPTY); a non-nil zero is a deliberate $0.
//
// A production deployment typically resolves this from org settings + seat-cost detection;
// in the OSS engine the adopter supplies it directly (no comp/headcount data ever
// enters the compute).
type Calibration struct {
	FteRateUSDPerHr    float64
	RateProvenance     RateProvenance
	AnnualToolSpendUSD *float64
	Provenance         CalibrationProvenance
}

// DeriveDollars computes the read-time dollar block from a measurement's recovered
// hours and the calibration, annualizing the window's hours by 365/windowDays.
// Pure — no dollars are stored.
//
// Suppression: an absent optional says WHICH figure is withheld, SuppressionReason
// says WHY. A figure is never rendered as infinity or an error:
//   - spend EMPTY (nil)      → net_gain/roi_multiple/payback withheld, reason SPEND_NOT_ENTERED (annual_value shown).
//   - spend a deliberate $0  → annual_value + net_gain shown; roi_multiple/payback withheld (÷0), reason NO_AI_SPEND.
//   - non-positive value     → payback withheld (would be infinite/negative); roi_multiple stays; no spend reason.
//   - windowDays<=0          → all dollars withheld, UNSPECIFIED reason (malformed window).
func DeriveDollars(hoursRecovered float64, cal Calibration, windowDays int32) *Dollars {
	if windowDays <= 0 {
		return &Dollars{SuppressionReason: SuppressionUnspecified}
	}
	annualValue := hoursRecovered * cal.FteRateUSDPerHr * 365.0 / float64(windowDays)
	d := &Dollars{AnnualValueUsd: &annualValue}

	if cal.AnnualToolSpendUSD == nil {
		d.SuppressionReason = SuppressionSpendNotEntered
		return d
	}

	spend := *cal.AnnualToolSpendUSD
	netGain := annualValue - spend
	d.NetGainUsd = &netGain

	if spend <= 0 {
		d.SuppressionReason = SuppressionNoAISpend
		return d
	}

	roi := annualValue / spend
	d.RoiMultiple = &roi
	if annualValue > 0 {
		payback := spend / (annualValue / 12.0)
		d.PaybackMonths = &payback
	}
	return d
}

// DeriveClassContributions computes each work type's dollar contribution —
// (baseline_median_hrs − ai_median_hrs) × ai_change_count × fte_rate — one entry
// per class row (derived at read, never stored). The full AI cohort scales the
// contribution; rows the min-n floor excluded are still emitted so contributions
// align 1:1 with class_rows.
func DeriveClassContributions(m *Measurement, fteRateUSDPerHr float64) []*ClassContribution {
	rows := m.ClassRows
	out := make([]*ClassContribution, 0, len(rows))
	for _, r := range rows {
		deltaHrs := r.BaselineMedianHrs - r.AiMedianHrs
		// Credit the full AI cohort; fall back to matched_n for an old row written
		// before ai_change_count existed (a no-op for a current row).
		n := r.AiChangeCount
		if n == 0 {
			n = r.MatchedN
		}
		out = append(out, &ClassContribution{
			WorkType:        r.WorkType,
			UsdContribution: deltaHrs * float64(n) * fteRateUSDPerHr,
		})
	}
	return out
}
