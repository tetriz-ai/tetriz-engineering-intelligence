// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Default bounds for one sweep tick.
const (
	// DefaultSweepBatchSize caps how many fixes one tick enqueues, so a large
	// backlog drains over several ticks rather than one unbounded scan.
	DefaultSweepBatchSize = 500

	// DefaultSweepLookback bounds how far back a tick looks.
	//
	// It has to outlast the event path's message retention plus a plausible
	// consumer outage, or a genuinely dropped event ages out before the backstop
	// can see it. Past it, a missed fix is accepted as lost rather than scanned
	// for forever.
	DefaultSweepLookback = 14 * 24 * time.Hour
)

// FixSupplier lists the fixes that ought to be traced. It is the caller's own
// view of merged bug fixes, because this package owns no pull requests.
type FixSupplier interface {
	// RecentBugFixes returns the merged bug fixes for one organisation whose
	// merge time is at or after since, capped at limit.
	RecentBugFixes(ctx context.Context, orgID string, since time.Time, limit int) ([]FixRef, error)
}

// SweepConfig bounds one [RunSweep] tick. The zero value uses the defaults.
type SweepConfig struct {
	// BatchSize caps fixes read per tick. Zero means [DefaultSweepBatchSize].
	BatchSize int
	// Lookback bounds how far back the scan reaches. Zero means
	// [DefaultSweepLookback].
	Lookback time.Duration
	// Observer receives the sweep's diagnostic events. Nil discards them.
	Observer Observer
}

// SweepStats is what one sweep tick did.
type SweepStats struct {
	// Scanned is how many fixes the supplier returned.
	Scanned int
	// Enqueued is how many were offered to the store. Because enqueueing is
	// insert-if-absent, this counts offers rather than new rows: a fix already in
	// flight is offered and ignored.
	Enqueued int
	// Failed is how many could not be enqueued this tick.
	Failed int
}

// RunSweep is the enqueue backstop. It finds recently merged bug fixes and
// enqueues each one.
//
// It covers the two gaps the event path leaves: a fix merged before tracing was
// switched on, and an event that was dropped. Enqueueing is insert-if-absent, so
// a fix the event path already enqueued, or that the drain has already advanced,
// costs nothing here. That is what makes the sweep safe to run on a schedule
// beside the event path rather than instead of it.
//
// One fix failing never abandons the tick: the rest are still enqueued, and the
// next tick retries whatever did not land.
func RunSweep(ctx context.Context, store Store, supplier FixSupplier, orgID string, cfg SweepConfig) (SweepStats, error) {
	var stats SweepStats
	if store == nil {
		return stats, errors.New("defectlineage: nil store")
	}
	if supplier == nil {
		return stats, errors.New("defectlineage: nil fix supplier")
	}
	obs := orNop(cfg.Observer)

	batch := cfg.BatchSize
	if batch <= 0 {
		batch = DefaultSweepBatchSize
	}
	lookback := cfg.Lookback
	if lookback <= 0 {
		lookback = DefaultSweepLookback
	}

	since := time.Now().UTC().Add(-lookback)
	refs, err := supplier.RecentBugFixes(ctx, orgID, since, batch)
	if err != nil {
		return stats, fmt.Errorf("scanning recent bug fixes for %s: %w", orgID, err)
	}
	stats.Scanned = len(refs)

	for _, ref := range refs {
		if err := store.Enqueue(ctx, ref); err != nil {
			obs.Debug("sweep_enqueue_failed", map[string]any{"fix": ref.BugFixPRID, "error": err.Error()})
			stats.Failed++
			continue
		}
		stats.Enqueued++
	}
	obs.Observe("sweep_scanned", float64(stats.Scanned), nil)
	return stats, nil
}
