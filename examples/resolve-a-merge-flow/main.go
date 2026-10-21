// Command resolve-a-merge-flow suggests a repository's promotion flow from its
// merge history, checks the suggestion, and resolves the flow the walk uses:
// inferred from the stored edges, or a person's confirmed one.
//
//	go run ./examples/resolve-a-merge-flow
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
)

func main() {
	at := func(hour int) time.Time { return time.Date(2026, 4, 2, hour, 0, 0, 0, time.UTC) }
	var prs []branchlineage.PR
	add := func(head, base string) {
		n := len(prs) + 1
		prs = append(prs, branchlineage.PR{Key: fmt.Sprint("PR-", n), Repository: "acme/shop", Number: n,
			HeadBranch: head, BaseBranch: base, MergedAt: at(n)})
	}
	add("feature/login", "develop")
	add("develop", "main")
	add("hotfix/header", "main")
	add("main", "develop") // a back-merge of the hotfix
	add("feature/cart", "develop")
	add("develop", "main")
	add("feature/search", "develop")
	add("develop", "main")

	suggested := branchlineage.DetectFlow(branchlineage.MergePairs(prs), nil)
	for _, h := range suggested.Hops {
		fmt.Println("suggested", h, "from", suggested.Support[h], "merges")
	}
	fmt.Println("confirmable:", branchlineage.ValidateFlow(suggested.Hops) == nil)

	g, err := branchlineage.Derive(prs)
	if err != nil {
		log.Fatal(err)
	}
	pairs := branchlineage.EdgePairs(g.Edges())
	fmt.Println("edge pairs", pairs)
	tracked := map[string]struct{}{"main": {}}

	inferred := branchlineage.ResolveFlow(branchlineage.FlowInput{DetectedHops: suggested.Hops, EdgePairs: pairs, Tracked: tracked})
	fmt.Println(inferred.Origin, inferred.Hops, "terminals", inferred.Terminals)

	confirmed := branchlineage.ResolveFlow(branchlineage.FlowInput{
		Confirmed: true, ConfirmedHops: []branchlineage.Hop{{Source: "develop", Target: "main"}, {Source: "release", Target: "main"}},
		EdgePairs: pairs, Tracked: tracked,
	})
	fmt.Println(confirmed.Origin, confirmed.Hops, "observed", confirmed.Observed)
}
