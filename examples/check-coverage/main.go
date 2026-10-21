// Command check-coverage measures how much of a git-flow repository's main
// branch the merge path explains, and names the release whose content it cannot see.
//
//	go run ./examples/check-coverage
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
)

func main() {
	at := func(hour int) time.Time { return time.Date(2026, 4, 2, hour, 0, 0, 0, time.UTC) }
	pr := func(n int, head, base string, hour int, lines int64) branchlineage.PR {
		return branchlineage.PR{Key: fmt.Sprint("PR-", n), Repository: "acme/shop", Number: n,
			HeadBranch: head, BaseBranch: base, MergedAt: at(hour), LinesAdded: lines}
	}
	prs := []branchlineage.PR{
		pr(1, "feature/login", "develop", 1, 120),
		pr(2, "feature/cart", "develop", 2, 80),
		pr(3, "develop", "main", 3, 210),
		// release/2.0 is cut from develop with its content already on it: no pull
		// request carries that content, only the fix merged after the cut.
		pr(4, "hotfix/price", "release/2.0", 4, 15),
		pr(5, "release/2.0", "main", 5, 1400),
		pr(6, "feature/readme", "main", 6, 10),
	}
	g, err := branchlineage.Derive(prs)
	if err != nil {
		log.Fatal(err)
	}
	flow := []branchlineage.Hop{{Source: "develop", Target: "main"}, {Source: "release/2.0", Target: "main"}}
	mergedIntoMain := 3
	cov := branchlineage.ComputeCoverage(prs, g.Edges(), flow, []string{"main"}, mergedIntoMain)
	fmt.Printf("carriers %d of %d merges into main (%.2f)\n", cov.CarrierCount, cov.TrackedMergedTotal, cov.CarrierShare)
	fmt.Printf("explained %d of %d (%.2f)\n", cov.ExplainedInBand, cov.CarrierCount, cov.ExplainedShare)
	for _, c := range cov.Unexplained() {
		if c.CarrierLines == nil {
			fmt.Printf("warning: %s has no line counts to reconcile (%s)\n", c.Carrier, c.Band)
			continue
		}
		fmt.Printf("warning: %s promoted %d lines, but the pull requests it carried account for only %d (%s)\n", c.Carrier, *c.CarrierLines, c.CarriedSum, c.Band)
	}
}
