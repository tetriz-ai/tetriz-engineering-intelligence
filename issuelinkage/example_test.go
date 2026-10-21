// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage_test

import (
	"context"
	"fmt"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/memory"
)

func ExampleExtractPR() {
	pr := issuelinkage.PR{Key: "acme/web#PR-1", Vendor: issuelinkage.VendorGitHub, Repository: "acme/web",
		Body: "Fixes #12, see PROJ-4"}
	for _, c := range issuelinkage.ExtractPR(pr, issuelinkage.VendorJira, issuelinkage.DefaultSignals()).Candidates {
		fmt.Println(c.Identifier, c.Namespace, c.Intent)
	}
	// Output:
	// PROJ-4 jira reference_only
	// acme/web#12 github fixes
}

func ExampleExtractIssue() {
	is := issuelinkage.Issue{Key: "5f0c2a9e-0000-4000-8000-000000000001", Vendor: issuelinkage.VendorLinear, Identifiers: []string{"ENG-42"},
		NativePRRefs: []issuelinkage.NativeRef{{Key: "acme/web#PR-12", Source: issuelinkage.VendorLinear}}}
	for _, c := range issuelinkage.ExtractIssue(is, issuelinkage.DefaultSignals()).Candidates {
		fmt.Println(c.Identifier, c.Signal, c.Intent)
	}
	// Output:
	// acme/web#PR-12 native fixes
}

func ExampleBareKeyNamespace() {
	fmt.Printf("%q %q\n",
		issuelinkage.BareKeyNamespace([]issuelinkage.Vendor{issuelinkage.VendorJira, issuelinkage.VendorGitHub}),
		issuelinkage.BareKeyNamespace([]issuelinkage.Vendor{issuelinkage.VendorJira, issuelinkage.VendorLinear}))
	// Output:
	// "jira" ""
}

func ExampleProcess() {
	ctx := context.Background()
	s := memory.New()
	_ = s.Put(ctx, []issuelinkage.PR{{Key: "pr-old", Vendor: issuelinkage.VendorGitHub, Repository: "acme/web", Number: 1,
		MergedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}, {Key: "pr-new", Vendor: issuelinkage.VendorGitHub,
		Repository: "acme/web", Number: 2, MergedAt: time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC)}}, nil)
	is := issuelinkage.Issue{Key: "acme/web#ISSUE-7", Vendor: issuelinkage.VendorGitHub,
		NativePRRefs: []issuelinkage.NativeRef{{Key: "acme/web#PR-1", Source: issuelinkage.VendorGitHub}, {Key: "acme/web#PR-2", Source: issuelinkage.VendorGitHub}}}
	res, _ := issuelinkage.Process(ctx, s, issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: is.Key},
		issuelinkage.ExtractIssue(is, issuelinkage.DefaultSignals()), time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	fmt.Println("closer", res.Closer)
	for _, l := range res.Links {
		fmt.Println(l.PR, l.Relation, l.Closes, l.Blocking)
	}
	// Output:
	// closer pr-new
	// pr-new fixes true [single_signal_floor]
	// pr-old part_of false [single_signal_floor non_elected_closer]
}

func ExampleRun() {
	ctx := context.Background()
	s := memory.New()
	_ = s.Put(ctx, []issuelinkage.PR{{Key: "pr-1", Vendor: issuelinkage.VendorGitHub, Title: "Fixes PROJ-1"}},
		[]issuelinkage.Issue{{Key: "10001", Vendor: issuelinkage.VendorJira, Identifiers: []string{"PROJ-1"}}})
	rep, _ := issuelinkage.Run(ctx, s, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), issuelinkage.Config{})
	fmt.Println(rep.Eligible, rep.Processed, len(rep.Failures))
	// Output:
	// 2 2 0
}
