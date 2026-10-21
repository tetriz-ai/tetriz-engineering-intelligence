// Command score-maturity-readings scores one organisation's readings and prints the
// review row of its AIDLC maturity map.
package main

import (
	"fmt"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

func main() {
	seats, users := 20.0, 14
	reviewers := float64(users)
	rd := aidlc.NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, aidlc.Reading{Value: &seats})
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, aidlc.Reading{Value: &reviewers, People: &users})

	res := aidlc.Score(rules.ForVersion("v1"), rd, aidlc.OrgRead{}, nil)
	fmt.Println(res.Profile.GetSummary())
	for _, st := range res.Profile.GetStages() {
		if st.GetStage() != aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW {
			continue
		}
		for _, c := range st.GetCells() {
			fmt.Println(c.GetLevel(), c.GetState(), c.GetAbstainKind())
		}
	}
}
