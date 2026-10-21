// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage

import (
	"context"
	"errors"
	"fmt"
	"time"

	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// DefaultLease is how long a claim is held before the fix is offered again.
//
// It bounds how long one dead worker can strand a fix, so it wants to exceed the
// slowest realistic walk — a walk is a handful of provider calls per anchor — and
// stay short enough that a crash is recovered within a drain cycle or two.
const DefaultLease = 15 * time.Minute

// ErrNoFixInput is what a [FixResolver] returns when a claimed fix's diff or
// landing commit cannot be found. It is a permanent answer, not a transient one:
// the drain settles the fix rather than re-offering it forever.
var ErrNoFixInput = errors.New("defectlineage: no blame input for this fix")

// FixResolver supplies the data this package does not own. The drain holds a
// claim on the fix when it calls Resolve, so an implementation should be a
// lookup rather than a long fetch.
//
// Returning [ErrNoFixInput], or an error wrapping it, settles the fix as having
// nothing to blame. Any other error leaves the claim to lapse and the fix to be
// retried.
type FixResolver interface {
	Resolve(ctx context.Context, ref FixRef) (FixInput, error)
}

// FixResolverFunc adapts a function to [FixResolver].
type FixResolverFunc func(ctx context.Context, ref FixRef) (FixInput, error)

// Resolve calls f.
func (f FixResolverFunc) Resolve(ctx context.Context, ref FixRef) (FixInput, error) {
	return f(ctx, ref)
}

// DrainParams configure a [Drain].
type DrainParams struct {
	// Store is the work list and the candidate rows. Required.
	Store Store
	// Resolver supplies each claimed fix's diff and landing commit. Required.
	Resolver FixResolver
	// Walker runs the blame walk. Nil builds one from the zero [WalkParams].
	Walker *Walker
	// Lease is how long a claim is held. Zero means [DefaultLease].
	Lease time.Duration
	// MaxChangedFiles is the ceiling above which a fix is skipped without a
	// provider call. Zero means [DefaultMaxChangedFiles].
	MaxChangedFiles int
	// Anchors bound how anchors are extracted from each fix's diff.
	Anchors Options
	// Observer receives the drain's diagnostic events. Nil discards them.
	Observer Observer
}

// Drain claims bug fixes, blames them and records the outcome. One Drain can be
// run by several workers against one store: a claim is leased, so two workers
// take disjoint batches.
type Drain struct {
	store           Store
	resolver        FixResolver
	walker          *Walker
	lease           time.Duration
	maxChangedFiles int
	anchors         Options
	obs             Observer
}

// NewDrain builds a Drain. It returns an error when a required dependency is
// missing, because a Drain with no store or no resolver can only fail at the
// first fix, and failing at construction says so where it is actionable.
func NewDrain(p DrainParams) (*Drain, error) {
	if p.Store == nil {
		return nil, errors.New("defectlineage: DrainParams.Store is required")
	}
	if p.Resolver == nil {
		return nil, errors.New("defectlineage: DrainParams.Resolver is required")
	}
	walker := p.Walker
	if walker == nil {
		w, err := NewWalker(WalkParams{Observer: p.Observer})
		if err != nil {
			return nil, err
		}
		walker = w
	}
	lease := p.Lease
	if lease == 0 {
		lease = DefaultLease
	}
	anchors := p.Anchors
	if anchors.Observer == nil {
		anchors.Observer = p.Observer
	}
	return &Drain{
		store:           p.Store,
		resolver:        p.Resolver,
		walker:          walker,
		lease:           lease,
		maxChangedFiles: p.MaxChangedFiles,
		anchors:         anchors,
		obs:             orNop(p.Observer),
	}, nil
}

// DrainStats is what one blame pass did.
type DrainStats struct {
	// Claimed is how many fixes the pass leased.
	Claimed int
	// Resolved is how many produced at least one candidate.
	Resolved int
	// Unattributed is how many were blamed cleanly with no candidate surviving.
	Unattributed int
	// Settled is how many were settled without a provider call — oversized, or
	// with no blameable anchor.
	Settled int
	// Failed is how many hit a provider error and were left for a later pass.
	Failed int
}

// RunBlame claims up to limit fixes for one connector and walks each against
// src.
//
// A fix that fails is left alone rather than settled, so its claim lapses and a
// later pass retries it. The pass itself only returns an error when the claim
// could not be taken at all: one fix's failure must not abandon the rest of the
// batch.
func (d *Drain) RunBlame(ctx context.Context, orgID, connectorID string, limit int, src Source) (DrainStats, error) {
	var stats DrainStats
	claims, err := d.store.ClaimPendingBlame(ctx, orgID, connectorID, limit, d.lease)
	if err != nil {
		return stats, fmt.Errorf("claiming fixes for %s: %w", connectorID, err)
	}
	stats.Claimed = len(claims)

	for _, claim := range claims {
		switch d.runOne(ctx, claim, src) {
		case outcomeResolved:
			stats.Resolved++
		case outcomeUnattributed:
			stats.Unattributed++
		case outcomeSettled:
			stats.Settled++
		default:
			stats.Failed++
		}
	}
	d.obs.Observe("blame_batch_size", float64(stats.Claimed), map[string]string{"connector": connectorID})
	return stats, nil
}

type passOutcome int

const (
	outcomeFailed passOutcome = iota
	outcomeResolved
	outcomeUnattributed
	outcomeSettled
)

// runOne walks one claimed fix. Every return path either advances the fix or
// deliberately leaves it for the lease to re-offer; nothing here swallows a fix
// into a state no pass will pick up again.
func (d *Drain) runOne(ctx context.Context, claim Claim, src Source) passOutcome {
	in, err := d.resolver.Resolve(ctx, claim.Ref)
	if err != nil {
		if errors.Is(err, ErrNoFixInput) {
			// A permanent absence. Settling it stops the fix being re-offered for
			// data that is never going to arrive.
			d.settle(ctx, claim, defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_ANCHORS_EMPTY)
			return outcomeSettled
		}
		d.obs.Debug("resolve_failed", map[string]any{"fix": claim.Ref.BugFixPRID, "error": err.Error()})
		return outcomeFailed
	}

	request, terminal, ok := BuildClaim(claim.Ref, in, d.maxChangedFiles, d.anchors)
	if !ok {
		d.settle(ctx, claim, terminal)
		return outcomeSettled
	}

	held, err := d.store.MarkPrepared(ctx, claim.Ref, claim.ClaimToken)
	if err != nil {
		d.obs.Debug("mark_prepared_failed", map[string]any{"fix": claim.Ref.BugFixPRID, "error": err.Error()})
		return outcomeFailed
	}
	if !held {
		// The lease lapsed and another worker took the fix. Walking on would pay
		// the provider for an answer that worker is already fetching, so this one
		// stops here rather than at the submit.
		d.obs.Count("claim_lost", map[string]string{"connector": claim.Ref.ConnectorID})
		return outcomeFailed
	}

	state, results, walkErr := d.walker.Walk(ctx, src, request)
	if walkErr != nil && len(results) == 0 {
		// A provider failure with nothing to show for it. Leaving the fix for the
		// lease to re-offer is right: the failure is usually transient, and
		// recording a terminal state here would bury a fix that one retry would
		// have answered.
		d.obs.Count("walk_failed", map[string]string{"connector": claim.Ref.ConnectorID})
		d.obs.Debug("walk_failed", map[string]any{"fix": claim.Ref.BugFixPRID, "error": walkErr.Error()})
		return outcomeFailed
	}
	if walkErr != nil {
		// A partial resolve: some anchors named culprits and at least one failed.
		// The culprits are kept and submitted, which advances the fix past the
		// blame stages — so the failed anchor is NOT retried, and whatever culprit
		// it would have named is not recovered. Keeping the culprits found is
		// worth more than re-walking the whole fix for the one that failed, but
		// the loss is real, so it is counted rather than only logged.
		d.obs.Count("walk_partial", map[string]string{"connector": claim.Ref.ConnectorID})
		d.obs.Debug("walk_partial", map[string]any{"fix": claim.Ref.BugFixPRID, "error": walkErr.Error()})
	}

	if err := d.store.SubmitBlame(ctx, claim.Ref, &defectlineagev1.LineageBlameOutcome{
		OrganizationId: claim.Ref.OrganizationID,
		BugFixPrId:     claim.Ref.BugFixPRID,
		Status:         state,
		Results:        results,
	}); err != nil {
		d.obs.Debug("submit_failed", map[string]any{"fix": claim.Ref.BugFixPRID, "error": err.Error()})
		return outcomeFailed
	}

	if state == defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_RESOLVED {
		return outcomeResolved
	}
	return outcomeUnattributed
}

// settle records a pre-provider terminal outcome, reporting a lost race rather
// than failing the pass: losing the claim means another worker is already on the
// fix.
func (d *Drain) settle(ctx context.Context, claim Claim, outcome defectlineagev1.DefectLineageTerminalOutcome) {
	moved, err := d.store.Settle(ctx, claim.Ref, claim.ClaimToken, outcome)
	if err != nil {
		d.obs.Debug("settle_failed", map[string]any{"fix": claim.Ref.BugFixPRID, "error": err.Error()})
		return
	}
	if !moved {
		d.obs.Count("settle_lost_claim", nil)
		return
	}
	d.obs.Count("fix_settled", map[string]string{"outcome": outcome.String()})
}
