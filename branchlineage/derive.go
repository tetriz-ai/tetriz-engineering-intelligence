// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"fmt"
	"sort"
	"time"
)

// ColdStartLookbackDays bounds the window of a pair's first promotion: it opens
// no earlier than this many calendar days before the promotion merged.
const ColdStartLookbackDays = 90

// Outcome is what deriving one merged pull request found.
type Outcome int

// The outcomes, in the order the rules decide them.
const (
	// OutcomeUnknown is the zero value; a derivation never returns it.
	OutcomeUnknown Outcome = iota
	// OutcomeDropped means no identifiable hop. The pull request has no repository,
	// is from a fork, lacks a head or base branch, or merges a branch into itself.
	OutcomeDropped
	// OutcomeNonCarrier means its head branch never received a merged pull request
	// before it, so it carried nothing (a feature branch merged directly).
	OutcomeNonCarrier
	// OutcomeDerivedEmpty means a promotion whose window held nothing to carry.
	OutcomeDerivedEmpty
	// OutcomeCarrier means a promotion that carried at least one pull request.
	OutcomeCarrier
)

func (o Outcome) String() string {
	switch o {
	case OutcomeDropped:
		return "dropped"
	case OutcomeNonCarrier:
		return "non_carrier"
	case OutcomeDerivedEmpty:
		return "derived_empty"
	case OutcomeCarrier:
		return "carrier"
	default:
		return "unknown"
	}
}

// ColdStart names what opened a window when the pair had no earlier promotion.
type ColdStart int

const (
	// ColdStartNone means the window opened at the pair's previous promotion.
	ColdStartNone ColdStart = iota
	// ColdStartFirstCommit means the window opened at the promotion's first commit.
	ColdStartFirstCommit
	// ColdStartLookback means the window opened ColdStartLookbackDays before the merge,
	// because the first commit was unknown, earlier than that, or after the merge.
	ColdStartLookback
)

func (c ColdStart) String() string {
	switch c {
	case ColdStartFirstCommit:
		return "first_commit"
	case ColdStartLookback:
		return "lookback"
	default:
		return "none"
	}
}

// Evidence records how a promotion's window was bounded, so a disputed edge can
// be explained.
type Evidence struct {
	// WindowStart (exclusive) and WindowEnd (inclusive, the promotion's merge)
	// bound the merges into the head branch that the promotion carried.
	// WindowStart is the previous promotion's merge, or its head commit when
	// that is earlier; inside the bounds, a merge the promotion's head did not
	// hold, or the previous promotion's head did, is not carried (see
	// PR.HeadCommitAt).
	WindowStart time.Time
	WindowEnd   time.Time
	// ChainedFrom is the previous promotion of the same pair, whose merge, or
	// head commit, opened the window; empty on a cold start.
	ChainedFrom string
	// ColdStart says what opened the window when ChainedFrom is empty.
	ColdStart ColdStart
}

// Edge is one carrier-to-carried pair: the promotion Carrier carried Carried
// from Source onto Target.
type Edge struct {
	Repository string
	Carrier    string
	Carried    string
	Source     string
	Target     string
	// CarrierLines and CarriedLines are the two pull requests' changed lines when
	// the edge was derived; nil when the pull request had no diff data.
	CarrierLines *int64
	CarriedLines *int64
}

// Derivation is one merged pull request's derived edge set.
type Derivation struct {
	Carrier    string
	Repository string
	// Source and Target are the carrier's head and base branches.
	Source  string
	Target  string
	Outcome Outcome
	// Evidence is set when the pull request passed the carrier test.
	Evidence *Evidence
	// Edges are in merge order of the carried pull requests.
	Edges []Edge
}

// Derive derives the edge set of the merged pull request key, from the
// history's state: rules 1 to 7 of the deriver.
//
//  1. A pull request with no repository, from a fork, missing a branch, or
//     merging a branch into itself is dropped.
//  2. It is a carrier only if its head branch received a merged pull request
//     before it.
//  3. A pull request into the head branch whose own head is the base branch is a
//     back-merge from the target, never carried.
//  4. It carries the merges into its head branch after the previous merge of the
//     same (head, base) pair, up to and including itself. When commit times
//     say a merge was not in the head it merged, the next promotion of the
//     pair carries it instead (see PR.HeadCommitAt).
//  5. With no previous promotion, the window opens at its first commit, or
//     ColdStartLookbackDays before its merge, whichever is later; a first commit after
//     the merge is ignored.
//  6. A carried pull request that is itself a carrier keeps its own edges; the
//     walk follows them.
//  7. The result replaces the carrier's whole previous edge set.
func (h *History) Derive(key string) (Derivation, error) {
	c, ok := h.byKey[key]
	if !ok {
		return Derivation{}, fmt.Errorf("%w: no pull request %q", ErrInvalidPR, key)
	}
	if !c.Merged() {
		return Derivation{}, fmt.Errorf("%w: pull request %q has not merged", ErrInvalidPR, key)
	}
	return h.observed(c), nil
}

// observed derives c and reports the outcome to the Observer.
func (h *History) observed(c PR) Derivation {
	d := h.derive(c)
	h.obs.Count("carrier_outcome", map[string]string{"outcome": d.Outcome.String()})
	if len(d.Edges) > 0 {
		h.obs.Observe("edges_per_carrier", float64(len(d.Edges)), nil)
	}
	return d
}

func (h *History) derive(c PR) Derivation {
	source, target := c.HeadBranch, c.BaseBranch
	d := Derivation{Carrier: c.Key, Repository: c.Repository, Source: source, Target: target}
	if c.Repository == "" || c.CrossRepository || target == "" || source == "" || source == target {
		d.Outcome = OutcomeDropped
		return d
	}
	ri := h.byRepo[c.Repository]
	at := c.pos()
	if ri == nil || !ri.hasPriorInto(source, at) {
		d.Outcome = OutcomeNonCarrier
		return d
	}

	ev := &Evidence{WindowEnd: c.MergedAt}
	var lower pos
	prev, chained := ri.prevPromotion(source, target, at)
	if chained {
		lower = prev.pos()
		// A merge the previous promotion's head did not hold landed after that head
		// was committed, so it merged after it too: read back to there.
		if hca := prev.HeadCommitAt; !hca.IsZero() && hca.Before(lower.at) {
			lower = pos{at: hca}
		}
		ev.ChainedFrom = prev.Key
	} else {
		// Number 0 and an empty key keep a pull request merged at the bound itself
		// inside the window.
		// UTC, so the floor is the same whatever zone MergedAt carries.
		floor := c.MergedAt.UTC().AddDate(0, 0, -ColdStartLookbackDays)
		lower = pos{at: floor}
		ev.ColdStart = ColdStartLookback
		if fca := c.FirstCommitAt; !fca.IsZero() && fca.After(floor) && !fca.After(c.MergedAt) {
			lower = pos{at: fca}
			ev.ColdStart = ColdStartFirstCommit
		}
	}
	ev.WindowStart = lower.at
	d.Evidence = ev

	carrierLines := c.lines()
	for _, p := range ri.window(source, lower, at) {
		if p.Key == c.Key || p.HeadBranch == target || !inHead(p, c) || (chained && inHead(p, prev)) {
			continue
		}
		d.Edges = append(d.Edges, Edge{
			Repository:   c.Repository,
			Carrier:      c.Key,
			Carried:      p.Key,
			Source:       source,
			Target:       target,
			CarrierLines: carrierLines,
			CarriedLines: p.lines(),
		})
	}
	if len(d.Edges) == 0 {
		d.Outcome = OutcomeDerivedEmpty
		return d
	}
	d.Outcome = OutcomeCarrier
	return d
}

// DeriveAll derives every merged pull request in the history, ordered by
// repository and then merge order.
func (h *History) DeriveAll() []Derivation {
	var merged []PR
	for _, p := range h.byKey {
		if p.Merged() {
			merged = append(merged, p)
		}
	}
	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Repository != merged[j].Repository {
			return merged[i].Repository < merged[j].Repository
		}
		return merged[i].pos().before(merged[j].pos())
	})
	out := make([]Derivation, 0, len(merged))
	for _, p := range merged {
		out = append(out, h.observed(p))
	}
	return out
}
