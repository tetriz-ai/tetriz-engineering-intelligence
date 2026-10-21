// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import "sort"

// FlowInput is one repository's evidence about its promotion flow.
type FlowInput struct {
	// Confirmed marks a flow a person decided. Confirmed with no hops is an
	// explicit off switch: the repository attributes nothing, whatever the edges say.
	Confirmed     bool
	ConfirmedHops []Hop
	// DetectedHops is the detector's suggestion (see DetectFlow). ResolveFlow may
	// add edge-proved hops to it. It never removes one unless refused.
	DetectedHops []Hop
	// EdgePairs is every (source, target) pair the deriver stored for the
	// repository, in both directions (see EdgePairs): a back-merge carrier
	// produces a pair just as a promotion does.
	EdgePairs []Hop
	// Tracked is the set of branches the organisation counts work on. Empty
	// leaves the edge evidence inert rather than guessing a direction.
	Tracked map[string]struct{}
}

// FlowOrigin says where a resolved flow came from.
type FlowOrigin int

const (
	// OriginNone means the repository attributes nothing.
	OriginNone FlowOrigin = iota
	// OriginConfirmed means the hops are a person's confirmed flow, used verbatim.
	OriginConfirmed
	// OriginInferred means the hops are inferred from detection and proved edges.
	OriginInferred
)

func (o FlowOrigin) String() string {
	switch o {
	case OriginConfirmed:
		return "confirmed"
	case OriginInferred:
		return "inferred"
	default:
		return "none"
	}
}

// Flow is one repository's resolved flow.
type Flow struct {
	// Hops is the active flow the walk is scoped to. Empty attributes nothing.
	Hops []Hop
	// Origin says whether Hops is a confirmed or an inferred flow.
	Origin FlowOrigin
	// Terminals are the branches a carrier must merge into for its carried work
	// to count: the tracked branches the flow reaches, else the flow's sinks.
	Terminals []string
	// Observed is the forward flow the stored edges prove, whatever the
	// confirmation state, and never a hop the reduction refused.
	Observed []Hop
	// EdgesReachNoTrackedBranch reports that an unconfirmed repository's edges
	// touch no tracked branch, so the detected hops stand alone. Usually a
	// branch-name mismatch.
	EdgesReachNoTrackedBranch bool
	// Refused lists the detected hops left out, each with why.
	Refused []RefusedHop
}

// RefusalReason says why a hop was left out of a flow.
type RefusalReason int

const (
	// RefusedUnknown is the zero value; no refusal.
	RefusedUnknown RefusalReason = iota
	// RefusedAmbiguousDirection means edges exist in both directions, so nothing says
	// which way work is carried. Only a person can settle it, by confirming.
	RefusedAmbiguousDirection
	// RefusedFlowsFromTrackedBranch means the hop leaves a tracked branch for an
	// untracked one, a back-merge.
	RefusedFlowsFromTrackedBranch
	// RefusedReachesNoTrackedBranch means following the hop never reaches a tracked
	// branch, so work carried along it lands nowhere that counts.
	RefusedReachesNoTrackedBranch
)

func (r RefusalReason) String() string {
	switch r {
	case RefusedAmbiguousDirection:
		return "ambiguous_direction"
	case RefusedFlowsFromTrackedBranch:
		return "flows_from_tracked_branch"
	case RefusedReachesNoTrackedBranch:
		return "reaches_no_tracked_branch"
	default:
		return "unknown"
	}
}

// RefusedHop is one refused hop and its reason.
type RefusedHop struct {
	Hop    Hop
	Reason RefusalReason
}

// ResolveFlow applies the flow rules to one repository's evidence.
//
// A confirmed flow is used verbatim. Otherwise the detected hops and the proved
// edge pairs are merged and reduced to a forward flow in one pass, because an
// ambiguous pair only shows once both halves are in one set: a detected
// develop->release beside a proved release->develop is a promotion and its
// back-merge, and both are refused. When the reduction admits nothing, or there
// are no edges to reduce, the detected hops stand as the flow, except those
// refused as ambiguous or as leaving a tracked branch.
func ResolveFlow(in FlowInput) Flow {
	reduced, why := forwardFlow(union(in.DetectedHops, in.EdgePairs), in.Tracked)
	observed := intersectPairs(reduced, in.EdgePairs)
	refused := refusedDetected(in.DetectedHops, why)

	if in.Confirmed {
		hops := union(in.ConfirmedHops)
		if len(hops) == 0 {
			return Flow{Observed: observed, Refused: refused}
		}
		return Flow{
			Hops:      hops,
			Origin:    OriginConfirmed,
			Terminals: terminalsOf(hops, in.Tracked),
			Observed:  observed,
			// A confirmed hop overrides the reduction, so it is not reported refused.
			Refused: refusedOutside(refused, hops),
		}
	}

	hops := union(in.DetectedHops)
	var reachedNoTracked bool
	switch {
	case len(in.EdgePairs) > 0 && len(in.Tracked) > 0 && len(reduced) > 0:
		hops = reduced
	case len(in.Tracked) > 0:
		// The detected hops stand, except back-merges out of a tracked branch and
		// hops the edges recorded in both directions, which stay refused. The
		// other refusals were verdicts on an output not in use, and would
		// contradict the flow returned.
		hops, refused = withoutHardRefusals(hops, refused)
		reachedNoTracked = len(in.EdgePairs) > 0 && !touchesTracked(in.EdgePairs, in.Tracked)
	}
	refused = refusedOutside(refused, hops)
	if len(hops) == 0 {
		return Flow{Observed: observed, EdgesReachNoTrackedBranch: reachedNoTracked, Refused: refused}
	}
	return Flow{
		Hops:                      hops,
		Origin:                    OriginInferred,
		Terminals:                 terminalsOf(hops, in.Tracked),
		Observed:                  observed,
		EdgesReachNoTrackedBranch: reachedNoTracked,
		Refused:                   refused,
	}
}

// withoutHardRefusals drops the hops refused as a back-merge out of a tracked
// branch or as ambiguous, and keeps only those refusals.
func withoutHardRefusals(hops []Hop, refused []RefusedHop) ([]Hop, []RefusedHop) {
	drop := map[Hop]bool{}
	var kept []RefusedHop
	for _, r := range refused {
		if r.Reason == RefusedAmbiguousDirection || r.Reason == RefusedFlowsFromTrackedBranch {
			drop[r.Hop] = true
			kept = append(kept, r)
		}
	}
	var out []Hop
	for _, h := range hops {
		if !drop[h] {
			out = append(out, h)
		}
	}
	return out, kept
}

// touchesTracked reports whether any pair starts or ends on a tracked branch.
func touchesTracked(pairs []Hop, tracked map[string]struct{}) bool {
	for _, p := range pairs {
		_, s := tracked[p.Source]
		_, t := tracked[p.Target]
		if s || t {
			return true
		}
	}
	return false
}

// EdgePairs returns the distinct (source, target) pairs of edges, sorted: the
// raw evidence FlowInput.EdgePairs takes.
func EdgePairs(edges []Edge) []Hop {
	seen := make(map[Hop]struct{}, len(edges))
	var out []Hop
	for _, e := range edges {
		h := Hop{Source: e.Source, Target: e.Target}
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Target < out[j].Target
	})
	return out
}

// refusedDetected narrows the reduction's refusals to the detected hops, the
// only ones a person sees and can confirm.
func refusedDetected(detected []Hop, why map[Hop]RefusalReason) []RefusedHop {
	if len(detected) == 0 || len(why) == 0 {
		return nil
	}
	var out []RefusedHop
	seen := make(map[Hop]struct{}, len(detected))
	for _, h := range detected {
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		if reason, refused := why[h]; refused {
			out = append(out, RefusedHop{Hop: h, Reason: reason})
		}
	}
	return out
}

// refusedOutside drops any refusal whose hop is in the flow, so Refused and
// Hops never share a hop.
func refusedOutside(refused []RefusedHop, hops []Hop) []RefusedHop {
	if len(refused) == 0 || len(hops) == 0 {
		return refused
	}
	kept := make(map[Hop]struct{}, len(hops))
	for _, h := range hops {
		kept[h] = struct{}{}
	}
	out := refused[:0:0]
	for _, r := range refused {
		if _, in := kept[r.Hop]; in {
			continue
		}
		out = append(out, r)
	}
	return out
}

// intersectPairs returns the members of hops that also appear in want, in hops' order.
func intersectPairs(hops, want []Hop) []Hop {
	if len(hops) == 0 || len(want) == 0 {
		return nil
	}
	keys := make(map[Hop]struct{}, len(want))
	for _, h := range want {
		keys[h] = struct{}{}
	}
	out := make([]Hop, 0, len(hops))
	for _, h := range hops {
		if _, ok := keys[h]; ok {
			out = append(out, h)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// union merges hop lists, dropping repeats and keeping first-appearance order.
func union(lists ...[]Hop) []Hop {
	var total int
	for _, l := range lists {
		total += len(l)
	}
	seen := make(map[Hop]struct{}, total)
	out := make([]Hop, 0, total)
	for _, l := range lists {
		for _, h := range l {
			if _, dup := seen[h]; dup {
				continue
			}
			seen[h] = struct{}{}
			out = append(out, h)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// terminalsOf returns the tracked branches the flow reaches, or, when it
// reaches none, the flow's sinks (targets that are never a source). Anchoring on
// the tracked set keeps a tracked branch terminal when the flow widens past it.
func terminalsOf(hops []Hop, tracked map[string]struct{}) []string {
	isSource := make(map[string]bool, len(hops))
	for _, h := range hops {
		isSource[h.Source] = true
	}
	var onTracked, sinks []string
	seenTracked := make(map[string]struct{}, len(hops))
	seenSink := make(map[string]struct{}, len(hops))
	for _, h := range hops {
		t := h.Target
		if t == "" {
			continue
		}
		if _, ok := tracked[t]; ok {
			if _, dup := seenTracked[t]; !dup {
				seenTracked[t] = struct{}{}
				onTracked = append(onTracked, t)
			}
		}
		if isSource[t] {
			continue
		}
		if _, dup := seenSink[t]; !dup {
			seenSink[t] = struct{}{}
			sinks = append(sinks, t)
		}
	}
	if len(onTracked) > 0 {
		return onTracked
	}
	return sinks
}

// forwardFlow reduces raw pairs to the forward flow toward the tracked
// branches, recording a reason for each refused pair:
//
//  1. A hop from a tracked branch into an untracked one is a back-merge. A hop
//     between two tracked branches stays: it may be an inter-trunk promotion.
//  2. A surviving hop whose reverse also survived is ambiguous; both go.
//  3. Only hops whose target reaches a tracked branch stay.
//
// No pairs or no tracked branches yields nil.
func forwardFlow(pairs []Hop, terminals map[string]struct{}) ([]Hop, map[Hop]RefusalReason) {
	if len(pairs) == 0 || len(terminals) == 0 {
		return nil, nil
	}
	why := make(map[Hop]RefusalReason, len(pairs))
	kept := make([]Hop, 0, len(pairs))
	present := make(map[Hop]struct{}, len(pairs))
	for _, h := range pairs {
		if h.Source == "" || h.Target == "" {
			continue // malformed, not refused: no reason to report
		}
		_, srcTracked := terminals[h.Source]
		_, dstTracked := terminals[h.Target]
		if srcTracked && !dstTracked {
			why[h] = RefusedFlowsFromTrackedBranch
			continue
		}
		kept = append(kept, h)
		present[h] = struct{}{}
	}
	cand := make([]Hop, 0, len(kept))
	revAdj := make(map[string][]string, len(kept))
	for _, h := range kept {
		if _, reverse := present[Hop{Source: h.Target, Target: h.Source}]; reverse {
			why[h] = RefusedAmbiguousDirection
			continue
		}
		cand = append(cand, h)
		revAdj[h.Target] = append(revAdj[h.Target], h.Source)
	}
	// Reverse breadth-first search from the tracked branches: cycle-safe, since
	// a node is never added twice.
	reaches := make(map[string]struct{}, len(revAdj))
	queue := make([]string, 0, len(terminals))
	for t := range terminals {
		reaches[t] = struct{}{}
		queue = append(queue, t)
	}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for _, src := range revAdj[n] {
			if _, seen := reaches[src]; !seen {
				reaches[src] = struct{}{}
				queue = append(queue, src)
			}
		}
	}
	out := make([]Hop, 0, len(cand))
	for _, h := range cand {
		if _, ok := reaches[h.Target]; ok {
			out = append(out, h)
			continue
		}
		why[h] = RefusedReachesNoTrackedBranch
	}
	if len(out) == 0 {
		return nil, why
	}
	return out, why
}
