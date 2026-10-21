// Command link-late-issues-on-sqlite keeps links in SQLite while pull requests
// and issues arrive in either order, and retries a reference whose issue has
// not arrived yet.
//
//	go run ./examples/link-late-issues-on-sqlite
//
// It writes a database in a temporary directory and removes it on exit.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/sqlite"
)

func main() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "issuelinkage")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	s, err := sqlite.Open(filepath.Join(dir, "links.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	day := func(d int) time.Time { return time.Date(2026, 6, d, 9, 0, 0, 0, time.UTC) }
	run := func(label string, now time.Time) {
		rep, err := issuelinkage.Run(ctx, s, now, issuelinkage.Config{})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s: processed %d\n", label, rep.Processed)
		rows, err := s.Rows(ctx)
		if err != nil {
			log.Fatal(err)
		}
		for _, r := range rows {
			fmt.Printf("  %s %s %s", r.Entity.Side, r.Entity.Key, r.Status)
			for _, l := range r.Result.Links {
				fmt.Printf(" linked:%s", l.Counterpart(r.Entity.Side))
			}
			for _, u := range r.Result.Unresolved {
				fmt.Printf(" waiting:%s(%s)", u.Candidate.Identifier, u.Reason)
			}
			fmt.Println()
		}
	}

	// Day 1: the pull request arrives before the issue it fixes.
	pr := issuelinkage.PR{Key: "acme/api#PR-8", Vendor: issuelinkage.VendorGitHub, Repository: "acme/api", Number: 8,
		Title: "Fixes OPS-12: cap the retry budget", UpdatedAt: day(1)}
	if err := s.Put(ctx, []issuelinkage.PR{pr}, nil); err != nil {
		log.Fatal(err)
	}
	run("day 1", day(1))

	// Day 2: the issue arrives. The pull request, updated within the retry
	// window, is retried, links it and re-arms it.
	issue := issuelinkage.Issue{Key: "20012", Vendor: issuelinkage.VendorJira, Identifiers: []string{"OPS-12"},
		Title: "Retries pile up", UpdatedAt: day(2)}
	if err := s.Put(ctx, nil, []issuelinkage.Issue{issue}); err != nil {
		log.Fatal(err)
	}
	run("day 2", day(2))

	// Day 3: the re-armed issue is read once more, and the pair settles.
	run("day 3", day(3))

	// Day 4: putting the same records again changes nothing, so there is no work.
	if err := s.Put(ctx, []issuelinkage.PR{pr}, []issuelinkage.Issue{issue}); err != nil {
		log.Fatal(err)
	}
	run("day 4", day(4))
}
