// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// day parses a fixture date; a malformed literal is a broken test, so it panics.
func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

// movementCell builds one week's cell row for the movement tests. band may be nil
// (an unread or NOT_REACHED cell); shareLow/shareHigh may both be nil (no share
// interval).
func movementCell(
	d string, state aidlcv1.CellState, band *aidlcv1.PrevalenceBand, shareLow, shareHigh *float64,
) aidlc.MovementCellRow {
	row := aidlc.MovementCellRow{
		AsOfDate: day(d), Stage: aidlcv1.AidlcStage_AIDLC_STAGE_CODE, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L4,
		State: state,
	}
	if band != nil {
		row.Band = *band
	}
	if shareLow != nil && shareHigh != nil {
		row.HasShare = true
		row.ShareLow = *shareLow
		row.ShareHigh = *shareHigh
	}
	return row
}

// movementSnap builds one PUBLISHED day carrying the given bottlenecks (may be
// none), the shape DeriveMovement reads for its published-week set and the
// stuck rule.
func movementSnap(d string, bottlenecks ...*aidlcv1.MaturityBottleneck) aidlc.MovementSnapshotRow {
	return aidlc.MovementSnapshotRow{AsOfDate: day(d), Published: true, Bottlenecks: bottlenecks}
}

func gapSnap(d string) aidlc.MovementSnapshotRow {
	return aidlc.MovementSnapshotRow{AsOfDate: day(d), Published: false}
}

func established() *aidlcv1.PrevalenceBand {
	b := aidlcv1.PrevalenceBand_PREVALENCE_BAND_ESTABLISHED
	return &b
}

func emerging() *aidlcv1.PrevalenceBand {
	b := aidlcv1.PrevalenceBand_PREVALENCE_BAND_EMERGING
	return &b
}

func fp(f float64) *float64 { return &f }

// movementSummary renders each point as "week:kind[:weeksHeld]" for compact
// assertions, weeksHeld included only when non-zero.
func movementSummary(points []*aidlcv1.MaturityMovementPoint) []string {
	out := make([]string, 0, len(points))
	for _, p := range points {
		if p.GetWeeksHeld() != 0 {
			out = append(out, fmt.Sprintf("%s:%s:%d", p.GetWeekStart(), p.GetKind(), p.GetWeeksHeld()))
			continue
		}
		out = append(out, fmt.Sprintf("%s:%s", p.GetWeekStart(), p.GetKind()))
	}
	return out
}

// TestDeriveMovement_WorkedExample pins a five-week worked example: the requested
// range is five weeks (from=week1, to=week5); a cell reads emerging in weeks 1-2, not-reached in
// week 3, an abstain in week 4, then emerging again in week 5. Week 0 (the
// not-reached reading week 1 promotes from) is supplied only in the input rows,
// never in the requested range, standing in for a baseline week a caller passes
// from before the range (see derivePerCellMovement); it must never itself appear as
// a point.
func TestDeriveMovement_WorkedExample(t *testing.T) {
	week0, week1, week2, week3, week4, week5 := "2026-05-25", "2026-06-01", "2026-06-08", "2026-06-15", "2026-06-22", "2026-06-29"
	cells := []aidlc.MovementCellRow{
		movementCell(week0, aidlcv1.CellState_CELL_STATE_NOT_REACHED, nil, nil, nil),
		movementCell(week1, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
		movementCell(week2, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
		movementCell(week3, aidlcv1.CellState_CELL_STATE_NOT_REACHED, nil, nil, nil),
		movementCell(week4, aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE, nil, nil, nil),
		movementCell(week5, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
	}
	snaps := []aidlc.MovementSnapshotRow{
		movementSnap(week0), movementSnap(week1), movementSnap(week2),
		movementSnap(week3), movementSnap(week4), movementSnap(week5),
	}

	out := aidlc.DeriveMovement(snaps, cells, day(week1), day(week5), day(week1))

	assert.Equal(t, []string{
		"2026-06-08:MATURITY_MOVEMENT_KIND_MOVED_UP:2",
		"2026-06-15:MATURITY_MOVEMENT_KIND_MOVED_DOWN",
		"2026-06-22:MATURITY_MOVEMENT_KIND_LOST_SIGHT",
	}, movementSummary(out), "week 5's promotion has held one week, so nothing yet; week 0 itself never appears")
}

// TestDeriveMovement_Promotion_HeldTwoWeeks isolates rule 4: a promotion is
// emitted only once it has held for two consecutive weeks, at the second week,
// carrying weeks_held.
func TestDeriveMovement_Promotion_HeldTwoWeeks(t *testing.T) {
	week0, week1, week2 := "2026-06-01", "2026-06-08", "2026-06-15"
	cells := []aidlc.MovementCellRow{
		movementCell(week0, aidlcv1.CellState_CELL_STATE_NOT_REACHED, nil, nil, nil),
		movementCell(week1, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
		movementCell(week2, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
	}
	snaps := []aidlc.MovementSnapshotRow{movementSnap(week0), movementSnap(week1), movementSnap(week2)}

	out := aidlc.DeriveMovement(snaps, cells, day(week0), day(week2), day(week0))

	require.Len(t, out, 1)
	assert.Equal(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_MOVED_UP, out[0].GetKind())
	assert.Equal(t, "2026-06-15", out[0].GetWeekStart())
	assert.Equal(t, int32(2), out[0].GetWeeksHeld())
	assert.Equal(t, aidlcv1.AidlcStage_AIDLC_STAGE_CODE, out[0].GetStage())
	assert.Equal(t, aidlcv1.AidlcLevel_AIDLC_LEVEL_L4, out[0].GetLevel())
}

// TestDeriveMovement_Promotion_BandRiseHeldTwoWeeks proves rule 4's other
// disjunct — "not met to met, OR a band rising" — independently of the
// not-met-to-met case TestDeriveMovement_Promotion_HeldTwoWeeks already covers:
// a cell already MET at EMERGING that rises to ESTABLISHED and holds it a second
// week is a promotion too.
func TestDeriveMovement_Promotion_BandRiseHeldTwoWeeks(t *testing.T) {
	week0, week1, week2 := "2026-06-01", "2026-06-08", "2026-06-15"
	cells := []aidlc.MovementCellRow{
		movementCell(week0, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
		movementCell(week1, aidlcv1.CellState_CELL_STATE_MET, established(), nil, nil),
		movementCell(week2, aidlcv1.CellState_CELL_STATE_MET, established(), nil, nil),
	}
	snaps := []aidlc.MovementSnapshotRow{movementSnap(week0), movementSnap(week1), movementSnap(week2)}

	out := aidlc.DeriveMovement(snaps, cells, day(week0), day(week2), day(week0))

	require.Len(t, out, 1)
	assert.Equal(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_MOVED_UP, out[0].GetKind())
	assert.Equal(t, "2026-06-15", out[0].GetWeekStart())
	assert.Equal(t, int32(2), out[0].GetWeeksHeld())
}

// TestDeriveMovement_PromotionConfirmationBeatsCoverageGained proves the
// per-cell-week decision emits at most one point (the first matching rule wins): when a promotion's confirming week ALSO narrows the cell's share
// bounds relative to the week before, only MOVED_UP is emitted — never
// COVERAGE_GAINED alongside it.
func TestDeriveMovement_PromotionConfirmationBeatsCoverageGained(t *testing.T) {
	week0, week1, week2 := "2026-06-01", "2026-06-08", "2026-06-15"
	cells := []aidlc.MovementCellRow{
		movementCell(week0, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
		movementCell(week1, aidlcv1.CellState_CELL_STATE_MET, established(), fp(0.55), fp(0.62)),
		// week2 repeats week1's verdict and band (the promotion confirms here) AND
		// narrows the share interval from week1's — the coverage-gained trigger —
		// in the same cell-week.
		movementCell(week2, aidlcv1.CellState_CELL_STATE_MET, established(), fp(0.57), fp(0.60)),
	}
	snaps := []aidlc.MovementSnapshotRow{movementSnap(week0), movementSnap(week1), movementSnap(week2)}

	out := aidlc.DeriveMovement(snaps, cells, day(week0), day(week2), day(week0))

	require.Len(t, out, 1, "exactly one point for this cell-week, not two")
	assert.Equal(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_MOVED_UP, out[0].GetKind())
	assert.Equal(t, int32(2), out[0].GetWeeksHeld())
}

// TestDeriveMovement_LastPublishedDayWins proves a week with more than one
// published day uses only its LAST published day — for a cell's verdict
// (groupCellsByWeek) and for a snapshot's ranked bottlenecks (
// groupSnapshotsByWeek) — never an earlier day in the same week.
func TestDeriveMovement_LastPublishedDayWins(t *testing.T) {
	t.Run("cell verdict", func(t *testing.T) {
		// week1 publishes twice: an early NOT_REACHED day, then a later
		// MET+ESTABLISHED day that must be the week's reading. week2 and week3
		// repeat that same MET+ESTABLISHED reading. If week1 wrongly used its
		// FIRST day (NOT_REACHED), week1->week2 would read as a promotion step
		// and week2->week3 would confirm it (MOVED_UP); using the last day
		// correctly, weeks 1-3 never change, so nothing is emitted at all.
		week1Early, week1Late := "2026-06-01", "2026-06-03"
		week2, week3 := "2026-06-08", "2026-06-15"
		cells := []aidlc.MovementCellRow{
			movementCell(week1Early, aidlcv1.CellState_CELL_STATE_NOT_REACHED, nil, nil, nil),
			movementCell(week1Late, aidlcv1.CellState_CELL_STATE_MET, established(), nil, nil),
			movementCell(week2, aidlcv1.CellState_CELL_STATE_MET, established(), nil, nil),
			movementCell(week3, aidlcv1.CellState_CELL_STATE_MET, established(), nil, nil),
		}
		snaps := []aidlc.MovementSnapshotRow{
			movementSnap(week1Early), movementSnap(week1Late), movementSnap(week2), movementSnap(week3),
		}

		out := aidlc.DeriveMovement(snaps, cells, day(week1Early), day(week3), day(week1Early))

		assert.Empty(t, out, "the week's reading is the later day (MET+ESTABLISHED), unchanged across weeks 1-3")
	})

	t.Run("snapshot bottlenecks", func(t *testing.T) {
		// week1 publishes twice: an early day carrying "b-old" only, then a later
		// day carrying "b-new" only. week2 and week3 also carry "b-new". If week1
		// wrongly used its first day, "b-new" would be missing from week1 and its
		// run would be two weeks, short of stuck; using the last day correctly,
		// "b-new" is present in all three weeks and is stuck.
		bOld := &aidlcv1.MaturityBottleneck{Id: "b-old", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L3}
		bNew := &aidlcv1.MaturityBottleneck{Id: "b-new", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L3}
		week1Early, week1Late, week2, week3 := "2026-06-01", "2026-06-03", "2026-06-08", "2026-06-15"
		snaps := []aidlc.MovementSnapshotRow{
			movementSnap(week1Early, bOld), movementSnap(week1Late, bNew), movementSnap(week2, bNew), movementSnap(week3, bNew),
		}

		out := aidlc.DeriveMovement(snaps, nil, day(week1Early), day(week3), day(week1Early))

		require.Len(t, out, 1)
		assert.Equal(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_STUCK, out[0].GetKind())
		assert.Equal(t, "b-new", out[0].GetBottleneckId())
		assert.Equal(t, int32(3), out[0].GetWeeksHeld())
	})
}

// TestDeriveMovement_OneWeekFlapEmitsNothing proves a promotion held for exactly
// one week (a band rise that reverts before a second confirming week) emits no
// movement — the anti-flap wait rule 4 requires, so a threshold-straddling cell
// does not draw a promotion line every time it crosses.
func TestDeriveMovement_OneWeekFlapEmitsNothing(t *testing.T) {
	week0, week1, week2 := "2026-06-01", "2026-06-08", "2026-06-15"
	cells := []aidlc.MovementCellRow{
		movementCell(week0, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
		movementCell(week1, aidlcv1.CellState_CELL_STATE_MET, established(), nil, nil),
		movementCell(week2, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
	}
	snaps := []aidlc.MovementSnapshotRow{movementSnap(week0), movementSnap(week1), movementSnap(week2)}

	out := aidlc.DeriveMovement(snaps, cells, day(week0), day(week2), day(week0))

	assert.Empty(t, out)
}

// TestDeriveMovement_Demotion_MeasuredImmediately proves rule 3 fires the week
// it is measured, with no hold requirement (unlike promotion).
func TestDeriveMovement_Demotion_MeasuredImmediately(t *testing.T) {
	week0, week1 := "2026-06-01", "2026-06-08"
	cells := []aidlc.MovementCellRow{
		movementCell(week0, aidlcv1.CellState_CELL_STATE_MET, established(), nil, nil),
		movementCell(week1, aidlcv1.CellState_CELL_STATE_NOT_REACHED, nil, nil, nil),
	}
	snaps := []aidlc.MovementSnapshotRow{movementSnap(week0), movementSnap(week1)}

	out := aidlc.DeriveMovement(snaps, cells, day(week0), day(week1), day(week0))

	require.Len(t, out, 1)
	assert.Equal(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_MOVED_DOWN, out[0].GetKind())
	assert.Equal(t, "2026-06-08", out[0].GetWeekStart())
	assert.Equal(t, int32(0), out[0].GetWeeksHeld(), "demotion carries no hold count")
}

// TestDeriveMovement_LostSightOf covers rule 1 from both eligible prior states —
// met and not-reached — into any abstain; it is never a movement.
func TestDeriveMovement_LostSightOf(t *testing.T) {
	tests := []struct {
		name       string
		priorState aidlcv1.CellState
	}{
		{"from met", aidlcv1.CellState_CELL_STATE_MET},
		{"from not reached", aidlcv1.CellState_CELL_STATE_NOT_REACHED},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			week0, week1 := "2026-06-01", "2026-06-08"
			var band *aidlcv1.PrevalenceBand
			if tt.priorState == aidlcv1.CellState_CELL_STATE_MET {
				band = emerging()
			}
			cells := []aidlc.MovementCellRow{
				movementCell(week0, tt.priorState, band, nil, nil),
				movementCell(week1, aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE, nil, nil, nil),
			}
			snaps := []aidlc.MovementSnapshotRow{movementSnap(week0), movementSnap(week1)}

			out := aidlc.DeriveMovement(snaps, cells, day(week0), day(week1), day(week0))

			require.Len(t, out, 1)
			assert.Equal(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_LOST_SIGHT, out[0].GetKind())
			assert.Equal(t, "2026-06-08", out[0].GetWeekStart())
		})
	}
}

// TestDeriveMovement_CoverageGained proves rule 2: an unchanged verdict and band
// with narrowed share bounds is coverage gained, never a movement — and a cell
// whose verdict changed is never labelled coverage gained even when its bounds
// also narrowed.
func TestDeriveMovement_CoverageGained(t *testing.T) {
	t.Run("bounds narrow, verdict unchanged", func(t *testing.T) {
		week0, week1 := "2026-06-01", "2026-06-08"
		cells := []aidlc.MovementCellRow{
			movementCell(week0, aidlcv1.CellState_CELL_STATE_NOT_REACHED, nil, fp(0.10), fp(0.50)),
			movementCell(week1, aidlcv1.CellState_CELL_STATE_NOT_REACHED, nil, fp(0.15), fp(0.40)),
		}
		snaps := []aidlc.MovementSnapshotRow{movementSnap(week0), movementSnap(week1)}

		out := aidlc.DeriveMovement(snaps, cells, day(week0), day(week1), day(week0))

		require.Len(t, out, 1)
		assert.Equal(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_COVERAGE_GAINED, out[0].GetKind())
	})

	t.Run("verdict changed is never coverage gained, even with narrower bounds", func(t *testing.T) {
		week0, week1 := "2026-06-01", "2026-06-08"
		cells := []aidlc.MovementCellRow{
			movementCell(week0, aidlcv1.CellState_CELL_STATE_NOT_REACHED, nil, fp(0.10), fp(0.50)),
			movementCell(week1, aidlcv1.CellState_CELL_STATE_MET, emerging(), fp(0.20), fp(0.35)),
		}
		snaps := []aidlc.MovementSnapshotRow{movementSnap(week0), movementSnap(week1)}

		out := aidlc.DeriveMovement(snaps, cells, day(week0), day(week1), day(week0))

		for _, p := range out {
			assert.NotEqual(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_COVERAGE_GAINED, p.GetKind())
		}
	})

	t.Run("bounds unchanged emits nothing", func(t *testing.T) {
		week0, week1 := "2026-06-01", "2026-06-08"
		cells := []aidlc.MovementCellRow{
			movementCell(week0, aidlcv1.CellState_CELL_STATE_NOT_REACHED, nil, fp(0.10), fp(0.50)),
			movementCell(week1, aidlcv1.CellState_CELL_STATE_NOT_REACHED, nil, fp(0.10), fp(0.50)),
		}
		snaps := []aidlc.MovementSnapshotRow{movementSnap(week0), movementSnap(week1)}

		out := aidlc.DeriveMovement(snaps, cells, day(week0), day(week1), day(week0))

		assert.Empty(t, out)
	})
}

// TestDeriveMovement_Stuck proves rule 5: only a bottleneck id present in every
// week of the range is stuck, and a gap week (no published day) disqualifies
// every id, since a constraint cannot be "stuck" through a week the pipeline
// never read.
func TestDeriveMovement_Stuck(t *testing.T) {
	stuck := &aidlcv1.MaturityBottleneck{Id: "gap_stuck", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L3}
	transient := &aidlcv1.MaturityBottleneck{Id: "gap_transient", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L3}
	week0, week1, week2 := "2026-06-01", "2026-06-08", "2026-06-15"

	t.Run("present every week is stuck", func(t *testing.T) {
		snaps := []aidlc.MovementSnapshotRow{
			movementSnap(week0, stuck, transient),
			movementSnap(week1, stuck, transient),
			movementSnap(week2, stuck),
		}
		out := aidlc.DeriveMovement(snaps, nil, day(week0), day(week2), day(week0))

		require.Len(t, out, 1)
		assert.Equal(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_STUCK, out[0].GetKind())
		assert.Equal(t, "gap_stuck", out[0].GetBottleneckId())
		assert.Equal(t, int32(3), out[0].GetWeeksHeld())
		assert.Equal(t, aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, out[0].GetStage())
	})

	t.Run("a gap week disqualifies every id", func(t *testing.T) {
		snaps := []aidlc.MovementSnapshotRow{
			movementSnap(week0, stuck),
			gapSnap(week1),
			movementSnap(week2, stuck),
		}
		out := aidlc.DeriveMovement(snaps, nil, day(week0), day(week2), day(week0))

		for _, p := range out {
			assert.NotEqual(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_STUCK, p.GetKind())
		}
	})
}

// TestDeriveMovement_GapDoesNotSpan proves a week with no published day breaks
// the chain: the week after a gap starts a fresh baseline rather than comparing
// against the week before the gap.
func TestDeriveMovement_GapDoesNotSpan(t *testing.T) {
	week0, week1, week2 := "2026-06-01", "2026-06-08", "2026-06-15"
	cells := []aidlc.MovementCellRow{
		movementCell(week0, aidlcv1.CellState_CELL_STATE_MET, established(), nil, nil),
		// week1 has no cell row — its snapshot was withheld, a gap.
		movementCell(week2, aidlcv1.CellState_CELL_STATE_MET, established(), nil, nil),
	}
	snaps := []aidlc.MovementSnapshotRow{movementSnap(week0), gapSnap(week1), movementSnap(week2)}

	out := aidlc.DeriveMovement(snaps, cells, day(week0), day(week2), day(week0))

	assert.Empty(t, out, "the gap breaks the chain — week2 is compared against nothing, not week0")
}

// TestDeriveMovement_TrailingUnpublishedWeekDropped pins trimAxis: a trailing week
// that has not run yet is dropped from the reported axis rather than treated as
// a gap: the current week does not yet exist. With a profile every day, this is the
// usual shape: a published day every day up to and
// including the Sunday before `to`, and `to` itself on the following Monday —
// week3's own ISO week has no published day at all yet.
func TestDeriveMovement_TrailingUnpublishedWeekDropped(t *testing.T) {
	b := &aidlcv1.MaturityBottleneck{Id: "b1", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L3}
	week0, week2, week3 := "2026-06-01", "2026-06-15", "2026-06-22" // Mondays
	var snaps []aidlc.MovementSnapshotRow
	for d := day(week0); d.Before(day(week3)); d = d.AddDate(0, 0, 1) {
		snaps = append(snaps, movementSnap(d.Format("2006-01-02"), b))
	}

	out := aidlc.DeriveMovement(snaps, nil, day(week0), day(week3), day(week0))

	require.Len(t, out, 1)
	assert.Equal(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_STUCK, out[0].GetKind())
	assert.Equal(t, week2, out[0].GetWeekStart(), "the reported axis ends at the last week that actually ran")
	assert.Equal(t, int32(3), out[0].GetWeeksHeld(), "weeks_held counts published weeks only")
}

// TestDeriveMovement_AxisCappedAtTwelveWeeks pins trimAxis's cap: a caller's
// range that touches thirteen ISO weeks (an 84-day range does exactly this, since
// neither endpoint need land on a week boundary) is still reported over twelve,
// the series' fixed width,
// dropping the oldest calendar week rather than inflating weeks_held.
func TestDeriveMovement_AxisCappedAtTwelveWeeks(t *testing.T) {
	weeks := []string{
		"2026-06-01", "2026-06-08", "2026-06-15", "2026-06-22", "2026-06-29",
		"2026-07-06", "2026-07-13", "2026-07-20", "2026-07-27", "2026-08-03",
		"2026-08-10", "2026-08-17", "2026-08-24",
	}
	require.Len(t, weeks, 13, "thirteen calendar weeks touched by an 84-day range")
	b := &aidlcv1.MaturityBottleneck{Id: "b1", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L3}
	var snaps []aidlc.MovementSnapshotRow
	for _, wk := range weeks {
		snaps = append(snaps, movementSnap(wk, b))
	}

	out := aidlc.DeriveMovement(snaps, nil, day(weeks[0]), day(weeks[len(weeks)-1]), day(weeks[0]))

	require.Len(t, out, 1)
	assert.Equal(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_STUCK, out[0].GetKind())
	assert.Equal(t, int32(12), out[0].GetWeeksHeld(), "capped to the twelve-week width, not the thirteen weeks touched")
}

// TestDeriveMovement_RegainedSightIsNotPromotion pins the promotion rule: a promotion is measured against the verdict a cell
// last held while visible, never against the abstaining week itself, so
// regaining sight is never reported as a movement on its own.
func TestDeriveMovement_RegainedSightIsNotPromotion(t *testing.T) {
	t.Run("same rung on return: only LOST_SIGHT, never MOVED_UP", func(t *testing.T) {
		week0, week1, week2, week3 := "2026-06-01", "2026-06-08", "2026-06-15", "2026-06-22"
		cells := []aidlc.MovementCellRow{
			movementCell(week0, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
			movementCell(week1, aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE, nil, nil, nil),
			movementCell(week2, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
			movementCell(week3, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
		}
		snaps := []aidlc.MovementSnapshotRow{
			movementSnap(week0), movementSnap(week1), movementSnap(week2), movementSnap(week3),
		}

		out := aidlc.DeriveMovement(snaps, cells, day(week0), day(week3), day(week0))

		assert.Equal(t, []string{"2026-06-08:MATURITY_MOVEMENT_KIND_LOST_SIGHT"}, movementSummary(out),
			"the cell returns to, and holds, the rung it left off at — it never moved")
	})

	t.Run("higher rung on return still needs the two-week hold", func(t *testing.T) {
		week0, week1, week2, week3 := "2026-06-01", "2026-06-08", "2026-06-15", "2026-06-22"
		cells := []aidlc.MovementCellRow{
			movementCell(week0, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
			movementCell(week1, aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE, nil, nil, nil),
			movementCell(week2, aidlcv1.CellState_CELL_STATE_MET, established(), nil, nil),
			movementCell(week3, aidlcv1.CellState_CELL_STATE_MET, established(), nil, nil),
		}
		snaps := []aidlc.MovementSnapshotRow{
			movementSnap(week0), movementSnap(week1), movementSnap(week2), movementSnap(week3),
		}

		out := aidlc.DeriveMovement(snaps, cells, day(week0), day(week3), day(week0))

		assert.Equal(t, []string{
			"2026-06-08:MATURITY_MOVEMENT_KIND_LOST_SIGHT",
			"2026-06-22:MATURITY_MOVEMENT_KIND_MOVED_UP:2",
		}, movementSummary(out), "a genuine rank rise across the abstain still promotes, held two weeks")
	})

	t.Run("abstain in the axis's own first week: the lookback row still seeds last-visible", func(t *testing.T) {
		lookback, week1, week2, week3 := "2026-05-25", "2026-06-01", "2026-06-08", "2026-06-15"
		cells := []aidlc.MovementCellRow{
			movementCell(lookback, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
			movementCell(week1, aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE, nil, nil, nil),
			movementCell(week2, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
			movementCell(week3, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
		}
		snaps := []aidlc.MovementSnapshotRow{
			movementSnap(lookback), movementSnap(week1), movementSnap(week2), movementSnap(week3),
		}

		// from=week1: the lookback week is supplied only in the input rows, the
		// same way a caller passes one calendar week before the requested range
		// purely as a comparison baseline; it must never itself be a candidate
		// to emit a point, but it must still seed last-visible for week1's own
		// abstain, since week1 (the axis's first entry) is never itself a pr.
		out := aidlc.DeriveMovement(snaps, cells, day(week1), day(week3), day(lookback))

		assert.Equal(t, []string{"2026-06-01:MATURITY_MOVEMENT_KIND_LOST_SIGHT"}, movementSummary(out),
			"the lookback row (met) must seed last-visible before week1's own abstain is checked, "+
				"so the week2/week3 regain at the same rung never reads as a promotion")
	})
}

// TestDeriveMovement_HistoryStarts covers rule 6's two disjuncts: a backfill
// boundary inside the range names when history begins, and too little published
// history yet carries an empty history_from (the "next week establishes the
// baseline" case).
func TestDeriveMovement_HistoryStarts(t *testing.T) {
	t.Run("backfilled_from later than range start", func(t *testing.T) {
		from, to := day("2026-06-01"), day("2026-06-15")
		backfilledFrom := day("2026-06-08")
		snaps := []aidlc.MovementSnapshotRow{movementSnap("2026-06-08"), movementSnap("2026-06-15")}

		out := aidlc.DeriveMovement(snaps, nil, from, to, backfilledFrom)

		require.Len(t, out, 1)
		assert.Equal(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_HISTORY_STARTS, out[0].GetKind())
		assert.Equal(t, "2026-06-08", out[0].GetHistoryFrom())
	})

	t.Run("fewer than two published weeks", func(t *testing.T) {
		from, to := day("2026-06-01"), day("2026-06-08")
		snaps := []aidlc.MovementSnapshotRow{movementSnap("2026-06-01")}

		out := aidlc.DeriveMovement(snaps, nil, from, to, from)

		require.Len(t, out, 1)
		assert.Equal(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_HISTORY_STARTS, out[0].GetKind())
		assert.Empty(t, out[0].GetHistoryFrom(), "too-little-history case carries no history_from")
	})

	t.Run("two or more published weeks and no later backfill emits no history-starts line", func(t *testing.T) {
		from, to := day("2026-06-01"), day("2026-06-08")
		snaps := []aidlc.MovementSnapshotRow{movementSnap("2026-06-01"), movementSnap("2026-06-08")}

		out := aidlc.DeriveMovement(snaps, nil, from, to, from)

		for _, p := range out {
			assert.NotEqual(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_HISTORY_STARTS, p.GetKind())
		}
	})

	t.Run("no week in the range published: the trimmed axis is empty but the baseline still emits", func(t *testing.T) {
		from, to := day("2026-06-01"), day("2026-06-08")
		snaps := []aidlc.MovementSnapshotRow{gapSnap("2026-06-01"), gapSnap("2026-06-08")}

		out := aidlc.DeriveMovement(snaps, nil, from, to, from)

		require.Len(t, out, 1)
		assert.Equal(t, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_HISTORY_STARTS, out[0].GetKind())
		assert.Equal(t, "2026-06-01", out[0].GetWeekStart(),
			"trimAxis trims every week here (none published), so the baseline anchors to the requested range's own first calendar week rather than going silent for lack of a reported axis")
	})
}

// TestDeriveMovement_BaselineOverEveryWeekRead pins that the verdict entering the
// axis is derived from every week the caller passed, not one pre-axis week. An
// 84-day range touches thirteen ISO weeks, so the twelve-week cap drops the oldest,
// and a baseline week before the range sits one week before that: two weeks precede
// the axis. Each case's history runs lookback, then the week before the
// first reported week, then the reported weeks.
func TestDeriveMovement_BaselineOverEveryWeekRead(t *testing.T) {
	const (
		met      = aidlcv1.CellState_CELL_STATE_MET
		notMet   = aidlcv1.CellState_CELL_STATE_NOT_REACHED
		abstains = aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE
	)
	type reading struct {
		state aidlcv1.CellState
		band  *aidlcv1.PrevalenceBand
	}
	// Fourteen consecutive Mondays: week 0 is the lookback, weeks 1-13 the thirteen
	// weeks a capped request touches.
	var mondays []string
	for d := day("2026-05-25"); len(mondays) < 14; d = d.AddDate(0, 0, 7) {
		mondays = append(mondays, d.Format("2006-01-02"))
	}

	tests := []struct {
		name string
		// fromIdx and toIdx bound the requested range; the read starts one week
		// before fromIdx. A range touching thirteen weeks is capped to twelve.
		fromIdx, toIdx int
		history        []reading // from mondays[0] onward
		want           []string
	}{
		{
			name:    "capped read: met, then abstaining from the dropped week, then met again at the same rung",
			fromIdx: 1, toIdx: 13,
			history: []reading{{met, emerging()}, {abstains, nil}, {abstains, nil}, {met, emerging()}, {met, emerging()}},
			want:    []string{mondays[2] + ":MATURITY_MOVEMENT_KIND_LOST_SIGHT"},
		},
		{
			name:    "uncapped read of the same history: the loss falls on the axis's first week",
			fromIdx: 1, toIdx: 4,
			history: []reading{{met, emerging()}, {abstains, nil}, {abstains, nil}, {met, emerging()}, {met, emerging()}},
			want:    []string{mondays[1] + ":MATURITY_MOVEMENT_KIND_LOST_SIGHT"},
		},
		{
			name:    "capped read: not reached before the abstains, met after, is a promotion once held",
			fromIdx: 1, toIdx: 13,
			history: []reading{{notMet, nil}, {abstains, nil}, {abstains, nil}, {met, emerging()}, {met, emerging()}},
			want: []string{
				mondays[2] + ":MATURITY_MOVEMENT_KIND_LOST_SIGHT",
				mondays[4] + ":MATURITY_MOVEMENT_KIND_MOVED_UP:2",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toIdx := tt.toIdx
			var cells []aidlc.MovementCellRow
			for w, rd := range tt.history {
				cells = append(cells, movementCell(mondays[w], rd.state, rd.band, nil, nil))
			}
			var snaps []aidlc.MovementSnapshotRow
			for w := 0; w <= toIdx; w++ {
				snaps = append(snaps, movementSnap(mondays[w]))
			}

			out := aidlc.DeriveMovement(snaps, cells, day(mondays[tt.fromIdx]), day(mondays[toIdx]), day(mondays[0]))

			assert.Equal(t, tt.want, movementSummary(out))
		})
	}
}

// TestDeriveMovement_HeldVerdictBaseline pins that promotion and demotion are both
// measured against the verdict a cell last held for a confirmed week, carried
// across abstains and gaps, and that a fall from an unconfirmed candidate is
// silent.
func TestDeriveMovement_HeldVerdictBaseline(t *testing.T) {
	const (
		met      = aidlcv1.CellState_CELL_STATE_MET
		notMet   = aidlcv1.CellState_CELL_STATE_NOT_REACHED
		abstains = aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE
	)
	type reading struct {
		state aidlcv1.CellState
		band  *aidlcv1.PrevalenceBand
		gap   bool // no published day this week
	}
	mondays := []string{"2026-06-01", "2026-06-08", "2026-06-15", "2026-06-22"}

	tests := []struct {
		name    string
		history []reading // one per entry of mondays, oldest first
		want    []string
	}{
		{
			name:    "lower rung on return emits LOST_SIGHT then MOVED_DOWN",
			history: []reading{{state: met, band: emerging()}, {state: abstains}, {state: notMet}, {state: notMet}},
			want: []string{
				"2026-06-08:MATURITY_MOVEMENT_KIND_LOST_SIGHT",
				"2026-06-15:MATURITY_MOVEMENT_KIND_MOVED_DOWN",
			},
		},
		{
			name:    "a cell straddling the cut never confirms a rise and never falls",
			history: []reading{{state: notMet}, {state: met, band: emerging()}, {state: notMet}, {state: met, band: emerging()}},
			want:    []string{},
		},
		{
			name:    "a promotion survives a gap",
			history: []reading{{state: notMet}, {gap: true}, {state: met, band: emerging()}, {state: met, band: emerging()}},
			want:    []string{"2026-06-22:MATURITY_MOVEMENT_KIND_MOVED_UP:2"},
		},
		{
			name:    "lost sight is judged across a gap and reported once",
			history: []reading{{state: met, band: emerging()}, {gap: true}, {state: abstains}, {state: abstains}},
			want:    []string{"2026-06-15:MATURITY_MOVEMENT_KIND_LOST_SIGHT"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Len(t, tt.history, len(mondays))
			var cells []aidlc.MovementCellRow
			var snaps []aidlc.MovementSnapshotRow
			for w, rd := range tt.history {
				if rd.gap {
					snaps = append(snaps, gapSnap(mondays[w]))
					continue
				}
				snaps = append(snaps, movementSnap(mondays[w]))
				cells = append(cells, movementCell(mondays[w], rd.state, rd.band, nil, nil))
			}

			out := aidlc.DeriveMovement(snaps, cells, day(mondays[0]), day(mondays[len(mondays)-1]), day(mondays[0]))

			assert.Equal(t, tt.want, movementSummary(out))
		})
	}
}

// TestDeriveMovement_AxisEdges pins the two rules keyed to the reported axis
// rather than the caller's raw range: start of history compares backfilled_from
// against the axis's first week, and stuck needs a trailing run of at least three published weeks.
func TestDeriveMovement_AxisEdges(t *testing.T) {
	b := &aidlcv1.MaturityBottleneck{Id: "b1", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L3}

	// An 84-day request ending on a Wednesday touches thirteen ISO weeks; the cap
	// drops the week holding from, so the axis starts the Monday after it.
	eightyFourTo := day("2026-08-26")
	eightyFourFrom := eightyFourTo.AddDate(0, 0, -84)
	var daily []aidlc.MovementSnapshotRow
	for d := day("2026-06-04"); !d.After(eightyFourTo); d = d.AddDate(0, 0, 1) {
		daily = append(daily, movementSnap(d.Format("2006-01-02")))
	}

	tests := []struct {
		name           string
		snaps          []aidlc.MovementSnapshotRow
		from, to       string
		backfilledFrom string
		want           []string
	}{
		{
			name:           "backfill landing in the capped-off week yields no status point",
			snaps:          daily,
			from:           eightyFourFrom.Format("2006-01-02"),
			to:             eightyFourTo.Format("2006-01-02"),
			backfilledFrom: "2026-06-04",
			want:           []string{},
		},
		{
			name:           "a one-week axis is a baseline, never stuck",
			snaps:          []aidlc.MovementSnapshotRow{movementSnap("2026-06-01", b), movementSnap("2026-06-03", b)},
			from:           "2026-06-01",
			to:             "2026-06-03",
			backfilledFrom: "2026-06-01",
			want:           []string{"2026-06-01:MATURITY_MOVEMENT_KIND_HISTORY_STARTS"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := aidlc.DeriveMovement(tt.snaps, nil, day(tt.from), day(tt.to), day(tt.backfilledFrom))

			assert.Equal(t, tt.want, movementSummary(out))
		})
	}
}

// TestDeriveMovement_StuckTrailingRun pins the stuck rule: a constraint is stuck
// when it is present in a trailing run of at least three consecutive axis weeks
// ending at the last one, and weeks_held is that run's length.
func TestDeriveMovement_StuckTrailingRun(t *testing.T) {
	b := &aidlcv1.MaturityBottleneck{Id: "b1", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L3}
	var mondays []string
	for d := day("2026-06-01"); len(mondays) < 12; d = d.AddDate(0, 0, 7) {
		mondays = append(mondays, d.Format("2006-01-02"))
	}

	tests := []struct {
		name    string
		present func(week int) bool // over the twelve axis weeks, oldest first
		want    []string
	}{
		{
			name:    "a run of three at the tail is stuck for three",
			present: func(w int) bool { return w >= 9 },
			want:    []string{"2026-08-17:MATURITY_MOVEMENT_KIND_STUCK:3"},
		},
		{
			name:    "present in the last ten of twelve is stuck for ten",
			present: func(w int) bool { return w >= 2 },
			want:    []string{"2026-08-17:MATURITY_MOVEMENT_KIND_STUCK:10"},
		},
		{
			name:    "a run of two is not stuck",
			present: func(w int) bool { return w >= 10 },
			want:    []string{},
		},
		{
			name:    "present in weeks one to five but not the last is not stuck",
			present: func(w int) bool { return w < 5 },
			want:    []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var snaps []aidlc.MovementSnapshotRow
			for w, monday := range mondays {
				if tt.present(w) {
					snaps = append(snaps, movementSnap(monday, b))
					continue
				}
				snaps = append(snaps, movementSnap(monday))
			}

			out := aidlc.DeriveMovement(snaps, nil, day(mondays[0]), day(mondays[len(mondays)-1]), day(mondays[0]))

			assert.Equal(t, tt.want, movementSummary(out))
		})
	}
}

// TestDeriveMovement_LostSightVisibleAgain pins that a LOST_SIGHT point says whether
// the same cell reads again in the axis's last week, and that no other point kind
// ever carries the flag.
func TestDeriveMovement_LostSightVisibleAgain(t *testing.T) {
	week0, week1, week2, week3 := "2026-06-01", "2026-06-08", "2026-06-15", "2026-06-22"
	met := func(d string) aidlc.MovementCellRow {
		return movementCell(d, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil)
	}
	unseen := func(d string) aidlc.MovementCellRow {
		return movementCell(d, aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE, nil, nil, nil)
	}
	tests := []struct {
		name  string
		cells []aidlc.MovementCellRow
		want  bool
	}{
		{"visible in the last axis week", []aidlc.MovementCellRow{met(week0), unseen(week1), met(week2), met(week3)}, true},
		{"still not visible in the last axis week", []aidlc.MovementCellRow{met(week0), unseen(week1), unseen(week2), unseen(week3)}, false},
		{"no reading in the last axis week", []aidlc.MovementCellRow{met(week0), unseen(week1), met(week2)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snaps := []aidlc.MovementSnapshotRow{
				movementSnap(week0), movementSnap(week1), movementSnap(week2), movementSnap(week3),
			}
			out := aidlc.DeriveMovement(snaps, tt.cells, day(week0), day(week3), day(week0))

			var lost []*aidlcv1.MaturityMovementPoint
			for _, p := range out {
				if p.GetKind() == aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_LOST_SIGHT {
					lost = append(lost, p)
					continue
				}
				assert.False(t, p.GetVisibleAgain(), "only a lost-sight point carries visible_again")
			}
			require.Len(t, lost, 1)
			assert.Equal(t, tt.want, lost[0].GetVisibleAgain())
		})
	}
}
