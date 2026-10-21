// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issueattribution

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func score(v float64) *float64 { return &v }

func TestPercent(t *testing.T) {
	scored := func(key string, s float64, lines int64, a, r bool) PR {
		return PR{Key: key, Complexity: score(s), Lines: lines, AIAuthored: a, AIReviewed: r}
	}
	unscored := func(key string, lines int64, a, r bool) PR {
		return PR{Key: key, Lines: lines, AIAuthored: a, AIReviewed: r}
	}
	tests := []struct {
		name  string
		prs   []PR
		dim   Dimension
		pol   Policy
		want  *float64
		basis Basis
	}{
		{name: "no linked pull request", want: nil, basis: BasisNone},
		{name: "scored, no AI", prs: []PR{scored("a", 5, 100, false, false)}, want: score(0), basis: BasisComplexity},
		{name: "scored, authored", prs: []PR{scored("a", 5, 100, true, false)}, want: score(80), basis: BasisComplexity},
		{name: "scored, reviewed", prs: []PR{scored("a", 7, 100, false, true)}, want: score(20), basis: BasisComplexity},
		{name: "scored, both", prs: []PR{scored("a", 3.5, 100, true, true)}, want: score(100), basis: BasisComplexity},
		{name: "three scored, mixed: (0.8x11 + 0.2x9) / 12",
			prs:  []PR{scored("a", 8, 200, true, true), scored("b", 3, 100, true, false), scored("c", 1, 50, false, true)},
			want: score(88.33333333333333), basis: BasisComplexity},
		{name: "scales never mix: an unscored large pull request is left out",
			prs:  []PR{scored("a", 2, 30, true, false), unscored("b", 10000, false, false)},
			want: score(80), basis: BasisComplexity},
		{name: "all scores zero, no AI: 0, not none", prs: []PR{scored("a", 0, 0, false, false)}, want: score(0), basis: BasisComplexityUnweighted},
		{name: "one zero score, authored", prs: []PR{scored("a", 0, 0, true, false)}, want: score(80), basis: BasisComplexityUnweighted},
		{name: "two zero scores, one authored", prs: []PR{scored("a", 0, 0, true, false), scored("b", 0, 0, false, false)}, want: score(40), basis: BasisComplexityUnweighted},
		{name: "lines: (0.8x300 + 0.2x250) / 350",
			prs:  []PR{unscored("a", 200, true, true), unscored("b", 100, true, false), unscored("c", 50, false, true)},
			want: score(82.857142857142854), basis: BasisLines},
		{name: "lines: a large human pull request dominates",
			prs:  []PR{unscored("a", 500, false, true), unscored("b", 50, true, true)},
			want: score(27.272727272727273), basis: BasisLines},
		{name: "lines: zero-line pull request, authored", prs: []PR{unscored("a", 0, true, false)}, want: score(80), basis: BasisLinesUnweighted},
		{name: "authored: authored only is 100", dim: DimensionAuthored, prs: []PR{scored("a", 5, 100, true, false)}, want: score(100), basis: BasisComplexity},
		{name: "authored: reviewed only is 0", dim: DimensionAuthored, prs: []PR{scored("a", 5, 100, false, true)}, want: score(0), basis: BasisComplexity},
		{name: "reviewed: reviewed only is 100", dim: DimensionReviewed, prs: []PR{scored("a", 5, 100, false, true)}, want: score(100), basis: BasisComplexity},
		{name: "reviewed: authored only is 0", dim: DimensionReviewed, prs: []PR{scored("a", 5, 100, true, false)}, want: score(0), basis: BasisComplexity},
		{name: "weights are normalised: 1 and 1 with both flags is 100", pol: Policy{AuthorWeight: 1, ReviewWeight: 1},
			prs: []PR{scored("a", 5, 100, true, true)}, want: score(100), basis: BasisComplexity},
		{name: "weights are normalised: 1 and 1 with one flag is 50", pol: Policy{AuthorWeight: 1, ReviewWeight: 1},
			prs: []PR{scored("a", 5, 100, true, false)}, want: score(50), basis: BasisComplexity},
		{name: "weights are normalised: 0.4 and 0.1, authored only, is 80", pol: Policy{AuthorWeight: 0.4, ReviewWeight: 0.1},
			prs: []PR{scored("a", 5, 100, true, false)}, want: score(80), basis: BasisComplexity},
		{name: "weights are normalised at zero lines", pol: Policy{AuthorWeight: 1, ReviewWeight: 1},
			prs: []PR{unscored("a", 0, true, true)}, want: score(100), basis: BasisLinesUnweighted},
		{name: "weights are normalised at zero score", pol: Policy{AuthorWeight: 1, ReviewWeight: 1},
			prs: []PR{scored("a", 0, 0, true, true)}, want: score(100), basis: BasisComplexityUnweighted},
		{name: "review weight zero: a reviewed-only pull request reads 0", pol: Policy{AuthorWeight: 1},
			prs: []PR{scored("a", 5, 100, false, true)}, want: score(0), basis: BasisComplexity},
		{name: "author weight zero: an authored-only pull request reads 0", pol: Policy{ReviewWeight: 1},
			prs: []PR{scored("a", 5, 100, true, false)}, want: score(0), basis: BasisComplexity},
		{name: "largest scores do not overflow the sum",
			prs: []PR{scored("a", math.MaxFloat64, 0, true, false), scored("b", math.MaxFloat64, 0, false, false)}, want: score(40), basis: BasisComplexity},
		{name: "a small score first, then two MaxFloat64: divided by the largest, not the first",
			prs:  []PR{scored("a", 1, 0, true, false), scored("b", math.MaxFloat64, 0, true, false), scored("c", math.MaxFloat64, 0, false, false)},
			want: score(40), basis: BasisComplexity},
		{name: "a subnormal score, reviewed only, is scaled up, not lost", prs: []PR{scored("a", 5e-324, 0, false, true)},
			want: score(20), basis: BasisComplexity},
		{name: "weights MaxFloat64 and 0, authored", pol: Policy{AuthorWeight: math.MaxFloat64},
			prs: []PR{scored("a", 5, 100, true, false)}, want: score(100), basis: BasisComplexity},
		{name: "weights 0 and MaxFloat64, reviewed", pol: Policy{ReviewWeight: math.MaxFloat64},
			prs: []PR{scored("a", 5, 100, false, true)}, want: score(100), basis: BasisComplexity},
		{name: "LinesOnly weighs by lines even when scored", pol: Policy{LinesOnly: true},
			prs: []PR{scored("a", 2, 30, true, false), scored("b", 9, 10000, false, false)}, want: score(80.0 * 30 / 10030), basis: BasisLines},
		{name: "reviewed: unweighted counts reviewed pull requests", dim: DimensionReviewed,
			prs: []PR{unscored("a", 0, false, true), unscored("b", 0, true, false)}, want: score(50), basis: BasisLinesUnweighted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var links []Link
			for _, p := range tt.prs {
				links = append(links, Link{IssueKey: "I", PRKey: p.Key})
			}
			p := tt.pol
			p.Dimension = tt.dim
			res, err := Compute([]Issue{{Key: "I"}}, links, tt.prs, p)
			require.NoError(t, err)
			ir := res.Issues[0]
			assert.Equal(t, tt.basis, ir.Basis)
			if tt.want == nil {
				assert.Nil(t, ir.Percent)
				return
			}
			require.NotNil(t, ir.Percent)
			assert.InDelta(t, *tt.want, *ir.Percent, 1e-9)
		})
	}
}

func TestHasSignal(t *testing.T) {
	authored, reviewed, none := PR{AIAuthored: true}, PR{AIReviewed: true}, PR{}
	tests := []struct {
		name string
		prs  []PR
		dim  Dimension
		pol  Policy
		want bool
	}{
		{name: "blended, review weight zero: reviewed only is not AI", prs: []PR{reviewed}, pol: Policy{AuthorWeight: 1}, want: false},
		{name: "blended, author weight zero: authored only is not AI", prs: []PR{authored}, pol: Policy{ReviewWeight: 1}, want: false},
		{"authored: one AI pull request among others", []PR{none, authored, none}, DimensionAuthored, Policy{}, true},
		{"authored: reviewed only", []PR{reviewed, reviewed}, DimensionAuthored, Policy{}, false},
		{"reviewed: one reviewed", []PR{none, reviewed}, DimensionReviewed, Policy{}, true},
		{"reviewed: authored only", []PR{authored}, DimensionReviewed, Policy{}, false},
		{"blended: either counts", []PR{reviewed}, DimensionBlended, Policy{}, true},
		{"no linked pull request", nil, DimensionAuthored, Policy{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var links []Link
			for i := range tt.prs {
				tt.prs[i].Key = string(rune('a' + i))
				links = append(links, Link{IssueKey: "I", PRKey: tt.prs[i].Key})
			}
			p := tt.pol
			p.Dimension = tt.dim
			res, err := Compute([]Issue{{Key: "I"}}, links, tt.prs, p)
			require.NoError(t, err)
			assert.Equal(t, tt.want, res.Issues[0].AI)
		})
	}
}

func TestCompute_HeadlineAndCoverage(t *testing.T) {
	issues := []Issue{{Key: "I1"}, {Key: "I2"}, {Key: "I3"}, {Key: "I4"}}
	prs := []PR{{Key: "P1", AIAuthored: true, Lines: 50}}
	res, err := Compute(issues, []Link{{IssueKey: "I1", PRKey: "P1"}}, prs, Policy{})
	require.NoError(t, err)
	assert.Equal(t, 4, res.Total)
	assert.Equal(t, 1, res.Linked)
	assert.Equal(t, 3, res.Unlinked)
	assert.Equal(t, 1, res.AI)
	require.NotNil(t, res.Headline)
	assert.InDelta(t, 100, *res.Headline, 1e-9)
	require.NotNil(t, res.Coverage)
	assert.InDelta(t, 25, *res.Coverage, 1e-9)
	assert.True(t, res.LowCoverage)

	res, err = Compute(nil, nil, nil, Policy{})
	require.NoError(t, err)
	assert.Nil(t, res.Headline, "nothing linked: no headline")
	assert.Nil(t, res.Coverage, "no issues: no coverage")
	assert.False(t, res.LowCoverage)
	assert.Equal(t, 0, res.Total)

	res, err = Compute([]Issue{{Key: "I"}}, nil, nil, Policy{})
	require.NoError(t, err)
	require.NotNil(t, res.Coverage)
	assert.InDelta(t, 0, *res.Coverage, 1e-9)
	assert.True(t, res.LowCoverage)
}

func TestCompute_Direction(t *testing.T) {
	issues := []Issue{{Key: "A"}, {Key: "B"}}
	prs := []PR{{Key: "p1"}, {Key: "p2"}, {Key: "p3"}}
	links := []Link{
		{IssueKey: "A", PRKey: "p1", Direction: DirectionIssueToPR},
		{IssueKey: "A", PRKey: "p1", Direction: DirectionPRToIssue},
		{IssueKey: "A", PRKey: "p2", Direction: DirectionPRToIssue},
		{IssueKey: "B", PRKey: "p3", Direction: DirectionPRToIssue},
	}
	tests := []struct {
		dir  Direction
		want map[string][]string
	}{
		{DirectionBoth, map[string][]string{"A": {"p1", "p2"}, "B": {"p3"}}},
		{DirectionIssueToPR, map[string][]string{"A": {"p1"}, "B": {}}},
		{DirectionPRToIssue, map[string][]string{"A": {"p1", "p2"}, "B": {"p3"}}},
	}
	for _, tt := range tests {
		res, err := Compute(issues, links, prs, Policy{Direction: tt.dir})
		require.NoError(t, err)
		for _, ir := range res.Issues {
			assert.Equal(t, tt.want[ir.Key], ir.PRs, "direction %d issue %s", tt.dir, ir.Key)
		}
	}
}

func TestCompute_InvalidInputIsAnError(t *testing.T) {
	nan := math.NaN()
	tests := []struct {
		name   string
		issues []Issue
		prs    []PR
		p      Policy
	}{
		{"issue with no key", []Issue{{}}, nil, Policy{}},
		{"issue twice", []Issue{{Key: "a"}, {Key: "a"}}, nil, Policy{}},
		{"pull request with no key", nil, []PR{{}}, Policy{}},
		{"pull request twice", nil, []PR{{Key: "p"}, {Key: "p"}}, Policy{}},
		{"negative lines", nil, []PR{{Key: "p", Lines: -1}}, Policy{}},
		{"NaN complexity", nil, []PR{{Key: "p", Complexity: &nan}}, Policy{}},
		{"negative complexity", nil, []PR{{Key: "p", Complexity: score(-1)}}, Policy{}},
		{"negative weight", nil, nil, Policy{AuthorWeight: -1}},
		{"NaN weight", nil, nil, Policy{ReviewWeight: nan}},
		{"Inf weight", nil, nil, Policy{AuthorWeight: math.Inf(1)}},
		{"weights whose sum overflows", nil, nil, Policy{AuthorWeight: 1e308, ReviewWeight: 1e308}},
		{"Inf complexity", nil, []PR{{Key: "p", Complexity: score(math.Inf(1))}}, Policy{}},
		{"negative low-coverage threshold", nil, nil, Policy{LowCoverageBelow: -1}},
		{"NaN low-coverage threshold", nil, nil, Policy{LowCoverageBelow: nan}},
		{"+Inf low-coverage threshold", nil, nil, Policy{LowCoverageBelow: math.Inf(1)}},
		{"-Inf low-coverage threshold", nil, nil, Policy{LowCoverageBelow: math.Inf(-1)}},
		{"-Inf review weight", nil, nil, Policy{ReviewWeight: math.Inf(-1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Compute(tt.issues, nil, tt.prs, tt.p)
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Nil(t, res)
		})
	}
}

// TestCompute_TheFirstRejectionIsReported pins the order of the policy
// checks: the author weight, then the review weight, then their sum, then the
// low-coverage threshold.
func TestCompute_TheFirstRejectionIsReported(t *testing.T) {
	nan := math.NaN()
	for _, tt := range []struct {
		p    Policy
		want string
	}{
		{Policy{AuthorWeight: nan, ReviewWeight: -1}, "issueattribution: invalid input: author weight NaN"},
		{Policy{ReviewWeight: -1, LowCoverageBelow: nan}, "issueattribution: invalid input: review weight -1"},
		{Policy{AuthorWeight: 1e308, ReviewWeight: 1e308, LowCoverageBelow: nan}, "issueattribution: invalid input: author and review weights sum past the largest float64"},
	} {
		res, err := Compute(nil, nil, nil, tt.p)
		require.EqualError(t, err, tt.want)
		assert.Nil(t, res)
	}
}

// TestCompute_ALowCoverageThresholdOver100AlwaysFlags: a threshold over 100
// is accepted, and flags even full coverage.
func TestCompute_ALowCoverageThresholdOver100AlwaysFlags(t *testing.T) {
	res, err := Compute([]Issue{{Key: "I"}}, []Link{{IssueKey: "I", PRKey: "p"}}, []PR{{Key: "p", Lines: 1}}, Policy{LowCoverageBelow: 150})
	require.NoError(t, err)
	require.NotNil(t, res.Coverage)
	assert.InDelta(t, 100, *res.Coverage, 1e-9)
	assert.True(t, res.LowCoverage)
}

func TestMondayOf(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{"Monday", time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC), "2026-05-18"},
		{"Wednesday", time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC), "2026-05-18"},
		{"Sunday ends the week", time.Date(2026, 5, 24, 23, 59, 59, 0, time.UTC), "2026-05-18"},
		{"wall clock, not UTC", time.Date(2026, 5, 11, 0, 0, 0, 0, time.FixedZone("half-hour east", 9*3600+1800)), "2026-05-11"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MondayOf(tt.in))
		})
	}
}

// TestWeekly: two issues closed the same week count
// separately, and the AI count never exceeds the linked count.
func TestWeekly(t *testing.T) {
	closed := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	res, err := Compute(
		[]Issue{{Key: "unlinked", ClosedAt: closed}, {Key: "linked", ClosedAt: closed}, {Key: "open"}},
		[]Link{{IssueKey: "linked", PRKey: "P1"}},
		[]PR{{Key: "P1", AIAuthored: true, AIReviewed: true, Lines: 100}}, Policy{})
	require.NoError(t, err)
	assert.Equal(t, map[string]Week{MondayOf(closed): {Linked: 1, AI: 1}}, Weekly(res))
}

func TestByRepository(t *testing.T) {
	res, err := Compute(
		[]Issue{{Key: "i1"}, {Key: "i2"}, {Key: "i3"}, {Key: "i4"}},
		[]Link{{IssueKey: "i1", PRKey: "acme/api#1"}, {IssueKey: "i2", PRKey: "acme/api#2"}, {IssueKey: "i3", PRKey: "acme/api#3"}, {IssueKey: "i4", PRKey: "acme/web#9"}},
		[]PR{{Key: "acme/api#1", AIAuthored: true}, {Key: "acme/api#2"}, {Key: "acme/api#3", AIAuthored: true}, {Key: "acme/web#9", AIAuthored: true}},
		Policy{})
	require.NoError(t, err)
	got := ByRepository(res, map[string][]string{"acme/api": {"acme/api#1", "acme/api#2", "acme/api#3"}})
	assert.InDelta(t, 200.0/3, got["acme/api"], 0.01)
	assert.NotContains(t, got, "acme/web")
	assert.Nil(t, ByRepository(nil, map[string][]string{"acme/api": {"x"}}))
	assert.Nil(t, ByRepository(res, nil))
	assert.Nil(t, ByRepository(&Result{}, map[string][]string{"acme/api": {"x"}}))
}

// TestCompute_LinkFloor: a blocked link is left out by default and counted
// with IncludeBlocked; MinTier leaves out weaker and ungraded links.
func TestCompute_LinkFloor(t *testing.T) {
	issues := []Issue{{Key: "I1"}, {Key: "I2"}, {Key: "I3"}}
	prs := []PR{{Key: "P1", AIAuthored: true}, {Key: "P2", AIAuthored: true}, {Key: "P3", AIAuthored: true}}
	links := []Link{
		{IssueKey: "I1", PRKey: "P1", Tier: TierLow, Blocked: true},
		{IssueKey: "I2", PRKey: "P2", Tier: TierLow},
		{IssueKey: "I3", PRKey: "P3", Tier: TierHigh},
	}
	tests := []struct {
		name   string
		p      Policy
		linked int
	}{
		{"blocked left out by default", Policy{}, 2},
		{"blocked counted on request", Policy{IncludeBlocked: true}, 3},
		{"a high floor keeps high links", Policy{MinTier: TierHigh}, 1},
		{"a medium floor keeps high links", Policy{MinTier: TierMedium, IncludeBlocked: true}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Compute(issues, links, prs, tt.p)
			require.NoError(t, err)
			assert.Equal(t, tt.linked, res.Linked)
			assert.Equal(t, tt.linked, res.AI)
		})
	}
	res, err := Compute([]Issue{{Key: "I"}}, []Link{{IssueKey: "I", PRKey: "P"}}, []PR{{Key: "P"}}, Policy{MinTier: TierLow})
	require.NoError(t, err)
	assert.Equal(t, 0, res.Linked, "an ungraded link is below any floor")
}

// TestCompute_AMissingPullRequestIsDroppedLimit: a link to a pull request missing from the input is
// counted, once per pair, not dropped silently.
func TestCompute_AMissingPullRequestIsDroppedLimit(t *testing.T) {
	res, err := Compute([]Issue{{Key: "I1"}, {Key: "I2"}},
		[]Link{
			{IssueKey: "I1", PRKey: "gone", Direction: DirectionIssueToPR},
			{IssueKey: "I1", PRKey: "gone", Direction: DirectionPRToIssue},
			{IssueKey: "I2", PRKey: "P"},
			{IssueKey: "out-of-scope", PRKey: "gone"},
		},
		[]PR{{Key: "P"}}, Policy{})
	require.NoError(t, err)
	assert.Equal(t, 1, res.DroppedLinks)
	assert.Equal(t, 1, res.Linked)
}

// TestByRepository_AnIssueCountsInEveryRepositoryItTouchedLimit pins the per
// repository rule: an issue with pull requests in two repositories counts in
// both rows, so the rows do not sum to the overall result.
func TestByRepository_AnIssueCountsInEveryRepositoryItTouchedLimit(t *testing.T) {
	res, err := Compute([]Issue{{Key: "I"}}, []Link{{IssueKey: "I", PRKey: "a#1"}, {IssueKey: "I", PRKey: "b#1"}},
		[]PR{{Key: "a#1", AIAuthored: true}, {Key: "b#1"}}, Policy{})
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"a": 100, "b": 100}, ByRepository(res, map[string][]string{"a": {"a#1"}, "b": {"b#1"}}))
}

// TestByRepository_KeysAreExact: keys are compared exactly, so a repository
// keyed in another case reaches no issue.
func TestByRepository_KeysAreExact(t *testing.T) {
	res, err := Compute([]Issue{{Key: "I"}}, []Link{{IssueKey: "I", PRKey: "acme/api#1"}}, []PR{{Key: "acme/api#1", AIAuthored: true}}, Policy{})
	require.NoError(t, err)
	got := ByRepository(res, map[string][]string{"acme/API": {"acme/API#1"}, "acme/api": {"acme/api#1"}})
	assert.Equal(t, map[string]float64{"acme/api": 100}, got)
}

// TestCompute_AMentionCountsAsDeliveryLimit pins the default floor: a bare or
// "see" mention (low tier, unblocked) counts as delivery unless MinTier says
// otherwise.
func TestCompute_AMentionCountsAsDeliveryLimit(t *testing.T) {
	links := []Link{{IssueKey: "I", PRKey: "P", Tier: TierLow}}
	prs := []PR{{Key: "P", AIAuthored: true}}
	res, err := Compute([]Issue{{Key: "I"}}, links, prs, Policy{})
	require.NoError(t, err)
	assert.Equal(t, 1, res.AI)
	res, err = Compute([]Issue{{Key: "I"}}, links, prs, Policy{MinTier: TierMedium})
	require.NoError(t, err)
	assert.Equal(t, 0, res.Linked)
}

func TestTierString(t *testing.T) {
	for tier, want := range map[Tier]string{TierUnknown: "unknown", TierLow: "low", TierMedium: "medium", TierHigh: "high", Tier(9): "unknown"} {
		assert.Equal(t, want, tier.String())
	}
}

// TestHasSignal_OneAIPullRequestIsEnoughLimit: an issue counts as AI-assisted
// when any of its pull requests is, however small.
func TestHasSignal_OneAIPullRequestIsEnoughLimit(t *testing.T) {
	res, err := Compute([]Issue{{Key: "I"}},
		[]Link{{IssueKey: "I", PRKey: "big"}, {IssueKey: "I", PRKey: "tiny"}},
		[]PR{{Key: "big", Lines: 5000}, {Key: "tiny", Lines: 1, AIAuthored: true}}, Policy{})
	require.NoError(t, err)
	assert.Equal(t, 1, res.AI)
	assert.Less(t, *res.Issues[0].Percent, 1.0)
}

// TestCompute_UnlinkedIssuesAreOutsideTheHeadlineLimit: the headline speaks
// for linked issues only, and LowCoverage flags coverage strictly under the
// threshold.
func TestCompute_UnlinkedIssuesAreOutsideTheHeadlineLimit(t *testing.T) {
	issues := []Issue{{Key: "1"}, {Key: "2"}, {Key: "3"}, {Key: "4"}, {Key: "5"}}
	links := []Link{{IssueKey: "1", PRKey: "p"}, {IssueKey: "2", PRKey: "p"}, {IssueKey: "3", PRKey: "p"}}
	res, err := Compute(issues, links, []PR{{Key: "p", AIAuthored: true}}, Policy{})
	require.NoError(t, err)
	assert.InDelta(t, 100, *res.Headline, 1e-9)
	assert.InDelta(t, 60, *res.Coverage, 1e-9)
	assert.False(t, res.LowCoverage, "exactly 60% is not low")
	res, err = Compute(issues, links[:2], []PR{{Key: "p", AIAuthored: true}}, Policy{})
	require.NoError(t, err)
	assert.True(t, res.LowCoverage)
}
