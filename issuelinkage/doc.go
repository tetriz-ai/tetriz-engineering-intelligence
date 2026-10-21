// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package issuelinkage links pull requests to the tracker issues they deliver
// or mention, with the evidence for each link.
//
// It works in two stages. Stage 1, [ExtractPR] and [ExtractIssue], reads one
// record and lists its candidates: keys, URLs and "#N" references in the
// title, body and branch name, and the links the vendor itself records (a
// GitHub closing reference, a Linear pull-request attachment). Stage 2,
// [Process], resolves each candidate to a stored counterpart through a
// [Lookup], folds the candidates that name one counterpart into one [Link] of
// the strongest class, keeps a closure claim apart from a mention, elects the
// closing pull request on an issue, and lists what did not resolve.
//
// The engine reads no network. [Run] is the reference runner over a [Store]:
// it processes the rows that a put armed, retries partially resolved rows
// while their counterpart may still arrive, and re-arms each newly linked
// counterpart once. The store/memory and store/sqlite packages implement
// Store.
package issuelinkage
