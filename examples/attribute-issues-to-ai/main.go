// Command attribute-issues-to-ai joins four packages on pull-request keys:
// issuelinkage links pull requests to issues, the attribution package's
// signature scanner marks AI-authored pull requests, prcomplexity scores each
// pull request, and issueattribution measures the issues.
//
//	go run ./examples/attribute-issues-to-ai
package main

import (
	"context"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/signature"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issueattribution"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/memory"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/prcomplexity"
)

func main() {
	ctx := context.Background()
	pr := func(n int, body string) issuelinkage.PR {
		return issuelinkage.PR{Key: fmt.Sprintf("acme/web#PR-%d", n), Vendor: issuelinkage.VendorGitHub,
			Repository: "acme/web", Number: n, Title: "change", Body: body}
	}
	prs := []issuelinkage.PR{
		pr(21, "Fixes ENG-1\n\nGenerated with Claude Code"),
		pr(22, "Implements ENG-2"),
		pr(23, "Tidy the retry loop.\n\n## Background\n\nENG-3 was the first attempt.\n\nGenerated with Claude Code"),
	}
	issue := func(key string) issuelinkage.Issue {
		return issuelinkage.Issue{Key: key, Vendor: issuelinkage.VendorLinear, Identifiers: []string{key}, Title: "work"}
	}
	issues := []issuelinkage.Issue{issue("ENG-1"), issue("ENG-2"), issue("ENG-3"), issue("ENG-4")}

	// 1. Link pull requests to issues.
	s := memory.New()
	if err := s.Put(ctx, prs, issues); err != nil {
		log.Fatal(err)
	}
	if _, err := issuelinkage.Run(ctx, s, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), issuelinkage.Config{}); err != nil {
		log.Fatal(err)
	}
	rows, err := s.Rows(ctx)
	if err != nil {
		log.Fatal(err)
	}
	var links []issueattribution.Link
	for _, r := range rows {
		for _, l := range r.Result.Links {
			dir := issueattribution.DirectionPRToIssue
			if l.From == issuelinkage.SideIssue {
				dir = issueattribution.DirectionIssueToPR
			}
			// Only a context-only mention keeps an issue from counting: a single
			// kind of evidence, or a lost closer election, still delivered work.
			blocked := slices.Contains(l.Blocking, issuelinkage.BlockedNegativeSection)
			links = append(links, issueattribution.Link{IssueKey: l.Issue, PRKey: l.PR, Direction: dir,
				Tier: issueattribution.Tier(l.Tier), Blocked: blocked})
			fmt.Printf("link %s -> %s %s blocked=%v\n", l.PR, l.Issue, l.Relation, blocked)
		}
	}

	// 2. Mark AI-authored pull requests and score each one.
	baseline := &prcomplexity.Baseline{PerLanguage: map[string]prcomplexity.LanguageStats{
		"Go": {MedianSLOC: 72, MedianFiles: 4, MuLogSLOC: 4.36, SigmaLogSLOC: 0.92, MuLogFiles: 1.41, SigmaLogFiles: 0.37},
	}}
	sizes := map[string]prcomplexity.LanguageLOC{
		"acme/web#PR-21": {SLOC: 300, Files: 6}, "acme/web#PR-22": {SLOC: 40, Files: 2}, "acme/web#PR-23": {SLOC: 90, Files: 3},
	}
	var measured []issueattribution.PR
	for _, p := range prs {
		score, ok, err := prcomplexity.ScoreComplexity(p.Key, map[string]prcomplexity.LanguageLOC{"Go": sizes[p.Key]},
			baseline, prcomplexity.FormulaCNCS, prcomplexity.DefaultDecay)
		if err != nil || !ok {
			log.Fatalf("scoring %s: %v %v", p.Key, ok, err)
		}
		ai := signature.ScanPRDescription(p.Body).IsAIAttributed
		measured = append(measured, issueattribution.PR{Key: p.Key, AIAuthored: ai, Complexity: score})
		fmt.Printf("pr %s ai=%v complexity=%.1f\n", p.Key, ai, *score)
	}

	// 3. Measure the completed issues. The blocked link is left out, so ENG-3
	// counts as unlinked although an AI-authored pull request mentions it.
	completed := []issueattribution.Issue{{Key: "ENG-1"}, {Key: "ENG-2"}, {Key: "ENG-3"}, {Key: "ENG-4"}}
	res, err := issueattribution.Compute(completed, links, measured, issueattribution.Policy{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("AI-assisted: %d of %d linked issues, coverage %.0f%%, low=%v\n", res.AI, res.Linked, *res.Coverage, res.LowCoverage)
	for _, ir := range res.Issues {
		fmt.Printf("%s linked=%v ai=%v\n", ir.Key, len(ir.PRs) > 0, ir.AI)
	}
}
