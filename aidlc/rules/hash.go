// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// computeHash is the ruleset's canonical serialisation and its digest. It walks the
// three layers, the band cuts, the population floors, the window, the precondition
// threshold and the gap remediation classes in a fixed order, maps sorted by key, so
// the same ruleset always serialises to the same bytes and any change to a released
// set moves the digest. The serialisation names every field, so a reordering or a
// silent default can never collide with a different set.
func computeHash(rs *RuleSet) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ruleset\x1fversion=%s\x1fwindow_days=%d\n", rs.Version, rs.WindowDays)
	fmt.Fprintf(&b, "bands\x1festablished=%.6f\x1femerging=%.6f\x1femerging_uncalibrated=%t\n",
		rs.BandCuts.Established, rs.BandCuts.Emerging, rs.BandCuts.EmergingUncalibrated)
	fmt.Fprintf(&b, "floors\x1fpeople=%d\x1fartifacts=%d\n",
		rs.PopulationFloors.People, rs.PopulationFloors.Artifacts)
	fmt.Fprintf(&b, "preconditions\x1fpath_coverage_withhold=%.6f\n", rs.PathCoverageWithholdThreshold)

	// Signals, sorted by key number.
	sigKeys := make([]int, 0, len(rs.Signals))
	for k := range rs.Signals {
		sigKeys = append(sigKeys, int(k))
	}
	sort.Ints(sigKeys)
	b.WriteString("signals\n")
	for _, ki := range sigKeys {
		d := rs.Signals[aidlcv1.SignalKey(ki)]
		fmt.Fprintf(&b, "  sig\x1fkey=%d\x1fname=%s\x1fcollector=%s\x1fpopulation=%s\x1fgrain=%d\x1fbound=%s\x1fstatus=%d\x1fobserves=%d\n",
			ki, d.Name, d.Collector, d.Population, int(d.Grain), d.CoverageBound, int(d.Status), int(d.Observes))
	}

	// Cells, sorted by (stage, level).
	cellKeys := make([]CellKey, 0, len(rs.Cells))
	for k := range rs.Cells {
		cellKeys = append(cellKeys, k)
	}
	sortCellKeys(cellKeys)
	b.WriteString("cells\n")
	for _, ck := range cellKeys {
		c := rs.Cells[ck]
		fmt.Fprintf(&b, "  cell\x1fstage=%d\x1flevel=%d\x1fkind=%d\x1fgap=%d\n",
			int(c.Stage), int(c.Level), int(c.Kind), int(c.Gap))
		for _, ch := range c.Checks {
			fmt.Fprintf(&b, "    check\x1fkind=%d\x1fsignal=%d\x1fden=%d\x1fpeople_min=%d\n",
				int(ch.Kind), int(ch.Signal), int(ch.Denominator), ch.PeopleMin)
		}
	}

	// Bottlenecks, in declared order (the order is part of the ruleset).
	b.WriteString("bottlenecks\n")
	for _, bn := range rs.Bottlenecks {
		fmt.Fprintf(&b, "  bn\x1fkind=%d\x1fsubject=%s\x1fhas_move=%t\n", int(bn.Kind), bn.Subject, bn.HasMove)
	}

	// Gap remediation, sorted by gap.
	gapKeys := make([]int, 0, len(rs.GapRemediation))
	for k := range rs.GapRemediation {
		gapKeys = append(gapKeys, int(k))
	}
	sort.Ints(gapKeys)
	b.WriteString("gap_remediation\n")
	for _, gk := range gapKeys {
		fmt.Fprintf(&b, "  gap\x1fid=%d\x1fclass=%d\n", gk, int(rs.GapRemediation[aidlcv1.InstrumentationGap(gk)]))
	}

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}
