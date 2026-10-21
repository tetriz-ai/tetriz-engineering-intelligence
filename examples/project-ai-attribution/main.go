// Command project-ai-attribution credits each promotion with the AI work it carried to main.
package main

import (
	"context"
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
	// The pull requests an AI coding session produced, as the attribution package
	// would report them.
	attributed := map[string]struct{}{"PR-1": {}, "PR-2": {}, "PR-3": {}, "PR-5": {}}

	g, err := branchlineage.Derive(prs)
	if err != nil {
		log.Fatal(err)
	}
	onMain := func(_, branch string) bool { return branch == "main" }
	seeds := branchlineage.TrackedSeeds(prs, onMain)
	proj, err := branchlineage.Project(context.Background(), g, branchlineage.ProjectInput{
		Seeds:      seeds,
		Flows:      map[string][]branchlineage.Hop{"acme/shop": {{Source: "develop", Target: "release"}, {Source: "release", Target: "main"}, {Source: "develop", Target: "main"}}},
		Attributed: attributed,
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, c := range proj.Credits {
		fmt.Println("credit", c.Carrier, "via", c.Path)
	}

	before, after := 0, 0
	credited := map[string]bool{}
	for _, k := range proj.Keys() {
		credited[k] = true
	}
	for _, s := range seeds {
		_, ai := attributed[s.Carrier]
		if ai {
			before++
		}
		if ai || credited[s.Carrier] {
			after++
		}
	}
	fmt.Printf("merges into main credited with AI work: %d of %d directly, %d of %d with lineage\n", before, len(seeds), after, len(seeds))

	for _, p := range branchlineage.CarriedBy(proj.Walk, nil).Pairs {
		fmt.Println("landed", p.Carried, "by", p.LandedBy)
	}
}
