// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issueattribution_test

import (
	"fmt"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issueattribution"
)

func ExampleCompute() {
	res, _ := issueattribution.Compute(
		[]issueattribution.Issue{{Key: "PROJ-1"}, {Key: "PROJ-2"}},
		[]issueattribution.Link{{IssueKey: "PROJ-1", PRKey: "pr-1"}},
		[]issueattribution.PR{{Key: "pr-1", AIReviewed: true, Lines: 30}},
		issueattribution.Policy{Dimension: issueattribution.DimensionReviewed})
	fmt.Printf("%d of %d, coverage %.0f%%, low=%v\n", res.AI, res.Linked, *res.Coverage, res.LowCoverage)
	// Output:
	// 1 of 1, coverage 50%, low=true
}

func ExampleByRepository() {
	res, _ := issueattribution.Compute([]issueattribution.Issue{{Key: "I"}},
		[]issueattribution.Link{{IssueKey: "I", PRKey: "acme/web#PR-1"}},
		[]issueattribution.PR{{Key: "acme/web#PR-1", AIAuthored: true}}, issueattribution.Policy{})
	fmt.Println(issueattribution.ByRepository(res, map[string][]string{"acme/web": {"acme/web#PR-1"}}))
	// Output:
	// map[acme/web:100]
}

func ExampleWeekly() {
	res, _ := issueattribution.Compute([]issueattribution.Issue{{Key: "I", ClosedAt: time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)}},
		nil, nil, issueattribution.Policy{})
	fmt.Println(issueattribution.Weekly(res))
	// Output:
	// map[2026-06-01:{0 0}]
}

func ExampleMondayOf() {
	fmt.Println(issueattribution.MondayOf(time.Date(2026, 6, 7, 23, 0, 0, 0, time.UTC)))
	// Output:
	// 2026-06-01
}
