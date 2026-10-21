// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage_test

import (
	"context"
	"fmt"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
)

// history is a small repository: two feature branches merge into develop,
// which is promoted to main.
func history() []branchlineage.PR {
	at := func(h int) time.Time { return time.Date(2026, 4, 1, h, 0, 0, 0, time.UTC) }
	return []branchlineage.PR{
		{Key: "acme/shop#PR-1#merged", Repository: "acme/shop", Number: 1, HeadBranch: "feature/login", BaseBranch: "develop", MergedAt: at(9), LinesAdded: 120},
		{Key: "acme/shop#PR-2#merged", Repository: "acme/shop", Number: 2, HeadBranch: "feature/cart", BaseBranch: "develop", MergedAt: at(10), LinesAdded: 80},
		{Key: "acme/shop#PR-3#merged", Repository: "acme/shop", Number: 3, HeadBranch: "develop", BaseBranch: "main", MergedAt: at(12), LinesAdded: 200},
	}
}

func ExampleDerive() {
	g, err := branchlineage.Derive(history())
	if err != nil {
		panic(err)
	}
	for _, e := range g.Edges() {
		fmt.Println(e.Carrier, "carried", e.Carried, "from", e.Source, "to", e.Target)
	}
	// Output:
	// acme/shop#PR-3#merged carried acme/shop#PR-1#merged from develop to main
	// acme/shop#PR-3#merged carried acme/shop#PR-2#merged from develop to main
}

func ExampleWalk() {
	g, _ := branchlineage.Derive(history())
	seeds := []branchlineage.Seed{{Carrier: "acme/shop#PR-3#merged", Target: "main"}}
	flow := []branchlineage.Hop{{Source: "develop", Target: "main"}}
	w, err := branchlineage.Walk(context.Background(), g, seeds, flow, branchlineage.Limits{})
	if err != nil {
		panic(err)
	}
	for _, e := range w.Edges {
		fmt.Println(e.Depth, e.Carrier, "->", e.Carried)
	}
	fmt.Println("capped:", len(w.DepthCapped) > 0 || w.RouteCapped)
	// Output:
	// 1 acme/shop#PR-3#merged -> acme/shop#PR-1#merged
	// 1 acme/shop#PR-3#merged -> acme/shop#PR-2#merged
	// capped: false
}

func ExampleProject() {
	prs := history()
	g, _ := branchlineage.Derive(prs)
	proj, err := branchlineage.Project(context.Background(), g, branchlineage.ProjectInput{
		Seeds:      branchlineage.TrackedSeeds(prs, func(_, branch string) bool { return branch == "main" }),
		Flows:      map[string][]branchlineage.Hop{"acme/shop": {{Source: "develop", Target: "main"}}},
		Attributed: map[string]struct{}{"acme/shop#PR-2#merged": {}},
	})
	if err != nil {
		panic(err)
	}
	for _, c := range proj.Credits {
		fmt.Println(c.Carrier, "credited through", c.Path)
	}
	// Output:
	// acme/shop#PR-3#merged credited through [acme/shop#PR-3#merged acme/shop#PR-2#merged]
}

func ExampleCarriedBy() {
	g, _ := branchlineage.Derive(history())
	w, _ := branchlineage.Walk(context.Background(), g,
		[]branchlineage.Seed{{Carrier: "acme/shop#PR-3#merged", Target: "main"}},
		[]branchlineage.Hop{{Source: "develop", Target: "main"}}, branchlineage.Limits{})
	for _, p := range branchlineage.CarriedBy(w, nil).Pairs {
		fmt.Println(p.Carried, "landed by", p.LandedBy)
	}
	// Output:
	// acme/shop#PR-1#merged landed by [acme/shop#PR-3#merged]
	// acme/shop#PR-2#merged landed by [acme/shop#PR-3#merged]
}

func ExampleComputeCoverage() {
	g, _ := branchlineage.Derive(history())
	cov := branchlineage.ComputeCoverage(history(), g.Edges(), []branchlineage.Hop{{Source: "develop", Target: "main"}}, []string{"main"}, 1)
	fmt.Printf("carriers %d, carrier share %.2f, explained share %.2f\n", cov.CarrierCount, cov.CarrierShare, cov.ExplainedShare)
	fmt.Println("unexplained:", len(cov.Unexplained()))
	// Output:
	// carriers 1, carrier share 1.00, explained share 1.00
	// unexplained: 0
}

func ExampleResolveFlow() {
	f := branchlineage.ResolveFlow(branchlineage.FlowInput{
		EdgePairs: []branchlineage.Hop{{Source: "develop", Target: "main"}, {Source: "main", Target: "develop"}, {Source: "feature", Target: "develop"}},
		Tracked:   map[string]struct{}{"main": {}, "develop": {}},
	})
	fmt.Println(f.Origin, f.Hops, f.Terminals)
	for _, r := range f.Refused {
		fmt.Println("refused", r.Hop, r.Reason)
	}
	// Output:
	// inferred [feature->develop] [develop]
}

func ExampleDetectFlow() {
	d := branchlineage.DetectFlow(branchlineage.MergePairs([]branchlineage.PR{
		{Key: "1", HeadBranch: "feature/a", BaseBranch: "develop", MergedAt: time.Unix(1, 0)},
		{Key: "2", HeadBranch: "feature/a", BaseBranch: "develop", MergedAt: time.Unix(2, 0)},
		{Key: "3", HeadBranch: "develop", BaseBranch: "main", MergedAt: time.Unix(3, 0)},
		{Key: "4", HeadBranch: "develop", BaseBranch: "main", MergedAt: time.Unix(4, 0)},
		{Key: "5", HeadBranch: "main", BaseBranch: "develop", MergedAt: time.Unix(5, 0)},
	}), nil)
	for _, h := range d.Hops {
		fmt.Println(h, d.Support[h])
	}
	// Output:
	// feature/a->develop 2
	// develop->main 2
}

func ExampleValidateFlow() {
	fmt.Println(branchlineage.ValidateFlow([]branchlineage.Hop{{Source: "develop", Target: "main"}}))
	fmt.Println(branchlineage.ValidateFlow([]branchlineage.Hop{{Source: "a", Target: "b"}, {Source: "b", Target: "a"}}))
	// Output:
	// <nil>
	// branchlineage: invalid flow: the flow contains a cycle
}

func ExampleInvalidate() {
	before, _ := branchlineage.NewHistory(history())
	late := branchlineage.PR{
		Key: "acme/shop#PR-4#merged", Repository: "acme/shop", Number: 4,
		HeadBranch: "feature/search", BaseBranch: "develop", MergedAt: time.Date(2026, 4, 1, 11, 0, 0, 0, time.UTC),
	}
	for _, s := range branchlineage.Invalidate(before, []branchlineage.PR{late}).Carriers {
		fmt.Println(s.Carrier, s.Reason)
	}
	// Output:
	// acme/shop#PR-3#merged late_leaf
}
