// Command index-on-sqlite keeps promotion edges in SQLite across a restart: a
// replayed batch changes nothing, and a late pull request makes the promotion
// that should have carried it stale until it is re-derived.
//
//	go run ./examples/index-on-sqlite
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage/store/sqlite"
)

func main() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "index-on-sqlite")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "lineage.db")

	at := func(hour int) time.Time { return time.Date(2026, 4, 2, hour, 0, 0, 0, time.UTC) }
	pr := func(n int, head, base string, hour int) branchlineage.PR {
		return branchlineage.PR{Key: fmt.Sprint("PR-", n), Repository: "acme/shop", Number: n,
			HeadBranch: head, BaseBranch: base, MergedAt: at(hour)}
	}
	batch := []branchlineage.PR{pr(1, "feature/login", "develop", 1), pr(3, "feature/cart", "develop", 3), pr(4, "develop", "main", 4)}

	s, err := sqlite.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	_, err = s.Put(ctx, batch...)
	must(err)
	n, err := s.Derive(ctx)
	must(err)
	fmt.Println("derived", n)
	show(ctx, s)
	must(s.Close())

	s, err = sqlite.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	inv, err := s.Put(ctx, batch...)
	must(err)
	pending, err := s.Pending(ctx)
	must(err)
	fmt.Println("replay after restart: stale", inv.Keys(), "pending", pending)

	inv, err = s.Put(ctx, pr(2, "feature/search", "develop", 2))
	must(err)
	for _, st := range inv.Carriers {
		fmt.Println("late PR-2 makes", st.Carrier, "stale:", st.Reason)
	}
	show(ctx, s)
	n, err = s.Derive(ctx)
	must(err)
	fmt.Println("derived", n)
	show(ctx, s)
}

func show(ctx context.Context, s *sqlite.Store) {
	edges, err := s.Edges(ctx, "acme/shop")
	must(err)
	var carried []string
	for _, e := range edges {
		carried = append(carried, e.Carrier+"->"+e.Carried)
	}
	fmt.Println("edges", carried)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
