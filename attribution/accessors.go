// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// The engine's date logic distinguishes "no date" from "a date", and treats the
// former as exempt from every window rather than as the epoch. A proto Timestamp
// that is unset returns 1970 from AsTime(), which IsZero() reports as false — so
// every read goes through these helpers, which return the zero time instead.
//
// This is not a nicety: an undated candidate must skip the direct-reference
// window ceiling, and the file-overlap signal reaches a still-open pull request
// through its open date (decaying from it, see decayDate). The branch signal
// matches merged pull requests only, so an undated candidate never branch-matches.

func tsTime(ts *timestamppb.Timestamp) time.Time {
	if ts == nil || !ts.IsValid() {
		return time.Time{}
	}
	return ts.AsTime()
}

// mergedAt is the candidate's merge instant, zero when it has not merged.
// It is the date the match windows compare against.
func mergedAt(pr *attributionv1.PRCandidate) time.Time { return tsTime(pr.GetMergedAt()) }

// openedAt is the candidate's open instant, zero when unknown. Reviewer-side
// candidates and records without a payload may carry neither date.
func openedAt(pr *attributionv1.PRCandidate) time.Time { return tsTime(pr.GetOpenedAt()) }

// sessionStartDate is the UTC calendar date every match window opens at.
func sessionStartDate(s *attributionv1.SessionEvidence) time.Time { return tsTime(s.GetStartDate()) }

// secondsInADay is the calendar-day constant the span clamps use.
const secondsInADay = 86400

// startedAtPtr adapts the full-precision start instant to the pointer shape the
// anchor logic expects: nil when the producer recorded only a calendar date.
func startedAtPtr(s *attributionv1.SessionEvidence) *time.Time {
	if t := tsTime(s.GetStartedAt()); !t.IsZero() {
		return &t
	}
	return nil
}
