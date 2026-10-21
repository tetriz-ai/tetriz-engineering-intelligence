// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import "sort"

// CarriedPair is one carried pull request and the seeds that landed it on a
// tracked branch.
type CarriedPair struct {
	Carried string
	// LandedBy are the visible seeds under which Carried was expanded and found
	// terminal, sorted.
	LandedBy []string
}

// CarriedWork is the carried-work view of a walk.
type CarriedWork struct {
	// Pairs are sorted by carried key.
	Pairs []CarriedPair
	// Unresolvable counts, each once, the carried pull requests that would be
	// pairs but are not visible, were landed only by seeds that are not visible,
	// or were cut by a walk cap under every seed that reached them. A pull request
	// delivered under any seed is a pair, never also counted here.
	Unresolvable int
}

// CarriedBy reduces walk edges to the terminal carried pull requests under each
// seed: the pull requests a promotion delivered, with no intermediate carrier
// listed as work of its own.
//
// Terminality is decided per (seed, carried): a carried pull request is terminal
// under a seed when it has no visible child of its own under that seed. A
// carrier whose only children are not visible is therefore terminal itself. A
// terminal pull request that is not visible is dropped and counted in
// Unresolvable, and so is one a walk cap stopped the walk from expanding
// (WalkResult.Unexpanded), since whether it carried anything is unknown. visible
// reports whether a key is in the population being shown, such as a reporting
// window; nil means everything is visible.
func CarriedBy(w WalkResult, visible func(key string) bool) CarriedWork {
	if visible == nil {
		visible = func(string) bool { return true }
	}
	edges := w.Edges
	capped := make(map[WalkNode]struct{}, len(w.Unexpanded))
	for _, n := range w.Unexpanded {
		capped[n] = struct{}{}
	}
	type seedNode struct{ seed, node string }
	hasVisibleChild := make(map[seedNode]bool, len(edges))
	for _, e := range edges {
		if visible(e.Carried) {
			hasVisibleChild[seedNode{e.Seed, e.Carrier}] = true
		}
	}
	landings := make(map[string]map[string]struct{}, len(edges))
	unresolvable := make(map[string]struct{})
	seen := make(map[seedNode]bool, len(edges))
	for _, e := range edges {
		k := seedNode{e.Seed, e.Carried}
		if hasVisibleChild[k] || seen[k] {
			continue
		}
		seen[k] = true
		if _, cut := capped[WalkNode{Seed: e.Seed, Carrier: e.Carried}]; cut || !visible(e.Carried) {
			unresolvable[e.Carried] = struct{}{}
			continue
		}
		if landings[e.Carried] == nil {
			landings[e.Carried] = map[string]struct{}{}
		}
		landings[e.Carried][e.Seed] = struct{}{}
	}
	out := CarriedWork{Pairs: make([]CarriedPair, 0, len(landings))}
	for carried, seeds := range landings {
		p := CarriedPair{Carried: carried, LandedBy: make([]string, 0, len(seeds))}
		for s := range seeds {
			if visible(s) {
				p.LandedBy = append(p.LandedBy, s)
			}
		}
		if len(p.LandedBy) == 0 {
			unresolvable[carried] = struct{}{} // landed only by seeds outside the population
			continue
		}
		sort.Strings(p.LandedBy)
		out.Pairs = append(out.Pairs, p)
	}
	for _, p := range out.Pairs {
		delete(unresolvable, p.Carried) // delivered under some seed, so not unresolved
	}
	out.Unresolvable = len(unresolvable)
	sort.Slice(out.Pairs, func(i, j int) bool { return out.Pairs[i].Carried < out.Pairs[j].Carried })
	return out
}
