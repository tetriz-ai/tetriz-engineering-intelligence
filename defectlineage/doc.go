// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package defectlineage traces a bug fix back to the change that introduced the
// bug, and ranks the changes it finds.
//
// Given a merged bug-fix pull request, the pipeline reads the diff the fix
// landed, takes each region the fix removed or replaced as a location anchor,
// and blames that anchor against the revision immediately before the fix. The
// commit blame names is a candidate: a claim with its evidence attached, never a
// verdict. A single fix routinely has several.
//
// The three stages are separable on purpose, because they cost different things.
// Building anchors is a parse over a diff you already hold. Blaming them costs
// one provider call per step and is the expensive half. Scoring is arithmetic
// over what blame already stored, so changing the formula costs a re-read rather
// than a re-fetch.
//
//   - [Anchors] turns a fix's unified diff into the locations a blame walk can
//     ask a provider about.
//   - [Walker.Walk] blames one fix's anchors back to the commits that materially
//     wrote the removed lines.
//   - [Combine] turns one candidate's evidence into a confidence score in [0,1],
//     with the derivation behind it.
//
// The walk fails closed. It stops on a positive match — the removed line's
// content is absent at a commit's parent, so that commit introduced it — and
// produces nothing whenever it cannot be certain. A miss is always preferable to
// a confident wrong author.
//
// Nothing here fetches. You supply a [Source] for blame and, for scoring, a
// [Comparator] for the syntax-tree comparison. The pipeline's work list and the
// candidate rows live behind [Store], whose reference implementation is in
// store/postgres.
package defectlineage
