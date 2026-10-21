// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prcomplexity_test

import (
	"errors"
	"fmt"
	"log"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/diffsize"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/prcomplexity"
)

// The quick start: score one PR against a stored baseline. Its body is the
// body of examples/score-one-pr/main.go (readme_test.go checks).
func ExampleScoreComplexity() {
	// A repository's Go baseline, as prcomplexity.BuildBaseline returns it.
	baseline := &prcomplexity.Baseline{PerLanguage: map[string]prcomplexity.LanguageStats{
		"Go": {MedianSLOC: 72, MedianFiles: 4, MuLogSLOC: 4.36, SigmaLogSLOC: 0.92, MuLogFiles: 1.41, SigmaLogFiles: 0.37},
	}}
	pr := map[string]prcomplexity.LanguageLOC{"Go": {SLOC: 300, Files: 6}}

	score, ok, err := prcomplexity.ScoreComplexity("pr-42.patch", pr, baseline, prcomplexity.FormulaCNCS, prcomplexity.DefaultDecay)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(*score, ok)
	// Output: 9.3 true
}

func ExampleScoreComplexity_unscoreable() {
	baseline := &prcomplexity.Baseline{PerLanguage: map[string]prcomplexity.LanguageStats{
		"Go": {MedianSLOC: 72, MedianFiles: 4},
	}}
	// Python has no anchor and Markdown is not code: nothing to score.
	pr := map[string]prcomplexity.LanguageLOC{"Python": {SLOC: 40, Files: 2}, "Markdown": {SLOC: 90, Files: 1}}
	score, ok, err := prcomplexity.ScoreComplexity("pr-7.patch", pr, baseline, prcomplexity.FormulaRatio, prcomplexity.DefaultDecay)
	fmt.Println(score, ok, err)

	// A diff over the caller's size cap scores 7.0 without a baseline.
	score, ok, err = prcomplexity.ScoreComplexity(prcomplexity.DiffTooLarge, nil, nil, "", prcomplexity.DefaultDecay)
	fmt.Println(*score, ok, err)

	_, _, err = prcomplexity.ScoreComplexity("pr-8.patch", pr, baseline, "linear", prcomplexity.DefaultDecay)
	fmt.Println(err)
	// Output:
	// <nil> false <nil>
	// 7 true <nil>
	// prcomplexity: unknown formula "linear" (want "cncs" or "ratio")
}

func ExampleBuildBaseline() {
	pr := func(sloc int64, files int) prcomplexity.SamplePR {
		return prcomplexity.SamplePR{
			DiffRef:    "a.patch",
			Analyzed:   true,
			ByLanguage: map[string]prcomplexity.LanguageLOC{"Go": {SLOC: sloc, Files: files}},
		}
	}
	sample := []prcomplexity.SamplePR{pr(40, 2), pr(90, 3), pr(120, 4), pr(60, 2)}

	// Small gates so four PRs are enough; DefaultBaselineOptions needs 31.
	opts := prcomplexity.BaselineOptions{MinSampleSize: 3, CoverageThresholdBps: 9000}
	baseline, err := prcomplexity.BuildBaseline(sample, opts)
	if err != nil {
		log.Fatal(err)
	}
	goStats := baseline.PerLanguage["Go"]
	fmt.Println(baseline.SampleSize, baseline.CoverageBps, goStats.MedianSLOC, goStats.MedianFiles)
	// Output: 4 10000 75 3
}

func ExampleBuildBaseline_notEnoughData() {
	sample := []prcomplexity.SamplePR{
		{DiffRef: "a.patch", Analyzed: true, ByLanguage: map[string]prcomplexity.LanguageLOC{"Go": {SLOC: 40, Files: 2}}},
		{DiffRef: prcomplexity.DiffNotAvailable},
	}
	baseline, err := prcomplexity.BuildBaseline(sample, prcomplexity.DefaultBaselineOptions())
	fmt.Println(errors.Is(err, prcomplexity.ErrNotEnoughData), baseline.SampleSize, baseline.CoverageBps)
	// Output: true 2 5000
}

func ExampleFromEffectiveLOC() {
	loc, err := diffsize.AnalyzeDiff([]byte("diff --git a/main.go b/main.go\n--- /dev/null\n+++ b/main.go\n@@ -0,0 +1,3 @@\n+package main\n+\n+func main() {}\n"), diffsize.FormatUnified)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(prcomplexity.FromEffectiveLOC(loc))
	// Output: map[Go:{2 1}]
}

func ExampleCodeSlices() {
	pr := map[string]prcomplexity.LanguageLOC{
		"Go": {SLOC: 30, Files: 2}, "YAML": {SLOC: 12, Files: 1}, "TypeScript": {SLOC: 0, Files: 1},
	}
	fmt.Println(prcomplexity.CodeSlices(pr))
	// Output: map[Go:{30 2}]
}

func ExampleIsCodeLanguage() {
	for _, lang := range []string{"Go", "HTML", "Protocol Buffer", "JSON", "Markdown"} {
		fmt.Println(lang, prcomplexity.IsCodeLanguage(lang))
	}
	// Output:
	// Go true
	// HTML true
	// Protocol Buffer true
	// JSON false
	// Markdown false
}

func ExampleMedian() {
	fmt.Println(prcomplexity.Median([]int{3, 1, 2}), prcomplexity.Median([]int{1, 2}), prcomplexity.Median(nil))
	// Output: 2 2 0
}
