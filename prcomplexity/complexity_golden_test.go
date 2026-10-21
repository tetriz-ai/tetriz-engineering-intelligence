// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prcomplexity

import (
	"math"
	"testing"
)

// Golden dataset for the cncs formula. All expected values are derived from
// s = clamp(1 + 9/(1 + exp(-1.702*z)), 1, 10), z = (log(value+1) - mu_log) / sigma_log.

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// TestSigmoidScore_CNCSGolden: z → s, round-tripped to 2dp.
func TestSigmoidScore_CNCSGolden(t *testing.T) {
	tests := []struct {
		z, want float64
	}{
		{0.0, 5.50},  // 50th percentile
		{1.0, 8.61},  // ~84th percentile; 1 + 9/(1+e^{-1.702}) ≈ 8.6124
		{-1.0, 2.39}, // ~16th percentile; 1 + 9/(1+e^{1.702}) ≈ 2.3878
		{2.0, 9.71},  // ~98th percentile
		{-2.0, 1.29}, // ~2nd percentile
		{0.5, 7.31},  // 1 + 9/(1+e^{-0.851}) ≈ 7.3143
		{-0.5, 3.69}, // 1 + 9/(1+e^{0.851}) ≈ 3.6857
	}
	for _, tt := range tests {
		if got := round2(sigmoidScore(tt.z)); !approx(got, tt.want) {
			t.Fatalf("round2(sigmoidScore(%v))=%v, want %v", tt.z, got, tt.want)
		}
	}
}

// TestCombineSlice_8020Golden: SLOC/files scores → sub-PR score via 80/20 rule.
func TestCombineSlice_8020Golden(t *testing.T) {
	tests := []struct {
		sSLOC, sFiles, want float64
	}{
		{8.61, 5.50, 7.99}, // 0.8*8.61 + 0.2*5.50
		{5.50, 8.61, 7.99}, // order-independent
		{5.50, 5.50, 5.50}, // both at mean
		{9.71, 5.50, 8.87}, // 0.8*9.71 + 0.2*5.50
		{2.39, 5.50, 4.88}, // 0.8*5.50 + 0.2*2.39 (files dominant)
	}
	for _, tt := range tests {
		if got := round2(combineSlice(tt.sSLOC, tt.sFiles)); !approx(got, tt.want) {
			t.Fatalf("combineSlice(%v,%v)=%v, want %v", tt.sSLOC, tt.sFiles, got, tt.want)
		}
	}
}

// TestScoreComplexity_CNCSGolden: full pipeline validated against formula.
func TestScoreComplexity_CNCSGolden(t *testing.T) {
	// Single Go anchor: mu_log = log(101), sigma = 2.0 → SLOC=100 gives z=0.
	//                  mu_log = log(11),  sigma = 0.8 → Files=10 gives z=0.
	goOnlyAnchor := &Baseline{
		PerLanguage: map[string]LanguageStats{
			"Go": {MuLogSLOC: math.Log(101), SigmaLogSLOC: 2.0, MuLogFiles: math.Log(11), SigmaLogFiles: 0.8},
		},
	}
	// Two-language anchor: each at med=50,5 → z=0 → sliceScore=5.5 each.
	// combineAcrossLanguages([5.5,5.5], 0.3) = 5.5 + 0.3×5.5 = 7.15 → 7.2
	twoLangAnchor := &Baseline{
		PerLanguage: map[string]LanguageStats{
			"Go":         {MuLogSLOC: math.Log(51), SigmaLogSLOC: 2.0, MuLogFiles: math.Log(6), SigmaLogFiles: 0.8},
			"TypeScript": {MuLogSLOC: math.Log(51), SigmaLogSLOC: 2.0, MuLogFiles: math.Log(6), SigmaLogFiles: 0.8},
		},
	}

	const decayD = 0.3

	tests := []struct {
		name       string
		anchor     *Baseline
		byLang     map[string]LanguageLOC
		wantScore  *float64
		wantEnough bool
	}{
		{
			// z_sloc=0, z_files=0 → both at mean → 5.5
			name:      "at log-mean → 5.5",
			anchor:    goOnlyAnchor,
			byLang:    map[string]LanguageLOC{"Go": {SLOC: 100, Files: 10}},
			wantScore: ptr(5.5), wantEnough: true,
		},
		{
			// SLOC=745 → log(746)≈log(101)+2 → z_sloc≈1 → s_sloc≈8.61
			// Files=10 → z_files=0 → s_files=5.5
			// 0.8*8.61+0.2*5.5 ≈ 7.99 → 8.0
			name:      "z_sloc≈1, z_files=0 → 8.0",
			anchor:    goOnlyAnchor,
			byLang:    map[string]LanguageLOC{"Go": {SLOC: 745, Files: 10}},
			wantScore: ptr(8.0), wantEnough: true,
		},
		{
			// SLOC=10 → log(11)=log(101)-log(101/11) ≈ log(101)-2.22 → z_sloc≈-1.11
			// s_sloc ≈ 2.17, z_files=0 → s_files=5.5
			// combineSlice(5.5, 2.17) = 0.8*5.5+0.2*2.17 = 4.83 → 4.8
			name:      "small PR (SLOC=10) → 4.8",
			anchor:    goOnlyAnchor,
			byLang:    map[string]LanguageLOC{"Go": {SLOC: 10, Files: 10}},
			wantScore: ptr(4.8), wantEnough: true,
		},
		{
			// Go{50,5} and TypeScript{50,5}, each anchored at their own median.
			// Each: z=0 → sliceScore=5.5. combineAcrossLanguages([5.5,5.5], 0.3) = 7.15 → 7.2.
			name:      "Go+TypeScript each scored per-language → 7.2",
			anchor:    twoLangAnchor,
			byLang:    map[string]LanguageLOC{"Go": {SLOC: 50, Files: 5}, "TypeScript": {SLOC: 50, Files: 5}},
			wantScore: ptr(7.2), wantEnough: true,
		},
		{
			name:      "docs/config-only → unscoreable (nil score)",
			anchor:    goOnlyAnchor,
			byLang:    map[string]LanguageLOC{"JSON": {SLOC: 200, Files: 5}, "YAML": {SLOC: 50, Files: 2}},
			wantScore: nil, wantEnough: false,
		},
		{
			name:      "baseline with no anchors → unscoreable",
			anchor:    &Baseline{}, // no PerLanguage → no anchors
			byLang:    map[string]LanguageLOC{"Go": {SLOC: 100, Files: 10}},
			wantScore: nil, wantEnough: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, enough, err := ScoreComplexity("diff-ref", tt.byLang, tt.anchor, FormulaCNCS, decayD)
			if err != nil {
				t.Fatalf("err=%v", err)
			}
			if enough != tt.wantEnough {
				t.Fatalf("enough=%v want %v", enough, tt.wantEnough)
			}
			switch {
			case tt.wantScore == nil && got != nil:
				t.Fatalf("score=%v want nil", *got)
			case tt.wantScore != nil && got == nil:
				t.Fatalf("score=nil want %v", *tt.wantScore)
			case tt.wantScore != nil && !approx(*got, *tt.wantScore):
				t.Fatalf("score=%v want %v", *got, *tt.wantScore)
			}
		})
	}
}
