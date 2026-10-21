// Command cap-a-walk walks one promotion chain with the default limits, a depth
// cap and a small route budget, and prints what each cap left out.
//
//	go run ./examples/cap-a-walk
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
)

// capEvents keeps the Observer's count events; it ignores the samples.
type capEvents struct{ events []string }

func (c *capEvents) Count(event string, labels map[string]string) {
	if r, ok := labels["repository"]; ok {
		event += "{" + r + "}"
	}
	c.events = append(c.events, event)
}

func (c *capEvents) Observe(string, float64, map[string]string) {}

func main() {
	at := func(hour int) time.Time { return time.Date(2026, 4, 2, hour, 0, 0, 0, time.UTC) }
	pr := func(n int, head, base string) branchlineage.PR {
		return branchlineage.PR{Key: fmt.Sprint("PR-", n), Repository: "acme/shop", Number: n,
			HeadBranch: head, BaseBranch: base, MergedAt: at(n)}
	}
	// Two features merge into integration, which is promoted to staging and on to main.
	g, err := branchlineage.Derive([]branchlineage.PR{
		pr(1, "feature/a", "integration"), pr(2, "feature/b", "integration"),
		pr(3, "integration", "staging"), pr(4, "staging", "main"),
	})
	if err != nil {
		log.Fatal(err)
	}
	seeds := []branchlineage.Seed{{Carrier: "PR-4", Target: "main"}}
	flow := []branchlineage.Hop{{Source: "integration", Target: "staging"}, {Source: "staging", Target: "main"}}

	for _, run := range []struct {
		name   string
		limits branchlineage.Limits
	}{
		{"default limits", branchlineage.Limits{}},
		{"MaxDepth 1", branchlineage.Limits{MaxDepth: 1}},
		{"MaxRoutes 1", branchlineage.Limits{MaxRoutes: 1}},
	} {
		obs := &capEvents{}
		w, err := branchlineage.Walk(context.Background(), g, seeds, flow, run.limits, branchlineage.WithObserver(obs))
		if err != nil {
			log.Fatal(err)
		}
		var edges, unexpanded, carried []string
		for _, e := range w.Edges {
			edges = append(edges, e.Carrier+"->"+e.Carried)
		}
		for _, n := range w.Unexpanded {
			unexpanded = append(unexpanded, n.Carrier)
		}
		cw := branchlineage.CarriedBy(w, nil)
		for _, p := range cw.Pairs {
			carried = append(carried, p.Carried)
		}
		fmt.Println(run.name)
		fmt.Println("  edges", edges)
		fmt.Println("  depth capped", w.DepthCapped, "route capped", w.RouteCapped, "unexpanded", unexpanded)
		fmt.Println("  carried", carried, "unresolvable", cw.Unresolvable)
		fmt.Println("  events", obs.events)
	}
}
