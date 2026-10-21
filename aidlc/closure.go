// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// ladderOrder is the five rungs in order, the axis downward closure and the frontier
// walk both read.
var ladderOrder = []aidlcv1.AidlcLevel{
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L2,
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L3,
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L4,
	aidlcv1.AidlcLevel_AIDLC_LEVEL_L5,
}

// closeDownward awards, as implied floors, the closable-abstain rungs beneath a met
// one. A level is a downward-closed capability claim: an org demonstrably doing L4
// work can do L3 work, so a rung its own reading could not decide is awarded when a
// higher rung on the stage is met.
//
// The walk runs top-down. A met rung sets metAbove. A closable abstain beneath one
// is floored. A non-closable abstain or a measured miss (NOT_REACHED) breaks the
// chain — the one denial on the row that informs stays unmet, and the bottleneck
// derivations read exactly it. No stage ceiling caps the walk.
func closeDownward(cells map[aidlcv1.AidlcLevel]*Cell) {
	metAbove := false
	for i := len(ladderOrder) - 1; i >= 0; i-- {
		c := cells[ladderOrder[i]]
		if c == nil {
			continue
		}
		if c.State == aidlcv1.CellState_CELL_STATE_MET {
			metAbove = true
			continue
		}
		closable := c.State == aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE && c.abstain.closable()
		if !closable {
			if metAbove {
				break
			}
			continue
		}
		if metAbove {
			c.State = aidlcv1.CellState_CELL_STATE_MET
			c.DecidedBy = aidlcv1.DecidedBy_DECIDED_BY_IMPLIED_FLOOR
			c.AwardedFloor = true
			c.abstain = abNone
			c.Gap = aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_UNSPECIFIED
			c.Reason = ""
			// The share, when the floored rung carried a straddling interval, is kept:
			// the abstain a capability one rung up outranks, without discarding what
			// was measured. Band stays UNSPECIFIED — an implied floor is not a
			// prevalence band.
		}
	}
}

// stageFrontier returns (frontier, emergingEdge) for a stage's cells. The frontier is the highest rung banded
// established; the emerging edge is the highest rung above it banded emerging. An
// implied floor sets neither — it carries no band. An absent frontier is a real
// reading, returned as UNSPECIFIED.
func stageFrontier(cells map[aidlcv1.AidlcLevel]*Cell) (frontier, edge aidlcv1.AidlcLevel) {
	frontierIdx := -1
	for i, lv := range ladderOrder {
		c := cells[lv]
		if c != nil && c.State == aidlcv1.CellState_CELL_STATE_MET &&
			c.Band == aidlcv1.PrevalenceBand_PREVALENCE_BAND_ESTABLISHED {
			frontier = lv
			frontierIdx = i
		}
	}
	for i := frontierIdx + 1; i < len(ladderOrder); i++ {
		c := cells[ladderOrder[i]]
		if c != nil && c.State == aidlcv1.CellState_CELL_STATE_MET &&
			c.Band == aidlcv1.PrevalenceBand_PREVALENCE_BAND_EMERGING {
			edge = ladderOrder[i]
		}
	}
	return frontier, edge
}
