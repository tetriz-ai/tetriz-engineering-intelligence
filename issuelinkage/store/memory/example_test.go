// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package memory_test

import (
	"context"
	"fmt"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/memory"
)

func ExampleNew() {
	ctx := context.Background()
	s := memory.New()
	_ = s.Put(ctx, []issuelinkage.PR{{Key: "pr-1", Vendor: issuelinkage.VendorGitHub, Title: "Fixes PROJ-1"}},
		[]issuelinkage.Issue{{Key: "10001", Vendor: issuelinkage.VendorJira, Identifiers: []string{"PROJ-1"}}})
	_, _ = issuelinkage.Run(ctx, s, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), issuelinkage.Config{})
	row, _, _ := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: "pr-1"})
	fmt.Println(row.Status, row.Result.Links[0].Issue)
	// Output:
	// processed 10001
}
