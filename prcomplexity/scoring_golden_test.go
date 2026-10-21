// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prcomplexity

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/diffsize"
)

// Golden cases through the effective-LOC bridge and the cncs formula. Baseline
// anchors are set at mu_log = log(median+1) with the fallback sigmas (2.0 /
// 0.8), so the scores are the cncs sigmoid's. Languages are limited to Go and
// TypeScript (both enry Programming, kept by the code-only filter).

func effectiveLOC(langName string, sloc int64, files int) *diffsize.EffectiveLOC {
	return &diffsize.EffectiveLOC{
		ByLanguage: map[string]*diffsize.LanguageDetail{
			langName: {SourceLinesAdded: sloc, FilesChanged: files},
		},
	}
}

// baselineAtMedian builds a baseline whose log stats are anchored at the
// per-language medians: mu_log = log(median+1), sigma = the fallback values.
// A PR at the median scores 5.5 (z=0).
func baselineAtMedian(langName string, medSloc, medFiles int) *Baseline {
	return &Baseline{
		PerLanguage: map[string]LanguageStats{
			langName: {
				MedianSLOC: medSloc, MedianFiles: medFiles,
				MuLogSLOC: math.Log(float64(medSloc) + 1), SigmaLogSLOC: 2.0,
				MuLogFiles: math.Log(float64(medFiles) + 1), SigmaLogFiles: 0.8,
			},
		},
	}
}

func TestScoreComplexity_ScoringGolden(t *testing.T) {
	tests := []struct {
		name      string
		lang      string
		sloc      int64
		files     int
		medSloc   int
		medFiles  int
		wantScore float64
	}{
		{"TypeScript, about 6x median SLOC → 8.1", "TypeScript", 221, 3, 35, 2, 8.1},
		{"Go, about 2x median SLOC → 6.4", "Go", 40, 2, 21, 2, 6.4},
		{"Go, just under median SLOC, half the files → 4.9", "Go", 18, 1, 21, 2, 4.9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			byLang := FromEffectiveLOC(effectiveLOC(tt.lang, tt.sloc, tt.files))
			score, enough, err := ScoreComplexity("diff-ref", byLang, baselineAtMedian(tt.lang, tt.medSloc, tt.medFiles), FormulaCNCS, DefaultDecay)
			require.NoError(t, err)
			require.True(t, enough)
			require.NotNil(t, score)
			require.InDelta(t, tt.wantScore, *score, 1e-9)
		})
	}
}
