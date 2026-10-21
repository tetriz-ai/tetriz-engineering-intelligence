// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// ErrInvalidFlow wraps every rejection of a flow by ValidateFlow.
var ErrInvalidFlow = errors.New("branchlineage: invalid flow")

// ValidateFlow checks the shape of a flow a person confirms: no branch promoted
// into itself, no empty or blank branch (whitespace-only, as unicode.IsSpace
// defines it), no repeated hop, no cycle, and one terminal
// branch. An acyclic
// flow with one terminal is always connected, so every hop chains into the same
// flow. An empty flow is valid (it means "no flow").
func ValidateFlow(hops []Hop) error {
	adj := map[string][]string{}
	indeg := map[string]int{}
	outdeg := map[string]int{}
	nodes := map[string]bool{}
	seen := map[Hop]bool{}
	for _, h := range hops {
		if blank(h.Source) || blank(h.Target) {
			return fmt.Errorf("%w: hop %q has an empty or blank branch", ErrInvalidFlow, h.String())
		}
		if h.Source == h.Target {
			return fmt.Errorf("%w: a hop cannot promote branch %q into itself", ErrInvalidFlow, h.Source)
		}
		if seen[h] {
			return fmt.Errorf("%w: duplicate hop %s", ErrInvalidFlow, h)
		}
		seen[h] = true
		adj[h.Source] = append(adj[h.Source], h.Target)
		outdeg[h.Source]++
		indeg[h.Target]++
		nodes[h.Source] = true
		nodes[h.Target] = true
	}
	if len(nodes) == 0 {
		return nil
	}
	if hasCycle(nodes, adj, indeg) {
		return fmt.Errorf("%w: the flow contains a cycle", ErrInvalidFlow)
	}
	sinks := 0
	for n := range nodes {
		if outdeg[n] == 0 {
			sinks++
		}
	}
	if sinks != 1 {
		return fmt.Errorf("%w: the flow must chain toward a single target branch, but found %d terminal branches", ErrInvalidFlow, sinks)
	}
	return nil
}

// hasCycle peels in-degree-0 nodes (Kahn); a residue means a cycle.
func hasCycle(nodes map[string]bool, adj map[string][]string, indeg map[string]int) bool {
	local := make(map[string]int, len(nodes))
	queue := make([]string, 0, len(nodes))
	for n := range nodes {
		local[n] = indeg[n]
		if local[n] == 0 {
			queue = append(queue, n)
		}
	}
	removed := 0
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		removed++
		for _, m := range adj[n] {
			local[m]--
			if local[m] == 0 {
				queue = append(queue, m)
			}
		}
	}
	return removed != len(nodes)
}

// blank reports whether a branch name is empty or whitespace-only, as
// unicode.IsSpace defines whitespace.
func blank(name string) bool { return strings.TrimSpace(name) == "" }

// TrackedBranchOnFlow reports whether a tracked entry matches some hop's
// target, so the flow delivers work to a branch that is counted. A tracked
// branch that is only ever a source receives nothing along the flow. Entries
// match as in [BranchMatches]. Empty tracked yields false.
func TrackedBranchOnFlow(hops []Hop, tracked []string) bool {
	targets := make(map[string]bool, len(hops))
	for _, h := range hops {
		targets[h.Target] = true
	}
	for target := range targets {
		for _, entry := range tracked {
			if BranchMatches(entry, target) {
				return true
			}
		}
	}
	return false
}

// BranchMatches reports whether a tracked-branch entry matches a branch name:
// an exact entry by equality, an entry holding *, ? or [ as a path.Match glob,
// so "release/*" matches "release/1.0" but not "release/hotfix/1". A malformed
// glob matches nothing.
func BranchMatches(entry, name string) bool {
	if !strings.ContainsAny(entry, "*?[") {
		return entry == name
	}
	ok, err := path.Match(entry, name)
	if errors.Is(err, path.ErrBadPattern) {
		return false
	}
	return ok
}
