// Command track-movement scores four weekly readings of the code stage, stores each
// week's cells and bottlenecks the way a caller would, and derives the movement series
// from them. It then reads the deployment's adoption state.
package main

import (
	"fmt"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

func main() {
	rs := rules.ForVersion("v1")
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	firstFriday := time.Date(2026, 3, 6, 0, 0, 0, 0, time.UTC)

	// The share of the code stage's 180 pull requests that an assistant helped author,
	// read on the Friday of each week, by six people.
	var snaps []aidlc.MovementSnapshotRow
	var cells []aidlc.MovementCellRow
	for week, aiPRs := range []float64{20, 60, 110, 115} {
		day := firstFriday.AddDate(0, 0, 7*week)
		pop, people := 180.0, 6
		rd := aidlc.NewReadings()
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, aidlc.Reading{Value: &aiPRs, People: &people})
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, aidlc.Reading{Value: &pop})
		res := aidlc.Score(rs, rd, aidlc.OrgRead{}, nil)

		snaps = append(snaps, aidlc.MovementSnapshotRow{AsOfDate: day, Published: true, Bottlenecks: res.Profile.GetBottlenecks()})
		for _, c := range res.Cells {
			row := aidlc.MovementCellRow{AsOfDate: day, Stage: c.Stage, Level: c.Level, State: c.State, Band: c.Band}
			if c.Share != nil {
				row.HasShare, row.ShareLow, row.ShareHigh = true, c.Share.Lo, c.Share.Hi
			}
			cells = append(cells, row)
		}
	}

	last := firstFriday.AddDate(0, 0, 21)
	for _, p := range aidlc.DeriveMovement(snaps, cells, firstFriday, last, firstFriday) {
		if p.GetStage() != aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED && p.GetStage() != code {
			continue
		}
		fmt.Println(p.GetWeekStart(), p.GetKind(), p.GetStage(), p.GetLevel(), "held", p.GetWeeksHeld())
	}

	orgCreated := time.Date(2025, 11, 1, 0, 0, 0, 0, time.UTC)
	fmt.Println(aidlc.ResolveAdoptionState(orgCreated, last, 14, 10))
}
