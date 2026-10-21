// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"context"
	"fmt"
	"sort"
)

// Hop is one directed step of a promotion flow: work landing on Source is
// promoted onto Target. A flow is a set of hops; order carries no meaning.
type Hop struct {
	Source string
	Target string
}

func (h Hop) String() string { return h.Source + "->" + h.Target }

// Seed is one walk start: a pull request merged into a tracked branch.
type Seed struct {
	// Repository groups seeds for Project; Walk does not read it.
	Repository string
	// Carrier is the seed pull request's key.
	Carrier string
	// Target is the branch it merged into. The walk never re-enters it.
	Target string
}

// WalkResult is a walk's output.
type WalkResult struct {
	// Edges are the admitted edges, in walk order.
	Edges []WalkEdge
	// DepthCapped names, sorted, the repositories whose lineage runs deeper than
	// Limits.MaxDepth: carried work past the cap is not in Edges.
	DepthCapped []string
	// RouteCapped reports that the walk held Limits.MaxRoutes routes and stopped
	// descending: carried work below the refused nodes is not in Edges.
	RouteCapped bool
	// Unexpanded are the nodes a cap stopped the walk from expanding, sorted:
	// under the depth cap, those with edges past it; under the route cap, every
	// node whose descent was refused. What they carried is unknown, so CarriedBy
	// counts them as unresolved rather than as delivered work.
	Unexpanded []WalkNode
}

// WalkNode is one carried pull request reached under one seed.
type WalkNode struct {
	Seed    string
	Carrier string
}

// WalkEdge is one admitted carrier-to-carried edge, tagged with the seed it
// descends from and its depth (1 is a direct edge of the seed).
type WalkEdge struct {
	Seed       string
	Carrier    string
	Carried    string
	Repository string
	Depth      int
}

// The walk's default bounds. Depth is a correctness bound: carried work deeper
// than it goes uncredited. Routes is a resource bound on the partial paths held
// at once, which grow without limit on a lattice of branches merged into several
// others.
const (
	DefaultMaxDepth  = 20
	DefaultMaxRoutes = 50000
)

// Limits bounds one walk. A zero or negative field takes its default.
type Limits struct {
	// MaxDepth is the deepest promotion hop followed, used exactly as given.
	MaxDepth int
	// MaxRoutes is the most partial paths held at once, used exactly as given.
	// It is the walk's only memory bound, so the caller owns it: a large value on
	// a lattice of branches can use a great deal of memory. It is checked at a
	// level boundary, and a level whose expansion it refuses still emits every
	// edge at that level, so the result never depends on edge order.
	MaxRoutes int
}

func (l Limits) depth() int {
	if l.MaxDepth <= 0 {
		return DefaultMaxDepth
	}
	return l.MaxDepth
}

func (l Limits) routes() int {
	if l.MaxRoutes <= 0 {
		return DefaultMaxRoutes
	}
	return l.MaxRoutes
}

// pathNode is one branch on one route, linked to the route it extends, so
// routes sharing a prefix share their nodes.
type pathNode struct {
	branch string
	parent *pathNode
}

func (p *pathNode) visited(branch string) bool {
	for n := p; n != nil; n = n.parent {
		if n.branch == branch {
			return true
		}
	}
	return false
}

// frontierNode is one carrier awaiting expansion on one route.
type frontierNode struct {
	seed    string
	carrier string
	path    *pathNode
}

// Walk expands each seed breadth-first through src's edges and returns every
// admitted edge, in walk order, with what the caps cut. src must not be nil.
//
// An edge is admitted when its (source, target) hop is in flow and its source
// branch is not already on the route: a route never re-enters a branch, which
// prunes a back-merge round trip and ends any cycle. A carried pull request
// reached twice under one seed through two carriers keeps both edges; an exact
// repeat of (seed, carrier, carried) is emitted once. No seeds or an empty flow
// admits nothing.
//
// The walk stops descending past limits.MaxDepth, and past limits.MaxRoutes
// routes. Both under-count carried work, so the result says so (DepthCapped,
// RouteCapped, Unexpanded), and so does the Observer (walk_depth_cap_hit,
// walk_route_cap_hit).
func Walk(ctx context.Context, src EdgeSource, seeds []Seed, flow []Hop, limits Limits, opts ...Option) (WalkResult, error) {
	obs := newConfig(opts).observer
	depthCap, routeCap := limits.depth(), limits.routes()
	if len(seeds) == 0 || len(flow) == 0 {
		return WalkResult{}, nil
	}
	flowSet := make(map[Hop]struct{}, len(flow))
	for _, h := range flow {
		flowSet[h] = struct{}{}
	}

	frontier := make([]frontierNode, 0, len(seeds))
	for _, s := range seeds {
		if s.Carrier == "" {
			continue
		}
		frontier = append(frontier, frontierNode{seed: s.Carrier, carrier: s.Carrier, path: &pathNode{branch: s.Target}})
	}

	var out []WalkEdge
	seen := make(map[[3]string]struct{})
	truncated := make(map[string]struct{})
	unexpanded := make(map[WalkNode]struct{})
	maxDepthReached, routesHeld, peakRoutes, peakDemand := 0, 0, 0, 0
	routeCapHit := false
	for depth := 1; len(frontier) > 0; depth++ {
		routesHeld += len(frontier)
		peakRoutes = max(peakRoutes, len(frontier))
		carriers := distinctCarriers(frontier)
		obs.Observe("walk_carriers_loaded", float64(len(carriers)), nil)
		obs.Observe("walk_routes", float64(len(frontier)), nil)
		edges, err := src.EdgesFrom(ctx, carriers)
		if err != nil {
			return WalkResult{}, fmt.Errorf("walk level %d: %w", depth, err)
		}
		byCarrier := make(map[string][]Edge, len(carriers))
		for _, e := range edges {
			byCarrier[e.Carrier] = append(byCarrier[e.Carrier], e)
		}
		for _, es := range byCarrier {
			sort.Slice(es, func(i, j int) bool { return es[i].Carried < es[j].Carried })
		}

		var next []frontierNode
		nextDemand := 0
		for _, node := range frontier {
			for _, e := range byCarrier[node.carrier] {
				if _, ok := flowSet[Hop{Source: e.Source, Target: e.Target}]; !ok {
					continue
				}
				if node.path.visited(e.Source) {
					continue
				}
				if depth > depthCap {
					truncated[e.Repository] = struct{}{}
					unexpanded[WalkNode{Seed: node.seed, Carrier: node.carrier}] = struct{}{}
					continue
				}
				maxDepthReached = max(maxDepthReached, depth)
				k := [3]string{node.seed, e.Carrier, e.Carried}
				if _, dup := seen[k]; !dup {
					seen[k] = struct{}{}
					out = append(out, WalkEdge{Seed: node.seed, Carrier: e.Carrier, Carried: e.Carried, Repository: e.Repository, Depth: depth})
				}
				// The level keeps emitting once the budget runs out; only the
				// descent is refused.
				nextDemand++
				if !routeCapHit && routesHeld+len(next)+1 > routeCap {
					routeCapHit = true
				}
				if routeCapHit {
					unexpanded[WalkNode{Seed: node.seed, Carrier: e.Carried}] = struct{}{}
				} else {
					next = append(next, frontierNode{seed: node.seed, carrier: e.Carried, path: &pathNode{branch: e.Source, parent: node.path}})
				}
			}
		}
		peakRoutes = max(peakRoutes, len(next))
		peakDemand = max(peakDemand, nextDemand)
		if depth > depthCap {
			break
		}
		if routeCapHit {
			// The descent stops here, so the nodes already queued are not expanded
			// either.
			for _, n := range next {
				unexpanded[WalkNode{Seed: n.seed, Carrier: n.carrier}] = struct{}{}
			}
			break
		}
		frontier = next
	}
	res := WalkResult{Edges: out, RouteCapped: routeCapHit}
	for r := range truncated {
		res.DepthCapped = append(res.DepthCapped, r)
	}
	sort.Strings(res.DepthCapped)
	for _, r := range res.DepthCapped {
		obs.Count("walk_depth_cap_hit", map[string]string{"repository": r})
	}
	for n := range unexpanded {
		res.Unexpanded = append(res.Unexpanded, n)
	}
	sort.Slice(res.Unexpanded, func(i, j int) bool {
		if res.Unexpanded[i].Seed != res.Unexpanded[j].Seed {
			return res.Unexpanded[i].Seed < res.Unexpanded[j].Seed
		}
		return res.Unexpanded[i].Carrier < res.Unexpanded[j].Carrier
	})
	obs.Observe("walk_depth_reached", float64(maxDepthReached), nil)
	obs.Observe("walk_peak_routes", float64(peakRoutes), nil)
	if routeCapHit {
		obs.Count("walk_route_cap_hit", nil)
		obs.Observe("walk_route_demand", float64(peakDemand), nil)
	}
	return res, nil
}

// distinctCarriers lists the frontier's carriers once each, in frontier order.
func distinctCarriers(frontier []frontierNode) []string {
	seen := make(map[string]struct{}, len(frontier))
	out := make([]string, 0, len(frontier))
	for _, n := range frontier {
		if _, ok := seen[n.carrier]; ok {
			continue
		}
		seen[n.carrier] = struct{}{}
		out = append(out, n.carrier)
	}
	return out
}
