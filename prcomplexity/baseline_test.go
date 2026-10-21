// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prcomplexity

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/diffsize"
)

// testOptions are small gates so a three-PR sample is admitted.
var testOptions = BaselineOptions{MinSampleSize: 2, CoverageThresholdBps: 5000}

// analyzedPR builds an analyzed merged PR whose footprint is a single language.
func analyzedPR(language string, sloc int64, files int) SamplePR {
	return SamplePR{
		DiffRef:    "diff-ref",
		Analyzed:   true,
		ByLanguage: map[string]LanguageLOC{language: {SLOC: sloc, Files: files}},
	}
}

// sentinelPR builds a merged PR whose diff resolved to a sentinel (TOO_LARGE /
// NOT_AVAILABLE): not analyzed, no footprint.
func sentinelPR(sentinel string) SamplePR {
	return SamplePR{DiffRef: sentinel}
}

func TestUsableDiff(t *testing.T) {
	tests := []struct {
		name string
		pr   SamplePR
		want bool
	}{
		{name: "no diff reference", pr: SamplePR{}, want: false},
		{
			name: "TOO_LARGE counts (scoreable at 7.0, no footprint)",
			pr:   sentinelPR(DiffTooLarge),
			want: true,
		},
		{
			name: "NOT_AVAILABLE excluded (unscoreable)",
			pr:   sentinelPR(DiffNotAvailable),
			want: false,
		},
		{
			name: "analyzed real diff counts",
			pr:   analyzedPR("Go", 100, 10),
			want: true,
		},
		{
			name: "real diff but not yet analyzed",
			pr:   SamplePR{DiffRef: "diff-ref"},
			want: false,
		},
		{
			name: "NOT_AVAILABLE marked analyzed stays excluded",
			pr:   SamplePR{DiffRef: DiffNotAvailable, Analyzed: true},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, usableDiff(tt.pr))
		})
	}
}

func TestBuildBaseline_GatePassesWithAnchors(t *testing.T) {
	sample := []SamplePR{
		analyzedPR("Go", 100, 10),
		analyzedPR("Go", 120, 12),
		analyzedPR("Go", 80, 8),
	}
	// Gate passes (n=3 > 2, coverage=100% > 50%, Go has 3 ≥ 2 with median>0).
	b, err := BuildBaseline(sample, testOptions)
	require.NoError(t, err)
	require.Equal(t, 3, b.SampleSize)
	require.Equal(t, 10000, b.CoverageBps)
	require.Len(t, b.PerLanguage, 1)
	goStats, ok := b.PerLanguage["Go"]
	require.True(t, ok)
	require.Equal(t, 100, goStats.MedianSLOC) // median(80,100,120)
	// Log stats: 3 PRs, p95 = sorted[2] = 120 (n*95/100 = 2),
	// logs = [log(81), log(101), log(121)], mu and sigma computed from those.
	require.Greater(t, goStats.MuLogSLOC, 0.0, "MuLogSLOC must be computed")
	require.Greater(t, goStats.SigmaLogSLOC, 0.0, "SigmaLogSLOC must be computed")
	require.Greater(t, goStats.MuLogFiles, 0.0, "MuLogFiles must be computed")
	require.Greater(t, goStats.SigmaLogFiles, 0.0, "SigmaLogFiles must be computed")
}

// TestBuildBaseline_TooLargeCountsTowardCoverage proves a TOO_LARGE PR is in the
// coverage numerator (it is scoreable) while a NOT_AVAILABLE PR is not, and that
// neither perturbs the per-language medians (neither has a footprint).
func TestBuildBaseline_TooLargeCountsTowardCoverage(t *testing.T) {
	// n=4: 2 analyzed Go PRs + 1 TOO_LARGE (counts coverage; no footprint so
	// not in the median) + 1 NOT_AVAILABLE (excluded) → diffOK=3, coverage = 7500 bps.
	sample := []SamplePR{
		analyzedPR("Go", 100, 10),
		analyzedPR("Go", 120, 12),
		sentinelPR(DiffTooLarge),
		sentinelPR(DiffNotAvailable),
	}
	b, err := BuildBaseline(sample, testOptions)
	require.NoError(t, err)
	require.Equal(t, 4, b.SampleSize)
	require.Equal(t, 3, b.DiffAvailableCount, "TOO_LARGE counts, NOT_AVAILABLE does not")
	require.Equal(t, 7500, b.CoverageBps)
	// Medians come only from the 2 analyzed Go PRs; sentinels add nothing.
	require.Len(t, b.PerLanguage, 1)
	require.Equal(t, 2, b.PerLanguage["Go"].SampleSize)
	require.Greater(t, b.PerLanguage["Go"].MuLogSLOC, 0.0, "MuLogSLOC must be computed from analyzed PRs")
	require.Greater(t, b.PerLanguage["Go"].SigmaLogSLOC, 0.0, "SigmaLogSLOC must be computed")
}

func TestBuildBaseline_GateFails_NotEnoughData(t *testing.T) {
	// Only 1 merged PR → n=1, not more than MinSampleSize(2) → gate fails.
	b, err := BuildBaseline([]SamplePR{analyzedPR("Go", 100, 10)}, testOptions)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNotEnoughData))
	require.Equal(t, 1, b.SampleSize)
	require.Nil(t, b.PerLanguage)
}

// TestBuildBaseline_NonCodeLanguageOnly_Admitted proves a sample that is
// entirely non-code (here JSON) is still admitted, with no anchors. Each PR is
// then decided on its own: a TOO_LARGE PR still scores 7.0, any other PR is
// unscoreable.
func TestBuildBaseline_NonCodeLanguageOnly_Admitted(t *testing.T) {
	b, err := BuildBaseline([]SamplePR{
		analyzedPR("JSON", 100, 10),
		analyzedPR("JSON", 120, 12),
		analyzedPR("JSON", 80, 8),
	}, testOptions)
	require.NoError(t, err)
	require.Equal(t, 3, b.SampleSize)
	require.Empty(t, b.PerLanguage, "non-code sample is admitted with no per-language anchor")

	// Positive control: the same JSON PRs beside three Go PRs anchor Go, and
	// still not JSON.
	b, err = BuildBaseline([]SamplePR{
		analyzedPR("JSON", 100, 10),
		analyzedPR("JSON", 120, 12),
		analyzedPR("JSON", 80, 8),
		analyzedPR("Go", 100, 10),
		analyzedPR("Go", 120, 12),
		analyzedPR("Go", 80, 8),
	}, testOptions)
	require.NoError(t, err)
	require.Contains(t, b.PerLanguage, "Go")
	require.NotContains(t, b.PerLanguage, "JSON")
}

// TestBuildBaseline_ProtocolBufferCountsAsCode proves a Protocol Buffer sample
// (enry type Data, but real source) produces a usable per-language median.
func TestBuildBaseline_ProtocolBufferCountsAsCode(t *testing.T) {
	b, err := BuildBaseline([]SamplePR{
		analyzedPR("Protocol Buffer", 100, 10),
		analyzedPR("Protocol Buffer", 120, 12),
		analyzedPR("Protocol Buffer", 80, 8),
	}, testOptions)
	require.NoError(t, err)
	require.Len(t, b.PerLanguage, 1)
	require.Equal(t, 100, b.PerLanguage["Protocol Buffer"].MedianSLOC) // median(80,100,120)
}

func TestLogStatsFromSample(t *testing.T) {
	const eps = 1e-9

	tests := []struct {
		name      string
		values    []int
		wantMu    float64
		wantSigma float64
		wantP95   int
	}{
		{
			// sigma=0 for an all-equal sample makes the scorer's sigma guard
			// fire: sigma outside [SigmaLogSLOCMin, SigmaLogSLOCMax] → fallback.
			name:      "all-equal → sigma=0",
			values:    []int{100, 100, 100},
			wantMu:    math.Log(101),
			wantSigma: 0,
			wantP95:   100,
		},
		{
			// logs=[log(1), log(10001)], symmetric about mu → sigma == mu == log(10001)/2.
			name:      "two-value spread — analytically verifiable",
			values:    []int{0, 10000},
			wantMu:    math.Log(10001) / 2,
			wantSigma: math.Log(10001) / 2,
			wantP95:   10000,
		},
		{
			// 21 values: 19 ones + 50 + 9999. p95Idx=19 → p95=50; 9999 is winsorized to 50.
			// Expected mu = (19·log(2) + 2·log(51)) / 21 — lower than uncapped (≈1.253),
			// proving the outlier did not inflate the mean.
			name:   "winsorization caps top-end outlier",
			values: []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 50, 9999},
			wantMu: (19*math.Log(2) + 2*math.Log(51)) / 21,
			wantSigma: func() float64 {
				mu := (19*math.Log(2) + 2*math.Log(51)) / 21
				d1, d2 := math.Log(2)-mu, math.Log(51)-mu
				return math.Sqrt((19*d1*d1 + 2*d2*d2) / 21)
			}(),
			wantP95: 50,
		},
		{
			// Every log is log(1) = 0, and the cap is raised to its floor of 1.
			name:      "all-zero → p95 floor 1",
			values:    []int{0, 0, 0},
			wantMu:    0,
			wantSigma: 0,
			wantP95:   1,
		},
	}
	// A value between p95 and twice p95 is capped too: 19 ones, 50, 90 → p95=50.
	tests = append(tests, struct {
		name      string
		values    []int
		wantMu    float64
		wantSigma float64
		wantP95   int
	}{
		name:    "winsorization caps a value just above p95",
		values:  []int{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 50, 90},
		wantMu:  (19*math.Log(2) + 2*math.Log(51)) / 21,
		wantP95: 50,
		wantSigma: func() float64 {
			mu := (19*math.Log(2) + 2*math.Log(51)) / 21
			d1, d2 := math.Log(2)-mu, math.Log(51)-mu
			return math.Sqrt((19*d1*d1 + 2*d2*d2) / 21)
		}(),
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mu, sigma, p95 := logStatsFromSample(tt.values)
			if math.Abs(mu-tt.wantMu) > eps {
				t.Errorf("mu=%v, want %v", mu, tt.wantMu)
			}
			if math.Abs(sigma-tt.wantSigma) > eps {
				t.Errorf("sigma=%v, want %v", sigma, tt.wantSigma)
			}
			if p95 != tt.wantP95 {
				t.Errorf("p95=%d, want %d", p95, tt.wantP95)
			}
		})
	}
}

// TestBuildBaseline_StrictGates pins the gates' comparisons: the sample needs
// strictly more than MinSampleSize PRs and strictly more coverage than the
// threshold, while a language needs at least MinSampleSize PRs.
func TestBuildBaseline_StrictGates(t *testing.T) {
	opts := BaselineOptions{MinSampleSize: 3, CoverageThresholdBps: 7500}
	three := []SamplePR{analyzedPR("Go", 10, 1), analyzedPR("Go", 20, 2), analyzedPR("Go", 30, 3)}

	_, err := BuildBaseline(three, opts)
	require.ErrorIs(t, err, ErrNotEnoughData, "n == MinSampleSize fails")

	// Four PRs, three usable: coverage exactly 7500 bps fails.
	_, err = BuildBaseline(append(three[:3:3], sentinelPR(DiffNotAvailable)), opts)
	require.ErrorIs(t, err, ErrNotEnoughData, "coverage == threshold fails")

	// Four usable PRs pass; Go touched by exactly 3 is anchored, Python by 1 is not.
	four := append(three[:3:3], analyzedPR("Python", 50, 1))
	b, err := BuildBaseline(four, opts)
	require.NoError(t, err)
	require.Equal(t, 3, b.PerLanguage["Go"].SampleSize)
	require.NotContains(t, b.PerLanguage, "Python")
}

// TestBuildBaseline_SampleOrder pins that the sample order changes nothing but,
// at most, the last bits of the log statistics: medians and scores agree.
func TestBuildBaseline_SampleOrder(t *testing.T) {
	var sample []SamplePR
	for i := 1; i <= 40; i++ {
		sample = append(sample, analyzedPR("Go", int64(i*i%97+1), i%5+1))
	}
	reversed := make([]SamplePR, len(sample))
	for i, pr := range sample {
		reversed[len(sample)-1-i] = pr
	}
	a, err := BuildBaseline(sample, DefaultBaselineOptions())
	require.NoError(t, err)
	b, err := BuildBaseline(reversed, DefaultBaselineOptions())
	require.NoError(t, err)
	ga, gb := a.PerLanguage["Go"], b.PerLanguage["Go"]
	require.Equal(t, ga.MedianSLOC, gb.MedianSLOC)
	require.Equal(t, ga.MedianFiles, gb.MedianFiles)
	require.InDelta(t, ga.MuLogSLOC, gb.MuLogSLOC, 1e-12)
	require.InDelta(t, ga.SigmaLogSLOC, gb.SigmaLogSLOC, 1e-12)
	pr := map[string]LanguageLOC{"Go": {SLOC: 60, Files: 3}}
	sa, _, _ := ScoreComplexity("x", pr, a, FormulaCNCS, DefaultDecay)
	sb, _, _ := ScoreComplexity("x", pr, b, FormulaCNCS, DefaultDecay)
	require.Equal(t, *sa, *sb)
}

// TestBuildBaseline_TooLargeFootprintAddsNothingToMedians proves a TOO_LARGE
// PR counts toward coverage and adds nothing to the medians even when it
// carries a footprint.
func TestBuildBaseline_TooLargeFootprintAddsNothingToMedians(t *testing.T) {
	var sample []SamplePR
	for i := 0; i < 31; i++ {
		sample = append(sample, analyzedPR("Go", 10, 1))
		sample = append(sample, SamplePR{
			DiffRef:    DiffTooLarge,
			Analyzed:   true,
			ByLanguage: map[string]LanguageLOC{"Go": {SLOC: 100000, Files: 500}},
		})
	}
	b, err := BuildBaseline(sample, DefaultBaselineOptions())
	require.NoError(t, err)
	require.Equal(t, 62, b.SampleSize)
	require.Equal(t, 62, b.DiffAvailableCount)
	goStats := b.PerLanguage["Go"]
	require.Equal(t, 10, goStats.MedianSLOC)
	require.Equal(t, 1, goStats.MedianFiles)
	require.Equal(t, 31, goStats.SampleSize)
}

// TestBuildBaseline_NegativeSizeIsAnError proves a negative SLOC or file count
// in a sample is rejected, as ScoreComplexity rejects it, instead of giving a
// non-finite anchor.
func TestBuildBaseline_NegativeSizeIsAnError(t *testing.T) {
	for _, bad := range []LanguageLOC{{SLOC: 10, Files: -1}, {SLOC: -1, Files: 5}} {
		var sample []SamplePR
		for i := 0; i < 31; i++ {
			sample = append(sample, analyzedPR("Go", 10, 5))
		}
		sample[7].ByLanguage = map[string]LanguageLOC{"Go": bad}
		b, err := BuildBaseline(sample, DefaultBaselineOptions())
		require.Error(t, err, "%+v", bad)
		require.NotErrorIs(t, err, ErrNotEnoughData)
		require.Nil(t, b)
	}
}

// TestBuildBaseline_InvalidOptionsAreAnError proves options that would switch
// a gate off are rejected, while the bounds themselves are accepted.
func TestBuildBaseline_InvalidOptionsAreAnError(t *testing.T) {
	sample := []SamplePR{analyzedPR("Go", 5, 1), analyzedPR("Go", 6, 1)}
	for _, opts := range []BaselineOptions{
		{},
		{MinSampleSize: 0, CoverageThresholdBps: 9500},
		{MinSampleSize: -1, CoverageThresholdBps: 9500},
		{MinSampleSize: 30, CoverageThresholdBps: -1},
		{MinSampleSize: 30, CoverageThresholdBps: 10000},
	} {
		b, err := BuildBaseline(sample, opts)
		require.Error(t, err, "%+v", opts)
		require.NotErrorIs(t, err, ErrNotEnoughData)
		require.Nil(t, b)
	}
	for _, opts := range []BaselineOptions{
		{MinSampleSize: 1, CoverageThresholdBps: 0},
		{MinSampleSize: 1, CoverageThresholdBps: 9999},
	} {
		_, err := BuildBaseline(sample, opts)
		require.NoError(t, err, "%+v", opts)
	}
}

// TestBuildBaseline_BothMediansMustBePositive proves a language whose median
// file count is zero gets no anchor, even with a positive SLOC median.
func TestBuildBaseline_BothMediansMustBePositive(t *testing.T) {
	b, err := BuildBaseline([]SamplePR{
		analyzedPR("Go", 10, 0),
		analyzedPR("Go", 20, 0),
		analyzedPR("Go", 30, 0),
	}, testOptions)
	require.NoError(t, err)
	require.NotContains(t, b.PerLanguage, "Go")
}

// TestFromEffectiveLOC_Nil proves a nil value gives nil.
func TestFromEffectiveLOC_Nil(t *testing.T) {
	require.Nil(t, FromEffectiveLOC(nil))
}

// TestFromEffectiveLOC_NilEntry proves a nil language entry is skipped, not
// dereferenced.
func TestFromEffectiveLOC_NilEntry(t *testing.T) {
	got := FromEffectiveLOC(&diffsize.EffectiveLOC{ByLanguage: map[string]*diffsize.LanguageDetail{"Go": nil}})
	require.NotNil(t, got)
	require.Empty(t, got)
}
