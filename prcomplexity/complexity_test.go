// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prcomplexity

import (
	"math"
	"math/big"
	"math/rand/v2"
	"testing"
)

const eps = 1e-9

func approx(a, b float64) bool { return math.Abs(a-b) < eps }

func TestSigmoidScore(t *testing.T) {
	tests := []struct {
		name string
		z    float64
		want float64
	}{
		{"at mean (z=0)", 0.0, 5.5},
		{"1σ above", 1.0, 8.6},
		{"1σ below", -1.0, 2.4},
		{"2σ above", 2.0, 9.7},
		{"2σ below", -2.0, 1.3},
		{"extreme positive capped", 100.0, 10.0},
		{"extreme negative capped", -100.0, 1.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := round1(sigmoidScore(tt.z)); !approx(got, tt.want) {
				t.Fatalf("round1(sigmoidScore(%v))=%v, want %v", tt.z, got, tt.want)
			}
		})
	}
}

func TestCombineSlice8020(t *testing.T) {
	if got := combineSlice(10, 5); !approx(got, 0.8*10+0.2*5) {
		t.Fatalf("combineSlice(10,5)=%v", got)
	}
	if got := combineSlice(5, 10); !approx(got, 0.8*10+0.2*5) {
		t.Fatalf("combineSlice(5,10)=%v (must equal the reversed args)", got)
	}
}

func TestMedian(t *testing.T) {
	tests := []struct {
		name string
		in   []int
		want int
	}{
		{"empty", nil, 0},
		{"single", []int{7}, 7},
		{"odd", []int{3, 1, 2}, 2},
		{"even rounds half up", []int{1, 2, 3, 4}, 3},
		{"even exact", []int{10, 20}, 15},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Median(tt.in); got != tt.want {
				t.Fatalf("Median(%v)=%d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsCodeLanguage(t *testing.T) {
	tests := []struct {
		lang string
		want bool
	}{
		{"Go", true},
		{"TypeScript", true},
		{"Protocol Buffer", true},
		{"HTML", true},
		{"HCL", true},
		{"Terraform", false},
		{"JSON", false},
		{"YAML", false},
		{"Markdown", false},
		{"Other", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			if got := IsCodeLanguage(tt.lang); got != tt.want {
				t.Fatalf("IsCodeLanguage(%q)=%v, want %v", tt.lang, got, tt.want)
			}
		})
	}
}

// langStats builds a LanguageStats with CNCS log stats anchored so that a PR
// at (medSloc, medFiles) has z=0 in both dims → score 5.5.
func langStats(lang string, medSloc, medFiles int, sigmaLogSLOC, sigmaLogFiles float64) namedStats {
	return namedStats{lang, LanguageStats{
		MedianSLOC:    medSloc,
		MedianFiles:   medFiles,
		MuLogSLOC:     math.Log(float64(medSloc) + 1),
		SigmaLogSLOC:  sigmaLogSLOC,
		MuLogFiles:    math.Log(float64(medFiles) + 1),
		SigmaLogFiles: sigmaLogFiles,
	}}
}

// namedStats pairs a language with its anchor, for building test baselines.
type namedStats struct {
	lang  string
	stats LanguageStats
}

// anchors wraps language stats into a Baseline.
func anchors(langs ...namedStats) *Baseline {
	b := &Baseline{PerLanguage: map[string]LanguageStats{}}
	for _, l := range langs {
		b.PerLanguage[l.lang] = l.stats
	}
	return b
}

func TestScoreComplexity(t *testing.T) {
	goAnchor := langStats("Go", 100, 10, 2.0, 0.8)
	atMedianAnchors := anchors(goAnchor)
	const decayD = 0.3

	tests := []struct {
		name       string
		diffRef    string
		byLang     map[string]LanguageLOC
		anchors    *Baseline
		wantScore  *float64
		wantEnough bool
	}{
		{
			name:       "TOO_LARGE -> flat 7.0",
			diffRef:    DiffTooLarge,
			anchors:    atMedianAnchors,
			wantScore:  ptr(7.0),
			wantEnough: true,
		},
		{
			name:       "docs/config-only (no code SLOC) -> unscoreable (nil, false)",
			diffRef:    "diff-ref",
			byLang:     map[string]LanguageLOC{"Markdown": {SLOC: 500, Files: 3}, "JSON": {SLOC: 40, Files: 1}},
			anchors:    atMedianAnchors,
			wantScore:  nil,
			wantEnough: false,
		},
		{
			// Pure-deletion PR: a code language touched but zero added SLOC. CodeSlices
			// drops it (requires SLOC > 0), leaving no slice to anchor → unscoreable.
			name:       "pure-deletion (code language, zero added SLOC) -> unscoreable (nil, false)",
			diffRef:    "diff-ref",
			byLang:     map[string]LanguageLOC{"Go": {SLOC: 0, Files: 2}},
			anchors:    atMedianAnchors,
			wantScore:  nil,
			wantEnough: false,
		},
		{
			// Go at mu → z=0 in both dims → score 5.5
			name:       "at log-mean in both dims -> 5.5",
			diffRef:    "diff-ref",
			byLang:     map[string]LanguageLOC{"Go": {SLOC: 100, Files: 10}},
			anchors:    atMedianAnchors,
			wantScore:  ptr(5.5),
			wantEnough: true,
		},
		{
			// Two languages each at their own mean: Go{50,5} with Go anchor at med=50,5;
			// TypeScript{50,5} with TS anchor at med=50,5.
			// Each slice: z=0 → score 5.5. combineAcrossLanguages([5.5,5.5], 0.3)=5.5+1.65=7.15 → 7.2
			name:    "multi-language — each scored against its own anchor",
			diffRef: "diff-ref",
			byLang:  map[string]LanguageLOC{"Go": {SLOC: 50, Files: 5}, "TypeScript": {SLOC: 50, Files: 5}},
			anchors: anchors(
				langStats("Go", 50, 5, 2.0, 0.8),
				langStats("TypeScript", 50, 5, 2.0, 0.8),
			),
			wantScore:  ptr(7.2),
			wantEnough: true,
		},
		{
			// Language with no anchor in the window is dropped; only Go scores.
			name:       "language with no anchor dropped — score from Go only",
			diffRef:    "diff-ref",
			byLang:     map[string]LanguageLOC{"Go": {SLOC: 100, Files: 10}, "HCL": {SLOC: 400, Files: 9}},
			anchors:    atMedianAnchors, // no HCL anchor
			wantScore:  ptr(5.5),        // HCL slice dropped
			wantEnough: true,
		},
		{
			// method.md's example: the files dimension dominates through the 80/20 rule.
			// Files z=(ln 11-1.41)/0.37=2.67 -> 9.90; SLOC z=(ln 6-4.36)/0.92=-2.79 -> 1.08;
			// 0.8*9.90 + 0.2*1.08 = 8.14 -> 8.1.
			name:    "5 lines over 10 files -> 8.1, promoted by the file count",
			diffRef: "diff-ref",
			byLang:  map[string]LanguageLOC{"Go": {SLOC: 5, Files: 10}},
			anchors: &Baseline{PerLanguage: map[string]LanguageStats{
				"Go": {MedianSLOC: 72, MedianFiles: 4, MuLogSLOC: 4.36, SigmaLogSLOC: 0.92, MuLogFiles: 1.41, SigmaLogFiles: 0.37},
			}},
			wantScore:  ptr(8.1),
			wantEnough: true,
		},
		{
			// diffsize's FilesChanged Declined limit: 3 added lines in one file plus 12
			// deletion-only files is 13 files, against 1 file without them (1.3).
			name:    "3 lines over 13 files (12 deletion-only) -> 8.2",
			diffRef: "diff-ref",
			byLang:  map[string]LanguageLOC{"Go": {SLOC: 3, Files: 13}},
			anchors: &Baseline{PerLanguage: map[string]LanguageStats{
				"Go": {MedianSLOC: 72, MedianFiles: 4, MuLogSLOC: 4.36, SigmaLogSLOC: 0.92, MuLogFiles: 1.41, SigmaLogFiles: 0.37},
			}},
			wantScore:  ptr(8.2),
			wantEnough: true,
		},
		{
			name:    "3 lines in 1 file -> 1.3",
			diffRef: "diff-ref",
			byLang:  map[string]LanguageLOC{"Go": {SLOC: 3, Files: 1}},
			anchors: &Baseline{PerLanguage: map[string]LanguageStats{
				"Go": {MedianSLOC: 72, MedianFiles: 4, MuLogSLOC: 4.36, SigmaLogSLOC: 0.92, MuLogFiles: 1.41, SigmaLogFiles: 0.37},
			}},
			wantScore:  ptr(1.3),
			wantEnough: true,
		},
		{
			name:       "NOT_AVAILABLE diff -> unscoreable (nil, false)",
			diffRef:    DiffNotAvailable,
			byLang:     nil,
			anchors:    atMedianAnchors,
			wantScore:  nil,
			wantEnough: false,
		},
		{
			// An anchor with medians but MuLogSLOC == 0 has no log stats.
			name:    "anchor without log stats -> unscoreable (nil, false)",
			diffRef: "diff-ref",
			byLang:  map[string]LanguageLOC{"Go": {SLOC: 100, Files: 10}},
			anchors: anchors(namedStats{"Go", LanguageStats{
				MedianSLOC: 100, MedianFiles: 10, // no MuLogSLOC
			}}),
			wantScore:  nil,
			wantEnough: false,
		},
		{
			// sigma outside [0.8,3.0] → falls back to 2.0; z=0 still → score 5.5
			name:       "sigma_sloc out of range -> global fallback used, z=0 unchanged",
			diffRef:    "diff-ref",
			byLang:     map[string]LanguageLOC{"Go": {SLOC: 100, Files: 10}},
			anchors:    anchors(langStats("Go", 100, 10, 0.5, 0.8)), // sigma_sloc=0.5 < 0.8
			wantScore:  ptr(5.5),
			wantEnough: true,
		},
		{
			name:       "sigma_files out of range -> global fallback used, z=0 unchanged",
			diffRef:    "diff-ref",
			byLang:     map[string]LanguageLOC{"Go": {SLOC: 100, Files: 10}},
			anchors:    anchors(langStats("Go", 100, 10, 2.0, 0.1)), // sigma_files=0.1 < 0.3
			wantScore:  ptr(5.5),
			wantEnough: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, enough, err := ScoreComplexity(tt.diffRef, tt.byLang, tt.anchors, FormulaCNCS, decayD)
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if enough != tt.wantEnough {
				t.Fatalf("enoughData=%v, want %v", enough, tt.wantEnough)
			}
			switch {
			case tt.wantScore == nil && got != nil:
				t.Fatalf("score=%v, want nil", *got)
			case tt.wantScore != nil && got == nil:
				t.Fatalf("score=nil, want %v", *tt.wantScore)
			case tt.wantScore != nil && got != nil && !approx(*got, *tt.wantScore):
				t.Fatalf("score=%v, want %v", *got, *tt.wantScore)
			}
		})
	}
}

// TestCombineAcrossLanguages pins the geometric rank-decay invariants independently
// of the end-to-end ScoreComplexity path. Scores here are raw (un-rounded).
func TestCombineAcrossLanguages(t *testing.T) {
	tests := []struct {
		name   string
		scores []float64
		decayD float64
		want   float64
	}{
		{"single — passthrough unchanged", []float64{7.5}, 0.3, 7.5},
		{"two equal at mean, d=0.3", []float64{5.5, 5.5}, 0.3, 5.5 + 0.3*5.5},
		{"input order irrelevant — sorts desc", []float64{3.0, 8.0}, 0.3, 8.0 + 0.3*3.0},
		{"three equal, d=0.3", []float64{5.5, 5.5, 5.5}, 0.3, 5.5 + 0.3*5.5 + 0.09*5.5},
		{"cap at 10 — two near-max", []float64{9.5, 9.5}, 0.3, 10.0},
		{"decay=0 — only first contributes", []float64{5.5, 7.0, 8.0}, 0.0, 8.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := combineAcrossLanguages(tt.scores, tt.decayD)
			if !approx(got, tt.want) {
				t.Fatalf("combineAcrossLanguages(%v, d=%v)=%v, want %v", tt.scores, tt.decayD, got, tt.want)
			}
		})
	}
}

func TestScoreComplexity_RatioFormula(t *testing.T) {
	const decayD = 0.3
	goAnchor := namedStats{"Go", LanguageStats{
		MedianSLOC: 100, MedianFiles: 10,
	}}
	tests := []struct {
		name       string
		byLang     map[string]LanguageLOC
		anchors    *Baseline
		wantScore  *float64
		wantEnough bool
	}{
		{
			name:       "at median -> 5.0",
			byLang:     map[string]LanguageLOC{"Go": {SLOC: 100, Files: 10}},
			anchors:    anchors(goAnchor),
			wantScore:  ptr(5.0),
			wantEnough: true,
		},
		{
			name:       "no anchor for language -> unscoreable",
			byLang:     map[string]LanguageLOC{"HCL": {SLOC: 100, Files: 5}},
			anchors:    anchors(goAnchor),
			wantScore:  nil,
			wantEnough: false,
		},
		{
			// An anchor carrying both log stats and medians scores under the ratio
			// formula from its medians alone, so one baseline serves both formulas.
			name:   "anchor with log stats and medians under the ratio formula",
			byLang: map[string]LanguageLOC{"Go": {SLOC: 100, Files: 10}},
			anchors: anchors(namedStats{"Go", LanguageStats{
				MedianSLOC: 100, MedianFiles: 10,
				MuLogSLOC: math.Log(101), SigmaLogSLOC: 2.0,
				MuLogFiles: math.Log(11), SigmaLogFiles: 0.8,
			}}),
			wantScore:  ptr(5.0), // at median → ratio = 1.0 → scoreFn → 5.0
			wantEnough: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, enough, err := ScoreComplexity("diff-ref", tt.byLang, tt.anchors, FormulaRatio, decayD)
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if enough != tt.wantEnough {
				t.Fatalf("enoughData=%v, want %v", enough, tt.wantEnough)
			}
			switch {
			case tt.wantScore == nil && got != nil:
				t.Fatalf("score=%v, want nil", *got)
			case tt.wantScore != nil && got == nil:
				t.Fatalf("score=nil, want %v", *tt.wantScore)
			case tt.wantScore != nil && got != nil && !approx(*got, *tt.wantScore):
				t.Fatalf("score=%v, want %v", *got, *tt.wantScore)
			}
		})
	}
}

func ptr(f float64) *float64 { return &f }

// TestScoreComplexity_InvalidInputIsAnError proves inputs that would score NaN
// or fall below the 1.0 floor are rejected instead of reported as scoreable.
// A NaN or negative decay changes the output only with two or more slices, so
// those cases score two languages.
func TestScoreComplexity_InvalidInputIsAnError(t *testing.T) {
	good := langStats("Go", 100, 10, 2.0, 0.8)
	two := anchors(good, langStats("Python", 100, 10, 2.0, 0.8))
	twoLangs := map[string]LanguageLOC{"Go": {SLOC: 100, Files: 10}, "Python": {SLOC: 100, Files: 10}}
	nan := math.NaN()
	tests := []struct {
		name     string
		byLang   map[string]LanguageLOC
		baseline *Baseline
		decay    float64
	}{
		{"negative files", map[string]LanguageLOC{"Go": {SLOC: 100, Files: -2}}, anchors(good), DefaultDecay},
		{"negative SLOC", map[string]LanguageLOC{"Go": {SLOC: -5, Files: 2}}, anchors(good), DefaultDecay},
		{"NaN decay, two slices", twoLangs, two, nan},
		{"negative decay, two slices", twoLangs, two, -1},
		{"decay above 1, two slices", twoLangs, two, 1.5},
		{"infinite decay", twoLangs, two, math.Inf(1)},
		{"NaN mu in baseline", map[string]LanguageLOC{"Go": {SLOC: 100, Files: 10}},
			anchors(namedStats{"Go", LanguageStats{MedianSLOC: 100, MedianFiles: 10, MuLogSLOC: nan, SigmaLogSLOC: 2, MuLogFiles: 1, SigmaLogFiles: 0.8}}), DefaultDecay},
		{"infinite sigma in baseline", map[string]LanguageLOC{"Go": {SLOC: 100, Files: 10}},
			anchors(namedStats{"Go", LanguageStats{MedianSLOC: 100, MedianFiles: 10, MuLogSLOC: 4, SigmaLogSLOC: 2, MuLogFiles: 1, SigmaLogFiles: math.Inf(1)}}), DefaultDecay},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, f := range []Formula{FormulaCNCS, FormulaRatio} {
				got, enough, err := ScoreComplexity("diff-ref", tt.byLang, tt.baseline, f, tt.decay)
				if err == nil {
					t.Fatalf("formula %s: err=nil, score=%v enough=%v", f, deref(got), enough)
				}
				if got != nil || enough {
					t.Fatalf("formula %s: score=%v enough=%v alongside err", f, deref(got), enough)
				}
			}
		})
	}

	// The decay bounds are inclusive.
	for _, d := range []float64{0, 1} {
		if _, _, err := ScoreComplexity("diff-ref", twoLangs, two, FormulaCNCS, d); err != nil {
			t.Fatalf("decay %v: err=%v", d, err)
		}
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// TestScoreComplexity_SigmaFallbackChangesTheScore proves an out-of-range
// sigma is replaced by the fallback away from z=0: the score equals the score
// with the fallback sigma itself.
func TestScoreComplexity_SigmaFallbackChangesTheScore(t *testing.T) {
	pr := map[string]LanguageLOC{"Go": {SLOC: 400, Files: 10}}
	for _, tc := range []struct{ bad, fallbackSLOC, badFiles, fallbackFiles float64 }{
		{0.5, 2.0, 0.8, 0.8}, // SLOC sigma below range
		{3.5, 2.0, 0.8, 0.8}, // SLOC sigma above range
		{2.0, 2.0, 0.1, 0.8}, // files sigma below range
		{2.0, 2.0, 2.5, 0.8}, // files sigma above range
	} {
		withBad, _, err := ScoreComplexity("x", pr, anchors(langStats("Go", 100, 4, tc.bad, tc.badFiles)), FormulaCNCS, DefaultDecay)
		if err != nil {
			t.Fatal(err)
		}
		withFallback, _, _ := ScoreComplexity("x", pr, anchors(langStats("Go", 100, 4, tc.fallbackSLOC, tc.fallbackFiles)), FormulaCNCS, DefaultDecay)
		if *withBad != *withFallback {
			t.Fatalf("sigma %v/%v: score %v, want the fallback's %v", tc.bad, tc.badFiles, *withBad, *withFallback)
		}
	}
}

// TestMedian_NoOverflow proves the middle pair's mean cannot overflow, and
// that every pair whose sum fits rounds (a+b)/2 half away from zero.
func TestMedian_NoOverflow(t *testing.T) {
	tests := []struct {
		in   []int
		want int
	}{
		{[]int{math.MaxInt, math.MaxInt - 1}, math.MaxInt},
		{[]int{math.MaxInt, math.MaxInt}, math.MaxInt},
		{[]int{math.MaxInt - 2, math.MaxInt}, math.MaxInt - 1},
		{[]int{math.MinInt, math.MinInt + 1}, math.MinInt},
		{[]int{math.MinInt, math.MinInt}, math.MinInt},
	}
	for _, tt := range tests {
		if got := Median(tt.in); got != tt.want {
			t.Errorf("Median(%v)=%d, want %d", tt.in, got, tt.want)
		}
	}
	for a := -40; a <= 40; a++ {
		for b := a; b <= 40; b++ {
			want := int(math.Round(float64(a+b) / 2.0))
			if got := Median([]int{a, b}); got != want {
				t.Fatalf("Median([%d %d])=%d, want %d", a, b, got, want)
			}
		}
	}
}

// TestScoreFn_RatioCurve pins the ratio formula's floor and its curve above
// the median.
func TestScoreFn_RatioCurve(t *testing.T) {
	tests := []struct{ ratio, want float64 }{
		{0.1, 1.0},
		{0.15, 1.0},
		{0.2, 1.0},
		{0.5, 2.5},
		{1, 5.0},
		{2, 7.5},
		{4, 8.75},
		{10, 9.5},
	}
	for _, tt := range tests {
		if got := scoreFn(tt.ratio*100, 100); !approx(got, tt.want) {
			t.Errorf("scoreFn at ratio %v = %v, want %v", tt.ratio, got, tt.want)
		}
	}
}

// TestScoreComplexity_SentinelsDoNotSkipValidation proves invalid input is an
// error even when diffRef is a sentinel.
func TestScoreComplexity_SentinelsDoNotSkipValidation(t *testing.T) {
	for _, ref := range []string{DiffTooLarge, DiffNotAvailable} {
		if _, _, err := ScoreComplexity(ref, nil, nil, "rato", DefaultDecay); err == nil {
			t.Errorf("%s with an unknown formula: err=nil", ref)
		}
		if _, _, err := ScoreComplexity(ref, nil, nil, FormulaCNCS, math.NaN()); err == nil {
			t.Errorf("%s with a NaN decay: err=nil", ref)
		}
	}
}

// TestMedian_ExactAbove2To53 proves the middle pair's mean keeps every bit
// where a float64 cannot hold the sum exactly.
func TestMedian_ExactAbove2To53(t *testing.T) {
	const p53 = 1 << 53
	tests := []struct {
		in   []int
		want int
	}{
		{[]int{p53 + 1, p53 + 1}, p53 + 1},
		{[]int{p53 + 1, p53 + 3}, p53 + 2},
		{[]int{-p53 - 1, -p53 - 1}, -p53 - 1},
		{[]int{p53 + 1, p53 + 2}, p53 + 2},
		{[]int{-p53 - 2, -p53 - 1}, -p53 - 2},
	}
	for _, tt := range tests {
		if got := Median(tt.in); got != tt.want {
			t.Errorf("Median(%v)=%d, want %d", tt.in, got, tt.want)
		}
	}
}

// TestMedian_Differential compares the middle pair's mean with two references
// over a large generated set of pairs: negatives, odd and even sums, every
// magnitude. Wherever a float64 holds a+b exactly, the result equals
// math.Round((a+b)/2). Everywhere, it equals the exact mean rounded half away
// from zero, computed with math/big.
func TestMedian_Differential(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	exact := func(a, b int) int {
		sum := new(big.Int).Add(big.NewInt(int64(a)), big.NewInt(int64(b)))
		q, r := new(big.Int).QuoRem(sum, big.NewInt(2), new(big.Int))
		if r.Sign() > 0 {
			q.Add(q, big.NewInt(1))
		} else if r.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		}
		return int(q.Int64())
	}
	floatChecked := 0
	for i := 0; i < 1_000_000; i++ {
		shift := rng.IntN(64)
		a := int(rng.Uint64() >> shift)
		b := int(rng.Uint64() >> shift)
		if rng.IntN(2) == 0 {
			a = -a
		}
		if rng.IntN(2) == 0 {
			b = -b
		}
		got := midpoint(a, b)
		if want := exact(a, b); got != want {
			t.Fatalf("midpoint(%d, %d)=%d, exact %d", a, b, got, want)
		}
		if a >= -(1<<52) && a <= 1<<52 && b >= -(1<<52) && b <= 1<<52 {
			floatChecked++
			if want := int(math.Round(float64(a+b) / 2.0)); got != want {
				t.Fatalf("midpoint(%d, %d)=%d, float rounding %d", a, b, got, want)
			}
		}
	}
	if floatChecked < 100_000 {
		t.Fatalf("only %d pairs in the float-exact range", floatChecked)
	}
}

// TestScoreComplexity_NotAvailableWithFootprint proves DiffNotAvailable is
// unscoreable even when the PR carries a footprint an anchor could score.
func TestScoreComplexity_NotAvailableWithFootprint(t *testing.T) {
	score, ok, err := ScoreComplexity(DiffNotAvailable, map[string]LanguageLOC{"Go": {SLOC: 100, Files: 10}},
		anchors(langStats("Go", 100, 10, 2.0, 0.8)), FormulaCNCS, DefaultDecay)
	if err != nil || score != nil || ok {
		t.Fatalf("got %v, %v, %v; want nil, false, nil", score, ok, err)
	}
}

// TestScoreComplexity_SigmaBoundsAreInclusive proves a sigma exactly at a
// bound of its valid range is used as is, not replaced by the fallback. The
// PR sits away from the medians, so z is not 0 and the sigma shows.
func TestScoreComplexity_SigmaBoundsAreInclusive(t *testing.T) {
	pr := map[string]LanguageLOC{"Go": {SLOC: 400, Files: 10}}
	score := func(sigmaSLOC, sigmaFiles float64) float64 {
		t.Helper()
		s, ok, err := ScoreComplexity("x", pr, anchors(langStats("Go", 100, 4, sigmaSLOC, sigmaFiles)), FormulaCNCS, DefaultDecay)
		if err != nil || !ok {
			t.Fatalf("sigma %v/%v: ok=%v err=%v", sigmaSLOC, sigmaFiles, ok, err)
		}
		return *s
	}
	for _, tc := range []struct {
		name                  string
		sigmaSLOC, sigmaFiles float64
		inside1, inside2      float64
		fallback1, fallback2  float64
	}{
		{"SLOC min", SigmaLogSLOCMin, 0.8, SigmaLogSLOCMin + 1e-9, 0.8, 2.0, 0.8},
		{"SLOC max", SigmaLogSLOCMax, 0.8, SigmaLogSLOCMax - 1e-9, 0.8, 2.0, 0.8},
		{"files min", 2.0, SigmaLogFilesMin, 2.0, SigmaLogFilesMin + 1e-9, 2.0, 0.8},
		{"files max", 2.0, SigmaLogFilesMax, 2.0, SigmaLogFilesMax - 1e-9, 2.0, 0.8},
	} {
		atBound := score(tc.sigmaSLOC, tc.sigmaFiles)
		if inside := score(tc.inside1, tc.inside2); atBound != inside {
			t.Errorf("%s: score %v at the bound, %v just inside it", tc.name, atBound, inside)
		}
		if fallback := score(tc.fallback1, tc.fallback2); atBound == fallback {
			t.Errorf("%s: score %v at the bound equals the fallback's", tc.name, atBound)
		}
	}
}

// TestScoreComplexity_RatioZeroMedianFilesIsUnscoreable proves the ratio
// formula drops an anchor whose files median is 0, instead of dividing by it.
func TestScoreComplexity_RatioZeroMedianFilesIsUnscoreable(t *testing.T) {
	b := &Baseline{PerLanguage: map[string]LanguageStats{"Go": {MedianSLOC: 100, MedianFiles: 0}}}
	score, ok, err := ScoreComplexity("x", map[string]LanguageLOC{"Go": {SLOC: 100, Files: 3}}, b, FormulaRatio, DefaultDecay)
	if err != nil || score != nil || ok {
		t.Fatalf("got %v, %v, %v; want nil, false, nil", score, ok, err)
	}
}
