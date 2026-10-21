// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issueattribution

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr(v float64) *float64 { return &v }

// TestResult_MarshalsToItsDocumentedShape pins the serialised contract: every
// name is snake_case, a nil percentage is null rather than absent, and the
// basis is its own name.
func TestResult_MarshalsToItsDocumentedShape(t *testing.T) {
	r := Result{
		Total: 3, Linked: 2, Unlinked: 1, AI: 1,
		Headline: ptr(50), Coverage: ptr(66.7), LowCoverage: true, DroppedLinks: 1,
		Issues: []IssueResult{
			{
				Key:      "PROJ-1",
				ClosedAt: time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
				PRs:      []string{"acme/web#PR-7"},
				AI:       true, Percent: ptr(100), Basis: BasisLines,
			},
			{Key: "PROJ-2", Basis: BasisNone},
		},
	}

	b, err := json.Marshal(r)
	require.NoError(t, err)

	assert.JSONEq(t, `{
	  "total": 3, "linked": 2, "unlinked": 1, "ai": 1,
	  "headline": 50, "coverage": 66.7, "low_coverage": true, "dropped_links": 1,
	  "issues": [
	    {"key": "PROJ-1", "closed_at": "2026-03-04T05:06:07Z", "prs": ["acme/web#PR-7"],
	     "ai": true, "percent": 100, "basis": "lines"},
	    {"key": "PROJ-2", "closed_at": "0001-01-01T00:00:00Z", "prs": null,
	     "ai": false, "percent": null, "basis": "none"}
	  ]
	}`, string(b))

	var back Result
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, r, back)
}

// TestPolicy_MarshalsItsEnumsByName: a stored policy says what it selects.
func TestPolicy_MarshalsItsEnumsByName(t *testing.T) {
	p := Policy{
		Dimension: DimensionAuthored, Direction: DirectionPRToIssue,
		AuthorWeight: 0.8, ReviewWeight: 0.2, LinesOnly: true,
		LowCoverageBelow: 60, IncludeBlocked: true, MinTier: TierHigh,
	}

	b, err := json.Marshal(p)
	require.NoError(t, err)

	assert.JSONEq(t, `{
	  "dimension": "authored", "direction": "pr_to_issue",
	  "author_weight": 0.8, "review_weight": 0.2, "lines_only": true,
	  "low_coverage_below": 60, "include_blocked": true, "min_tier": "high"
	}`, string(b))

	var back Policy
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, p, back)
}

// TestPolicy_DefaultsMarshalAsTheirNames: the zero value of each enum is a
// real choice here, so it carries a name rather than reading as unset.
func TestPolicy_DefaultsMarshalAsTheirNames(t *testing.T) {
	b, err := json.Marshal(Policy{})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"dimension":"blended"`)
	assert.Contains(t, string(b), `"direction":"both"`)
	assert.Contains(t, string(b), `"min_tier":"unknown"`)

	var back Policy
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, Policy{}, back)
}

// TestEnums_RejectANameTheyDoNotHave: a typo is an error, not a silently
// weaker filter that would quietly change what the measure counts.
func TestEnums_RejectANameTheyDoNotHave(t *testing.T) {
	err := json.Unmarshal([]byte(`{"min_tier":"urgent"}`), &Policy{})
	require.ErrorIs(t, err, ErrUnknownEnum)
	assert.Contains(t, err.Error(), "urgent")

	require.ErrorIs(t, json.Unmarshal([]byte(`{"dimension":"authored "}`), &Policy{}), ErrUnknownEnum)
	require.ErrorIs(t, json.Unmarshal([]byte(`{"direction":"pr-to-issue"}`), &Policy{}), ErrUnknownEnum)
}

// TestEnums_RoundTripEveryName: every value has a name that reads back as it.
func TestEnums_RoundTripEveryName(t *testing.T) {
	for _, v := range []Direction{DirectionBoth, DirectionIssueToPR, DirectionPRToIssue} {
		var back Direction
		require.NoError(t, back.UnmarshalText([]byte(v.String())))
		assert.Equal(t, v, back)
	}
	for _, v := range []Dimension{DimensionBlended, DimensionAuthored, DimensionReviewed} {
		var back Dimension
		require.NoError(t, back.UnmarshalText([]byte(v.String())))
		assert.Equal(t, v, back)
	}
	for _, v := range []Tier{TierUnknown, TierLow, TierMedium, TierHigh} {
		var back Tier
		require.NoError(t, back.UnmarshalText([]byte(v.String())))
		assert.Equal(t, v, back)
	}
}
