// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package branchlineage records which pull requests a promotion pull request
// carried from one branch to the next, so work merged into an integration branch
// can be traced to the tracked branch it later reached.
//
// A promotion is a merged pull request whose head branch received a merged pull
// request before it, such as develop merged into main. It carries the pull requests merged into its head
// branch since the previous promotion of the same pair of branches. The package
// derives those carrier-to-carried edges from pull-request history alone (head
// branch, base branch and merge time), walks them back from the pull requests
// that reached a tracked branch, and credits each promotion with the attributed
// work it carried.
//
// The engine does no I/O and reads no clock: the caller supplies every pull
// request. [Derive] builds the edge graph in memory. The store/memory and
// store/sqlite packages keep it incrementally, re-deriving only the promotions a
// late or corrected pull request made stale.
package branchlineage
