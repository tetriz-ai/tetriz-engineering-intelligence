// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package rules

// buildV1 is the released ruleset. Its band cuts, floors, window and precondition
// threshold are below; its signals, cells and bottlenecks are in signals.go,
// cells.go and bottlenecks.go. Once released its contents are frozen: a change ships
// as a new version, never an edit here (TestHashFrozen).
func buildV1() *RuleSet {
	rs := &RuleSet{
		Version:    "v1",
		WindowDays: 90,
		BandCuts: BandCuts{
			Established:          0.50,
			Emerging:             0.20,
			EmergingUncalibrated: true,
		},
		PopulationFloors: PopulationFloors{
			People:    5,
			Artifacts: 20,
		},
		// Below four-fifths of merged work carrying file paths, the path-defined
		// stages read too little to publish.
		PathCoverageWithholdThreshold: 0.80,
		Signals:                       buildSignals(),
		Cells:                         buildCells(),
		Bottlenecks:                   buildBottlenecks(),
		GapRemediation:                buildGapRemediation(),
	}
	rs.hash = computeHash(rs)
	return rs
}
