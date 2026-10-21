// Command measure-issue-ai-share measures how many completed issues AI-assisted pull requests delivered.
package main

import (
	"fmt"
	"log"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issueattribution"
)

func main() {
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
}
