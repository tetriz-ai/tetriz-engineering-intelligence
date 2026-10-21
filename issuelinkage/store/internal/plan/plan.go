// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package plan holds the rules both reference stores apply, so they cannot
// drift: how a put moves a row, which rows a run picks up and in what order,
// and the canonical form a record is compared and stored in.
package plan

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
)

// Touch returns an entity's row after a put. exists says whether prev is a
// stored row; changed whether the put's record differs from the stored one.
func Touch(prev issuelinkage.Row, exists bool, e issuelinkage.Entity, updatedAt time.Time, changed bool) issuelinkage.Row {
	if !exists {
		return issuelinkage.Row{Entity: e, Status: issuelinkage.StatusYetToProcess, UpdatedAt: updatedAt.UTC()}
	}
	row := prev
	if updatedAt.After(row.UpdatedAt) {
		row.UpdatedAt = updatedAt.UTC()
	}
	if changed && row.Status.Terminal() {
		row.Status = issuelinkage.StatusToBeReprocessed
	}
	return row
}

// Eligible returns the entities of rows a run at now picks up, in run order.
func Eligible(rows []issuelinkage.Row, now time.Time, w issuelinkage.Window) []issuelinkage.Entity {
	w = w.Normalized()
	cutoff := now.Add(-w.Retry)
	floor := now.Add(-issuelinkage.RetryFloor)
	var picked []issuelinkage.Row
	for _, r := range rows {
		switch {
		case r.Status.Armed():
			picked = append(picked, r)
		case r.Status == issuelinkage.StatusPartiallyProcessed &&
			!r.UpdatedAt.Before(cutoff) &&
			(r.LastRunAt.IsZero() || r.LastRunAt.Before(floor)):
			picked = append(picked, r)
		}
	}
	sort.SliceStable(picked, func(i, j int) bool {
		a, b := picked[i], picked[j]
		if a.Status.Armed() != b.Status.Armed() {
			return a.Status.Armed()
		}
		if !a.LastRunAt.Equal(b.LastRunAt) {
			if a.LastRunAt.IsZero() || b.LastRunAt.IsZero() {
				return a.LastRunAt.IsZero()
			}
			return a.LastRunAt.Before(b.LastRunAt)
		}
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
		if a.Entity.Side != b.Entity.Side {
			return a.Entity.Side < b.Entity.Side
		}
		return a.Entity.Key < b.Entity.Key
	})
	if len(picked) > w.BatchSize {
		picked = picked[:w.BatchSize]
	}
	out := make([]issuelinkage.Entity, 0, len(picked))
	for _, r := range picked {
		out = append(out, r.Entity)
	}
	return out
}

// Write returns row after stage 2 wrote r at at.
func Write(row issuelinkage.Row, r issuelinkage.Result, at time.Time) (issuelinkage.Row, error) {
	if !r.Status.Terminal() {
		return row, fmt.Errorf("%w: status %s is not terminal", issuelinkage.ErrInvalidRecord, r.Status)
	}
	row.Status = r.Status
	row.LastRunAt = at.UTC()
	if row.UpdatedAt.IsZero() {
		// A record with no update time is dated by its first run, so the
		// retry window still opens for it.
		row.UpdatedAt = at.UTC()
	}
	row.Result = r
	return row, nil
}

// Rearm returns row with a terminal status moved to ToBeReprocessed.
func Rearm(row issuelinkage.Row) issuelinkage.Row {
	if row.Status.Terminal() {
		row.Status = issuelinkage.StatusToBeReprocessed
	}
	return row
}

// KeepNativePR returns p with the stored native list when p carries none
// (nil): the record was fetched without the vendor's list.
func KeepNativePR(stored issuelinkage.PR, exists bool, p issuelinkage.PR) issuelinkage.PR {
	if exists && p.NativeIssueRefs == nil {
		p.NativeIssueRefs = stored.NativeIssueRefs
	}
	return p
}

// KeepNativeIssue is KeepNativePR for an issue, truncation flag included.
func KeepNativeIssue(stored issuelinkage.Issue, exists bool, is issuelinkage.Issue) issuelinkage.Issue {
	if exists && is.NativePRRefs == nil {
		is.NativePRRefs, is.NativeRefsTruncated = stored.NativePRRefs, stored.NativeRefsTruncated
	}
	return is
}

// EncodePR is a pull request's canonical form: times in UTC. A nil native
// list and an empty one stay distinct.
func EncodePR(p issuelinkage.PR) ([]byte, error) {
	p.MergedAt, p.UpdatedAt = utc(p.MergedAt), utc(p.UpdatedAt)
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encoding pull request %s: %w", p.Key, err)
	}
	return b, nil
}

// EncodeIssue is an issue's canonical form.
func EncodeIssue(is issuelinkage.Issue) ([]byte, error) {
	is.UpdatedAt = utc(is.UpdatedAt)
	if len(is.Identifiers) == 0 {
		is.Identifiers = nil
	}
	b, err := json.Marshal(is)
	if err != nil {
		return nil, fmt.Errorf("encoding issue %s: %w", is.Key, err)
	}
	return b, nil
}

func utc(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.UTC()
}

// MergeMoved reports whether a put changes a stored pull request's merge
// time, which can change the closer election of every issue it links.
func MergeMoved(stored issuelinkage.PR, exists bool, p issuelinkage.PR) bool {
	return exists && !stored.MergedAt.Equal(p.MergedAt)
}

// Links reports whether row's last result links the pull request prKey.
func Links(row issuelinkage.Row, prKey string) bool {
	for _, l := range row.Result.Links {
		if l.PR == prKey {
			return true
		}
	}
	return false
}

// Names reports whether row's last result reads one of idents: an
// unresolved candidate or a link's evidence with that identifier, compared
// exactly (a case-folded branch key is stored upper-cased).
func Names(row issuelinkage.Row, idents map[string]bool) bool {
	for _, u := range row.Result.Unresolved {
		if idents[u.Candidate.Identifier] {
			return true
		}
	}
	for _, l := range row.Result.Links {
		for _, c := range l.Evidence {
			if idents[c.Identifier] {
				return true
			}
		}
	}
	return false
}

// IssueIdents is the identifier set an issue put touches: the new
// Identifiers and the stored ones, for an issue that is new or changed;
// empty for an unchanged put.
func IssueIdents(set map[string]bool, stored issuelinkage.Issue, exists, changed bool, is issuelinkage.Issue) {
	if !changed {
		return
	}
	for _, id := range is.Identifiers {
		set[id] = true
	}
	if exists {
		for _, id := range stored.Identifiers {
			set[id] = true
		}
	}
}
