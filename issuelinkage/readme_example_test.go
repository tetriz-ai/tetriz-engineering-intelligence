// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage_test

// Each Example here is a program in README.md, with the same main body;
// readme_test.go checks the two match and that the README shows this output.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"testing"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/adapter/vcs"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/memory"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/sqlite"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/storetest"
)

func Example_quickStart() {
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
	// Output:
	// processed
	// 10003 reference/low references_context closes=false blocking=[negative_section]
	// 10007 direct/high fixes closes=true blocking=[]
	// 10009 reference/low reference_only closes=false blocking=[]
}

func Example_extractAndProcess() {
	ctx := context.Background()
	// Any Lookup resolves candidates; a store is one.
	s := memory.New()
	issues := []issuelinkage.Issue{{Key: "10042", Vendor: issuelinkage.VendorJira, Identifiers: []string{"PROJ-42"}}}
	if err := s.Put(ctx, nil, issues); err != nil {
		log.Fatal(err)
	}
	pr := issuelinkage.PR{Key: "acme/web#PR-5", Vendor: issuelinkage.VendorGitHub, Repository: "acme/web",
		Title: "feat(PROJ-42): export to CSV", Body: "Refs PROJ-77."}

	ex := issuelinkage.ExtractPR(pr, issuelinkage.VendorJira, issuelinkage.DefaultSignals())
	for _, c := range ex.Candidates {
		fmt.Printf("candidate %s %s %s keyword=%q\n", c.Identifier, c.Field, c.Intent, c.Keyword)
	}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	res, err := issuelinkage.Process(ctx, s, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: pr.Key}, ex, now)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res.Status)
	for _, l := range res.Links {
		fmt.Printf("link %s %s/%s %s evidence=%d\n", l.Issue, l.Bucket, l.Tier, l.Relation, len(l.Evidence))
	}
	for _, u := range res.Unresolved {
		fmt.Printf("unresolved %s %s\n", u.Candidate.Identifier, u.Reason)
	}
	// Output:
	// candidate PROJ-42 pr_title bare keyword=""
	// candidate PROJ-77 pr_body reference_only keyword="refs"
	// candidate PROJ-42 pr_title implements keyword="conv_commit"
	// partially_processed
	// link 10042 direct/medium implements evidence=2
	// unresolved PROJ-77 not_found
}

func Example_keepLinksInSQLite() {
	ctx := context.Background()
	s, err := sqlite.Open(":memory:") // a file path keeps the links across restarts
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	pr := issuelinkage.PR{Key: "acme/api#PR-8", Vendor: issuelinkage.VendorGitHub, Repository: "acme/api", Number: 8,
		Title: "Fixes OPS-12", UpdatedAt: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)}
	issue := issuelinkage.Issue{Key: "20012", Vendor: issuelinkage.VendorJira, Identifiers: []string{"OPS-12"}}

	for day, put := range []func() error{
		func() error { return s.Put(ctx, []issuelinkage.PR{pr}, nil) },
		func() error { return s.Put(ctx, nil, []issuelinkage.Issue{issue}) },
	} {
		if err := put(); err != nil {
			log.Fatal(err)
		}
		now := time.Date(2026, 6, 1+day, 9, 0, 0, 0, time.UTC)
		if _, err := issuelinkage.Run(ctx, s, now, issuelinkage.Config{}); err != nil {
			log.Fatal(err)
		}
		// ok reports whether the store holds a row for the pull request.
		row, ok, err := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: pr.Key})
		if err != nil || !ok {
			log.Fatalf("no row for %s: %v", pr.Key, err)
		}
		fmt.Printf("day %d: %s, %d linked, %d waiting\n", day+1, row.Status, len(row.Result.Links), len(row.Result.Unresolved))
	}
	// Output:
	// day 1: partially_processed, 0 linked, 1 waiting
	// day 2: processed, 1 linked, 0 waiting
}

func Example_readConnectorRecords() {
	ctx := context.Background()
	prs := vcs.PRs(issuelinkage.VendorGitHub, []vcs.Record{{
		ExternalID: "acme/web#PR-12#merged",
		Metadata:   map[string]string{"repo": "acme/web", "head_branch": "fix/login"},
		Payload:    json.RawMessage(`{"number":12,"title":"Fix login","body":"","closingIssuesReferences":{"nodes":[{"number":7}]}}`),
	}})
	issues := vcs.Issues(issuelinkage.VendorGitHub, []vcs.Record{{
		ExternalID: "acme/web#ISSUE-7#snapshot",
		Payload:    json.RawMessage(`{"number":7,"title":"Login fails","repository":{"nameWithOwner":"acme/web"}}`),
	}})
	fmt.Println(prs[0].Key, prs[0].NativeIssueRefs, issues[0].Key, issues[0].Identifiers)

	s := memory.New()
	if err := s.Put(ctx, prs, issues); err != nil {
		log.Fatal(err)
	}
	if _, err := issuelinkage.Run(ctx, s, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), issuelinkage.Config{}); err != nil {
		log.Fatal(err)
	}
	// ok reports whether the store holds a row for the pull request.
	row, ok, err := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: prs[0].Key})
	if err != nil || !ok {
		log.Fatalf("no row for %s: %v", prs[0].Key, err)
	}
	l := row.Result.Links[0]
	fmt.Println(l.Issue, l.Relation, l.Signals, l.Closes)
	// Output:
	// acme/web#PR-12 [{acme/web#7 github}] acme/web#ISSUE-7 [acme/web#7]
	// acme/web#ISSUE-7 fixes [native] true
}

// TestMyStore is the README's "Test your own store" program.
func TestMyStore(t *testing.T) {
	// Replace memory.New with your own store's constructor.
	storetest.RunConformance(t, func(t *testing.T) issuelinkage.Store { return memory.New() })
}
