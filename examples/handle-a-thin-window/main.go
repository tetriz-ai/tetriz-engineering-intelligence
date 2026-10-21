// Command handle-a-thin-window builds a baseline from a window too thin for
// the default gates, treats the window's PRs as unscoreable, and then builds
// one with lowered gates.
//
//	go run ./examples/handle-a-thin-window
package main

import (
	"errors"
	"fmt"
	"log"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/prcomplexity"
)

func main() {
	// A young repository: 11 analyzed Go PRs and one PR whose diff could not
	// be fetched.
	var window []prcomplexity.SamplePR
	for i := 1; i <= 11; i++ {
		window = append(window, prcomplexity.SamplePR{
			DiffRef:    fmt.Sprintf("pr-%d.patch", i),
			Analyzed:   true,
			ByLanguage: map[string]prcomplexity.LanguageLOC{"Go": {SLOC: int64(20 + 7*i), Files: 1 + i%3}},
		})
	}
	window = append(window, prcomplexity.SamplePR{DiffRef: prcomplexity.DiffNotAvailable})
	newPR := map[string]prcomplexity.LanguageLOC{"Go": {SLOC: 150, Files: 4}}

	// The default gates need more than 30 PRs. The error wraps
	// ErrNotEnoughData, and the counts still say why.
	baseline, err := prcomplexity.BuildBaseline(window, prcomplexity.DefaultBaselineOptions())
	if !errors.Is(err, prcomplexity.ErrNotEnoughData) {
		log.Fatalf("want ErrNotEnoughData, got %v", err)
	}
	fmt.Printf("default gates: no baseline (%d PRs, coverage %d bps)\n", baseline.SampleSize, baseline.CoverageBps)

	// With no baseline, the host records the window's PRs as unscoreable. A nil
	// baseline has no anchors, so ScoreComplexity agrees.
	score, ok, err := prcomplexity.ScoreComplexity("pr-13.patch", newPR, nil, prcomplexity.FormulaCNCS, prcomplexity.DefaultDecay)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("new PR:", score, ok)

	// The zero BaselineOptions is an error, not "no gates".
	_, err = prcomplexity.BuildBaseline(window, prcomplexity.BaselineOptions{})
	fmt.Println("zero options:", err)

	// Lowered gates, starting from the defaults: 10 PRs and 90% coverage.
	opts := prcomplexity.DefaultBaselineOptions()
	opts.MinSampleSize = 10
	opts.CoverageThresholdBps = 9000
	baseline, err = prcomplexity.BuildBaseline(window, opts)
	if err != nil {
		log.Fatal(err)
	}
	goStats := baseline.PerLanguage["Go"]
	fmt.Printf("lowered gates: Go anchor from %d PRs, median %d SLOC in %d files\n", goStats.SampleSize, goStats.MedianSLOC, goStats.MedianFiles)

	score, ok, err = prcomplexity.ScoreComplexity("pr-13.patch", newPR, baseline, prcomplexity.FormulaCNCS, prcomplexity.DefaultDecay)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("new PR:", *score, ok)
}
