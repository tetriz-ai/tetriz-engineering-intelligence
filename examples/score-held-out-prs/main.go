// Command score-held-out-prs builds a baseline from the synthetic corpus's
// baseline PRs and scores its held-out PRs with both formulas.
//
//	go run ./examples/score-held-out-prs
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/diffsize"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/prcomplexity"
)

type pr struct {
	ID      string `json:"id"`
	Role    string `json:"role"`
	DiffRef string `json:"diff_ref"`
	Format  string `json:"format"`
	Diff    string `json:"diff"`
}

// excludes are the repository's own exclude globs: lock files never count.
var excludes = []string{"*.lock"}

func main() {
	raw, err := os.ReadFile("examples/score-held-out-prs/testdata/corpus.json")
	if err != nil {
		log.Fatal(err)
	}
	var prs []pr
	if err := json.Unmarshal(raw, &prs); err != nil {
		log.Fatal(err)
	}

	// Build the baseline from the baseline PRs, in corpus order.
	var sample []prcomplexity.SamplePR
	for _, p := range prs {
		if p.Role == "baseline" {
			sample = append(sample, samplePR(p))
		}
	}
	baseline, err := prcomplexity.BuildBaseline(sample, prcomplexity.DefaultBaselineOptions())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("baseline: %d PRs, coverage %d bps\n", baseline.SampleSize, baseline.CoverageBps)
	langs := make([]string, 0, len(baseline.PerLanguage))
	for lang := range baseline.PerLanguage {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	for _, lang := range langs {
		a := baseline.PerLanguage[lang]
		fmt.Printf("  %-10s %2d PRs, median %d SLOC in %d files\n", lang, a.SampleSize, a.MedianSLOC, a.MedianFiles)
	}

	// Score each held-out PR against it.
	fmt.Printf("%-24s %6s %6s\n", "held-out PR", "cncs", "ratio")
	for _, p := range prs {
		if p.Role != "held-out" {
			continue
		}
		s := samplePR(p)
		fmt.Printf("%-24s %6s %6s\n", p.ID,
			score(s, baseline, prcomplexity.FormulaCNCS),
			score(s, baseline, prcomplexity.FormulaRatio))
	}
}

// samplePR analyses a PR's diff, if it has one.
func samplePR(p pr) prcomplexity.SamplePR {
	s := prcomplexity.SamplePR{DiffRef: p.DiffRef}
	if p.Diff == "" {
		return s // a sentinel: TOO_LARGE or NOT_AVAILABLE
	}
	loc, err := diffsize.AnalyzeDiffWithExcludes([]byte(p.Diff), p.Format, excludes)
	if err != nil {
		log.Fatalf("%s: %v", p.ID, err)
	}
	s.Analyzed = true
	s.ByLanguage = prcomplexity.FromEffectiveLOC(loc)
	return s
}

func score(s prcomplexity.SamplePR, b *prcomplexity.Baseline, f prcomplexity.Formula) string {
	v, ok, err := prcomplexity.ScoreComplexity(s.DiffRef, s.ByLanguage, b, f, prcomplexity.DefaultDecay)
	switch {
	case err != nil:
		log.Fatal(errors.Join(errors.New(s.DiffRef), err))
	case !ok:
		return "-"
	}
	return fmt.Sprintf("%.1f", *v)
}
