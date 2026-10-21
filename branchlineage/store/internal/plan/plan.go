// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package plan is the bookkeeping both reference stores share, so the two
// cannot decide differently what a Put queues or what a Derive writes.
package plan

import (
	"fmt"
	"sort"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
)

// PutPlan is what one Put must apply.
type PutPlan struct {
	Invalidation branchlineage.Invalidation
	// DropEdges are the carriers whose live edges go: the stale ones, and any
	// pull request the batch made unmerged.
	DropEdges []string
	// Queue are the keys to queue for derivation.
	Queue []string
	// Forget are the keys no longer merged: unqueue them and mark them not derived.
	Forget []string
}

// Repositories returns the repositories a batch touches: those of the incoming
// pull requests and of the stored versions they replace. The caller loads every
// stored pull request in them, plus the stored versions themselves, for Put.
func Repositories(incoming, storedVersions []branchlineage.PR) []string {
	set := map[string]struct{}{}
	for _, l := range [][]branchlineage.PR{incoming, storedVersions} {
		for _, p := range l {
			if p.Repository != "" {
				set[p.Repository] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// Put plans a batch. stored is the stored state of every repository the batch
// touches plus the stored versions of the incoming keys; derived reports whether
// a key has been derived.
func Put(stored []branchlineage.PR, derived func(key string) bool, incoming []branchlineage.PR, opts ...branchlineage.Option) (PutPlan, error) {
	if err := checkBatch(incoming); err != nil {
		return PutPlan{}, err
	}
	before, err := branchlineage.NewHistory(dedupe(stored), opts...)
	if err != nil {
		return PutPlan{}, fmt.Errorf("indexing the stored pull requests: %w", err)
	}
	inv := branchlineage.Invalidate(before, incoming, opts...)

	after := map[string]bool{} // key -> merged in the new state, for keys the plan touches
	for _, s := range inv.Carriers {
		p, _ := before.PR(s.Carrier)
		after[s.Carrier] = p.Merged()
	}
	for _, p := range incoming {
		after[p.Key] = p.Merged()
	}

	var pl PutPlan
	pl.Invalidation = inv
	stale := map[string]struct{}{}
	for _, s := range inv.Carriers {
		stale[s.Carrier] = struct{}{}
	}
	keys := make([]string, 0, len(after))
	for k := range after {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, isStale := stale[k]
		switch {
		case !after[k]:
			pl.DropEdges = append(pl.DropEdges, k)
			pl.Forget = append(pl.Forget, k)
		case isStale:
			pl.DropEdges = append(pl.DropEdges, k)
			pl.Queue = append(pl.Queue, k)
		case !derived(k):
			pl.Queue = append(pl.Queue, k)
		}
	}
	return pl, nil
}

// dedupe keeps the first pull request per key: a caller's repository load and
// stored-version lookup overlap.
func dedupe(prs []branchlineage.PR) []branchlineage.PR {
	seen := make(map[string]struct{}, len(prs))
	out := make([]branchlineage.PR, 0, len(prs))
	for _, p := range prs {
		if _, dup := seen[p.Key]; dup {
			continue
		}
		seen[p.Key] = struct{}{}
		out = append(out, p)
	}
	return out
}

// checkBatch rejects an empty or repeated key, the same rule NewHistory applies.
func checkBatch(prs []branchlineage.PR) error {
	_, err := branchlineage.NewHistory(prs)
	return err
}

// Derive derives keys from state, the current pull requests of the keys'
// repositories. Put queues only merged pull requests and unqueues any it makes
// unmerged, so a queued key that is missing or not merged is an error.
func Derive(state []branchlineage.PR, keys []string, opts ...branchlineage.Option) ([]branchlineage.Derivation, error) {
	h, err := branchlineage.NewHistory(state, opts...)
	if err != nil {
		return nil, fmt.Errorf("indexing the current pull requests: %w", err)
	}
	ds := make([]branchlineage.Derivation, 0, len(keys))
	for _, k := range keys {
		d, err := h.Derive(k)
		if err != nil {
			return nil, fmt.Errorf("deriving %s: %w", k, err)
		}
		ds = append(ds, d)
	}
	return ds, nil
}
