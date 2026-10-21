// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"math"
	"testing"
	"time"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"

	"github.com/stretchr/testify/assert"
)

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func instant(y int, m time.Month, d, h int) *time.Time {
	t := time.Date(y, m, d, h, 0, 0, 0, time.UTC)
	return &t
}

func insightWith(startDate time.Time, startedAt *time.Time, durationS int64) *attributionv1.SessionEvidence {
	return &attributionv1.SessionEvidence{
		StartDate:              ts(startDate),
		StartedAt:              tsPtr(startedAt),
		SessionDurationSeconds: durationS,
	}
}

func TestSessionMatchAnchor(t *testing.T) {
	start := day(2026, time.January, 1)

	tests := []struct {
		name     string
		session  *attributionv1.SessionEvidence
		spanDays int
		want     time.Time
	}{
		{
			// The pre-anchor behaviour, and what an unmeasurable session must keep.
			name:     "zero duration anchors on the start date",
			session:  insightWith(start, instant(2026, time.January, 1, 9), 0),
			spanDays: 30,
			want:     start,
		},
		{
			name:     "negative duration anchors on the start date",
			session:  insightWith(start, instant(2026, time.January, 1, 9), -3600),
			spanDays: 30,
			want:     start,
		},
		{
			name:     "nil usage metadata anchors on the start date",
			session:  &attributionv1.SessionEvidence{StartDate: ts(start)},
			spanDays: 30,
			want:     start,
		},
		{
			name:     "sub-day session stays on its start date",
			session:  insightWith(start, instant(2026, time.January, 1, 9), 2*3600),
			spanDays: 30,
			want:     start,
		},
		{
			// The reason the full-precision instant is used: a short session that
			// crosses midnight genuinely last ran on the next calendar day.
			name:     "sub-day session crossing midnight advances one day",
			session:  insightWith(start, instant(2026, time.January, 1, 23), 2*3600),
			spanDays: 30,
			want:     day(2026, time.January, 2),
		},
		{
			name:     "multi-day session advances by whole days",
			session:  insightWith(start, instant(2026, time.January, 1, 9), 20*24*3600),
			spanDays: 30,
			want:     day(2026, time.January, 21),
		},
		{
			name:     "duration beyond the span cap clamps to the cap",
			session:  insightWith(start, instant(2026, time.January, 1, 0), 40*24*3600),
			spanDays: 30,
			want:     day(2026, time.January, 31),
		},
		{
			// A wrapped multiply would land behind the session; the clamp must run
			// before the conversion to a Duration.
			name:     "absurd duration clamps rather than overflowing",
			session:  insightWith(start, instant(2026, time.January, 1, 0), math.MaxInt64),
			spanDays: 30,
			want:     day(2026, time.January, 31),
		},
		{
			// A producer that recorded only a calendar date.
			name:     "nil started_at falls back to the start date",
			session:  insightWith(start, nil, 5*24*3600),
			spanDays: 30,
			want:     day(2026, time.January, 6),
		},
		{
			// An unset knob must not read as "no extension allowed".
			name:     "unset span cap falls back to the default",
			session:  insightWith(start, instant(2026, time.January, 1, 0), 40*24*3600),
			spanDays: 0,
			want:     start.AddDate(0, 0, defaultMaxSessionSpanDays),
		},
		{
			// started_at disagreeing with the frozen start date must never pull the
			// anchor behind it — every window opens at start_date.
			name:     "started_at behind the start date never yields an earlier anchor",
			session:  insightWith(start, instant(2025, time.December, 20, 0), 3600),
			spanDays: 30,
			want:     start,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sessionMatchAnchor(tt.session, testParamsWithSpan(tt.spanDays))
			assert.Equal(t, tt.want, got)
			assert.False(t, got.Before(sessionStartDate(tt.session)),
				"anchor must never precede the session start date")
		})
	}
}

func TestAnchorEffect(t *testing.T) {
	start := day(2026, time.January, 1)

	tests := []struct {
		name    string
		session *attributionv1.SessionEvidence
		want    string
	}{
		{
			name:    "no duration is unmeasurable",
			session: insightWith(start, instant(2026, time.January, 1, 9), 0),
			want:    "unmeasurable",
		},
		{
			// Measurable but sub-day: the anchor lands on the start date, which is
			// indistinguishable downstream from the unmeasurable case — the whole
			// reason the two need separate label values.
			name:    "measurable but too short to cross a day",
			session: insightWith(start, instant(2026, time.January, 1, 9), 2*3600),
			want:    "none",
		},
		{
			name:    "multi-day session is extended",
			session: insightWith(start, instant(2026, time.January, 1, 9), 5*24*3600),
			want:    "extended",
		},
		{
			name:    "beyond the span cap is clamped",
			session: insightWith(start, instant(2026, time.January, 1, 0), 45*24*3600),
			want:    "clamped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, anchorEffect(tt.session, testParamsWithSpan(30)))
		})
	}
}
