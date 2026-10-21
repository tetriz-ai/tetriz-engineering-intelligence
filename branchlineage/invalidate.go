// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import "sort"

// Reason says why a derived carrier went stale.
type Reason int

const (
	// ReasonUnknown is the zero value.
	ReasonUnknown Reason = iota
	// ReasonCorrection means the carrier's own inputs changed, or a pull request it
	// carried changed.
	ReasonCorrection
	// ReasonLateLeaf means a pull request arrived (or moved) into its window.
	ReasonLateLeaf
	// ReasonLatePromotion means an earlier promotion of the same pair arrived (or
	// moved), so the window's lower bound changed.
	ReasonLatePromotion
)

func (r Reason) String() string {
	switch r {
	case ReasonCorrection:
		return "correction"
	case ReasonLateLeaf:
		return "late_leaf"
	case ReasonLatePromotion:
		return "late_promotion"
	default:
		return "unknown"
	}
}

// Stale is one carrier to re-derive, and why.
type Stale struct {
	Carrier string
	Reason  Reason
}

// Invalidation lists the derived carriers a batch of pull requests made stale.
type Invalidation struct {
	// Carriers are sorted by key. Each appears once, under the first reason in
	// the order correction, late leaf, late promotion.
	Carriers []Stale
}

// Keys returns the stale carriers' keys, sorted.
func (inv Invalidation) Keys() []string {
	out := make([]string, 0, len(inv.Carriers))
	for _, s := range inv.Carriers {
		out = append(out, s.Carrier)
	}
	return out
}

// Invalidate returns the carriers in before, which must not be nil, that
// incoming pull requests make stale. before is the stored state; each incoming pull request replaces the
// stored one with its key whole. The caller then re-derives the returned
// carriers, and every incoming merged pull request it has not derived, from
// the new state.
//
// For each incoming merged pull request it probes, at its new position:
//
//   - as a leaf, the promotions whose window now holds it: per target, the
//     earliest merge from its base branch at or after it, or a later one when
//     commit times say the earlier heads did not hold it;
//   - as a promotion, the next promotion of its own pair, whose window it now
//     bounds.
//
// When it replaces a stored merged pull request whose derivation inputs differ
// (branches, merge time, first, head or merge commit, fork flag, line counts),
// the stored one is stale itself, and the same probes run at the stored
// position, so the promotions on the old branch pair re-derive too. An unchanged re-delivery changes nothing.
// A stored merged pull request replaced by an unmerged one is probed at its
// stored position only.
func Invalidate(before *History, incoming []PR, opts ...Option) Invalidation {
	obs := newConfig(opts).observer
	reason := map[string]Reason{}
	add := func(key string, r Reason) {
		if old, ok := reason[key]; !ok || r < old {
			reason[key] = r
		}
	}
	for _, e := range incoming {
		old, stored := before.byKey[e.Key]
		oldDerivable := stored && old.Merged() && old.Repository != ""
		if oldDerivable && !inputsChanged(old, e) {
			continue
		}
		if e.Merged() && e.Repository != "" {
			before.probe(e, add)
		}
		if !oldDerivable {
			// A stored merged pull request that had no repository derived nothing;
			// a version that changes its inputs, such as giving it one, re-derives it.
			if stored && old.Merged() && e.Merged() && inputsChanged(old, e) {
				add(e.Key, ReasonCorrection)
			}
			continue
		}
		// The stored pull request is stale itself, and every carrier that carried
		// it (a covering carrier at its old position) re-derives.
		add(old.Key, ReasonCorrection)
		if ri := before.byRepo[old.Repository]; ri != nil && old.BaseBranch != "" {
			for _, c := range ri.coveringCarriers(old) {
				add(c.Key, ReasonCorrection)
			}
		}
		before.probe(old, add)
	}
	inv := Invalidation{Carriers: make([]Stale, 0, len(reason))}
	for k, r := range reason {
		inv.Carriers = append(inv.Carriers, Stale{Carrier: k, Reason: r})
	}
	sort.Slice(inv.Carriers, func(i, j int) bool { return inv.Carriers[i].Carrier < inv.Carriers[j].Carrier })
	for _, s := range inv.Carriers {
		obs.Count("invalidation", map[string]string{"reason": s.Reason.String()})
	}
	return inv
}

// probe adds the stored carriers p's position makes stale: the covering
// carriers of p as a leaf and the next promotion of p's pair.
func (h *History) probe(p PR, add func(string, Reason)) {
	ri := h.byRepo[p.Repository]
	if ri == nil {
		return
	}
	if p.BaseBranch != "" {
		for _, c := range ri.coveringCarriers(p) {
			if c.Key != p.Key {
				add(c.Key, ReasonLateLeaf)
			}
		}
	}
	if p.HeadBranch != "" && p.BaseBranch != "" {
		if next, ok := ri.nextPromotion(p.HeadBranch, p.BaseBranch, p.pos()); ok && next.Key != p.Key {
			add(next.Key, ReasonLatePromotion)
		}
	}
}

// inputsChanged reports whether any field the deriver reads differs.
func inputsChanged(a, b PR) bool {
	return a.Repository != b.Repository ||
		a.Number != b.Number ||
		a.HeadBranch != b.HeadBranch ||
		a.BaseBranch != b.BaseBranch ||
		!a.MergedAt.Equal(b.MergedAt) ||
		!a.FirstCommitAt.Equal(b.FirstCommitAt) ||
		!a.HeadCommitAt.Equal(b.HeadCommitAt) ||
		!a.MergeCommitAt.Equal(b.MergeCommitAt) ||
		a.LinesAdded != b.LinesAdded ||
		a.LinesDeleted != b.LinesDeleted ||
		a.CrossRepository != b.CrossRepository
}
