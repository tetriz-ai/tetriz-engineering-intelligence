// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package airoi computes an AI ROI value measurement for one subject (an org or
// a team) over a trailing window. It turns per-(work-type, complexity-band,
// cohort) aggregates — medians and counts — into a Measurement: the
// class-corrected, tax-adjusted headline lift, recovered effort hours, and the
// per-class breakdown a view renders. Dollars are derived separately at read time
// from a plain Calibration.
//
// The compute is deterministic over a SubjectFrame and needs no database — the
// aggregation (medians, counts) is the adopter's job, supplied through the
// SourceReader interface. The compute lives here; the data systems behind it
// (SQL source, AI-attribution, quality reader, seat-cost calibration, scheduler,
// and gRPC service) stay behind the interfaces in interfaces.go — an adopter
// supplies their own.
//
// Faithful to the AI-ROI methodology this engine implements:
// proto types are replaced by plain structs, and WorkType is a plain slug string.
package airoi

import "sort"

// WorkType is the change's work-type label (e.g. "feature", "bugfix"). It comes
// from a PR classifier upstream; this engine treats it opaquely as a grouping key.
type WorkType string

// Cohort partitions a subject's changes into the AI-attributed set and the
// baseline (non-AI) set the AI cohort is measured against.
type Cohort int

// The two cohorts a subject's changes split into.
const (
	CohortBaseline Cohort = iota // non-AI comparison set
	CohortAI                     // AI-attributed set
)

// Band is a change's complexity band. Comparing AI against baseline within a band,
// then re-weighting bands by the AI cohort's mix, removes the mix-shift bias a
// naive blended average carries (the class-correction). Cutoffs live in BandBounds.
type Band int

// The complexity bands, smallest to largest; BandUnknown is the unbinnable catch-all.
const (
	BandUnknown Band = iota // no comparable band — excluded from matched bands
	BandXS
	BandS
	BandM
	BandL
)

// BandStat is one (work-type, band, cohort) cell: the cohort's median cycle time
// and median review rounds over its changes in that band, and the change count N
// (the band weight). Medians are the adopter's PERCENTILE_CONT aggregates.
type BandStat struct {
	WorkType           WorkType
	Band               Band
	Cohort             Cohort
	MedianCycleHrs     float64
	MedianReviewRounds float64
	N                  int
}

// SubjectFrame is the full input to a measurement: the per-cell aggregates plus
// the subject-level counts the coverage and adoption figures need.
type SubjectFrame struct {
	BandStats        []BandStat // one entry per non-empty (work-type, band, cohort) cell
	AIChangeCount    int        // size of the AI cohort in the window
	TotalChangeCount int        // every merged change in the window (AI + baseline)
}

// BandBounds are the complexity-band cutoffs the source reader bins by — the
// single source of truth, injected into the reader rather than hard-coded there.
type BandBounds struct {
	ScoreXSMax, ScoreSMax, ScoreMMax float64
	LOCXSMax, LOCSMax, LOCMMax       int
}

// Config carries the v1 methodology constants (methodology card values).
type Config struct {
	MinBaselineN            int        // drop a class from the headline below this baseline sample
	MinMatchedN             int        // drop a class from the headline below this matched AI sample
	BaselineCollapseAIShare float64    // AI share ≥ this ⇒ baseline collapsed ⇒ leverage rung ([0,1]; 0 disables)
	EffortRatio             float64    // fraction of a change's cycle-time delta counted as recovered effort
	EffortCapHrs            float64    // cap a single change's recovered effort (symmetric)
	BandBounds              BandBounds // complexity-band cutoffs
}

// DefaultConfig is the v1 methodology default (the methodology card values).
func DefaultConfig() Config {
	return Config{
		MinBaselineN:            15,
		MinMatchedN:             10,
		BaselineCollapseAIShare: 0.75,
		EffortRatio:             0.5,
		EffortCapHrs:            8.0,
		// score ≤2 XS, ≤4 S, ≤7 M, else L; code-only-LOC fallback <50/<200/<600.
		BandBounds: BandBounds{ScoreXSMax: 2, ScoreSMax: 4, ScoreMMax: 7, LOCXSMax: 50, LOCSMax: 200, LOCMMax: 600},
	}
}

// BaselineBasis discriminates the measurement modes. Integer values are pinned to
// a fixed wire ordering (UNSPECIFIED=0 … INSUFFICIENT_DATA=5): the one-way clamp and
// isNumericBasis depend on this ordering, so DO NOT renumber.
type BaselineBasis int

// The measurement modes, in the one-way descent order the cascade clamps to.
const (
	BasisUnspecified       BaselineBasis = 0
	BasisComputing         BaselineBasis = 1
	BasisFromYourData      BaselineBasis = 2 // AI vs the org's non-AI baseline ("ROIResult")
	BasisWithinOrgLeverage BaselineBasis = 3 // high-quality AI vs low-quality AI ("LeverageAssessment")
	BasisBenchmarked       BaselineBasis = 4 // cross-org — reserved, not implemented
	BasisInsufficientData  BaselineBasis = 5 // neither usable ("unavailable")
)

// isNumericBasis reports whether a basis carries a real comparison (and so may
// derive dollars). FROM_YOUR_DATA / WITHIN_ORG_LEVERAGE / BENCHMARKED.
func isNumericBasis(b BaselineBasis) bool {
	return b == BasisFromYourData || b == BasisWithinOrgLeverage || b == BasisBenchmarked
}

// MixReferenceState labels the mix-shift reference distribution's origin.
type MixReferenceState int

// Where the mix-shift reference distribution came from.
const (
	MixRefUnspecified MixReferenceState = iota
	MixRefPreAdoption
	MixRefWithinOrgLeverage
	MixRefInsufficientData
)

// SuppressionReason says WHY a dollar figure was withheld.
type SuppressionReason int

// Why a dollar figure was withheld rather than shown as a number.
const (
	SuppressionUnspecified     SuppressionReason = iota
	SuppressionNoAISpend                         // spend a deliberate $0
	SuppressionSpendNotEntered                   // spend never entered
)

// RateProvenance / CalibrationProvenance record where the calibration came from.
type RateProvenance int

// Where the FTE rate came from.
const (
	RateProvenanceUnspecified RateProvenance = iota
	RateProvenanceDefault
	RateProvenanceUserEntered
)

// CalibrationProvenance records where the tool-spend side of the calibration came from.
type CalibrationProvenance int

// Where the tool-spend figure came from.
const (
	CalibrationProvenanceUnspecified CalibrationProvenance = iota
	CalibrationProvenanceDetected
	CalibrationProvenanceUserEntered
	CalibrationProvenanceEmpty
)

// ClassRow is one work type's band-weighted comparison (stored facts).
type ClassRow struct {
	WorkType             WorkType
	BaselineMedianHrs    float64
	AiMedianHrs          float64
	BaselineN            int32
	MatchedN             int32
	MinNExcluded         bool
	BaselineReviewRounds float64
	AiReviewRounds       float64
	Attention            bool
	AiChangeCount        int32
}

// MixShiftRow is one work type's historical-vs-current AI share.
type MixShiftRow struct {
	WorkType        WorkType
	HistoricalShare float64
	AiShare         float64
}

// MixShift is the work-mix reinvestment comparison against a frozen reference.
type MixShift struct {
	ReferenceState MixReferenceState
	ReferenceLabel string
	Rows           []*MixShiftRow
}

// Measurement is the stored facts of one subject's ROI (no dollars — decision 28).
type Measurement struct {
	NetLift          float64
	GrossLift        float64
	AiTaxCoefficient float64
	HoursRecovered   float64
	GrossHours       float64
	AiChangeCount    int32
	AiAdoptionShare  float64
	ClassRows        []*ClassRow
	MixShift         *MixShift
}

// Dollars is the read-time dollar block derived from a Measurement + Calibration.
// Optional figures are pointers: nil means withheld (see SuppressionReason).
type Dollars struct {
	AnnualValueUsd    *float64
	NetGainUsd        *float64
	RoiMultiple       *float64
	PaybackMonths     *float64
	SuppressionReason SuppressionReason
}

// ClassContribution is one work type's read-time dollar contribution.
type ClassContribution struct {
	WorkType        WorkType
	UsdContribution float64
}

// higherValueWorkTypes is the reinvestment "higher-value" set: feature and docs
// count as higher-value; everything else rolls up as toil. Slugs are lowercased.
var higherValueWorkTypes = map[WorkType]struct{}{
	"feature": {},
	"docs":    {},
}

// IsHigherValueWorkType reports whether a work type is counted as higher-value.
func IsHigherValueWorkType(wt WorkType) bool {
	_, ok := higherValueWorkTypes[wt]
	return ok
}

// HigherValueWorkTypes returns the set as a sorted slice.
func HigherValueWorkTypes() []WorkType {
	out := make([]WorkType, 0, len(higherValueWorkTypes))
	for wt := range higherValueWorkTypes {
		out = append(out, wt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
