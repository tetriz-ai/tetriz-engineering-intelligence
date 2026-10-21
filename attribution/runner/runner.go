// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package runner drives the attribution engine over a store.
//
// A Runner loads pending sessions from a SessionSource, attributes them and
// writes the merged result back.
package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// defaultBatchSize bounds one run when the caller passes no limit.
const defaultBatchSize = 1000

// SessionSource is where a batch comes from and where its result goes.
//
// The two ends are one interface because they share a contract the engine
// cannot enforce: whatever LoadPending selects, WriteResults must be able to
// persist. A source has one writer: run one worker per store, and do not share
// a store across processes.
type SessionSource interface {
	// LoadPending returns up to limit sessions awaiting attribution, oldest
	// first. Returning fewer is normal; returning none ends the run.
	LoadPending(ctx context.Context, orgID string, limit int) ([]*attributionv1.SessionEvidence, error)

	// WriteResults persists one batch's reduction.
	//
	// loadStartedAt is when the run began its pending read. A store that
	// snapshots matcher inputs at write time can use it to skip a row that
	// changed since it was read. Both reference stores ignore it: a change to a
	// session's inputs raises its re-attribution request, so the row is selected
	// again after the write.
	//
	// It returns how many rows actually persisted. A short count is not an
	// error — the rest of the batch is good — but it must not be silent: those
	// rows keep their outstanding request and are recomputed every run, so a
	// sustained short count is a starving session, not noise.
	WriteResults(ctx context.Context, orgID string, r attribution.Reduction, loadStartedAt time.Time) (int, error)
}

// Runner ties an engine to a source.
type Runner struct {
	engine *attribution.Engine
	source SessionSource
	obs    attribution.Observer
}

// New builds a Runner. A nil Observer discards events.
func New(engine *attribution.Engine, source SessionSource, obs attribution.Observer) *Runner {
	return &Runner{engine: engine, source: source, obs: obs}
}

// Result summarises one batch.
type Result struct {
	Sessions     int
	Processed    int
	Withdrawn    int
	Reattributed int
	// ShortCount is how many rows the write did not persist. Non-zero means some
	// reported withdrawals may not have committed.
	ShortCount int

	// Breakdown says what the batch resolved to, by signal and by abstention
	// reason. Processed alone cannot distinguish fewer matches from the same
	// matches on weaker evidence.
	Breakdown attribution.Breakdown

	// MeanMatches and P99Matches describe how many pull requests a session
	// linked to. A rising tail is the signature of a reused branch name linking
	// one session to work it had no part in.
	MeanMatches float64
	P99Matches  int
}

// RunBatch attributes one batch for one org and returns what it did.
//
// Running it again is safe: a session the previous run resolved is not selected
// again unless its inputs changed, and the reduction is adds-only, so a repeated
// run over the same sessions converges rather than accumulating.
func (r *Runner) RunBatch(ctx context.Context, orgID string, limit int) (Result, error) {
	if limit <= 0 {
		limit = defaultBatchSize
	}

	// Captured BEFORE the pending read so the write can tell which rows changed
	// mid-run. A row that moved since it was read is left to be selected again
	// rather than stamped with a result computed from what it held when loaded.
	loadStartedAt := time.Now().UTC()

	sessions, err := r.source.LoadPending(ctx, orgID, limit)
	if err != nil {
		return Result{}, fmt.Errorf("loading pending sessions: %w", err)
	}
	if len(sessions) == 0 {
		return Result{}, nil
	}

	results, err := r.engine.Attribute(ctx, orgID, sessions)
	if err != nil {
		return Result{}, fmt.Errorf("attributing batch: %w", err)
	}
	if len(results) == 0 {
		return Result{Sessions: len(sessions)}, nil
	}

	reduction := attribution.Reduce(results)
	persisted, err := r.source.WriteResults(ctx, orgID, reduction, loadStartedAt)
	if err != nil {
		return Result{}, fmt.Errorf("writing attributions for org %s: %w", orgID, err)
	}

	mean, p99 := attribution.MatchesDistribution(results)
	out := Result{
		Sessions:     len(sessions),
		Processed:    len(results),
		Withdrawn:    len(reduction.Withdrawals),
		Reattributed: reduction.Reattributed,
		Breakdown:    attribution.Summarise(results),
		MeanMatches:  mean,
		P99Matches:   p99,
	}
	if want := len(reduction.Attributions); persisted < want {
		out.ShortCount = want - persisted
	}

	// Reported only now: a withdrawal is real when the write commits, and
	// shortCount records that some of these may not have.
	r.report(orgID, reduction.Withdrawals, out.ShortCount > 0)
	return out, nil
}

func (r *Runner) report(orgID string, withdrawals []attribution.Withdrawal, shortCount bool) {
	if r.obs == nil {
		return
	}
	for _, w := range withdrawals {
		r.obs.Count("matches_withdrawn", map[string]string{
			"method":   w.Match.GetMethod().String(),
			"ide_type": w.IDEType,
		})
		r.obs.Debug("withdrew stored match", map[string]any{
			"org_id":              orgID,
			"session_id":          w.SessionID,
			"pr_key":              w.Match.GetPrKey(),
			"method":              w.Match.GetMethod().String(),
			"evidence":            w.Match.GetEvidence().String(),
			"participation_kind":  w.Match.GetParticipationKind().String(),
			"batch_short_counted": shortCount,
		})
	}
}
