// Command link-a-pr-to-issues links a pull request to the tracker issues it names.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/memory"
)

func main() {
	ctx := context.Background()
	pr := issuelinkage.PR{
		Key: "acme/web#PR-42", Vendor: issuelinkage.VendorGitHub, Repository: "acme/web", Number: 42,
		Title:      "Shorter login timeout",
		Body:       "Fixes PROJ-7, see PROJ-9.\n\n## Background\n\nPROJ-3 tried this first.",
		HeadBranch: "feature/PROJ-7-timeout",
	}
	issue := func(key, ident, title string) issuelinkage.Issue {
		return issuelinkage.Issue{Key: key, Vendor: issuelinkage.VendorJira, Identifiers: []string{ident}, Title: title}
	}
	issues := []issuelinkage.Issue{
		issue("10003", "PROJ-3", "Retry the login call"),
		issue("10007", "PROJ-7", "Login times out"),
		issue("10009", "PROJ-9", "Session expiry is unclear"),
	}

	s := memory.New()
	if err := s.Put(ctx, []issuelinkage.PR{pr}, issues); err != nil {
		log.Fatal(err)
	}
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if _, err := issuelinkage.Run(ctx, s, now, issuelinkage.Config{}); err != nil {
		log.Fatal(err)
	}
	// ok reports whether the store holds a row for the pull request.
	row, ok, err := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: pr.Key})
	if err != nil || !ok {
		log.Fatalf("no row for %s: %v", pr.Key, err)
	}
	fmt.Println(row.Status)
	for _, l := range row.Result.Links {
		fmt.Printf("%s %s/%s %s closes=%v blocking=%v\n", l.Issue, l.Bucket, l.Tier, l.Relation, l.Closes, l.Blocking)
	}
}
