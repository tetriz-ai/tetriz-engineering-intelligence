// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Config tunes a run.
type Config struct {
	// Signals says which generators run per vendor; nil takes DefaultSignals.
	Signals SignalSet
	// Window bounds which rows the run picks up.
	Window Window
}

// Report says what a run did.
type Report struct {
	// Eligible is how many rows the run picked up.
	Eligible int
	// Processed is how many it wrote.
	Processed int
	// Failures lists every row it could not process, and every counterpart
	// it could not re-arm. A failed row stays armed for the next run.
	Failures []Failure
}

// Failure is one entity a run could not finish.
type Failure struct {
	Entity Entity
	Err    error
}

// Run is the reference runner: it processes every eligible row of s once.
// For each row it extracts the stored record, processes it, writes the
// result, and re-arms each counterpart the row newly links, so the
// counterpart's row is read again with its own evidence and its closer
// election. An issue whose own text names no pull request keeps no link on
// its row. A counterpart already linked is not re-armed, so a linked pair
// settles after one round. When an issue's elected closer changes, every pull
// request whose own row links the issue is re-armed before the issue's row is
// written, so each reads the new election; if that fails, nothing is written
// and the issue's row is armed, even one the retry window picked up.
//
// Run fails only when it cannot list the rows or the trackers; a row that
// fails is reported in Report.Failures and left armed.
//
// now is the run's clock: it decides which rows are eligible and stamps what
// the run writes, so a run is reproducible.
func Run(ctx context.Context, s Store, now time.Time, cfg Config) (Report, error) {
	now = now.UTC()
	signals := cfg.Signals
	if signals == nil {
		signals = DefaultSignals()
	}
	entities, err := s.Eligible(ctx, now, cfg.Window.Normalized())
	if err != nil {
		return Report{}, fmt.Errorf("listing eligible rows: %w", err)
	}
	rep := Report{Eligible: len(entities)}
	trackers, err := s.Trackers(ctx)
	if err != nil {
		return rep, fmt.Errorf("listing trackers: %w", err)
	}
	bareKeys := BareKeyNamespace(trackers)
	for _, e := range entities {
		if err := runOne(ctx, s, e, bareKeys, signals, now, &rep); err != nil {
			rep.Failures = append(rep.Failures, Failure{Entity: e, Err: err})
			continue
		}
		rep.Processed++
	}
	return rep, nil
}

func runOne(ctx context.Context, s Store, e Entity, bareKeys Vendor, signals SignalSet, now time.Time, rep *Report) error {
	var ex Extraction
	truncated := false
	switch e.Side {
	case SidePR:
		pr, ok, err := s.PR(ctx, e.Key)
		if err != nil {
			return fmt.Errorf("loading pull request: %w", err)
		}
		if ok {
			ex = ExtractPR(pr, bareKeys, signals)
		}
	case SideIssue:
		is, ok, err := s.Issue(ctx, e.Key)
		if err != nil {
			return fmt.Errorf("loading issue: %w", err)
		}
		if ok {
			ex = ExtractIssue(is, signals)
			truncated = is.NativeRefsTruncated
		}
	default:
		return fmt.Errorf("unknown side %d", e.Side)
	}
	res, err := Process(ctx, s, e, ex, now)
	if err != nil {
		return fmt.Errorf("processing: %w", err)
	}
	res.NativeRefsTruncated = truncated
	prior, _, err := s.Row(ctx, e)
	if err != nil {
		return fmt.Errorf("reading prior row: %w", err)
	}
	priorLinked := map[string]time.Time{}
	for _, ln := range prior.Result.Links {
		priorLinked[ln.Counterpart(e.Side)] = ln.FirstSeenAt
	}
	for i := range res.Links {
		if first, ok := priorLinked[res.Links[i].Counterpart(e.Side)]; ok && !first.IsZero() && first.Before(res.Links[i].FirstSeenAt) {
			res.Links[i].FirstSeenAt = first
		}
	}
	if e.Side == SideIssue && prior.Result.Closer != res.Closer {
		// The election moved: every pull request whose own row links this
		// issue reads it again. Re-arm them before writing, so a failure
		// leaves this row unwritten. The row is re-armed too, since the
		// retry window may have picked it up unarmed, and the next run
		// tries again.
		if err := rearmElection(ctx, s, e.Key); err != nil {
			if rerr := s.Rearm(ctx, e); rerr != nil {
				return errors.Join(err, fmt.Errorf("re-arming the issue's own row: %w", rerr))
			}
			return err
		}
	}
	if err := s.Write(ctx, res, now); err != nil {
		return fmt.Errorf("writing result: %w", err)
	}
	rearm := map[string]bool{}
	for _, ln := range res.Links {
		if other := ln.Counterpart(e.Side); !hasKey(priorLinked, other) {
			rearm[other] = true
		}
	}
	keys := make([]string, 0, len(rearm))
	for k := range rearm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		oe := Entity{Side: e.Side.Other(), Key: k}
		if err := s.Rearm(ctx, oe); err != nil {
			rep.Failures = append(rep.Failures, Failure{Entity: oe, Err: fmt.Errorf("re-arming counterpart: %w", err)})
		}
	}
	return nil
}

func hasKey(m map[string]time.Time, k string) bool {
	_, ok := m[k]
	return ok
}

// rearmElection re-arms every pull-request row whose last result links the
// issue issueKey.
func rearmElection(ctx context.Context, s Store, issueKey string) error {
	rows, err := s.Rows(ctx)
	if err != nil {
		return fmt.Errorf("listing rows to re-arm after an election: %w", err)
	}
	for _, r := range rows {
		if r.Entity.Side != SidePR {
			continue
		}
		for _, ln := range r.Result.Links {
			if ln.Issue == issueKey {
				if err := s.Rearm(ctx, r.Entity); err != nil {
					return fmt.Errorf("re-arming %s after an election: %w", r.Entity.Key, err)
				}
				break
			}
		}
	}
	return nil
}
