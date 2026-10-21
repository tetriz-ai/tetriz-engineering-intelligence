// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage

import (
	"context"
	"time"
)

// Row is one entity's row: its processing state and its last result.
type Row struct {
	Entity Entity
	Status Status
	// UpdatedAt is the newest UpdatedAt any put of the entity carried, or the
	// first run's clock when none carried one.
	UpdatedAt time.Time
	// LastRunAt is when stage 2 last wrote the row, zero before the first run.
	LastRunAt time.Time
	// Result is the last result written, empty before the first run.
	Result Result
}

// Window bounds which rows a run picks up.
type Window struct {
	// Retry is how recent a partially processed row's UpdatedAt must be for
	// a run to retry it, so a late counterpart can still link. Zero or
	// negative takes DefaultRetryWindow.
	Retry time.Duration
	// BatchSize caps the rows one run processes. Zero or negative takes
	// DefaultBatchSize.
	BatchSize int
}

// Defaults for Window.
const (
	DefaultRetryWindow = 30 * 24 * time.Hour
	DefaultBatchSize   = 5000
	// RetryFloor is the least time between two runs over one partially
	// processed row.
	RetryFloor = 5 * time.Minute
)

// Normalized returns w with zero or negative fields set to their defaults.
func (w Window) Normalized() Window {
	if w.Retry <= 0 {
		w.Retry = DefaultRetryWindow
	}
	if w.BatchSize <= 0 {
		w.BatchSize = DefaultBatchSize
	}
	return w
}

// Store keeps pull requests, issues and their rows: the reference
// runner's state. store/memory and store/sqlite implement it, and
// store/storetest is the conformance suite both pass. A Store has one writer.
type Store interface {
	Lookup
	// Put records pull requests and issues, each replacing the stored record
	// with its key whole, except that a nil native list keeps the stored one
	// (see PR.NativeIssueRefs). A new key gets a row in StatusYetToProcess. A
	// record that differs from the stored one re-arms a terminal row
	// (StatusToBeReprocessed); the row keeps the newest UpdatedAt. A record
	// equal to the stored one changes nothing. A pull request whose merge time
	// changes also re-arms every issue row that links it, so its closer
	// election runs again. A new or changed issue re-arms every pull-request
	// row whose last result reads one of its new or previous Identifiers, as
	// an unresolved candidate or as a link's evidence. An empty or
	// repeated key fails the whole batch.
	Put(ctx context.Context, prs []PR, issues []Issue) error
	// Force marks existing rows StatusMarkedForForceReprocess. An unknown
	// entity is skipped.
	Force(ctx context.Context, entities ...Entity) error
	// PR and Issue return a stored record.
	PR(ctx context.Context, key string) (PR, bool, error)
	Issue(ctx context.Context, key string) (Issue, bool, error)
	// Trackers lists the vendors of the stored issues, sorted.
	Trackers(ctx context.Context) ([]Vendor, error)
	// Eligible lists the rows a run at now processes, in order: armed rows
	// first, then partially processed rows updated within w.Retry of now and
	// not run within RetryFloor; within each, the oldest run first (never run
	// first), then the newest update, then side and key. At most w.BatchSize.
	Eligible(ctx context.Context, now time.Time, w Window) ([]Entity, error)
	// Row returns an entity's row.
	Row(ctx context.Context, e Entity) (Row, bool, error)
	// Rows returns every row, sorted by side then key.
	Rows(ctx context.Context) ([]Row, error)
	// Write stores r as its entity's terminal result, run at at.
	Write(ctx context.Context, r Result, at time.Time) error
	// Rearm moves a terminal row to StatusToBeReprocessed; other rows are
	// left as they are.
	Rearm(ctx context.Context, e Entity) error
}
