// Command score-one-pr scores one pull request against a stored baseline.
package main

import (
	"fmt"
	"log"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/prcomplexity"
)

func main() {
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
}
