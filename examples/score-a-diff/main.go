// Command score-a-diff turns a pull request's diff into per-language sizes
// with diffsize and scores them with prcomplexity.
//
//	go run ./examples/score-a-diff
package main

import (
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/diffsize"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/prcomplexity"
)

func main() {
	diff, err := os.ReadFile("examples/score-a-diff/testdata/change.patch")
	if err != nil {
		log.Fatal(err)
	}

	// Lock files never count in this repository.
	loc, err := diffsize.AnalyzeDiffWithExcludes(diff, diffsize.FormatUnified, []string{"*.lock"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("excluded:", loc.ExcludedFiles)
	fmt.Println("docs lines:", loc.TotalDocLinesAdded)

	byLanguage := prcomplexity.FromEffectiveLOC(loc)
	langs := make([]string, 0, len(byLanguage))
	for lang := range byLanguage {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	for _, lang := range langs {
		code := prcomplexity.IsCodeLanguage(lang)
		fmt.Printf("%-10s %3d SLOC in %d files (code: %v)\n", lang, byLanguage[lang].SLOC, byLanguage[lang].Files, code)
	}

	// The repository's baseline, stored from an earlier BuildBaseline run.
	baseline := &prcomplexity.Baseline{PerLanguage: map[string]prcomplexity.LanguageStats{
		"Go":         {MedianSLOC: 72, MedianFiles: 4, MuLogSLOC: 4.36, SigmaLogSLOC: 0.92, MuLogFiles: 1.41, SigmaLogFiles: 0.37},
		"TypeScript": {MedianSLOC: 44, MedianFiles: 3, MuLogSLOC: 3.78, SigmaLogSLOC: 0.83, MuLogFiles: 1.40, SigmaLogFiles: 0.30},
	}}
	for _, f := range []prcomplexity.Formula{prcomplexity.FormulaCNCS, prcomplexity.FormulaRatio} {
		score, ok, err := prcomplexity.ScoreComplexity("change.patch", byLanguage, baseline, f, prcomplexity.DefaultDecay)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s: %.1f (scoreable: %v)\n", f, *score, ok)
	}
}
