// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"context"
	"fmt"
	"sort"
)

// TrackedSeeds returns a seed for each merged pull request whose base branch
// tracked, which must not be nil, reports as counted, in merge order within each repository. Restrict
// prs to the window being measured first.
func TrackedSeeds(prs []PR, tracked func(repository, branch string) bool) []Seed {
	var in []PR
	for _, p := range prs {
		if p.Merged() && tracked(p.Repository, p.BaseBranch) {
			in = append(in, p)
		}
	}
	sort.Slice(in, func(i, j int) bool {
		if in[i].Repository != in[j].Repository {
			return in[i].Repository < in[j].Repository
		}
		return in[i].pos().before(in[j].pos())
	})
	out := make([]Seed, 0, len(in))
	for _, p := range in {
		out = append(out, Seed{Repository: p.Repository, Carrier: p.Key, Target: p.BaseBranch})
	}
	return out
}

// ProjectInput is what a projection reads.
type ProjectInput struct {
	// Seeds are the pull requests merged into a tracked branch (TrackedSeeds).
	Seeds []Seed
	// Flows is each repository's active flow (ResolveFlow's Hops). A repository
	// with no flow projects nothing.
	Flows map[string][]Hop
	// Attributed is the set of keys directly attributed to AI work, such as the
	// attribution engine's matched pr_keys. Keys compare exactly.
	Attributed map[string]struct{}
	// Limits bounds each repository's walk.
	Limits Limits
}

// Credit is one carrier credited with attributed work, and the chain that
// carried it: Path runs from Carrier through any intermediate carriers to the
// attributed pull request, the shortest such chain, ties broken by the chain's
// keys in order.
type Credit struct {
	Repository string
	Carrier    string
	Path       []string
}

// Projection is a projection's result.
type Projection struct {
	// Credits are sorted by carrier key. A carrier appears once however much
	// attributed work it carried, and intermediate carriers off the tracked
	// branch appear too: intersect with the population being counted.
	Credits []Credit
	// Walk is every repository's walk, merged: the admitted edges and what the
	// caps cut. A capped walk can leave carriers uncredited; check DepthCapped
	// and RouteCapped.
	Walk WalkResult
}

// Keys returns the credited carriers' keys, sorted.
func (p Projection) Keys() []string {
	out := make([]string, 0, len(p.Credits))
	for _, c := range p.Credits {
		out = append(out, c.Carrier)
	}
	return out
}

// Project walks each repository's seeds through its flow and credits every
// walked carrier whose carried lineage reaches an attributed key. An empty
// Attributed set credits nothing, and Walk still holds the walk. src must not be
// nil.
func Project(ctx context.Context, src EdgeSource, in ProjectInput, opts ...Option) (Projection, error) {
	if len(in.Flows) == 0 {
		return Projection{}, nil
	}
	byRepo := map[string][]Seed{}
	for _, s := range in.Seeds {
		if _, ok := in.Flows[s.Repository]; !ok {
			continue
		}
		byRepo[s.Repository] = append(byRepo[s.Repository], s)
	}
	repos := make([]string, 0, len(byRepo))
	for r := range byRepo {
		repos = append(repos, r)
	}
	sort.Strings(repos)

	var merged WalkResult
	for _, r := range repos {
		w, err := Walk(ctx, src, byRepo[r], in.Flows[r], in.Limits, opts...)
		if err != nil {
			return Projection{}, fmt.Errorf("walking repository %s: %w", r, err)
		}
		merged.Edges = append(merged.Edges, w.Edges...)
		merged.DepthCapped = append(merged.DepthCapped, w.DepthCapped...)
		merged.RouteCapped = merged.RouteCapped || w.RouteCapped
		merged.Unexpanded = append(merged.Unexpanded, w.Unexpanded...)
	}
	sort.Slice(merged.Unexpanded, func(i, j int) bool {
		if merged.Unexpanded[i].Seed != merged.Unexpanded[j].Seed {
			return merged.Unexpanded[i].Seed < merged.Unexpanded[j].Seed
		}
		return merged.Unexpanded[i].Carrier < merged.Unexpanded[j].Carrier
	})
	sort.Strings(merged.DepthCapped)
	edges := merged.Edges
	if len(edges) == 0 || len(in.Attributed) == 0 {
		return Projection{Walk: merged}, nil
	}

	adj := map[string][]string{}
	repoOf := map[string]string{}
	for _, e := range edges {
		adj[e.Carrier] = append(adj[e.Carrier], e.Carried)
		repoOf[e.Carrier] = e.Repository
	}
	carriers := make([]string, 0, len(adj))
	for c, children := range adj {
		carriers = append(carriers, c)
		sort.Strings(children)
	}
	sort.Strings(carriers)

	var credits []Credit
	for _, c := range carriers {
		if path := pathToAttributed(c, adj, in.Attributed); path != nil {
			credits = append(credits, Credit{Repository: repoOf[c], Carrier: c, Path: path})
		}
	}
	return Projection{Credits: credits, Walk: merged}, nil
}

// pathToAttributed is a breadth-first search from carrier over the carried
// edges to the nearest attributed carried pull request. A visited set makes it
// safe on any graph; nil means none is reached.
func pathToAttributed(carrier string, adj map[string][]string, attributed map[string]struct{}) []string {
	parent := map[string]string{carrier: ""}
	queue := []string{carrier}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, child := range adj[n] {
			if _, seen := parent[child]; seen {
				continue
			}
			parent[child] = n
			if _, ok := attributed[child]; ok {
				var path []string
				for at := child; at != ""; at = parent[at] {
					path = append(path, at)
				}
				for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
					path[i], path[j] = path[j], path[i]
				}
				return path
			}
			queue = append(queue, child)
		}
	}
	return nil
}
