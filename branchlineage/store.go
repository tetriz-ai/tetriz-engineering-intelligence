// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import "context"

// Store keeps pull requests and their derived edges incrementally: the
// reference indexer. store/memory and store/sqlite implement it, and
// store/storetest is the conformance suite both pass.
//
// A Store has one writer. Put records pull requests and drops the edges of the
// carriers they made stale; Derive then re-derives every queued carrier and
// replaces its edge set whole. Reads between the two see the stale carriers with
// no edges, never with a mix of old and new ones.
type Store interface {
	EdgeSource
	// Put records prs, each replacing the stored pull request with its key
	// whole. It queues every carrier the batch made stale (see Invalidate) with
	// its edges removed, and every merged pull request not yet derived, and
	// returns the invalidation. An empty or repeated key fails the whole batch.
	Put(ctx context.Context, prs ...PR) (Invalidation, error)
	// Pending lists the queued pull requests, sorted.
	Pending(ctx context.Context) ([]string, error)
	// Derive derives every queued pull request from the current state, replaces
	// each one's edge set whole, empties the queue, and returns how many it derived.
	Derive(ctx context.Context) (int, error)
	// Edges returns every live edge of repository, ordered by carrier then carried.
	Edges(ctx context.Context, repository string) ([]Edge, error)
}
