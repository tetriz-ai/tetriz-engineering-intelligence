// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issueattribution_test

// Each Example here is a program in README.md, with the same main body;
// readme_test.go checks the two match and that the README shows this output.

import (
	"fmt"
	"log"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issueattribution"
)

func Example_quickStart() {
	issues := []issueattribution.Issue{{Key: "PROJ-1"}, {Key: "PROJ-2"}, {Key: "PROJ-3"}, {Key: "PROJ-4"}}
	links := []issueattribution.Link{
		{IssueKey: "PROJ-1", PRKey: "acme/web#PR-10"},
		{IssueKey: "PROJ-1", PRKey: "acme/web#PR-11"},
		{IssueKey: "PROJ-2", PRKey: "acme/web#PR-11"},
		{IssueKey: "PROJ-3", PRKey: "acme/web#PR-12"},
	}
	score := func(v float64) *float64 { return &v }
	prs := []issueattribution.PR{
		{Key: "acme/web#PR-10", AIAuthored: true, Complexity: score(6)},
		{Key: "acme/web#PR-11", Complexity: score(2)},
		{Key: "acme/web#PR-12", AIReviewed: true, Lines: 40},
	}

	res, err := issueattribution.Compute(issues, links, prs, issueattribution.Policy{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("AI-assisted: %d of %d linked issues (%.0f%%)\n", res.AI, res.Linked, *res.Headline)
	fmt.Printf("coverage: %d of %d issues linked (%.0f%%), low=%v\n", res.Linked, res.Total, *res.Coverage, res.LowCoverage)
	for _, ir := range res.Issues {
		if ir.Percent == nil {
			fmt.Printf("%s no linked pull request\n", ir.Key)
			continue
		}
		fmt.Printf("%s ai=%v %.0f%% by %s\n", ir.Key, ir.AI, *ir.Percent, ir.Basis)
	}
	// Output:
	// AI-assisted: 2 of 3 linked issues (67%)
	// coverage: 3 of 4 issues linked (75%), low=false
	// PROJ-1 ai=true 60% by complexity
	// PROJ-2 ai=false 0% by complexity
	// PROJ-3 ai=true 20% by lines
	// PROJ-4 no linked pull request
}

func Example_linkFloor() {
	issues := []issueattribution.Issue{{Key: "PROJ-1"}, {Key: "PROJ-2"}}
	links := []issueattribution.Link{
		// A mention only under a Background heading: context, not delivery.
		{IssueKey: "PROJ-1", PRKey: "acme/web#PR-10", Tier: issueattribution.TierLow, Blocked: true},
		{IssueKey: "PROJ-2", PRKey: "acme/web#PR-11", Tier: issueattribution.TierLow},
		{IssueKey: "PROJ-2", PRKey: "acme/web#PR-12", Tier: issueattribution.TierHigh},
		{IssueKey: "PROJ-2", PRKey: "acme/web#PR-99", Tier: issueattribution.TierHigh},
	}
	prs := []issueattribution.PR{
		{Key: "acme/web#PR-10", AIAuthored: true, Lines: 10},
		{Key: "acme/web#PR-11", AIAuthored: true, Lines: 10},
		{Key: "acme/web#PR-12", Lines: 10},
	}
	for _, p := range []issueattribution.Policy{
		{},
		{IncludeBlocked: true},
		{MinTier: issueattribution.TierHigh},
	} {
		res, err := issueattribution.Compute(issues, links, prs, p)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("blocked=%v min=%v: linked %d, AI %d, dropped %d\n", p.IncludeBlocked, p.MinTier, res.Linked, res.AI, res.DroppedLinks)
	}
	// Output:
	// blocked=false min=unknown: linked 1, AI 1, dropped 1
	// blocked=true min=unknown: linked 2, AI 2, dropped 1
	// blocked=false min=high: linked 1, AI 0, dropped 1
}

func Example_byRepositoryAndWeek() {
	week := func(day int) time.Time { return time.Date(2026, 6, day, 15, 0, 0, 0, time.UTC) }
	issues := []issueattribution.Issue{
		{Key: "PROJ-1", ClosedAt: week(2)}, {Key: "PROJ-2", ClosedAt: week(3)}, {Key: "PROJ-3", ClosedAt: week(10)},
	}
	links := []issueattribution.Link{
		{IssueKey: "PROJ-1", PRKey: "acme/web#PR-1"},
		{IssueKey: "PROJ-1", PRKey: "acme/api#PR-1"},
		{IssueKey: "PROJ-2", PRKey: "acme/api#PR-2"},
		{IssueKey: "PROJ-3", PRKey: "acme/web#PR-3"},
	}
	prs := []issueattribution.PR{
		{Key: "acme/web#PR-1", AIAuthored: true}, {Key: "acme/api#PR-1"}, {Key: "acme/api#PR-2"}, {Key: "acme/web#PR-3"},
	}
	res, err := issueattribution.Compute(issues, links, prs, issueattribution.Policy{})
	if err != nil {
		log.Fatal(err)
	}
	merged := map[string][]string{
		"acme/web": {"acme/web#PR-1", "acme/web#PR-3"},
		"acme/api": {"acme/api#PR-1", "acme/api#PR-2"},
	}
	byRepo := issueattribution.ByRepository(res, merged)
	fmt.Printf("acme/api %.0f%%, acme/web %.0f%%\n", byRepo["acme/api"], byRepo["acme/web"])
	weeks := issueattribution.Weekly(res)
	for _, monday := range []string{"2026-06-01", "2026-06-08"} {
		fmt.Printf("week of %s: %d linked, %d AI\n", monday, weeks[monday].Linked, weeks[monday].AI)
	}
	// Output:
	// acme/api 50%, acme/web 50%
	// week of 2026-06-01: 2 linked, 1 AI
	// week of 2026-06-08: 1 linked, 0 AI
}
