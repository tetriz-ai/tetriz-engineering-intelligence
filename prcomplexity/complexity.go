// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package prcomplexity scores a pull request's complexity from 1.0 to 10.0
// relative to its own repository's recent pull requests, per language. Two
// formulas are supported (see ScoreComplexity):
//
//   - FormulaCNCS, the log-normal z-score sigmoid formula (named cncs), is
//     the default.
//   - FormulaRatio is the per-language median ratio formula.
//
// The package does no I/O and reads no clock or configuration: the baseline,
// the formula and the cross-language decay are passed in. BuildBaseline and
// ScoreComplexity apply the same code-only language filter, IsCodeLanguage.
package prcomplexity

import (
	"fmt"
	"math"
	"sort"

	enry "github.com/go-enry/go-enry/v2"
)

// Formula selects the scoring formula.
type Formula string

// The supported formulas. The empty Formula means FormulaCNCS.
const (
	FormulaCNCS  Formula = "cncs"
	FormulaRatio Formula = "ratio"
)

// DefaultDecay is the default cross-language geometric decay factor d.
const DefaultDecay = 0.3

// Diff sentinels: pass one as diffRef when there is no diff to size.
// DiffTooLarge means the diff was over the caller's size cap, and it scores a
// flat 7.0. DiffNotAvailable means there is no diff, and it is unscoreable.
const (
	DiffTooLarge     = "TOO_LARGE"
	DiffNotAvailable = "NOT_AVAILABLE"
)

const (
	tooLargeScore = 7.0
	maxScore      = 10.0
	scoreFloor    = 1.0

	// sigmoidCoeff is the logistic approximation of the normal CDF probit coefficient.
	sigmoidCoeff = 1.702

	// Sigma validity guards: a baseline sigma outside these ranges is
	// degenerate (a single-commit repo, a uniform-PR codebase) and is replaced
	// by the fallback value.
	sigmaLogSLOCMin      = 0.8
	sigmaLogSLOCMax      = 3.0
	sigmaLogSLOCFallback = 2.0

	sigmaLogFilesMin      = 0.3
	sigmaLogFilesMax      = 2.0
	sigmaLogFilesFallback = 0.8
)

// The valid sigma ranges. A baseline sigma outside its range is replaced by a
// fallback before scoring.
const (
	SigmaLogSLOCMin  = sigmaLogSLOCMin
	SigmaLogSLOCMax  = sigmaLogSLOCMax
	SigmaLogFilesMin = sigmaLogFilesMin
	SigmaLogFilesMax = sigmaLogFilesMax
)

// LanguageLOC is one language's added-line footprint on a PR: the scoring
// input for that language. Maps of LanguageLOC are keyed by go-enry's exact
// language name ("Go", not "go"); a name go-enry does not know is not code.
type LanguageLOC struct {
	// SLOC is the added source lines in this language.
	SLOC int64
	// Files is the number of files changed in this language.
	Files int
}

// codeDataLanguages are enry-typed Data languages that are nonetheless real
// source whose SLOC/files count toward complexity, so they count as code despite their type.
var codeDataLanguages = map[string]bool{
	"Protocol Buffer": true,
}

// IsCodeLanguage reports whether a language counts as code for complexity:
// go-enry's Programming or Markup type, or Protocol Buffer. Other Data
// (JSON, YAML), Prose (Markdown) and unknown names are not code. The name must
// be go-enry's exact language name: "go" is unknown, so it is not code.
// BuildBaseline and ScoreComplexity both go through it, so a PR is never
// scored against a distribution it does not contribute to.
func IsCodeLanguage(language string) bool {
	switch enry.GetLanguageType(language) {
	case enry.Programming, enry.Markup:
		return true
	case enry.Data:
		return codeDataLanguages[language]
	default:
		return false
	}
}

// CodeSlices keeps only code-only languages actually touched (SLOC > 0).
func CodeSlices(byLanguage map[string]LanguageLOC) map[string]LanguageLOC {
	slices := make(map[string]LanguageLOC, len(byLanguage))
	for lang, loc := range byLanguage {
		if loc.SLOC > 0 && IsCodeLanguage(lang) {
			slices[lang] = loc
		}
	}
	return slices
}

// sigmoidScore maps a z-score to [1, 10] via the CNCS sigmoid:
// clamp(1 + 9/(1 + exp(-1.702*z)), 1, 10).
func sigmoidScore(z float64) float64 {
	s := 1.0 + 9.0/(1.0+math.Exp(-sigmoidCoeff*z))
	if s < scoreFloor {
		return scoreFloor
	}
	if s > maxScore {
		return maxScore
	}
	return s
}

// scoreFn maps a dimension value to a 1.0–10.0 score relative to its language
// median (ratio formula). median must be > 0 (the anchor gate guarantees it).
func scoreFn(value, median float64) float64 {
	ratio := value / median
	switch {
	case ratio <= 0.2:
		return scoreFloor
	case ratio <= 1.0:
		return 5.0 * ratio
	default:
		return maxScore - (5.0 / ratio)
	}
}

// combineSlice combines a PR's SLOC and files scores with the 80/20
// dominant-dimension rule.
func combineSlice(sSLOC, sFiles float64) float64 {
	hi, lo := sSLOC, sFiles
	if lo > hi {
		hi, lo = lo, hi
	}
	return 0.8*hi + 0.2*lo
}

// combineAcrossLanguages combines per-slice scores with geometric rank decay
// (sort desc, sum s_1 + d·s_2 + d²·s_3 + …, capped at 10).
// Returns the raw value. ScoreComplexity rounds it to one decimal place.
func combineAcrossLanguages(sliceScores []float64, decayD float64) float64 {
	sorted := append([]float64(nil), sliceScores...)
	sort.Sort(sort.Reverse(sort.Float64Slice(sorted)))
	sum, weight := 0.0, 1.0
	for _, s := range sorted {
		sum += weight * s
		weight *= decayD
	}
	return math.Min(maxScore, sum)
}

// Median returns the median of values. On an even count it rounds the mean
// of the two middle values half away from zero. It returns 0 for an empty
// slice.
func Median(values []int) int {
	n := len(values)
	if n == 0 {
		return 0
	}
	sorted := append([]int(nil), values...)
	sort.Ints(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return midpoint(sorted[n/2-1], sorted[n/2])
}

// midpoint returns the mean of a and b rounded half away from zero, in integer
// arithmetic only, so it neither overflows nor loses precision. It halves each
// side first: a+b = 2q + r with r in [-2, 2], since Go's / and % truncate
// toward zero.
func midpoint(a, b int) int {
	q, r := a/2+b/2, a%2+b%2
	switch {
	case r == 2, r == 1 && q >= 0:
		return q + 1
	case r == -2, r == -1 && q <= 0:
		return q - 1
	}
	return q
}

// ScoreComplexity applies the selected formula. It returns the score, whether
// the PR was scoreable (ok), and an error:
//   - (7.0, true)   for a TOO_LARGE diff
//   - (score, true) for a normal scored PR, rounded to one decimal
//   - (nil, false)  when the PR is unscoreable: a NOT_AVAILABLE diff, no added
//     code-only SLOC at all (docs/config-only or pure-deletion), or code
//     languages with no baseline anchor carrying the stats the formula needs
//
// diffRef is the PR's diff reference (any value) or one of the sentinels
// DiffTooLarge and DiffNotAvailable. formula is FormulaCNCS or FormulaRatio;
// the empty Formula means FormulaCNCS and any other value is an error. decayD
// is the geometric cross-language decay factor used by both formulas when
// combining per-language slice scores; it must be in [0, 1] (DefaultDecay is
// 0.3). A nil baseline has no anchors.
//
// Invalid input is an error, with a nil score and false: an unknown formula, a
// decay outside [0, 1] or NaN, a negative SLOC or file count in byLanguage, or
// a baseline anchor with a NaN or infinite log statistic.
func ScoreComplexity(diffRef string, byLanguage map[string]LanguageLOC, baseline *Baseline, formula Formula, decayD float64) (*float64, bool, error) {
	if err := validateInput(byLanguage, baseline, formula, decayD); err != nil {
		return nil, false, err
	}
	if diffRef == DiffNotAvailable {
		return nil, false, nil
	}
	if diffRef == DiffTooLarge {
		v := tooLargeScore
		return &v, true, nil
	}
	// CodeSlices keeps only code languages with added SLOC, so a docs-only or
	// pure-deletion PR leaves slices empty and falls through to the no-scoreable-slice
	// return below — no special-case guard needed.
	slices := CodeSlices(byLanguage)

	var anchors map[string]LanguageStats
	if baseline != nil {
		anchors = baseline.PerLanguage
	}

	var score *float64
	var enough bool
	if formula == FormulaRatio {
		score, enough = scoreRatio(slices, anchors, decayD)
	} else {
		score, enough = scoreCNCS(slices, anchors, decayD)
	}
	return score, enough, nil
}

// validateInput rejects inputs that would score NaN or below the 1.0 floor.
func validateInput(byLanguage map[string]LanguageLOC, baseline *Baseline, formula Formula, decayD float64) error {
	switch formula {
	case "", FormulaCNCS, FormulaRatio:
	default:
		return fmt.Errorf("prcomplexity: unknown formula %q (want %q or %q)", formula, FormulaCNCS, FormulaRatio)
	}
	if !(decayD >= 0 && decayD <= 1) { // also rejects NaN
		return fmt.Errorf("prcomplexity: decay %v outside [0, 1]", decayD)
	}
	for lang, loc := range byLanguage {
		if loc.SLOC < 0 || loc.Files < 0 {
			return fmt.Errorf("prcomplexity: negative size for %q: %d SLOC, %d files", lang, loc.SLOC, loc.Files)
		}
	}
	if baseline == nil {
		return nil
	}
	for lang, a := range baseline.PerLanguage {
		for _, v := range []float64{a.MuLogSLOC, a.SigmaLogSLOC, a.MuLogFiles, a.SigmaLogFiles} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("prcomplexity: baseline anchor for %q has a non-finite log statistic", lang)
			}
		}
	}
	return nil
}

// scoreCNCS scores using the log-normal z-score sigmoid per language,
// combined across languages with the same geometric rank-decay rule as the ratio formula.
// Returns (nil, false) when no code-only language has log stats.
func scoreCNCS(slices map[string]LanguageLOC, anchors map[string]LanguageStats, decayD float64) (*float64, bool) {
	var sliceScores []float64
	for lang, loc := range slices {
		a, ok := anchors[lang]
		if !ok || a.MuLogSLOC == 0 {
			continue // no log stats for this language: skip the slice
		}
		sigmaLogSLOC := a.SigmaLogSLOC
		if sigmaLogSLOC < sigmaLogSLOCMin || sigmaLogSLOC > sigmaLogSLOCMax {
			sigmaLogSLOC = sigmaLogSLOCFallback
		}
		sigmaLogFiles := a.SigmaLogFiles
		if sigmaLogFiles < sigmaLogFilesMin || sigmaLogFiles > sigmaLogFilesMax {
			sigmaLogFiles = sigmaLogFilesFallback
		}
		zSLOC := (math.Log(float64(loc.SLOC)+1) - a.MuLogSLOC) / sigmaLogSLOC
		zFiles := (math.Log(float64(loc.Files)+1) - a.MuLogFiles) / sigmaLogFiles
		sliceScores = append(sliceScores, combineSlice(sigmoidScore(zSLOC), sigmoidScore(zFiles)))
	}
	if len(sliceScores) == 0 {
		return nil, false // no language had log stats: unscoreable
	}
	score := round1(combineAcrossLanguages(sliceScores, decayD))
	return &score, true
}

// scoreRatio scores using the per-language median ratio formula.
// Returns (nil, false) when no code-only language has a usable median anchor.
func scoreRatio(slices map[string]LanguageLOC, anchors map[string]LanguageStats, decayD float64) (*float64, bool) {
	var sliceScores []float64
	for lang, loc := range slices {
		a, ok := anchors[lang]
		if !ok || a.MedianSLOC <= 0 || a.MedianFiles <= 0 {
			continue // no usable anchor for this language → drop the slice
		}
		sSLOC := scoreFn(float64(loc.SLOC), float64(a.MedianSLOC))
		sFiles := scoreFn(float64(loc.Files), float64(a.MedianFiles))
		sliceScores = append(sliceScores, combineSlice(sSLOC, sFiles))
	}
	if len(sliceScores) == 0 {
		return nil, false // no anchored language: unscoreable
	}
	score := round1(combineAcrossLanguages(sliceScores, decayD))
	return &score, true
}

// round1 rounds to one decimal place. math.Round is half-away-from-zero.
func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
