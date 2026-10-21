// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prcomplexity

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/diffsize"
)

// Default baseline gates.
const (
	// DefaultMinSampleSize is the sample-size floor for the baseline as a whole
	// (strictly more PRs than this) and for each language (at least this many
	// PRs touching it).
	DefaultMinSampleSize = 30
	// DefaultCoverageThresholdBps is the coverage gate in basis points: the
	// share of sample PRs with a usable diff must be strictly above it.
	DefaultCoverageThresholdBps = 9500
)

// ErrNotEnoughData is returned, wrapped, by BuildBaseline when the sample
// fails the sample-size or coverage gate.
var ErrNotEnoughData = errors.New("prcomplexity: not enough data for a baseline")

// Baseline is a repository's PR-size distribution over one window of merged
// PRs: the anchors ScoreComplexity scores against.
type Baseline struct {
	// SampleSize is the number of PRs in the sample (the coverage denominator).
	SampleSize int
	// DiffAvailableCount is the number of sample PRs with a usable diff (the
	// coverage numerator).
	DiffAvailableCount int
	// CoverageBps is DiffAvailableCount / SampleSize in basis points,
	// truncated (9500 = 95.00%).
	CoverageBps int
	// PerLanguage holds one anchor per language that cleared the per-language
	// sample size with usable medians, keyed by enry language name. A language
	// with no entry is not an anchor: its PR slice is dropped at scoring time.
	PerLanguage map[string]LanguageStats
}

// LanguageStats is one language's anchor: the median-based values the ratio
// formula reads and the log-normal values the cncs formula reads.
type LanguageStats struct {
	// MedianSLOC is the median added source lines over PRs touching this language.
	MedianSLOC int
	// MedianFiles is the median files changed over those PRs.
	MedianFiles int
	// SampleSize is the number of sample PRs that touched this language.
	SampleSize int
	// MuLogSLOC is the mean of log(winsorised SLOC + 1) after winsorising at
	// P95SLOC. Zero means "no log stats": the cncs formula skips the language.
	MuLogSLOC float64
	// SigmaLogSLOC is the population standard deviation of log(winsorised SLOC + 1).
	SigmaLogSLOC float64
	// P95SLOC is the winsorisation cap: the sorted SLOC sample at index
	// floor(0.95n), at least 1.
	P95SLOC int
	// MuLogFiles is the mean of log(winsorised files + 1).
	MuLogFiles float64
	// SigmaLogFiles is the population standard deviation of log(winsorised files + 1).
	SigmaLogFiles float64
	// P95Files is the winsorisation cap for files, as P95SLOC.
	P95Files int
}

// SamplePR is one merged PR in a baseline window.
type SamplePR struct {
	// DiffRef is the PR's diff reference: any non-empty value for a fetched
	// diff, one of the sentinels DiffTooLarge and DiffNotAvailable, or empty
	// when no diff was fetched.
	DiffRef string
	// Analyzed reports whether the diff's effective LOC is present.
	Analyzed bool
	// ByLanguage is the PR's per-language footprint (see FromEffectiveLOC).
	ByLanguage map[string]LanguageLOC
}

// BaselineOptions holds the baseline gates. Start from DefaultBaselineOptions
// and change the field you need: the zero value is an error, not "no gates".
type BaselineOptions struct {
	// MinSampleSize: the sample needs strictly more PRs than this, and a
	// language needs at least this many PRs touching it. It must be at least 1.
	MinSampleSize int
	// CoverageThresholdBps: the coverage must be strictly above this. It must
	// be in [0, 10000).
	CoverageThresholdBps int
}

// DefaultBaselineOptions returns the default gates: 30 PRs and 95% coverage.
func DefaultBaselineOptions() BaselineOptions {
	return BaselineOptions{
		MinSampleSize:        DefaultMinSampleSize,
		CoverageThresholdBps: DefaultCoverageThresholdBps,
	}
}

// BuildBaseline builds a repository's baseline from the merged PRs of one
// window, in the order given. The window gate admits the sample when it holds
// strictly more than MinSampleSize PRs and its coverage is strictly above
// CoverageThresholdBps; otherwise BuildBaseline returns the counts with no
// anchors and an error wrapping ErrNotEnoughData. An admitted sample whose
// languages all fall below MinSampleSize yields a baseline with no anchors
// and a nil error: every PR scored against it is then unscoreable unless its
// diff is TOO_LARGE.
//
// Invalid input is an error with a nil baseline: a MinSampleSize below 1, a
// CoverageThresholdBps outside [0, 10000), or a negative SLOC or file count
// in any sample PR's ByLanguage.
//
// The log statistics sum in sample order, so two orders of the same sample
// can differ in the last bit.
func BuildBaseline(sample []SamplePR, opts BaselineOptions) (*Baseline, error) {
	if err := validateBaselineInput(sample, opts); err != nil {
		return nil, err
	}
	byLangSloc := map[string][]int{}
	byLangFiles := map[string][]int{}
	n, diffOK := 0, 0
	for _, pr := range sample {
		n++
		if !usableDiff(pr) {
			continue
		}
		diffOK++
		// A TOO_LARGE PR counts toward coverage above and adds nothing to the
		// per-language samples, even when it carries a footprint.
		if pr.DiffRef == DiffTooLarge {
			continue
		}
		for lang, loc := range CodeSlices(pr.ByLanguage) {
			byLangSloc[lang] = append(byLangSloc[lang], int(loc.SLOC))
			byLangFiles[lang] = append(byLangFiles[lang], loc.Files)
		}
	}

	coverageBps := 0
	if n > 0 {
		coverageBps = 10000 * diffOK / n
	}
	b := &Baseline{SampleSize: n, DiffAvailableCount: diffOK, CoverageBps: coverageBps}

	if n > opts.MinSampleSize && coverageBps > opts.CoverageThresholdBps {
		b.PerLanguage = perLanguageStats(byLangSloc, byLangFiles, opts.MinSampleSize)
		return b, nil
	}
	return b, fmt.Errorf("%w: %d PRs (need more than %d), coverage %d bps (need more than %d)",
		ErrNotEnoughData, n, opts.MinSampleSize, coverageBps, opts.CoverageThresholdBps)
}

// validateBaselineInput rejects gates that admit every sample and sizes that
// would give a non-finite log statistic.
func validateBaselineInput(sample []SamplePR, opts BaselineOptions) error {
	if opts.MinSampleSize < 1 {
		return fmt.Errorf("prcomplexity: MinSampleSize %d below 1", opts.MinSampleSize)
	}
	if opts.CoverageThresholdBps < 0 || opts.CoverageThresholdBps >= 10000 {
		return fmt.Errorf("prcomplexity: CoverageThresholdBps %d outside [0, 10000)", opts.CoverageThresholdBps)
	}
	for i, pr := range sample {
		for lang, loc := range pr.ByLanguage {
			if loc.SLOC < 0 || loc.Files < 0 {
				return fmt.Errorf("prcomplexity: sample PR %d: negative size for %q: %d SLOC, %d files", i, lang, loc.SLOC, loc.Files)
			}
		}
	}
	return nil
}

// perLanguageStats keeps only languages that cleared minSample with a usable
// (>0) median in both dimensions. Log stats are computed from the same
// per-language sloc/files samples and stored alongside the medians, so both
// scoring formulas can use the anchor.
func perLanguageStats(byLangSloc, byLangFiles map[string][]int, minSample int) map[string]LanguageStats {
	perLang := map[string]LanguageStats{}
	for lang, slocs := range byLangSloc {
		if len(slocs) < minSample {
			continue
		}
		medS := Median(slocs)
		medF := Median(byLangFiles[lang])
		if medS <= 0 || medF <= 0 {
			continue
		}
		files := byLangFiles[lang]
		muLogSLOC, sigmaLogSLOC, p95SLOC := logStatsFromSample(slocs)
		muLogFiles, sigmaLogFiles, p95Files := logStatsFromSample(files)
		perLang[lang] = LanguageStats{
			MedianSLOC:    medS,
			MedianFiles:   medF,
			SampleSize:    len(slocs),
			MuLogSLOC:     muLogSLOC,
			SigmaLogSLOC:  sigmaLogSLOC,
			P95SLOC:       p95SLOC,
			MuLogFiles:    muLogFiles,
			SigmaLogFiles: sigmaLogFiles,
			P95Files:      p95Files,
		}
	}
	return perLang
}

// usableDiff reports whether a PR counts toward coverage. TOO_LARGE counts
// (it scores 7.0); BuildBaseline keeps it out of the medians. NOT_AVAILABLE is
// unscoreable and excluded.
func usableDiff(pr SamplePR) bool {
	if pr.DiffRef == "" {
		return false
	}
	if pr.DiffRef == DiffTooLarge {
		return true
	}
	if !pr.Analyzed {
		return false
	}
	if pr.DiffRef == DiffNotAvailable {
		return false
	}
	return true
}

// logStatsFromSample computes p95-winsorized log-mean and population log-std of a
// non-empty sample of integer totals.
func logStatsFromSample(values []int) (muLog, sigmaLog float64, p95 int) {
	n := len(values)
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	p95Idx := n * 95 / 100 // always < n for n >= 1 (floor(0.95n) < n)
	p95 = sorted[p95Idx]
	if p95 < 1 {
		p95 = 1
	}

	logs := make([]float64, n)
	for i, v := range values {
		capped := v
		if capped > p95 {
			capped = p95
		}
		logs[i] = math.Log(float64(capped) + 1)
	}

	var sum float64
	for _, l := range logs {
		sum += l
	}
	muLog = sum / float64(n)

	var variance float64
	for _, l := range logs {
		d := l - muLog
		variance += d * d
	}
	sigmaLog = math.Sqrt(variance / float64(n))
	return
}

// FromEffectiveLOC converts a diff's effective LOC into the per-language
// footprint ScoreComplexity and BuildBaseline read: source lines added and
// files changed per language. A nil value gives nil.
func FromEffectiveLOC(e *diffsize.EffectiveLOC) map[string]LanguageLOC {
	if e == nil {
		return nil
	}
	out := make(map[string]LanguageLOC, len(e.ByLanguage))
	for lang, d := range e.ByLanguage {
		if d == nil {
			continue
		}
		out[lang] = LanguageLOC{SLOC: d.SourceLinesAdded, Files: d.FilesChanged}
	}
	return out
}
