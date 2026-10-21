// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prcomplexity_test

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/diffsize"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/prcomplexity"
)

// diff and Example_scoreADiff are the README's "Score a diff" program
// (readme_test.go checks).
const diff = `diff --git a/server/health.go b/server/health.go
--- /dev/null
+++ b/server/health.go
@@ -0,0 +1,6 @@
+package server
+
+// Health reports that the server is up.
+func Health() string {
+	return "ok"
+}
`

func Example_scoreADiff() {
	loc, err := diffsize.AnalyzeDiffWithExcludes([]byte(diff), diffsize.FormatUnified, []string{"*.lock"})
	if err != nil {
		log.Fatal(err)
	}
	byLanguage := prcomplexity.FromEffectiveLOC(loc)
	fmt.Println(byLanguage)

	baseline := &prcomplexity.Baseline{PerLanguage: map[string]prcomplexity.LanguageStats{
		"Go": {MedianSLOC: 72, MedianFiles: 4, MuLogSLOC: 4.36, SigmaLogSLOC: 0.92, MuLogFiles: 1.41, SigmaLogFiles: 0.37},
	}}
	score, ok, err := prcomplexity.ScoreComplexity("health.patch", byLanguage, baseline, "", prcomplexity.DefaultDecay)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(*score, ok)
	// Output:
	// map[Go:{4 1}]
	// 1.3 true
}

// Example_buildABaseline is the README's "Build a baseline" program
// (readme_test.go checks).
func Example_buildABaseline() {
	var sample []prcomplexity.SamplePR
	for i := 1; i <= 40; i++ {
		sample = append(sample, prcomplexity.SamplePR{
			DiffRef:    fmt.Sprintf("pr-%d.patch", i),
			Analyzed:   true,
			ByLanguage: map[string]prcomplexity.LanguageLOC{"Go": {SLOC: int64(10 * i), Files: 1 + i%4}},
		})
	}

	baseline, err := prcomplexity.BuildBaseline(sample, prcomplexity.DefaultBaselineOptions())
	if err != nil {
		log.Fatal(err)
	}
	goStats := baseline.PerLanguage["Go"]
	fmt.Println(baseline.SampleSize, baseline.CoverageBps, goStats.MedianSLOC, goStats.MedianFiles)
	// Output: 40 10000 205 3
}

// blobs, scoreStoredDiff and Example_useWithTetrizConnectors are the README's
// "Use with tetriz-connectors" program (readme_test.go checks).

// blobs stands in for the blob store the diff fetches write to.
var blobs = map[string]string{
	"pr-diffs/org-1/github/101/7.patch": "diff --git a/main.go b/main.go\n--- /dev/null\n+++ b/main.go\n" +
		"@@ -0,0 +1,3 @@\n+package main\n+\n+func main() {}\n",
	"pr-diffs/org-1/gitlab/202/8.json": `[{"new_path": "big.go", "diff": "", "too_large": true}]`,
	"pr-diffs/org-1/gitlab/202/9.json": `[{"new_path": "main.go", "diff": "@@ -0,0 +1,3 @@\n+package main\n+\n+func main() {}\n"},` +
		`{"new_path": "package-lock.json", "diff": "", "too_large": true}]`,
}

// scoreStoredDiff scores one PR from the diff reference its fetch reported.
func scoreStoredDiff(diffRef string, excludes []string, baseline *prcomplexity.Baseline) (*float64, bool, error) {
	byLanguage := map[string]prcomplexity.LanguageLOC{}
	if diffRef != prcomplexity.DiffTooLarge && diffRef != prcomplexity.DiffNotAvailable {
		format := diffsize.FormatUnified
		if strings.HasSuffix(diffRef, ".json") {
			format = diffsize.FormatGitLabJSON
		}
		loc, err := diffsize.AnalyzeDiffWithExcludes([]byte(blobs[diffRef]), format, excludes)
		switch {
		case errors.Is(err, diffsize.ErrDiffExcluded):
			diffRef = prcomplexity.DiffTooLarge // a stored diff cannot be fetched again
		case err != nil:
			return nil, false, err
		default:
			byLanguage = prcomplexity.FromEffectiveLOC(loc)
		}
	}
	return prcomplexity.ScoreComplexity(diffRef, byLanguage, baseline, prcomplexity.FormulaCNCS, prcomplexity.DefaultDecay)
}

func Example_useWithTetrizConnectors() {
	baseline := &prcomplexity.Baseline{PerLanguage: map[string]prcomplexity.LanguageStats{
		"Go": {MedianSLOC: 72, MedianFiles: 4, MuLogSLOC: 4.36, SigmaLogSLOC: 0.92, MuLogFiles: 1.41, SigmaLogFiles: 0.37},
	}}
	// From GetVCSSettings: file_exclude_patterns and repo_file_exclude_overrides.
	excludes := diffsize.PatternsForRepo("example/api", []string{"*.pb.go", "go.sum", "package-lock.json", "*.lock"}, nil)

	for _, ref := range []string{"pr-diffs/org-1/github/101/7.patch", "pr-diffs/org-1/gitlab/202/8.json", "pr-diffs/org-1/gitlab/202/9.json", prcomplexity.DiffNotAvailable} {
		score, ok, err := scoreStoredDiff(ref, excludes, baseline)
		if err != nil {
			log.Fatal(err)
		}
		if !ok {
			fmt.Println(ref, "unscoreable")
			continue
		}
		fmt.Println(ref, *score)
	}
	// Output:
	// pr-diffs/org-1/github/101/7.patch 1.3
	// pr-diffs/org-1/gitlab/202/8.json 7
	// pr-diffs/org-1/gitlab/202/9.json 1.3
	// NOT_AVAILABLE unscoreable
}
