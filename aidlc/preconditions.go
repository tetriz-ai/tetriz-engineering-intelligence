// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"fmt"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// Preconditions are the two readings a profile is published under.
type Preconditions struct {
	// PathCoverage is the share of the window's merged pull requests that carry a
	// file list, 1.0 when there are none.
	PathCoverage float64
	// MergedPRCount is the merged pull requests PathCoverage is read over.
	MergedPRCount int
}

// CheckPreconditions reads the preconditions against the ruleset and returns every
// check, and the key of the one that withholds the profile, or
// PRECONDITION_KEY_UNSPECIFIED when the profile may be published. No merged pull
// request withholds first, because path coverage over zero pull requests is
// trivially 1.0; then path coverage below the ruleset's threshold withholds, because
// five of nine stages read file paths that are not there.
//
// A PathCoverage that is not a share between 0 and 1 (NaN included) cannot be read,
// so the path-coverage check fails on it rather than passing.
//
// Score does not consult the preconditions: a withheld profile is still a valid
// profile, and whether to show it is the caller's decision. rs must not be nil: a nil
// ruleset (such as ForVersion returns for an unknown version) panics.
func CheckPreconditions(rs *rules.RuleSet, pre Preconditions) ([]*aidlcv1.PreconditionCheck, aidlcv1.PreconditionKey) {
	coverageReadable := pre.PathCoverage >= 0 && pre.PathCoverage <= 1
	coverageDetail := fmt.Sprintf("%.2f of %d merged pull requests carry a file list; a profile needs at least %.2f",
		pre.PathCoverage, pre.MergedPRCount, rs.PathCoverageWithholdThreshold)
	if !coverageReadable {
		coverageDetail = fmt.Sprintf("path coverage %v is not a share between 0 and 1, so it cannot be read", pre.PathCoverage)
	}
	checks := []*aidlcv1.PreconditionCheck{
		{
			Key:           aidlcv1.PreconditionKey_PRECONDITION_KEY_NO_SEATED_ACTIVITY,
			Passed:        pre.MergedPRCount > 0,
			MeasuredValue: float64(pre.MergedPRCount),
			Detail:        fmt.Sprintf("%d merged pull requests in the last %d days", pre.MergedPRCount, rs.WindowDays),
		},
		{
			Key:           aidlcv1.PreconditionKey_PRECONDITION_KEY_PATH_COVERAGE,
			Passed:        coverageReadable && pre.PathCoverage >= rs.PathCoverageWithholdThreshold,
			MeasuredValue: pre.PathCoverage,
			Threshold:     rs.PathCoverageWithholdThreshold,
			Detail:        coverageDetail,
		},
	}
	for _, c := range checks {
		if !c.GetPassed() {
			return checks, c.GetKey()
		}
	}
	return checks, aidlcv1.PreconditionKey_PRECONDITION_KEY_UNSPECIFIED
}
