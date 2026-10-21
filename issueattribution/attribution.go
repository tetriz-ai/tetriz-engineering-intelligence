// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package issueattribution measures how much delivered tracker work was
// AI-assisted: the share of completed issues that at least one AI-assisted pull
// request delivered, how many issues have a linked pull request at all, and a
// per-issue AI percentage weighted by each pull request's size.
//
// It reads plain inputs joined on issue and pull-request key strings: the
// completed issues, the issue to pull request links (such as issuelinkage's),
// and per-pull-request AI signals and sizes (such as attribution's and
// prcomplexity's). It does no I/O.
package issueattribution

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"time"
)

// Issue is one completed issue.
type Issue struct {
	// Key identifies the issue; links name it.
	Key string `json:"key"`
	// ClosedAt is when the issue was completed. Weekly reads it; zero leaves
	// the issue out of the weekly series.
	ClosedAt time.Time `json:"closed_at"`
}

// Direction is which side's evidence a link came from.
type Direction int

// The directions. DirectionBoth, the zero value, means either.
const (
	DirectionBoth Direction = iota
	// DirectionIssueToPR: the issue's own record names the pull request,
	// such as a vendor's closed-by list.
	DirectionIssueToPR
	// DirectionPRToIssue: the pull request names the issue, such as
	// "Fixes PROJ-1" in its body.
	DirectionPRToIssue
)

// Tier is a link's strength, as the linkage engine grades it. TierUnknown,
// the zero value, is a link with no grade.
type Tier int

// The tiers. They share issuelinkage.Tier's values, so
// issueattribution.Tier(link.Tier) converts one.
const (
	TierUnknown Tier = iota
	TierLow
	TierMedium
	TierHigh
)

// String returns "low", "medium", "high" or "unknown".
func (t Tier) String() string {
	switch t {
	case TierLow:
		return "low"
	case TierMedium:
		return "medium"
	case TierHigh:
		return "high"
	}
	return "unknown"
}

// Link connects an issue to a pull request.
type Link struct {
	IssueKey  string    `json:"issue_key"`
	PRKey     string    `json:"pr_key"`
	Direction Direction `json:"direction"`
	// Tier is the link's strength; Policy.MinTier can require one.
	Tier Tier `json:"tier"`
	// Blocked marks a link whose only evidence is context, such as
	// issuelinkage's negative_section reason (a mention under a Background
	// heading). Blocked links are left out unless Policy.IncludeBlocked.
	// issuelinkage's other reasons, single_signal_floor and
	// non_elected_closer, describe links that still delivered work.
	Blocked bool `json:"blocked"`
}

// PR is what the measure reads about one pull request.
type PR struct {
	Key string `json:"key"`
	// AIAuthored and AIReviewed are the pull request's AI signals.
	AIAuthored bool `json:"ai_authored"`
	AIReviewed bool `json:"ai_reviewed"`
	// Complexity is the pull request's complexity score, nil when it has
	// none. When any linked pull request of an issue has a score, the issue's
	// percentage weighs its scored pull requests by score.
	Complexity *float64 `json:"complexity"`
	// Lines is the pull request's size in lines, the weight when none of an
	// issue's pull requests has a score.
	Lines int64 `json:"lines"`
}

// Dimension selects which AI signal counts.
type Dimension int

// The dimensions. DimensionBlended, the zero value, counts either signal and
// blends them by the policy's weights.
const (
	DimensionBlended Dimension = iota
	DimensionAuthored
	DimensionReviewed
)

// Basis names what weighted an issue's percentage.
type Basis string

// The bases.
const (
	// BasisNone: no linked pull request is known; the percentage is nil.
	BasisNone Basis = "none"
	// BasisComplexity: weighted by complexity score over the scored pull
	// requests.
	BasisComplexity Basis = "complexity"
	// BasisComplexityUnweighted: every score was zero, so each scored pull
	// request counts once.
	BasisComplexityUnweighted Basis = "complexity_unweighted"
	// BasisLines: no pull request was scored, so weighted by lines.
	BasisLines Basis = "lines"
	// BasisLinesUnweighted: every size was zero, so each pull request counts
	// once.
	BasisLinesUnweighted Basis = "lines_unweighted"
)

// Policy configures the measure. The zero value is the default.
type Policy struct {
	Dimension Dimension `json:"dimension"`
	// Direction keeps only links of one direction; DirectionBoth keeps all.
	// A link whose own Direction is DirectionBoth (unset) is left out when a
	// direction is chosen.
	Direction Direction `json:"direction"`
	// AuthorWeight and ReviewWeight blend the two signals in a blended
	// percentage, normalised to sum to 1, so only their ratio counts. Both
	// zero takes 0.8 and 0.2; a pair whose sum overflows is an error.
	AuthorWeight float64 `json:"author_weight"`
	ReviewWeight float64 `json:"review_weight"`
	// LinesOnly weighs by lines even when scores are known.
	LinesOnly bool `json:"lines_only"`
	// LowCoverageBelow is the coverage percentage under which the result is
	// flagged low. Zero takes 60. A value over 100 is accepted and flags
	// every result, full coverage included; a negative or non-finite one is
	// an error.
	LowCoverageBelow float64 `json:"low_coverage_below"`
	// IncludeBlocked counts blocked links too. By default they are left out.
	IncludeBlocked bool `json:"include_blocked"`
	// MinTier leaves out links graded below it; TierUnknown, the default,
	// keeps every tier, ungraded links included.
	MinTier Tier `json:"min_tier"`
}

// Defaults.
const (
	DefaultAuthorWeight     = 0.8
	DefaultReviewWeight     = 0.2
	DefaultLowCoverageBelow = 60.0
)

// IssueResult is one issue's measure.
type IssueResult struct {
	Key      string    `json:"key"`
	ClosedAt time.Time `json:"closed_at"`
	// PRs are the linked pull requests the measure knows, sorted.
	PRs []string `json:"prs"`
	// AI is whether at least one of them carries the dimension's signal.
	AI bool `json:"ai"`
	// Percent is the size-weighted AI share of the issue's pull requests,
	// nil when none is known.
	Percent *float64 `json:"percent"`
	Basis   Basis    `json:"basis"`
}

// Result is the measure over a set of completed issues.
type Result struct {
	// Total is the number of completed issues; Linked of those with at least
	// one known linked pull request; Unlinked the rest; AI of the linked ones
	// that are AI-assisted.
	Total    int `json:"total"`
	Linked   int `json:"linked"`
	Unlinked int `json:"unlinked"`
	AI       int `json:"ai"`
	// Headline is AI / Linked as a percentage, nil when nothing is linked.
	Headline *float64 `json:"headline"`
	// Coverage is Linked / Total as a percentage, nil when there are no
	// issues.
	Coverage *float64 `json:"coverage"`
	// LowCoverage is set when Coverage is under the policy's threshold; never
	// when Coverage is nil.
	LowCoverage bool `json:"low_coverage"`
	// DroppedLinks counts the distinct issue and pull-request pairs a kept
	// link named whose pull request is not in the input, so their issues may
	// show as unlinked.
	DroppedLinks int `json:"dropped_links"`
	// Issues has one entry per completed issue, sorted by key.
	Issues []IssueResult `json:"issues"`
}

// ErrInvalidInput wraps every rejection of an input.
var ErrInvalidInput = errors.New("issueattribution: invalid input")

// Compute measures issues. It returns an error wrapping ErrInvalidInput, and
// no result, when:
//
//   - an issue or pull request has no key or appears twice;
//   - a pull request has negative lines, or a negative or non-finite
//     complexity;
//   - the policy has a negative or non-finite author or review weight, a
//     pair of weights whose sum overflows, or a negative or non-finite
//     low-coverage threshold.
//
// The policy is checked in that order (author weight, review weight, their
// sum, threshold), and the first rejection is reported.
//
// A link is counted when it passes the policy's direction, blocked and tier
// filters and both its issue and its pull request are in the input. A link
// to an issue not in issues is ignored (the issue is not in scope); one to a
// pull request not in prs is counted in DroppedLinks.
func Compute(issues []Issue, links []Link, prs []PR, p Policy) (*Result, error) {
	p, err := normalize(p)
	if err != nil {
		return nil, err
	}
	byIssue := make(map[string]Issue, len(issues))
	for i, is := range issues {
		if is.Key == "" {
			return nil, fmt.Errorf("%w: issue %d has no key", ErrInvalidInput, i)
		}
		if _, dup := byIssue[is.Key]; dup {
			return nil, fmt.Errorf("%w: issue %q appears twice", ErrInvalidInput, is.Key)
		}
		byIssue[is.Key] = is
	}
	byPR := make(map[string]PR, len(prs))
	for i, pr := range prs {
		if pr.Key == "" {
			return nil, fmt.Errorf("%w: pull request %d has no key", ErrInvalidInput, i)
		}
		if _, dup := byPR[pr.Key]; dup {
			return nil, fmt.Errorf("%w: pull request %q appears twice", ErrInvalidInput, pr.Key)
		}
		if pr.Lines < 0 {
			return nil, fmt.Errorf("%w: pull request %q has negative lines", ErrInvalidInput, pr.Key)
		}
		if pr.Complexity != nil && (math.IsNaN(*pr.Complexity) || math.IsInf(*pr.Complexity, 0) || *pr.Complexity < 0) {
			return nil, fmt.Errorf("%w: pull request %q has complexity %v", ErrInvalidInput, pr.Key, *pr.Complexity)
		}
		byPR[pr.Key] = pr
	}
	linked := map[string]map[string]struct{}{}
	dropped := map[[2]string]struct{}{}
	for _, l := range links {
		if p.Direction != DirectionBoth && l.Direction != p.Direction {
			continue
		}
		if l.Blocked && !p.IncludeBlocked {
			continue
		}
		if p.MinTier != TierUnknown && l.Tier < p.MinTier {
			continue
		}
		if _, ok := byIssue[l.IssueKey]; !ok {
			continue
		}
		if _, ok := byPR[l.PRKey]; !ok {
			dropped[[2]string{l.IssueKey, l.PRKey}] = struct{}{}
			continue
		}
		if linked[l.IssueKey] == nil {
			linked[l.IssueKey] = map[string]struct{}{}
		}
		linked[l.IssueKey][l.PRKey] = struct{}{}
	}

	res := &Result{Total: len(issues), DroppedLinks: len(dropped)}
	for _, is := range issues {
		keys := make([]string, 0, len(linked[is.Key]))
		for k := range linked[is.Key] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ir := IssueResult{Key: is.Key, ClosedAt: is.ClosedAt, PRs: keys}
		if len(keys) > 0 {
			res.Linked++
		} else {
			res.Unlinked++
		}
		ir.Percent, ir.Basis = percent(keys, byPR, p)
		ir.AI = hasSignal(keys, byPR, p)
		if ir.AI {
			res.AI++
		}
		res.Issues = append(res.Issues, ir)
	}
	sort.Slice(res.Issues, func(i, j int) bool { return res.Issues[i].Key < res.Issues[j].Key })
	if res.Linked > 0 {
		h := float64(res.AI) / float64(res.Linked) * 100
		res.Headline = &h
	}
	if res.Total > 0 {
		c := float64(res.Linked) / float64(res.Total) * 100
		res.Coverage = &c
		res.LowCoverage = c < p.LowCoverageBelow
	}
	return res, nil
}

func normalize(p Policy) (Policy, error) {
	for _, w := range []struct {
		name string
		v    float64
	}{{"author", p.AuthorWeight}, {"review", p.ReviewWeight}} {
		if math.IsNaN(w.v) || math.IsInf(w.v, 0) || w.v < 0 {
			return p, fmt.Errorf("%w: %s weight %v", ErrInvalidInput, w.name, w.v)
		}
	}
	if math.IsInf(p.AuthorWeight+p.ReviewWeight, 0) {
		return p, fmt.Errorf("%w: author and review weights sum past the largest float64", ErrInvalidInput)
	}
	if c := p.LowCoverageBelow; math.IsNaN(c) || math.IsInf(c, 0) || c < 0 {
		return p, fmt.Errorf("%w: low-coverage threshold %v", ErrInvalidInput, c)
	}
	if p.AuthorWeight == 0 && p.ReviewWeight == 0 {
		p.AuthorWeight, p.ReviewWeight = DefaultAuthorWeight, DefaultReviewWeight
	}
	if p.LowCoverageBelow == 0 {
		p.LowCoverageBelow = DefaultLowCoverageBelow
	}
	return p, nil
}

// weights returns the numerator multipliers for the policy's dimension.
func weights(p Policy) (author, review float64) {
	switch p.Dimension {
	case DimensionAuthored:
		return 1, 0
	case DimensionReviewed:
		return 0, 1
	}
	return p.AuthorWeight, p.ReviewWeight
}

// hasSignal reports whether any linked pull request carries the dimension's
// signal: authored, reviewed, or, blended, either one with a positive weight.
func hasSignal(keys []string, byPR map[string]PR, p Policy) bool {
	aw, rw := weights(p)
	for _, k := range keys {
		pr := byPR[k]
		if (aw > 0 && pr.AIAuthored) || (rw > 0 && pr.AIReviewed) {
			return true
		}
	}
	return false
}

// accumulator sums one basis's weights.
type accumulator struct {
	total, authored, reviewed           float64
	count, authoredCount, reviewedCount int
}

func (a *accumulator) add(w float64, authored, reviewed bool) {
	a.count++
	a.total += w
	if authored {
		a.authored += w
		a.authoredCount++
	}
	if reviewed {
		a.reviewed += w
		a.reviewedCount++
	}
}

// percent is (author weight x authored + review weight x reviewed) / total,
// or the same over counts when the total is zero.
//
// The weights are normalised to sum to 1, so a pull request that is both
// AI-authored and AI-reviewed reads 100 whatever the weights; the defaults
// already sum to 1.
func (a accumulator) percent(aw, rw float64) (float64, bool) {
	sum := aw + rw
	aw, rw = aw/sum, rw/sum
	if a.total == 0 {
		return (aw*float64(a.authoredCount) + rw*float64(a.reviewedCount)) / float64(a.count) * 100, true
	}
	return (aw*a.authored + rw*a.reviewed) / a.total * 100, false
}

// percent computes one issue's AI percentage: by complexity over the scored
// pull requests when any is scored, else by lines over all of them; the two
// scales never mix.
func percent(keys []string, byPR map[string]PR, p Policy) (*float64, Basis) {
	// Scores are summed divided by the largest one, so finite scores can
	// never overflow the sum; the ratio is the same.
	largest := 0.0
	for _, k := range keys {
		if c := byPR[k].Complexity; c != nil && *c > largest {
			largest = *c
		}
	}
	var score, lines accumulator
	for _, k := range keys {
		pr := byPR[k]
		lines.add(float64(pr.Lines), pr.AIAuthored, pr.AIReviewed)
		if pr.Complexity != nil && !p.LinesOnly {
			w := *pr.Complexity
			if largest > 0 {
				w /= largest
			}
			score.add(w, pr.AIAuthored, pr.AIReviewed)
		}
	}
	if lines.count == 0 {
		return nil, BasisNone
	}
	aw, rw := weights(p)
	if score.count > 0 {
		v, unweighted := score.percent(aw, rw)
		if unweighted {
			return &v, BasisComplexityUnweighted
		}
		return &v, BasisComplexity
	}
	v, unweighted := lines.percent(aw, rw)
	if unweighted {
		return &v, BasisLinesUnweighted
	}
	return &v, BasisLines
}

// ByRepository returns, per repository, the AI share among the issues with at
// least one linked pull request merged there: AI issues over linked issues,
// as a percentage. merged maps a repository to its merged pull-request keys,
// compared exactly with the links' keys, so supply both in one normalised
// form. An issue with pull requests in two repositories counts in both, so
// the rows do not sum to the overall result. A repository no issue reaches is
// absent.
func ByRepository(r *Result, merged map[string][]string) map[string]float64 {
	if r == nil || len(r.Issues) == 0 || len(merged) == 0 {
		return nil
	}
	sets := make(map[string]map[string]struct{}, len(merged))
	for repo, keys := range merged {
		s := make(map[string]struct{}, len(keys))
		for _, k := range keys {
			s[k] = struct{}{}
		}
		sets[repo] = s
	}
	type tally struct{ ai, linked int }
	tallies := map[string]*tally{}
	for _, ir := range r.Issues {
		repos := map[string]struct{}{}
		for _, k := range ir.PRs {
			for repo, s := range sets {
				if _, ok := s[k]; ok {
					repos[repo] = struct{}{}
				}
			}
		}
		for repo := range repos {
			t := tallies[repo]
			if t == nil {
				t = &tally{}
				tallies[repo] = t
			}
			t.linked++
			if ir.AI {
				t.ai++
			}
		}
	}
	out := make(map[string]float64, len(tallies))
	for repo, t := range tallies {
		out[repo] = float64(t.ai) / float64(t.linked) * 100
	}
	return out
}

// Week is one week's counts.
type Week struct {
	// Linked is the issues with at least one linked pull request; AI is
	// those that are AI-assisted.
	Linked int `json:"linked"`
	AI     int `json:"ai"`
}

// Weekly buckets the issues by the Monday of their close week, as
// "2006-01-02", in each ClosedAt's own time zone. An issue with no ClosedAt
// is left out.
func Weekly(r *Result) map[string]Week {
	out := map[string]Week{}
	if r == nil {
		return out
	}
	for _, ir := range r.Issues {
		if ir.ClosedAt.IsZero() {
			continue
		}
		k := MondayOf(ir.ClosedAt)
		w := out[k]
		if len(ir.PRs) > 0 {
			w.Linked++
		}
		if ir.AI {
			w.AI++
		}
		out[k] = w
	}
	return out
}

// MondayOf returns the Monday of t's ISO week as "2006-01-02", on t's own
// wall clock, not UTC.
func MondayOf(t time.Time) string {
	wd := int(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	m := time.Date(t.Year(), t.Month(), t.Day()-(wd-1), 0, 0, 0, 0, t.Location())
	return m.Format("2006-01-02")
}
