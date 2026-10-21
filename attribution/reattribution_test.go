// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIsSharedDefaultBranch(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		want   bool
	}{
		{name: "main is shared", branch: "main", want: true},
		{name: "master is shared", branch: "master", want: true},
		{name: "develop is shared", branch: "develop", want: true},
		{name: "case and padding do not matter", branch: "  MASTER ", want: true},
		{name: "a feature branch is not shared", branch: "feat/rate-limiter", want: false},
		{name: "a branch merely containing main is not shared", branch: "feat/main-menu", want: false},
		{name: "empty is not shared", branch: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsSharedDefaultBranch(tt.branch))
		})
	}
}

func TestReattributionWindow(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}

	tests := []struct {
		name             string
		opened, merged   time.Time
		wantFrom, wantTo time.Time
	}{
		{
			name:     "a same-day merge looks back from that day",
			opened:   day(2026, time.March, 10),
			merged:   day(2026, time.March, 10),
			wantFrom: day(2026, time.February, 8),
			wantTo:   day(2026, time.March, 11),
		},
		{
			name: "a long-lived pull request anchors on the merge, not the open",
			// Opened in January, merged in March: anchoring on the open date
			// would reach back to December, further than any session that could
			// have contributed to the merge.
			opened:   day(2026, time.January, 5),
			merged:   day(2026, time.March, 10),
			wantFrom: day(2026, time.February, 8),
			wantTo:   day(2026, time.March, 11),
		},
		{
			name:   "an open pull request still gets a window",
			opened: day(2026, time.March, 10),
			// A reviewer-side session can name an unmerged pull request, so no
			// merge date must not mean no window.
			merged:   time.Time{},
			wantFrom: day(2026, time.February, 8),
			wantTo:   day(2026, time.March, 11),
		},
		{
			name:     "a sub-day timestamp is normalised to its calendar day",
			opened:   time.Date(2026, time.March, 10, 23, 59, 59, 0, time.UTC),
			merged:   time.Date(2026, time.March, 10, 23, 59, 59, 0, time.UTC),
			wantFrom: day(2026, time.February, 8),
			wantTo:   day(2026, time.March, 11),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to := ReattributionWindow(tt.opened, tt.merged)
			assert.Equal(t, tt.wantFrom, from, "from")
			assert.Equal(t, tt.wantTo, to, "to")
		})
	}
}

// Both bounds are inclusive calendar dates: a session starting on the slack day
// after the merge, or exactly 30 days before it, is in range, and one day
// further out on either side is not.
func TestReattributionWindowBoundsAreInclusiveDates(t *testing.T) {
	merged := time.Date(2026, time.March, 10, 15, 4, 5, 0, time.UTC)
	from, to := ReattributionWindow(merged.AddDate(0, 0, -2), merged)
	inRange := func(d time.Time) bool { return !d.Before(from) && !d.After(to) }

	day := func(offset int) time.Time {
		return time.Date(2026, time.March, 10+offset, 0, 0, 0, 0, time.UTC)
	}
	assert.True(t, inRange(day(0)), "the merge day")
	assert.True(t, inRange(day(1)), "the slack day after the merge is the inclusive end")
	assert.False(t, inRange(day(2)), "two days after the merge")
	assert.True(t, inRange(day(-30)), "30 days before the merge is the inclusive start")
	assert.False(t, inRange(day(-31)), "31 days before the merge")
}
