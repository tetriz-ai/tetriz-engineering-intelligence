// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold_test

import (
	"fmt"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/fold"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// Eight people merge forty feature pull requests; an AI session authored thirty of them,
// each in three human turns. The body is the aidlc README's "Fold raw facts" program
// (aidlc/readme_test.go checks).
func Example() {
	facts := fold.Facts{
		Attribution: fold.Attribution{AuthoredPRKeys: map[string]struct{}{}},
		HumanTurns:  map[string]int{},
	}
	for n := 0; n < 40; n++ {
		person := fmt.Sprintf("person-%d", n%8)
		key := fmt.Sprintf("acme/api#%d", n)
		if n < 8 {
			facts.Roster = append(facts.Roster, person)
		}
		facts.MergedPRs = append(facts.MergedPRs, fold.MergedPR{
			PRKey: key, AuthorIdentityID: &person, AuthorSeatLinked: true,
			WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE,
			Paths:    []string{"api/handler.go"}, HasFileList: true,
		})
		if n < 30 {
			facts.Attribution.AuthoredPRKeys[key] = struct{}{}
			facts.HumanTurns[key] = 3
		}
	}

	folded := fold.Fold(facts)
	rs := rules.ForVersion("v1")
	_, withheld := aidlc.CheckPreconditions(rs, folded.Preconditions)
	fmt.Println("withheld by:", withheld)
	res := aidlc.Score(rs, folded.Readings, folded.Org, nil)
	for _, c := range res.Cells {
		if c.Stage == aidlcv1.AidlcStage_AIDLC_STAGE_CODE {
			fmt.Println(c.Level, c.State, c.Band, c.AbstainKind())
		}
	}
	// Output:
	// withheld by: PRECONDITION_KEY_UNSPECIFIED
	// AIDLC_LEVEL_L1 CELL_STATE_MET PREVALENCE_BAND_ESTABLISHED ABSTAIN_KIND_UNSPECIFIED
	// AIDLC_LEVEL_L2 CELL_STATE_MET PREVALENCE_BAND_UNSPECIFIED ABSTAIN_KIND_UNSPECIFIED
	// AIDLC_LEVEL_L3 CELL_STATE_MET PREVALENCE_BAND_UNSPECIFIED ABSTAIN_KIND_UNSPECIFIED
	// AIDLC_LEVEL_L4 CELL_STATE_MET PREVALENCE_BAND_ESTABLISHED ABSTAIN_KIND_UNSPECIFIED
	// AIDLC_LEVEL_L5 CELL_STATE_NOT_YET_VISIBLE PREVALENCE_BAND_UNSPECIFIED ABSTAIN_KIND_GAP
}
