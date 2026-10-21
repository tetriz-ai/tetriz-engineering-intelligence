// Command derive-promotion-edges derives which pull requests each promotion carried.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
)

func main() {
	at := func(hour float64) time.Time {
		return time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC).Add(time.Duration(hour * float64(time.Hour)))
	}
	pr := func(n int, head, base string, hour float64) branchlineage.PR {
		return branchlineage.PR{Key: fmt.Sprint("PR-", n), Repository: "acme/shop", Number: n,
			HeadBranch: head, BaseBranch: base, MergedAt: at(hour)}
	}
	prs := []branchlineage.PR{
		pr(1, "feature/login", "develop", 1), pr(2, "feature/cart", "develop", 2),
		pr(4, "feature/search", "develop", 3), pr(5, "feature/fix", "main", 4),
		pr(6, "feature/docs", "main", 5), pr(10, "develop", "release", 6),
		pr(3, "feature/pay", "develop", 7), pr(11, "release", "main", 8),
		pr(12, "develop", "main", 9),
	}
	prs[8].FirstCommitAt = at(6.5) // PR-12's earliest commit came after PR-10 merged

	g, err := branchlineage.Derive(prs)
	if err != nil {
		log.Fatal(err)
	}
	for _, d := range g.Derivations() {
		if d.Outcome == branchlineage.OutcomeCarrier {
			fmt.Println(d.Carrier, d.Source+"->"+d.Target, "carried", carried(d))
		}
	}
}

func carried(d branchlineage.Derivation) []string {
	var out []string
	for _, e := range d.Edges {
		out = append(out, e.Carried)
	}
	return out
}
