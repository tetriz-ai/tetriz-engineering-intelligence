// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import "sort"

// History is an indexed, read-only set of pull requests: what the deriver and
// the invalidation read. Build one with [NewHistory].
type History struct {
	byKey  map[string]PR
	byRepo map[string]*repoIndex
	obs    Observer
}

// repoIndex holds one repository's merged pull requests, each list in merge
// order, under the three lookups the rules make.
type repoIndex struct {
	// into lists every merged pull request by base branch: the carrier test and
	// the contents window read it. Forks are included; a fork can be carried.
	into map[string][]PR
	// hop lists merged, non-fork pull requests by (head, base): the previous and
	// next promotion of a pair.
	hop map[[2]string][]PR
	// from lists merged, non-fork pull requests by head branch: the promotions
	// whose window a late leaf on that branch falls into.
	from map[string][]PR
}

// NewHistory indexes prs. It fails on an empty or repeated key (ErrInvalidPR).
func NewHistory(prs []PR, opts ...Option) (*History, error) {
	if err := validate(prs); err != nil {
		return nil, err
	}
	cfg := newConfig(opts)
	h := &History{
		byKey:  make(map[string]PR, len(prs)),
		byRepo: make(map[string]*repoIndex),
		obs:    cfg.observer,
	}
	for _, p := range prs {
		h.byKey[p.Key] = p
		if !p.Merged() || p.Repository == "" {
			continue
		}
		ri := h.byRepo[p.Repository]
		if ri == nil {
			ri = &repoIndex{into: map[string][]PR{}, hop: map[[2]string][]PR{}, from: map[string][]PR{}}
			h.byRepo[p.Repository] = ri
		}
		ri.into[p.BaseBranch] = append(ri.into[p.BaseBranch], p)
		if !p.CrossRepository {
			ri.hop[[2]string{p.HeadBranch, p.BaseBranch}] = append(ri.hop[[2]string{p.HeadBranch, p.BaseBranch}], p)
			ri.from[p.HeadBranch] = append(ri.from[p.HeadBranch], p)
		}
	}
	for _, ri := range h.byRepo {
		for _, l := range ri.into {
			sortByPos(l)
		}
		for _, l := range ri.hop {
			sortByPos(l)
		}
		for _, l := range ri.from {
			sortByPos(l)
		}
	}
	return h, nil
}

// PR returns the pull request with key, and whether there is one.
func (h *History) PR(key string) (PR, bool) {
	p, ok := h.byKey[key]
	return p, ok
}

// Keys returns every key in the history, sorted.
func (h *History) Keys() []string {
	out := make([]string, 0, len(h.byKey))
	for k := range h.byKey {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// firstAfter returns the index of the first entry in l strictly after at.
func firstAfter(l []PR, at pos) int {
	return sort.Search(len(l), func(i int) bool { return at.before(l[i].pos()) })
}

// firstAtOrAfter returns the index of the first entry in l at or after at.
func firstAtOrAfter(l []PR, at pos) int {
	return sort.Search(len(l), func(i int) bool { return !l[i].pos().before(at) })
}

// hasPriorInto is rule 1: did branch receive any merged pull request strictly
// before at?
func (ri *repoIndex) hasPriorInto(branch string, at pos) bool {
	l := ri.into[branch]
	return len(l) > 0 && l[0].pos().before(at)
}

// prevPromotion is rule 4's lower bound: the latest non-fork merge of the same
// (head, base) pair strictly before at.
func (ri *repoIndex) prevPromotion(head, base string, at pos) (PR, bool) {
	l := ri.hop[[2]string{head, base}]
	i := firstAtOrAfter(l, at)
	if i == 0 {
		return PR{}, false
	}
	return l[i-1], true
}

// nextPromotion is the first non-fork merge of the same pair strictly after at.
func (ri *repoIndex) nextPromotion(head, base string, at pos) (PR, bool) {
	l := ri.hop[[2]string{head, base}]
	i := firstAfter(l, at)
	if i == len(l) {
		return PR{}, false
	}
	return l[i], true
}

// coveringCarriers returns the non-fork merges from the leaf's base branch whose
// window holds the leaf: per pair, each promotion at or after it whose head held
// it while the previous promotion's head did not. Without commit times that is
// the earliest promotion at or after the leaf, apart from a leaf that merges a
// branch into itself, which derives nothing.
func (ri *repoIndex) coveringCarriers(leaf PR) []PR {
	branch, at := leaf.BaseBranch, leaf.pos()
	l := ri.from[branch]
	seen := map[string]struct{}{}
	var out []PR
	for _, p := range l[firstAtOrAfter(l, at):] {
		if _, done := seen[p.BaseBranch]; done {
			continue
		}
		seen[p.BaseBranch] = struct{}{}
		pair := ri.hop[[2]string{branch, p.BaseBranch}]
		for i := firstAtOrAfter(pair, at); i < len(pair); i++ {
			if inHead(leaf, pair[i]) && (i == 0 || !inHead(leaf, pair[i-1])) {
				out = append(out, pair[i])
			}
		}
	}
	return out
}

// window lists the merges into branch in (lower, upper], in merge order.
func (ri *repoIndex) window(branch string, lower, upper pos) []PR {
	l := ri.into[branch]
	start, end := firstAfter(l, lower), firstAfter(l, upper)
	if start >= end {
		return nil
	}
	return l[start:end]
}
