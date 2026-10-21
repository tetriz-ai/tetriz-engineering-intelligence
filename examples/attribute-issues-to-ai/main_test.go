// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issueattribution"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/memory"
)

// TestTiersLineUp pins the conversion issueattribution.Tier(link.Tier): the
// two packages' tiers share their values.
func TestTiersLineUp(t *testing.T) {
	pairs := []struct {
		l issuelinkage.Tier
		a issueattribution.Tier
	}{
		{issuelinkage.TierLow, issueattribution.TierLow},
		{issuelinkage.TierMedium, issueattribution.TierMedium},
		{issuelinkage.TierHigh, issueattribution.TierHigh},
	}
	for _, p := range pairs {
		if issueattribution.Tier(p.l) != p.a || p.l.String() != p.a.String() {
			t.Fatalf("tier %v converts to %v, want %v", p.l, issueattribution.Tier(p.l), p.a)
		}
	}
}

// Example_fromLinkageLinks is the issueattribution README's "From
// issuelinkage links" program; TestFromLinkageLinksIsTheREADMEProgram pins it.
func Example_fromLinkageLinks() {
	ctx := context.Background()
	s := memory.New()
	prs := []issuelinkage.PR{
		{Key: "acme/web#PR-1", Vendor: issuelinkage.VendorGitHub, Title: "Fixes PROJ-1"},
		{Key: "acme/web#PR-2", Vendor: issuelinkage.VendorGitHub, Body: "## Background\n\nPROJ-2 came first."},
	}
	issues := []issuelinkage.Issue{
		{Key: "10001", Vendor: issuelinkage.VendorJira, Identifiers: []string{"PROJ-1"}},
		{Key: "10002", Vendor: issuelinkage.VendorJira, Identifiers: []string{"PROJ-2"}},
	}
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
			links = append(links, issueattribution.Link{
				IssueKey: l.Issue, PRKey: l.PR, Direction: dir,
				// The two packages' tiers share their values.
				Tier: issueattribution.Tier(l.Tier),
				// Only a context-only mention is blocked.
				Blocked: slices.Contains(l.Blocking, issuelinkage.BlockedNegativeSection),
			})
		}
	}
	for _, l := range links {
		fmt.Printf("%s -> %s tier=%v blocked=%v\n", l.PRKey, l.IssueKey, l.Tier, l.Blocked)
	}

	res, err := issueattribution.Compute(
		[]issueattribution.Issue{{Key: "10001"}, {Key: "10002"}}, links,
		[]issueattribution.PR{{Key: "acme/web#PR-1", AIAuthored: true}, {Key: "acme/web#PR-2", AIAuthored: true}},
		issueattribution.Policy{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("AI-assisted: %d of %d linked, %d of %d issues linked\n", res.AI, res.Linked, res.Linked, res.Total)
	// Output:
	// acme/web#PR-1 -> 10001 tier=high blocked=false
	// acme/web#PR-2 -> 10002 tier=low blocked=true
	// AI-assisted: 1 of 1 linked, 1 of 2 issues linked
}

// TestFromLinkageLinksIsTheREADMEProgram pins the README program under
// "### From issuelinkage links" to Example_fromLinkageLinks: the same main
// body and the same output.
func TestFromLinkageLinksIsTheREADMEProgram(t *testing.T) {
	readme, err := os.ReadFile("../../issueattribution/README.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)### From issuelinkage links\n.*?```go\n(.*?)```\\s*```text\n(.*?)```").FindSubmatch(readme)
	if m == nil {
		t.Fatal("README.md: no go and text block under From issuelinkage links")
	}
	block := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(block, m[1], 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := body(t, block, "main"), body(t, "main_test.go", "Example_fromLinkageLinks"); got != want {
		t.Fatalf("README block and Example_fromLinkageLinks differ:\n%s\n---\n%s", got, want)
	}
	src, err := os.ReadFile("main_test.go")
	if err != nil {
		t.Fatal(err)
	}
	out := regexp.MustCompile(`(?s)func Example_fromLinkageLinks.*?// Output:\n(.*?)\n}`).FindSubmatch(src)
	var lines []string
	for _, l := range strings.Split(string(out[1]), "\n") {
		lines = append(lines, strings.TrimPrefix(strings.TrimSpace(l), "// "))
	}
	if got, want := strings.Join(lines, "\n"), strings.TrimSpace(string(m[2])); got != want {
		t.Fatalf("README output and the Example's Output differ:\n%s\n---\n%s", want, got)
	}
}

// body prints a function's body without comments or blank lines.
func body(t *testing.T, file, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
			var b bytes.Buffer
			if err := printer.Fprint(&b, fset, fd.Body); err != nil {
				t.Fatal(err)
			}
			var out []string
			for _, l := range strings.Split(b.String(), "\n") {
				if strings.TrimSpace(l) != "" {
					out = append(out, l)
				}
			}
			return strings.Join(out, "\n")
		}
	}
	t.Fatalf("%s: no func %s", file, name)
	return ""
}
