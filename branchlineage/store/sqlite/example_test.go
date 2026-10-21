// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package sqlite_test

import (
	"context"
	"fmt"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage/store/sqlite"
)

func ExampleOpen() {
	ctx := context.Background()
	s, err := sqlite.Open(":memory:")
	if err != nil {
		panic(err)
	}
	defer func() { _ = s.Close() }()
	at := func(h int) time.Time { return time.Date(2026, 4, 1, h, 0, 0, 0, time.UTC) }
	_, _ = s.Put(ctx,
		branchlineage.PR{Key: "PR-1", Repository: "acme/shop", Number: 1, HeadBranch: "feature/a", BaseBranch: "develop", MergedAt: at(9)},
		branchlineage.PR{Key: "PR-2", Repository: "acme/shop", Number: 2, HeadBranch: "develop", BaseBranch: "main", MergedAt: at(12)},
	)
	pending, _ := s.Pending(ctx)
	fmt.Println("pending", pending)
	_, _ = s.Derive(ctx)
	edges, _ := s.Edges(ctx, "acme/shop")
	fmt.Println(len(edges), edges[0].Carrier, "->", edges[0].Carried)
	// Output:
	// pending [PR-1 PR-2]
	// 1 PR-2 -> PR-1
}
