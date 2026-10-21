// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc_test

import (
	"fmt"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// The quick start: score two readings and print the review row. Its body is the body
// of examples/score-maturity-readings/main.go (readme_test.go checks).
func ExampleScore() {
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
	// Output:
	// In Review, a person does the work and an assistant helps on request. Nothing could be measured in Product spec, Tech spec, Plan, Code, Test & QA, CI/CD, Deploy and Operate.
	// AIDLC_LEVEL_L1 CELL_STATE_MET ABSTAIN_KIND_UNSPECIFIED
	// AIDLC_LEVEL_L2 CELL_STATE_NOT_YET_VISIBLE ABSTAIN_KIND_GAP
	// AIDLC_LEVEL_L3 CELL_STATE_NOT_YET_VISIBLE ABSTAIN_KIND_GAP
	// AIDLC_LEVEL_L4 CELL_STATE_NOT_YET_VISIBLE ABSTAIN_KIND_GAP
	// AIDLC_LEVEL_L5 CELL_STATE_NOT_YET_VISIBLE ABSTAIN_KIND_GAP
}

// A window where 70 of 100 merged pull requests carry a file list is under the
// ruleset's 0.80 threshold, so the profile is withheld on path coverage.
func ExampleCheckPreconditions() {
	_, withheld := aidlc.CheckPreconditions(rules.ForVersion("v1"), aidlc.Preconditions{PathCoverage: 0.7, MergedPRCount: 100})
	fmt.Println(withheld)
	// Output: PRECONDITION_KEY_PATH_COVERAGE
}

// An organisation older than sixty days whose active engineers went from 10 to 12
// (twenty percent, not more) reads steady.
func ExampleResolveAdoptionState() {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	fmt.Println(aidlc.ResolveAdoptionState(created, asOf, 12, 10))
	// Output: MATURITY_ADOPTION_STATE_STEADY
}
