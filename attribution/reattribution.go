// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"strings"
	"time"
)

// When a pull request merges, the sessions that might now attribute to it are not
// only the ones near the merge. This file holds the policy for finding them. The
// store write that raises each session's marker is the caller's; the rules for
// which sessions qualify, and over what window, are the engine's, so two callers
// cannot drift on them.

// candidateWindowSlackDays widens a pull request's own lifecycle into the range
// its candidate sessions are looked up over.
//
// The slack exists for one case, and it is not the matcher's time window: a
// reviewer-side match on an open pull request comes from the direct reference,
// which matches on (repository, number) in the session's own evidence and applies
// no date filter at all. So the bound is not "how far can a session sit from the
// pull request" but "how stale can a session's start date be relative to it" —
// and that is the session's own lifetime, because the start date is frozen at
// first capture while the tool keeps appending to the session.
const candidateWindowSlackDays = 1

// candidateWindowMaxLookbackDays floors how far before the merge the range may
// reach. Over-selecting is correct but not free: a wider range is rows actually
// read, so widen it only with a reason.
const candidateWindowMaxLookbackDays = 30

// sharedDefaultBranches are branches that carry no attribution signal because
// every session in the repository sits on them at some point.
var sharedDefaultBranches = []string{"main", "master", "develop", "dev", "trunk"}

// IsSharedDefaultBranch reports whether a branch is one every session shares, and
// so cannot say which session did the work.
func IsSharedDefaultBranch(branch string) bool {
	b := strings.ToLower(strings.TrimSpace(branch))
	for _, shared := range sharedDefaultBranches {
		if b == shared {
			return true
		}
	}
	return false
}

// ReattributionWindow returns the date range to look for sessions that may now
// attribute to a pull request that just merged.
//
// Both bounds are UTC midnights, and both are inclusive: a session belongs to the
// range when its start_date d satisfies from <= d <= to. from is 30 days before
// the merge (or the open date, for an open pull request). to is the day after the
// merge, the one day of slack a session's frozen start date needs.
//
// openedAt is required; mergedAt may be zero for a pull request that is still
// open, which a reviewer-side session can still name. The range is anchored on
// the merge where there is one, because a pull request opened long before it
// merged would otherwise reach further back than the sessions can possibly be.
func ReattributionWindow(openedAt, mergedAt time.Time) (from, to time.Time) {
	end := openedAt
	if !mergedAt.IsZero() && mergedAt.After(end) {
		end = mergedAt
	}

	from = startOfDay(openedAt).AddDate(0, 0, -candidateWindowMaxLookbackDays)
	if !mergedAt.IsZero() {
		floor := startOfDay(mergedAt).AddDate(0, 0, -candidateWindowMaxLookbackDays)
		if from.Before(floor) {
			from = floor
		}
	}
	return from, startOfDay(end).AddDate(0, 0, candidateWindowSlackDays)
}

func startOfDay(t time.Time) time.Time {
	u := t.UTC()
	return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
}
