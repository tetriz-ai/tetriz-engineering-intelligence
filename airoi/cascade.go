// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package airoi

import (
	"math"
	"sort"
)

// PriorRung is the subject's previous snapshot rung: the basis to clamp against
// (one-way rule) and the mix-shift whose frozen reference rides forward. Nil means
// no prior row (first-ever compute).
type PriorRung struct {
	Basis    BaselineBasis
	MixShift *MixShift
}

// CascadeResult is the chosen basis and the measurement computed on that rung.
type CascadeResult struct {
	Basis       BaselineBasis
	Measurement *Measurement
}

// Cascade decides which baseline basis a subject's ROI is measured on and returns
// the measurement on that rung, honoring the one-way rule.
//
// Rung order (strongest→weakest): FROM_YOUR_DATA → WITHIN_ORG_LEVERAGE →
// INSUFFICIENT_DATA. Which is preferred turns on whether the non-AI baseline has
// collapsed (AI share ≥ cfg.BaselineCollapseAIShare). The min-n floors decide
// which classes enter a rung's headline (and so whether a rung is measurable).
//
// One-way rule: a subject only ever descends among numeric rungs — a collapsed
// cohort does not credibly return within a window — so the basis is clamped to at
// least the prior rung's. With no prior row the floor is COMPUTING and any derived
// basis is a legal first descent.
func Cascade(fromYourData, leverage *SubjectFrame, prior *PriorRung, cfg Config) CascadeResult {
	if fromYourData == nil {
		fromYourData = &SubjectFrame{}
	}
	priorBasis := BasisComputing
	if prior != nil && prior.Basis != BasisUnspecified {
		priorBasis = prior.Basis
	}

	// Measure both frames up front so the one-way clamp can emit whichever rung's
	// frame the final basis lands on — including a clamp UP to a prior rung.
	fydMeasurement := Measure(fromYourData, cfg)
	byBasis := map[BaselineBasis]*Measurement{BasisFromYourData: fydMeasurement}
	if leverage != nil {
		byBasis[BasisWithinOrgLeverage] = Measure(leverage, cfg)
	}

	levMeasurement := byBasis[BasisWithinOrgLeverage]
	fydUsable := measurementUsable(fydMeasurement)
	levUsable := levMeasurement != nil && measurementUsable(levMeasurement)

	derived := BasisInsufficientData
	switch {
	case baselineCollapsed(fromYourData, cfg) && levUsable:
		derived = BasisWithinOrgLeverage
	case fydUsable:
		derived = BasisFromYourData
	case levUsable:
		derived = BasisWithinOrgLeverage
	}

	// One-way rule: the reverse climb among NUMERIC rungs is blocked. A non-numeric
	// prior (COMPUTING/INSUFFICIENT_DATA) imposes no floor.
	finalBasis := derived
	if isNumericBasis(priorBasis) && isNumericBasis(derived) && int(derived) < int(priorBasis) {
		finalBasis = priorBasis
	}

	// Honesty guard: a numeric basis is stamped only when that rung's frame is
	// usable THIS window; otherwise fall through to INSUFFICIENT_DATA.
	if isNumericBasis(finalBasis) && !measurementUsable(byBasis[finalBasis]) {
		finalBasis = BasisInsufficientData
	}

	measurement := byBasis[finalBasis]
	if measurement == nil {
		measurement = insufficientMeasurement(fromYourData)
	}
	measurement.MixShift = mixShift(finalBasis, measurement, prior)
	return CascadeResult{Basis: finalBasis, Measurement: measurement}
}

// baselineCollapsed reports whether the subject's non-AI baseline has structurally
// vanished, measured as its AI share of changes clearing cfg.BaselineCollapseAIShare.
func baselineCollapsed(frame *SubjectFrame, cfg Config) bool {
	if cfg.BaselineCollapseAIShare <= 0 {
		return false
	}
	return ratio(frame.AIChangeCount, frame.TotalChangeCount) >= cfg.BaselineCollapseAIShare
}

// measurementUsable reports whether a rung produced a usable headline — at least
// one class cleared the min-n floor AND its baseline median is above the
// numerical-stability floor (mirrors Measure's headline-inclusion condition).
func measurementUsable(m *Measurement) bool {
	for _, row := range m.ClassRows {
		if !row.MinNExcluded && row.BaselineMedianHrs >= minBaselineMedianHrs {
			return true
		}
	}
	return false
}

// insufficientMeasurement is the headline-zeroed measurement for INSUFFICIENT_DATA:
// the cohort counts still describe the window, but no lift/hours/class rows.
func insufficientMeasurement(frame *SubjectFrame) *Measurement {
	return &Measurement{
		AiChangeCount:   int32(frame.AIChangeCount),
		AiAdoptionShare: ratio(frame.AIChangeCount, frame.TotalChangeCount),
	}
}

// BucketPRsByBand splits AI PR keys into low and high quality bands by the terciles
// of their own mean quality-score distribution: bottom band = low (baseline role),
// top band = high (AI role), middle dropped. Cuts are by score VALUE (ties stay
// whole), relative to the subject's own distribution. Non-finite scores excluded.
// Returns empty bands (no split) when fewer than three finite scores, or no usable
// spread — the caller reads that as "no leverage split".
func BucketPRsByBand(meanScoreByPRKey map[string]float64) (lowKeys, highKeys []string) {
	sorted := make([]float64, 0, len(meanScoreByPRKey))
	for _, score := range meanScoreByPRKey {
		if math.IsNaN(score) || math.IsInf(score, 0) {
			continue
		}
		sorted = append(sorted, score)
	}
	n := len(sorted)
	if n < 3 {
		return nil, nil
	}
	sort.Float64s(sorted)

	outer := (n + 2) / 3       // ceil(n/3): outer bands absorb the remainder
	lowCut := sorted[outer-1]  // largest score in the bottom band
	highCut := sorted[n-outer] // smallest score in the top band

	band := func(inLow, inHigh func(float64) bool) (low, high []string) {
		for key, score := range meanScoreByPRKey {
			if math.IsNaN(score) || math.IsInf(score, 0) {
				continue
			}
			switch {
			case inLow(score):
				low = append(low, key)
			case inHigh(score):
				high = append(high, key)
			}
		}
		return low, high
	}

	if lowCut < highCut {
		return band(
			func(s float64) bool { return s <= lowCut },
			func(s float64) bool { return s >= highCut },
		)
	}

	// A heavy middle collapsed both cuts onto one value: split strictly around it.
	mid := lowCut
	lowKeys, highKeys = band(
		func(s float64) bool { return s < mid },
		func(s float64) bool { return s > mid },
	)
	if len(lowKeys) == 0 || len(highKeys) == 0 {
		return nil, nil
	}
	return lowKeys, highKeys
}

// BuildLeverageFrame stitches the two band-scoped reads into one leverage frame:
// the high-band read's AI cells become the AI cohort, the low-band read's AI cells
// become the baseline cohort.
func BuildLeverageFrame(highBandAICells, lowBandAICells []BandStat, aiChangeCount, totalChangeCount int) *SubjectFrame {
	frame := &SubjectFrame{
		BandStats:        make([]BandStat, 0, len(highBandAICells)+len(lowBandAICells)),
		AIChangeCount:    aiChangeCount,
		TotalChangeCount: totalChangeCount,
	}
	for _, c := range highBandAICells {
		c.Cohort = CohortAI
		frame.BandStats = append(frame.BandStats, c)
	}
	for _, c := range lowBandAICells {
		c.Cohort = CohortBaseline
		frame.BandStats = append(frame.BandStats, c)
	}
	return frame
}

// mixShift resolves the work-mix reinvestment comparison. The historical reference
// is frozen at onboarding: the first-ever compute derives it from the winning
// frame's baseline cohort; later computes copy the prior row's frozen
// historical_share forward, recomputing only ai_share. INSUFFICIENT_DATA suppresses it.
func mixShift(basis BaselineBasis, measurement *Measurement, prior *PriorRung) *MixShift {
	refState := referenceStateFor(basis)
	if refState == MixRefInsufficientData {
		return &MixShift{ReferenceState: refState}
	}

	aiShare := classShareFromRows(measurement)
	if prior != nil && prior.MixShift != nil && len(prior.MixShift.Rows) > 0 {
		rows := make([]*MixShiftRow, 0, len(prior.MixShift.Rows))
		seen := make(map[WorkType]struct{}, len(prior.MixShift.Rows))
		for _, pr := range prior.MixShift.Rows {
			rows = append(rows, &MixShiftRow{
				WorkType:        pr.WorkType,
				HistoricalShare: pr.HistoricalShare,
				AiShare:         aiShare[pr.WorkType],
			})
			seen[pr.WorkType] = struct{}{}
		}
		// A work type in the current window but absent from the frozen reference is
		// still part of the current mix — emit it with historical_share=0. Sorted.
		currentOnly := make([]WorkType, 0)
		for wt := range aiShare {
			if _, ok := seen[wt]; !ok {
				currentOnly = append(currentOnly, wt)
			}
		}
		sort.Slice(currentOnly, func(i, j int) bool { return currentOnly[i] < currentOnly[j] })
		for _, wt := range currentOnly {
			rows = append(rows, &MixShiftRow{WorkType: wt, HistoricalShare: 0, AiShare: aiShare[wt]})
		}
		return &MixShift{
			ReferenceState: prior.MixShift.ReferenceState,
			ReferenceLabel: prior.MixShift.ReferenceLabel,
			Rows:           rows,
		}
	}

	// First-ever compute: freeze the reference from this window's baseline cohort.
	baseShare := baselineClassShare(measurement)
	rows := make([]*MixShiftRow, 0, len(measurement.ClassRows))
	for _, row := range measurement.ClassRows {
		rows = append(rows, &MixShiftRow{
			WorkType:        row.WorkType,
			HistoricalShare: baseShare[row.WorkType],
			AiShare:         aiShare[row.WorkType],
		})
	}
	return &MixShift{ReferenceState: refState, Rows: rows}
}

// referenceStateFor maps a baseline basis to its mix-shift reference state.
func referenceStateFor(basis BaselineBasis) MixReferenceState {
	switch basis {
	case BasisFromYourData:
		return MixRefPreAdoption
	case BasisWithinOrgLeverage:
		return MixRefWithinOrgLeverage
	default:
		return MixRefInsufficientData
	}
}

// classShareFromRows is each work type's share of the AI cohort's matched changes.
func classShareFromRows(m *Measurement) map[WorkType]float64 {
	total := 0
	for _, row := range m.ClassRows {
		total += int(row.MatchedN)
	}
	shares := map[WorkType]float64{}
	if total == 0 {
		return shares
	}
	for _, row := range m.ClassRows {
		shares[row.WorkType] = float64(row.MatchedN) / float64(total)
	}
	return shares
}

// baselineClassShare is each work type's share of the baseline cohort (frozen
// reference on a first compute).
func baselineClassShare(m *Measurement) map[WorkType]float64 {
	total := 0
	for _, row := range m.ClassRows {
		total += int(row.BaselineN)
	}
	shares := map[WorkType]float64{}
	if total == 0 {
		return shares
	}
	for _, row := range m.ClassRows {
		shares[row.WorkType] = float64(row.BaselineN) / float64(total)
	}
	return shares
}
