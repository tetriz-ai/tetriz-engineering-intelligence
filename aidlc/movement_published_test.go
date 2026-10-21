// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"testing"
	"time"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// TestDeriveMovement_WithheldDaysAreNotRead pins that a cell row from a day the
// snapshots do not mark published never becomes a week's reading: code L1 is met on
// every published day, and a withheld day's not-reached row reports no movement, either
// as the week's last day or as the week's only day.
func TestDeriveMovement_WithheldDaysAreNotRead(t *testing.T) {
	d := func(m, day int) time.Time { return time.Date(2026, time.Month(m), day, 0, 0, 0, 0, time.UTC) }
	code, l1 := aidlcv1.AidlcStage_AIDLC_STAGE_CODE, aidlcv1.AidlcLevel_AIDLC_LEVEL_L1
	type day struct {
		at        time.Time
		published bool
		met       bool
	}
	cases := []struct {
		name string
		days []day
	}{
		{"withheld day last in its week", []day{
			{d(9, 7), true, true}, {d(9, 14), true, true}, {d(9, 21), true, true},
			{d(9, 22), false, false}, {d(9, 28), true, true},
		}},
		{"week whose only day was withheld", []day{
			{d(9, 7), true, true}, {d(9, 14), true, true},
			{d(9, 22), false, false}, {d(9, 28), true, true},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var snaps []MovementSnapshotRow
			var cells []MovementCellRow
			for _, dd := range tc.days {
				snaps = append(snaps, MovementSnapshotRow{AsOfDate: dd.at, Published: dd.published})
				row := MovementCellRow{AsOfDate: dd.at, Stage: code, Level: l1, State: aidlcv1.CellState_CELL_STATE_NOT_REACHED,
					HasShare: true, ShareLow: 0.1, ShareHigh: 0.1}
				if dd.met {
					row.State, row.Band, row.ShareLow, row.ShareHigh = aidlcv1.CellState_CELL_STATE_MET, aidlcv1.PrevalenceBand_PREVALENCE_BAND_ESTABLISHED, 0.8, 0.8
				}
				cells = append(cells, row)
			}
			for _, p := range DeriveMovement(snaps, cells, d(9, 7), d(9, 28), d(9, 7)) {
				if p.GetStage() == code {
					t.Errorf("%s %s %s: a withheld day's row moved the cell", p.GetWeekStart(), p.GetKind(), p.GetLevel())
				}
			}
		})
	}
}
