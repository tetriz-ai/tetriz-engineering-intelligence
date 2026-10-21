// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage_test

// Each Example here is a program in README.md, with the same main body;
// readme_test.go checks the two match and that the README shows this output.

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage/adapter/vcs"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage/store/sqlite"
)

func Example_quickStart() {
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
	// Output:
	// credit PR-10 via [PR-10 PR-1]
	// credit PR-11 via [PR-11 PR-10 PR-1]
	// credit PR-12 via [PR-12 PR-3]
	// merges into main credited with AI work: 1 of 4 directly, 3 of 4 with lineage
	// landed PR-1 by [PR-11]
	// landed PR-2 by [PR-11]
	// landed PR-3 by [PR-12]
	// landed PR-4 by [PR-11]
}

func Example_derivePromotionEdges() {
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
	// Output:
	// PR-10 develop->release carried [PR-1 PR-2 PR-4]
	// PR-11 release->main carried [PR-10]
	// PR-12 develop->main carried [PR-3]
}

func Example_resolveAFlow() {
	var prs []branchlineage.PR
	for i, hop := range [][2]string{
		{"feature/login", "develop"}, {"develop", "main"}, {"feature/cart", "develop"},
		{"develop", "main"}, {"main", "develop"}, {"feature/search", "develop"},
	} {
		prs = append(prs, branchlineage.PR{Key: fmt.Sprint("PR-", i+1), Repository: "acme/shop", Number: i + 1,
			HeadBranch: hop[0], BaseBranch: hop[1], MergedAt: time.Date(2026, 4, 2, i, 0, 0, 0, time.UTC)})
	}
	suggested := branchlineage.DetectFlow(branchlineage.MergePairs(prs), nil)
	fmt.Println("suggested", suggested.Hops, "valid:", branchlineage.ValidateFlow(suggested.Hops) == nil)

	g, err := branchlineage.Derive(prs)
	if err != nil {
		log.Fatal(err)
	}
	flow := branchlineage.ResolveFlow(branchlineage.FlowInput{
		DetectedHops: suggested.Hops,
		EdgePairs:    branchlineage.EdgePairs(g.Edges()),
		Tracked:      map[string]struct{}{"main": {}},
	})
	fmt.Println(flow.Origin, flow.Hops, "terminals", flow.Terminals)
	// Output:
	// suggested [develop->main] valid: true
	// inferred [develop->main] terminals [main]
}

func Example_creditPromotions() {
	at := func(hour int) time.Time { return time.Date(2026, 4, 2, hour, 0, 0, 0, time.UTC) }
	prs := []branchlineage.PR{
		{Key: "PR-1", Repository: "acme/shop", Number: 1, HeadBranch: "feature/login", BaseBranch: "develop", MergedAt: at(1)},
		{Key: "PR-2", Repository: "acme/shop", Number: 2, HeadBranch: "feature/cart", BaseBranch: "develop", MergedAt: at(2)},
		{Key: "PR-3", Repository: "acme/shop", Number: 3, HeadBranch: "develop", BaseBranch: "main", MergedAt: at(3)},
	}
	g, err := branchlineage.Derive(prs)
	if err != nil {
		log.Fatal(err)
	}
	proj, err := branchlineage.Project(context.Background(), g, branchlineage.ProjectInput{
		Seeds:      branchlineage.TrackedSeeds(prs, func(_, branch string) bool { return branch == "main" }),
		Flows:      map[string][]branchlineage.Hop{"acme/shop": {{Source: "develop", Target: "main"}}},
		Attributed: map[string]struct{}{"PR-2": {}},
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, c := range proj.Credits {
		fmt.Println("credit", c.Carrier, "via", c.Path)
	}
	for _, p := range branchlineage.CarriedBy(proj.Walk, nil).Pairs {
		fmt.Println("landed", p.Carried, "by", p.LandedBy)
	}
	// Output:
	// credit PR-3 via [PR-3 PR-2]
	// landed PR-1 by [PR-3]
	// landed PR-2 by [PR-3]
}

func Example_checkCoverage() {
	at := func(hour int) time.Time { return time.Date(2026, 4, 2, hour, 0, 0, 0, time.UTC) }
	prs := []branchlineage.PR{
		{Key: "PR-1", Repository: "acme/shop", Number: 1, HeadBranch: "feature/login", BaseBranch: "develop", MergedAt: at(1), LinesAdded: 120},
		{Key: "PR-2", Repository: "acme/shop", Number: 2, HeadBranch: "develop", BaseBranch: "main", MergedAt: at(2), LinesAdded: 130},
		{Key: "PR-3", Repository: "acme/shop", Number: 3, HeadBranch: "release/2.0", BaseBranch: "main", MergedAt: at(3), LinesAdded: 1400},
	}
	g, err := branchlineage.Derive(prs)
	if err != nil {
		log.Fatal(err)
	}
	flow := []branchlineage.Hop{{Source: "develop", Target: "main"}, {Source: "release/2.0", Target: "main"}}
	mergedIntoMain := 2
	cov := branchlineage.ComputeCoverage(prs, g.Edges(), flow, []string{"main"}, mergedIntoMain)
	fmt.Printf("carrier share %.2f, explained share %.2f\n", cov.CarrierShare, cov.ExplainedShare)
	for _, c := range cov.Unexplained() {
		fmt.Println("unexplained", c.Carrier, c.Band, "carried", c.CarriedSum, "lines")
	}
	// Output:
	// carrier share 1.00, explained share 1.00
	// unexplained PR-3 zero_carried carried 0 lines
}

func Example_keepTheIndex() {
	ctx := context.Background()
	s, err := sqlite.Open(":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	at := func(hour int) time.Time { return time.Date(2026, 4, 2, hour, 0, 0, 0, time.UTC) }
	if _, err := s.Put(ctx,
		branchlineage.PR{Key: "PR-1", Repository: "acme/shop", Number: 1, HeadBranch: "feature/login", BaseBranch: "develop", MergedAt: at(1)},
		branchlineage.PR{Key: "PR-3", Repository: "acme/shop", Number: 3, HeadBranch: "develop", BaseBranch: "main", MergedAt: at(3)},
	); err != nil {
		log.Fatal(err)
	}
	if _, err := s.Derive(ctx); err != nil {
		log.Fatal(err)
	}

	late := branchlineage.PR{Key: "PR-2", Repository: "acme/shop", Number: 2, HeadBranch: "feature/cart", BaseBranch: "develop", MergedAt: at(2)}
	inv, err := s.Put(ctx, late)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("stale", inv.Keys())
	if _, err := s.Derive(ctx); err != nil {
		log.Fatal(err)
	}
	edges, err := s.EdgesFrom(ctx, []string{"PR-3"})
	if err != nil {
		log.Fatal(err)
	}
	for _, e := range edges {
		fmt.Println(e.Carrier, "->", e.Carried)
	}
	// Output:
	// stale [PR-3]
	// PR-3 -> PR-1
	// PR-3 -> PR-2
}

func Example_readConnectorRecords() {
	day := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)
	prs := vcs.Convert([]vcs.Record{
		{ExternalID: "mr-41!7#merged", DataDate: day.Add(9 * time.Hour),
			Metadata: map[string]string{"repo": "acme/shop", "head_branch": "feature/search", "target_branch": "develop"}},
		{ExternalID: "mr-41!8#merged", DataDate: day.Add(15 * time.Hour),
			Metadata: map[string]string{"repo": "acme/shop", "head_branch": "develop", "target_branch": "main"}},
	})
	g, err := branchlineage.Derive(prs)
	if err != nil {
		log.Fatal(err)
	}
	for _, e := range g.Edges() {
		fmt.Println(e.Carrier, "carried", e.Carried)
	}
	// Output:
	// mr-41!8#merged carried mr-41!7#merged
}

func carried(d branchlineage.Derivation) []string {
	var out []string
	for _, e := range d.Edges {
		out = append(out, e.Carried)
	}
	return out
}
