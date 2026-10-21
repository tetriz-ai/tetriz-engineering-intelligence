// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"math"
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// TestCheckPreconditions pins the withhold order and the 0.80 bar: no merged pull
// request withholds first (path coverage over none is a vacuous 1.0), then path
// coverage strictly below the threshold withholds, and exactly the threshold
// publishes. A coverage that is not a number withholds rather than publishing.
func TestCheckPreconditions(t *testing.T) {
	rs := rules.ForVersion("v1")
	tests := []struct {
		name string
		pre  Preconditions
		want aidlcv1.PreconditionKey
	}{
		{"no merged pull request", Preconditions{PathCoverage: 1, MergedPRCount: 0}, aidlcv1.PreconditionKey_PRECONDITION_KEY_NO_SEATED_ACTIVITY},
		{"no merged pull request wins over low coverage", Preconditions{PathCoverage: 0, MergedPRCount: 0}, aidlcv1.PreconditionKey_PRECONDITION_KEY_NO_SEATED_ACTIVITY},
		{"coverage under the bar", Preconditions{PathCoverage: 0.79, MergedPRCount: 100}, aidlcv1.PreconditionKey_PRECONDITION_KEY_PATH_COVERAGE},
		{"coverage exactly at the bar publishes", Preconditions{PathCoverage: 0.80, MergedPRCount: 100}, aidlcv1.PreconditionKey_PRECONDITION_KEY_UNSPECIFIED},
		{"full coverage publishes", Preconditions{PathCoverage: 1, MergedPRCount: 1}, aidlcv1.PreconditionKey_PRECONDITION_KEY_UNSPECIFIED},
		{"NaN coverage withholds", Preconditions{PathCoverage: math.NaN(), MergedPRCount: 10}, aidlcv1.PreconditionKey_PRECONDITION_KEY_PATH_COVERAGE},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checks, got := CheckPreconditions(rs, tt.pre)
			if got != tt.want {
				t.Errorf("withheld by %v, want %v", got, tt.want)
			}
			if len(checks) != 2 {
				t.Fatalf("got %d checks, want both", len(checks))
			}
			for _, c := range checks {
				if c.GetDetail() == "" {
					t.Errorf("check %v has no detail", c.GetKey())
				}
			}
			if checks[1].GetThreshold() != rs.PathCoverageWithholdThreshold {
				t.Errorf("path coverage threshold = %v, want the ruleset's %v", checks[1].GetThreshold(), rs.PathCoverageWithholdThreshold)
			}
		})
	}
}
