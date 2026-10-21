// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"sort"
	"time"
)

// BranchPair counts the merged pull requests from one head branch into one base
// branch: the detector's input.
type BranchPair struct {
	Head  string
	Base  string
	Count int
	// FirstMergedAt and LastMergedAt are the pair's earliest and latest merges.
	// The detector reads them to order merges into a branch against its merges
	// out; zero means unknown, and an unknown time never drops a hop.
	FirstMergedAt time.Time
	LastMergedAt  time.Time
}

// MergePairs counts merged, non-fork pull requests per (head, base), with the
// earliest and latest merge of each, skipping an empty or blank branch and a
// branch merged into itself. Restrict prs to the window the detection should read
// before calling it.
func MergePairs(prs []PR) []BranchPair {
	agg := map[[2]string]*BranchPair{}
	for _, p := range prs {
		if !p.Merged() || p.CrossRepository || blank(p.HeadBranch) || blank(p.BaseBranch) || p.HeadBranch == p.BaseBranch {
			continue
		}
		k := [2]string{p.HeadBranch, p.BaseBranch}
		bp := agg[k]
		if bp == nil {
			bp = &BranchPair{Head: k[0], Base: k[1], FirstMergedAt: p.MergedAt, LastMergedAt: p.MergedAt}
			agg[k] = bp
		}
		bp.Count++
		if p.MergedAt.Before(bp.FirstMergedAt) {
			bp.FirstMergedAt = p.MergedAt
		}
		if p.MergedAt.After(bp.LastMergedAt) {
			bp.LastMergedAt = p.MergedAt
		}
	}
	out := make([]BranchPair, 0, len(agg))
	for _, bp := range agg {
		out = append(out, *bp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Head != out[j].Head {
			return out[i].Head < out[j].Head
		}
		return out[i].Base < out[j].Base
	})
	return out
}

// DeclaredFlow is a vendor-declared promotion direction, such as a branching
// model's development and production branches. Both must be set to count.
type DeclaredFlow struct {
	Development string
	Production  string
}

// FlowFacts are optional detection priors. They only break a tie or bias a
// choice among hops the merge counts already produced; a nil or empty FlowFacts
// gives the merge-history-only result.
type FlowFacts struct {
	// Recency maps a branch to its head commit time. It breaks an equal split
	// toward the more recently updated branch.
	Recency map[string]time.Time
	// Merged marks branches already merged into the default branch: a poor
	// terminal, so a non-merged sink is preferred.
	Merged map[string]bool
	// Declared is a vendor-declared direction. It breaks an equal split first.
	Declared *DeclaredFlow
}

// DetectionSupport is the fewest merges a direction needs to be suggested.
const DetectionSupport = 2

// DetectedFlow is the detector's suggestion for one repository.
type DetectedFlow struct {
	// Hops are in promotion order. Empty means no flow was observed.
	Hops []Hop
	// Support maps each hop to the merges behind it.
	Support map[Hop]int
}

type flowEdge struct {
	src, dst string
	count    int
}

// DetectFlow reduces merge counts to a suggested promotion flow:
//
//   - each unordered branch pair keeps only its dominant direction; an equal
//     split is dropped unless a prior breaks it, so a back-merge is never
//     suggested;
//   - a hop needs DetectionSupport merges;
//   - a residual cycle across three or more branches is broken at its weakest
//     edge, and the hops are ordered along the chain;
//   - a hop into a terminal branch survives only if its source receives merges
//     from a branch other than that terminal, no later than the hop's last
//     merge, so a feature branch merged straight to the trunk a few times is not
//     suggested, even when something is stacked onto it afterwards;
//   - the result is reduced to one connected flow into one terminal.
func DetectFlow(pairs []BranchPair, facts *FlowFacts) DetectedFlow {
	edges := dominantEdges(pairs, facts)
	edges = orderAcyclic(edges)
	// Gate before choosing the terminal, so a noisy leaf into one sink cannot
	// out-vote a quieter genuine chain into another.
	edges = keepLaneTerminals(edges, pairs)
	edges = reduceToSingleSink(edges, facts)

	if len(edges) == 0 {
		return DetectedFlow{}
	}
	out := DetectedFlow{Hops: make([]Hop, 0, len(edges)), Support: make(map[Hop]int, len(edges))}
	for _, e := range edges {
		h := Hop{Source: e.src, Target: e.dst}
		out.Hops = append(out.Hops, h)
		out.Support[h] = e.count
	}
	return out
}

// keepLaneTerminals drops a hop into a sink unless its source receives a merge,
// whatever the count, from a branch other than the hop's own target, no later
// than the hop's last merge: a merge of the trunk into a feature branch is a
// sync, not proof of a promotion source, and a merge after the branch's last
// promotion was promoted by none of them. A hop that is a chain link is kept.
func keepLaneTerminals(edges []flowEdge, pairs []BranchPair) []flowEdge {
	if len(edges) == 0 {
		return edges
	}
	inbound := make(map[string][]BranchPair, len(pairs))
	lastOut := make(map[[2]string]time.Time, len(pairs))
	for _, p := range pairs {
		if p.Base != "" && p.Head != "" && p.Head != p.Base && p.Count > 0 {
			inbound[p.Base] = append(inbound[p.Base], p)
			k := [2]string{p.Head, p.Base}
			if p.LastMergedAt.After(lastOut[k]) {
				lastOut[k] = p.LastMergedAt
			}
		}
	}
	receivesFromOther := func(branch, target string) bool {
		out := lastOut[[2]string{branch, target}]
		for _, in := range inbound[branch] {
			if in.Head != target && (out.IsZero() || in.FirstMergedAt.IsZero() || !in.FirstMergedAt.After(out)) {
				return true
			}
		}
		return false
	}
	isSrc := make(map[string]struct{}, len(edges))
	for _, e := range edges {
		isSrc[e.src] = struct{}{}
	}
	kept := make([]flowEdge, 0, len(edges))
	for _, e := range edges {
		if _, dstIsSrc := isSrc[e.dst]; !dstIsSrc {
			if !receivesFromOther(e.src, e.dst) {
				continue
			}
		}
		kept = append(kept, e)
	}
	return kept
}

// dominantEdges collapses each unordered pair to its dominant direction, after
// dropping pairs below DetectionSupport.
func dominantEdges(pairs []BranchPair, facts *FlowFacts) []flowEdge {
	type agg struct {
		src, dst string
		count    int
		tie      bool
	}
	best := map[[2]string]*agg{}
	for _, p := range pairs {
		if p.Count < DetectionSupport || blank(p.Head) || blank(p.Base) {
			continue
		}
		key := unordered(p.Head, p.Base)
		a := best[key]
		switch {
		case a == nil:
			best[key] = &agg{src: p.Head, dst: p.Base, count: p.Count}
		case p.Count > a.count:
			a.src, a.dst, a.count, a.tie = p.Head, p.Base, p.Count, false
		case p.Count == a.count:
			a.tie = true
		}
	}
	out := make([]flowEdge, 0, len(best))
	for _, a := range best {
		if a.tie {
			if e, ok := resolveTie(a.src, a.dst, a.count, facts); ok {
				out = append(out, e)
			}
			continue
		}
		out = append(out, flowEdge{src: a.src, dst: a.dst, count: a.count})
	}
	sort.Slice(out, func(i, j int) bool { return lessEdge(out[i], out[j]) })
	return out
}

// lessEdge orders by support descending, then names: never map order.
func lessEdge(a, b flowEdge) bool {
	if a.count != b.count {
		return a.count > b.count
	}
	if a.src != b.src {
		return a.src < b.src
	}
	return a.dst < b.dst
}

// lessWeakEdge orders by support ascending, then names.
func lessWeakEdge(a, b flowEdge) bool {
	if a.count != b.count {
		return a.count < b.count
	}
	if a.src != b.src {
		return a.src < b.src
	}
	return a.dst < b.dst
}

// resolveTie picks a direction for an equal split from the priors, declared
// flow first and recency second, or reports false to drop the pair.
func resolveTie(x, y string, count int, facts *FlowFacts) (flowEdge, bool) {
	if facts == nil {
		return flowEdge{}, false
	}
	if df := facts.Declared; df != nil && df.Development != "" && df.Production != "" {
		switch {
		case df.Development == x && df.Production == y:
			return flowEdge{src: x, dst: y, count: count}, true
		case df.Development == y && df.Production == x:
			return flowEdge{src: y, dst: x, count: count}, true
		}
	}
	rx, okx := facts.Recency[x]
	ry, oky := facts.Recency[y]
	if okx && oky && !rx.Equal(ry) {
		if ry.After(rx) {
			return flowEdge{src: x, dst: y, count: count}, true
		}
		return flowEdge{src: y, dst: x, count: count}, true
	}
	return flowEdge{}, false
}

func unordered(a, b string) [2]string {
	if a <= b {
		return [2]string{a, b}
	}
	return [2]string{b, a}
}

// orderAcyclic orders the edges along the chain, dropping the weakest edge on a
// cycle until a topological order exists.
func orderAcyclic(edges []flowEdge) []flowEdge {
	for {
		order, stuck := kahnOrder(edges)
		if len(stuck) == 0 {
			rank := make(map[string]int, len(order))
			for i, n := range order {
				rank[n] = i
			}
			sort.SliceStable(edges, func(i, j int) bool {
				if rank[edges[i].src] != rank[edges[j].src] {
					return rank[edges[i].src] < rank[edges[j].src]
				}
				return edges[i].count > edges[j].count
			})
			return edges
		}
		before := len(edges)
		edges = dropWeakestOnCycle(edges)
		if len(edges) >= before {
			return edges
		}
	}
}

// reduceToSingleSink keeps the sub-flow into one terminal: a non-merged sink
// before a merged one, then the most supporting merges, then the smaller name.
func reduceToSingleSink(edges []flowEdge, facts *FlowFacts) []flowEdge {
	if len(edges) == 0 {
		return edges
	}
	isSrc := map[string]bool{}
	nodes := map[string]bool{}
	for _, e := range edges {
		isSrc[e.src] = true
		nodes[e.src] = true
		nodes[e.dst] = true
	}
	var sinks []string
	for n := range nodes {
		if !isSrc[n] {
			sinks = append(sinks, n)
		}
	}
	if len(sinks) <= 1 {
		return edges
	}
	sort.Strings(sinks)

	rev := map[string][]string{}
	for _, e := range edges {
		rev[e.dst] = append(rev[e.dst], e.src)
	}
	var bestEdges []flowEdge
	bestMerged := true
	bestSupport := -1
	for _, s := range sinks {
		reaches := map[string]bool{s: true}
		stack := []string{s}
		for len(stack) > 0 {
			n := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, p := range rev[n] {
				if !reaches[p] {
					reaches[p] = true
					stack = append(stack, p)
				}
			}
		}
		kept := make([]flowEdge, 0, len(edges))
		support := 0
		for _, e := range edges {
			if reaches[e.dst] {
				kept = append(kept, e)
				support += e.count
			}
		}
		sMerged := facts != nil && facts.Merged[s]
		var better bool
		if sMerged != bestMerged {
			better = !sMerged
		} else {
			better = support > bestSupport
		}
		if bestEdges == nil || better {
			bestMerged, bestSupport, bestEdges = sMerged, support, kept
		}
	}
	return bestEdges
}

// kahnOrder returns a topological order and the nodes left unordered, which is
// non-empty exactly when the graph has a cycle.
func kahnOrder(edges []flowEdge) ([]string, map[string]bool) {
	indeg := map[string]int{}
	adj := map[string][]string{}
	nodes := map[string]bool{}
	for _, e := range edges {
		nodes[e.src] = true
		nodes[e.dst] = true
		indeg[e.dst]++
		adj[e.src] = append(adj[e.src], e.dst)
	}
	queue := make([]string, 0, len(nodes))
	for n := range nodes {
		if indeg[n] == 0 {
			queue = append(queue, n)
		}
	}
	sort.Strings(queue)
	var order []string
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		order = append(order, n)
		next := append([]string(nil), adj[n]...)
		sort.Strings(next)
		for _, m := range next {
			indeg[m]--
			if indeg[m] == 0 {
				queue = append(queue, m)
			}
		}
	}
	if len(order) == len(nodes) {
		return order, nil
	}
	resolved := map[string]bool{}
	for _, n := range order {
		resolved[n] = true
	}
	stuck := map[string]bool{}
	for n := range nodes {
		if !resolved[n] {
			stuck[n] = true
		}
	}
	return order, stuck
}

// dropWeakestOnCycle removes the weakest edge whose two ends lie on one cycle
// (the same strongly connected component), so an acyclic hop entering or
// leaving a cycle is never the one dropped.
func dropWeakestOnCycle(edges []flowEdge) []flowEdge {
	comp := components(edges)
	weakest := -1
	for i, e := range edges {
		if comp[e.src] != comp[e.dst] {
			continue
		}
		if weakest == -1 || lessWeakEdge(e, edges[weakest]) {
			weakest = i
		}
	}
	if weakest == -1 {
		return edges
	}
	return append(edges[:weakest], edges[weakest+1:]...)
}

// components labels each node with its strongly connected component (Tarjan),
// visiting nodes and neighbours in sorted order so labels are deterministic.
func components(edges []flowEdge) map[string]int {
	adj := map[string][]string{}
	var nodes []string
	seen := map[string]bool{}
	for _, e := range edges {
		adj[e.src] = append(adj[e.src], e.dst)
		for _, n := range []string{e.src, e.dst} {
			if !seen[n] {
				seen[n] = true
				nodes = append(nodes, n)
			}
		}
	}
	sort.Strings(nodes)
	for _, l := range adj {
		sort.Strings(l)
	}
	index, low := map[string]int{}, map[string]int{}
	onStack := map[string]bool{}
	comp := map[string]int{}
	var stack []string
	next, label := 0, 0
	var visit func(n string)
	visit = func(n string) {
		index[n], low[n] = next, next
		next++
		stack = append(stack, n)
		onStack[n] = true
		for _, m := range adj[n] {
			if _, ok := index[m]; !ok {
				visit(m)
				low[n] = min(low[n], low[m])
			} else if onStack[m] {
				low[n] = min(low[n], index[m])
			}
		}
		if low[n] == index[n] {
			for {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[top] = false
				comp[top] = label
				if top == n {
					break
				}
			}
			label++
		}
	}
	for _, n := range nodes {
		if _, ok := index[n]; !ok {
			visit(n)
		}
	}
	return comp
}
