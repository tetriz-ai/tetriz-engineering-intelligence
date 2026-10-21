// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package memory_test

import (
	"context"
	"fmt"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage/store/memory"
)

// A late pull request makes the promotion that should have carried it stale;
// Derive re-derives only that promotion.
func Example() {
	ctx := context.Background()
	at := func(h int) time.Time { return time.Date(2026, 4, 1, h, 0, 0, 0, time.UTC) }
	s := memory.New()
	_, _ = s.Put(ctx,
		branchlineage.PR{Key: "PR-1", Repository: "acme/shop", Number: 1, HeadBranch: "feature/a", BaseBranch: "develop", MergedAt: at(9)},
		branchlineage.PR{Key: "PR-3", Repository: "acme/shop", Number: 3, HeadBranch: "develop", BaseBranch: "main", MergedAt: at(12)},
	)
	n, _ := s.Derive(ctx)
	fmt.Println("derived", n)

	inv, _ := s.Put(ctx, branchlineage.PR{Key: "PR-2", Repository: "acme/shop", Number: 2, HeadBranch: "feature/b", BaseBranch: "develop", MergedAt: at(10)})
	fmt.Println("stale", inv.Keys())
	n, _ = s.Derive(ctx)
	fmt.Println("derived", n)
	edges, _ := s.EdgesFrom(ctx, []string{"PR-3"})
	for _, e := range edges {
		fmt.Println(e.Carrier, "->", e.Carried)
	}
	// Output:
	// derived 2
	// stale [PR-3]
	// derived 2
	// PR-3 -> PR-1
	// PR-3 -> PR-2
}
