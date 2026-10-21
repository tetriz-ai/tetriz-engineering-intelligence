// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"time"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// sessionMatchAnchor returns the UTC calendar date of a session's last observed
// activity. Every match window opens at start_date and extends forward from
// this anchor, so a session that stayed active for weeks keeps matching PRs
// raised near the end of that work rather than only near its start.
//
// The anchor is derived, not stored: session_duration_seconds is the span from
// the session's start to its last update, and a producer that re-exports a
// resumed session raises it, so the anchor advances even though start_date is
// frozen at first observation.
//
// The result is a UTC calendar date: every window it feeds compares against a
// candidate's merge date, itself a calendar date, so returning an event instant
// here would mix the two grains rather than remove a mismatch.
//
// Guarantees callers rely on: the anchor is never earlier than start_date, and
// never more than MaxSessionSpanDays past it. A session whose duration is
// unmeasurable (the producer reported none) anchors on its start date, leaving
// its windows exactly as wide as a start-anchored window.
func sessionMatchAnchor(s *attributionv1.SessionEvidence, params Params) time.Time {
	start := sessionStartDate(s)
	durationS := s.GetSessionDurationSeconds()
	if durationS <= 0 {
		return start
	}

	// Clamp before converting to a Duration: an absurd stored value would
	// otherwise overflow the int64 nanosecond multiply and wrap to a date behind
	// the session.
	if maxSpanS := int64(effectiveMaxSessionSpanDays(params)) * secondsInADay; durationS > maxSpanS {
		durationS = maxSpanS
	}

	// Measure from the full-precision instant when the session carries one.
	// session_duration_seconds is measured from started_at, so adding it to the
	// truncated calendar date instead would under-report last activity by the
	// session's time-of-day offset.
	from := start
	if startedAt := startedAtPtr(s); startedAt != nil {
		from = *startedAt
	}

	// Offset start_date by whole days rather than building a fresh midnight: the
	// anchor is compared against values the caller produced, so it must keep the
	// location and time-of-day the caller gave the start date. A synthesised UTC
	// midnight would sit hours away from a start date materialised in another
	// location, skewing every window by up to a day.
	days := utcDayDiff(from, from.Add(time.Duration(durationS)*time.Second))
	if days < 0 {
		days = 0
	}
	if maxSpan := effectiveMaxSessionSpanDays(params); days > maxSpan {
		days = maxSpan
	}
	return start.AddDate(0, 0, days)
}

// utcDayDiff returns the number of UTC calendar days crossed between two
// instants. Both sides are normalised to UTC first so the count reflects the
// calendar-day grain the match windows compare on, independent of the location
// either instant carries.
func utcDayDiff(from, to time.Time) int {
	f, t := from.UTC(), to.UTC()
	fDay := time.Date(f.Year(), f.Month(), f.Day(), 0, 0, 0, 0, time.UTC)
	tDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	return int(tDay.Sub(fDay).Hours() / 24)
}
