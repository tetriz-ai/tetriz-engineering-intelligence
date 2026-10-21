// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// TestDeriveMovement_PromotionNeedsTheWeekRightAfter pins that a promotion is confirmed
// only by the calendar week right after the rise: not reached, met, a week with no
// published day, met. The gap clears the candidate, so the met after it is a new
// candidate, confirmed only by the following week.
func TestDeriveMovement_PromotionNeedsTheWeekRightAfter(t *testing.T) {
	w0, w1, w2, w3, w4 := "2026-06-01", "2026-06-08", "2026-06-15", "2026-06-22", "2026-06-29"
	cells := []aidlc.MovementCellRow{
		movementCell(w0, aidlcv1.CellState_CELL_STATE_NOT_REACHED, nil, nil, nil),
		movementCell(w1, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
		movementCell(w3, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
		movementCell(w4, aidlcv1.CellState_CELL_STATE_MET, emerging(), nil, nil),
	}
	snaps := []aidlc.MovementSnapshotRow{movementSnap(w0), movementSnap(w1), gapSnap(w2), movementSnap(w3), movementSnap(w4)}

	assert.Empty(t, movementSummary(aidlc.DeriveMovement(snaps[:4], cells[:3], day(w0), day(w3), day(w0))),
		"a met week after a gap is a new candidate, not a confirmation")
	assert.Equal(t, []string{"2026-06-29:MATURITY_MOVEMENT_KIND_MOVED_UP:2"},
		movementSummary(aidlc.DeriveMovement(snaps, cells, day(w0), day(w4), day(w0))))
}

// TestDeriveMovement_BandFallWithinMetIsNotReported pins a design choice: movement
// reports MOVED_DOWN only for met to not reached. A cell that stays met but falls from
// established to emerging produces no point, while its rise back is reported.
func TestDeriveMovement_BandFallWithinMetIsNotReported(t *testing.T) {
	weeks := []string{"2026-03-02", "2026-03-09", "2026-03-16", "2026-03-23", "2026-03-30", "2026-04-06"}
	hits := []float64{150, 150, 70, 70, 150, 150}
	var cells []aidlc.MovementCellRow
	var snaps []aidlc.MovementSnapshotRow
	for i, w := range weeks {
		share := hits[i] / 180
		band := established()
		if share <= 0.5 {
			band = emerging()
		}
		cells = append(cells, movementCell(w, aidlcv1.CellState_CELL_STATE_MET, band, fp(share), fp(share)))
		snaps = append(snaps, movementSnap(w))
	}
	assert.Equal(t, []string{"2026-04-06:MATURITY_MOVEMENT_KIND_MOVED_UP:2"},
		movementSummary(aidlc.DeriveMovement(snaps, cells, day(weeks[0]), day(weeks[5]), day(weeks[0]))))
}
