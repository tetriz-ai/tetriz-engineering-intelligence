# issueattribution

AI attribution answers which pull requests were AI-assisted, but a lead plans and reports in issues, and an issue is often delivered by several pull requests or by none that anyone linked. `issueattribution` says what share of completed issues AI-assisted pull requests delivered, says how many issues it could see a pull request for at all, and gives no share rather than a zero when it sees none.

## Contents

- [What it does](#what-it-does) lists the entry points and what they read.
- [Quick start](#quick-start) measures four issues in a short complete program.
- [Choosing an entry point](#choosing-an-entry-point) maps what you have to the call to make.
- [Usage](#usage) shows each entry point in a complete program.
- [Reference](#reference) gives the exact rules, defaults and the JSON contract.
- [Before you rely on it](#before-you-rely-on-it) lists where the share can mislead.
- [Project](../README.md#project), at the repository root, covers versioning, tests, contributing, security and the license.

## What it does

- **`Compute(issues, links, prs, policy) (*Result, error)`** counts the completed issues, the ones with a linked pull request, and the ones at least one AI-assisted pull request delivered. The headline is the AI-assisted share of linked issues, and coverage is the linked share of all issues, with a low-coverage flag. Each issue also gets an AI percentage weighted by complexity score, or by lines when none is scored, with authored work at 0.8 and reviewed work at 0.2 by default.
- **`ByRepository(result, merged)`** gives the share per repository, over the issues with a merged pull request there.
- **`Weekly(result)`** counts the linked and AI-assisted issues per week of completion.
- **`Policy`** chooses whether authored work, reviewed work or either counts as AI, the link direction, the blend weights, and the floor a link must clear. By default a link whose only evidence is context is left out, but a bare or "see" mention still counts. Set `MinTier` to count only closing and delivering links ([Before you rely on it](#shares-that-read-higher-than-the-work)).

`Compute` keeps no state and does no I/O, and its inputs are plain structs joined on key strings.

Links come from [`issuelinkage`](../issuelinkage/README.md) or your own source, AI signals from [`attribution`](../README.md) and complexity scores from [`prcomplexity`](../prcomplexity/README.md). The [attribute-issues-to-ai](../examples/attribute-issues-to-ai/) example joins all four.

### What it does not do

- It does not find links or decide which pull requests were AI-assisted. Both are inputs.
- It does not choose which issues count as completed in a period. Pass the ones you count.

## Quick start

You need Go 1.25.2 or later, and nothing else: no service, account or credential. Start a module. The `go get` line resolves once v0.1.0 is tagged.

```sh
mkdir try-issueattribution && cd try-issueattribution && go mod init example.com/try
go get github.com/tetriz-ai/tetriz-engineering-intelligence@v0.1.0
```

Save this as `main.go`.

```go
// Command measure-issue-ai-share measures how many completed issues AI-assisted pull requests delivered.
package main

import (
	"fmt"
	"log"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issueattribution"
)

func main() {
	issues := []issueattribution.Issue{{Key: "PROJ-1"}, {Key: "PROJ-2"}, {Key: "PROJ-3"}, {Key: "PROJ-4"}}
	links := []issueattribution.Link{
		{IssueKey: "PROJ-1", PRKey: "acme/web#PR-10"},
		{IssueKey: "PROJ-1", PRKey: "acme/web#PR-11"},
		{IssueKey: "PROJ-2", PRKey: "acme/web#PR-11"},
		{IssueKey: "PROJ-3", PRKey: "acme/web#PR-12"},
	}
	score := func(v float64) *float64 { return &v }
	prs := []issueattribution.PR{
		{Key: "acme/web#PR-10", AIAuthored: true, Complexity: score(6)},
		{Key: "acme/web#PR-11", Complexity: score(2)},
		{Key: "acme/web#PR-12", AIReviewed: true, Lines: 40},
	}

	res, err := issueattribution.Compute(issues, links, prs, issueattribution.Policy{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("AI-assisted: %d of %d linked issues (%.0f%%)\n", res.AI, res.Linked, *res.Headline)
	fmt.Printf("coverage: %d of %d issues linked (%.0f%%), low=%v\n", res.Linked, res.Total, *res.Coverage, res.LowCoverage)
	for _, ir := range res.Issues {
		if ir.Percent == nil {
			fmt.Printf("%s no linked pull request\n", ir.Key)
			continue
		}
		fmt.Printf("%s ai=%v %.0f%% by %s\n", ir.Key, ir.AI, *ir.Percent, ir.Basis)
	}
}
```

Run it.

```sh
go mod tidy && go run .
```

```text
AI-assisted: 2 of 3 linked issues (67%)
coverage: 3 of 4 issues linked (75%), low=false
PROJ-1 ai=true 60% by complexity
PROJ-2 ai=false 0% by complexity
PROJ-3 ai=true 20% by lines
PROJ-4 no linked pull request
```

`PROJ-1` counts as AI-assisted because one of its two pull requests is AI-authored, and its 60% is 0.8 of that pull request's 6 of 8 complexity. `PROJ-3`'s only pull request is AI-reviewed, which weighs 0.2, so it reads 20%. `PROJ-4` has no linked pull request, so it lowers coverage and has no percentage, not a zero.

## Choosing an entry point

| You have | Use | Example |
|---|---|---|
| Completed issues, links and per-pull-request signals | [`Compute`](#quick-start) | [measure-issue-ai-share](../examples/measure-issue-ai-share/) |
| Links of mixed strength | [`Policy`](#leave-out-weak-links) | [Leave out weak links](#leave-out-weak-links) |
| A result to split by repository or week | [`ByRepository`, `Weekly`](#per-repository-and-per-week) | [Per repository and per week](#per-repository-and-per-week) |
| Links from `issuelinkage` | [`Link`](#from-issuelinkage-links) | [attribute-issues-to-ai](../examples/attribute-issues-to-ai/) |

Every entry point compares issue and pull-request keys exactly. Supply them in one normalised form, such as the keys `issuelinkage` gives.

## Usage

### Leave out weak links

When: some links are mentions rather than delivery, and you want to choose which ones count.

```go
package main

import (
	"fmt"
	"log"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issueattribution"
)

func main() {
	issues := []issueattribution.Issue{{Key: "PROJ-1"}, {Key: "PROJ-2"}}
	links := []issueattribution.Link{
		// A mention only under a Background heading: context, not delivery.
		{IssueKey: "PROJ-1", PRKey: "acme/web#PR-10", Tier: issueattribution.TierLow, Blocked: true},
		{IssueKey: "PROJ-2", PRKey: "acme/web#PR-11", Tier: issueattribution.TierLow},
		{IssueKey: "PROJ-2", PRKey: "acme/web#PR-12", Tier: issueattribution.TierHigh},
		{IssueKey: "PROJ-2", PRKey: "acme/web#PR-99", Tier: issueattribution.TierHigh},
	}
	prs := []issueattribution.PR{
		{Key: "acme/web#PR-10", AIAuthored: true, Lines: 10},
		{Key: "acme/web#PR-11", AIAuthored: true, Lines: 10},
		{Key: "acme/web#PR-12", Lines: 10},
	}
	for _, p := range []issueattribution.Policy{
		{},
		{IncludeBlocked: true},
		{MinTier: issueattribution.TierHigh},
	} {
		res, err := issueattribution.Compute(issues, links, prs, p)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("blocked=%v min=%v: linked %d, AI %d, dropped %d\n", p.IncludeBlocked, p.MinTier, res.Linked, res.AI, res.DroppedLinks)
	}
}
```

```text
blocked=false min=unknown: linked 1, AI 1, dropped 1
blocked=true min=unknown: linked 2, AI 2, dropped 1
blocked=false min=high: linked 1, AI 0, dropped 1
```

`PR-99` is not in the input, so its link is counted in `DroppedLinks` rather than lost without a word.

Remember:

- Set `Blocked` for a link whose only evidence is context. [From issuelinkage links](#from-issuelinkage-links) shows which reason that is.
- Once you set `MinTier`, a link with no `Tier` falls below it. The unset `MinTier`, printed as `unknown`, counts every tier.

### Per repository and per week

When: you report the share per repository, or as a weekly trend.

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issueattribution"
)

func main() {
	week := func(day int) time.Time { return time.Date(2026, 6, day, 15, 0, 0, 0, time.UTC) }
	issues := []issueattribution.Issue{
		{Key: "PROJ-1", ClosedAt: week(2)}, {Key: "PROJ-2", ClosedAt: week(3)}, {Key: "PROJ-3", ClosedAt: week(10)},
	}
	links := []issueattribution.Link{
		{IssueKey: "PROJ-1", PRKey: "acme/web#PR-1"},
		{IssueKey: "PROJ-1", PRKey: "acme/api#PR-1"},
		{IssueKey: "PROJ-2", PRKey: "acme/api#PR-2"},
		{IssueKey: "PROJ-3", PRKey: "acme/web#PR-3"},
	}
	prs := []issueattribution.PR{
		{Key: "acme/web#PR-1", AIAuthored: true}, {Key: "acme/api#PR-1"}, {Key: "acme/api#PR-2"}, {Key: "acme/web#PR-3"},
	}
	res, err := issueattribution.Compute(issues, links, prs, issueattribution.Policy{})
	if err != nil {
		log.Fatal(err)
	}
	merged := map[string][]string{
		"acme/web": {"acme/web#PR-1", "acme/web#PR-3"},
		"acme/api": {"acme/api#PR-1", "acme/api#PR-2"},
	}
	byRepo := issueattribution.ByRepository(res, merged)
	fmt.Printf("acme/api %.0f%%, acme/web %.0f%%\n", byRepo["acme/api"], byRepo["acme/web"])
	weeks := issueattribution.Weekly(res)
	for _, monday := range []string{"2026-06-01", "2026-06-08"} {
		fmt.Printf("week of %s: %d linked, %d AI\n", monday, weeks[monday].Linked, weeks[monday].AI)
	}
}
```

```text
acme/api 50%, acme/web 50%
week of 2026-06-01: 2 linked, 1 AI
week of 2026-06-08: 1 linked, 0 AI
```

`PROJ-1` has pull requests in both repositories, so it counts in both rows.

Remember:

- A repository's row covers the issues with a merged pull request there, so the rows do not sum to the overall result.
- A week is keyed by its Monday on each completion time's own clock, not in UTC. An issue with no `ClosedAt` is left out of `Weekly`.

### From issuelinkage links

When: your links come from `issuelinkage`, and you need `issueattribution.Link` values.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issueattribution"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/memory"
)

func main() {
	ctx := context.Background()
	s := memory.New()
	prs := []issuelinkage.PR{
		{Key: "acme/web#PR-1", Vendor: issuelinkage.VendorGitHub, Title: "Fixes PROJ-1"},
		{Key: "acme/web#PR-2", Vendor: issuelinkage.VendorGitHub, Body: "## Background\n\nPROJ-2 came first."},
	}
	issues := []issuelinkage.Issue{
		{Key: "10001", Vendor: issuelinkage.VendorJira, Identifiers: []string{"PROJ-1"}},
		{Key: "10002", Vendor: issuelinkage.VendorJira, Identifiers: []string{"PROJ-2"}},
	}
	if err := s.Put(ctx, prs, issues); err != nil {
		log.Fatal(err)
	}
	if _, err := issuelinkage.Run(ctx, s, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), issuelinkage.Config{}); err != nil {
		log.Fatal(err)
	}
	rows, err := s.Rows(ctx)
	if err != nil {
		log.Fatal(err)
	}

	var links []issueattribution.Link
	for _, r := range rows {
		for _, l := range r.Result.Links {
			dir := issueattribution.DirectionPRToIssue
			if l.From == issuelinkage.SideIssue {
				dir = issueattribution.DirectionIssueToPR
			}
			links = append(links, issueattribution.Link{
				IssueKey: l.Issue, PRKey: l.PR, Direction: dir,
				// The two packages' tiers share their values.
				Tier: issueattribution.Tier(l.Tier),
				// Only a context-only mention is blocked.
				Blocked: slices.Contains(l.Blocking, issuelinkage.BlockedNegativeSection),
			})
		}
	}
	for _, l := range links {
		fmt.Printf("%s -> %s tier=%v blocked=%v\n", l.PRKey, l.IssueKey, l.Tier, l.Blocked)
	}

	res, err := issueattribution.Compute(
		[]issueattribution.Issue{{Key: "10001"}, {Key: "10002"}}, links,
		[]issueattribution.PR{{Key: "acme/web#PR-1", AIAuthored: true}, {Key: "acme/web#PR-2", AIAuthored: true}},
		issueattribution.Policy{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("AI-assisted: %d of %d linked, %d of %d issues linked\n", res.AI, res.Linked, res.Linked, res.Total)
}
```

```text
acme/web#PR-1 -> 10001 tier=high blocked=false
acme/web#PR-2 -> 10002 tier=low blocked=true
AI-assisted: 1 of 1 linked, 1 of 2 issues linked
```

`PR-2` mentions `PROJ-2` only under a Background heading, so its link is blocked and `PROJ-2` counts as unlinked.

Remember:

- `issueattribution.Tier(l.Tier)` is safe because the two packages share tier values, and a test in the [attribute-issues-to-ai](../examples/attribute-issues-to-ai/) example pins that.
- Set `Blocked` only for `issuelinkage.BlockedNegativeSection`. Its other reasons describe links that still delivered work.

The full API is on [pkg.go.dev](https://pkg.go.dev/github.com/tetriz-ai/tetriz-engineering-intelligence/issueattribution). [examples/README.md](../examples/README.md) lists every example.

## Reference

### Compute

- **Linked.** An issue is linked when at least one link passes the policy's filters and names a pull request in the input.
- **AI-assisted.** A linked issue is AI-assisted when a linked pull request carries the policy's signal. Blended reads either flag whose weight is above zero.
- **Headline.** AI-assisted over linked issues, as a percentage, nil when none is linked.
- **Coverage.** Linked over all issues, as a percentage, nil when there are no issues. `LowCoverage` is set below `LowCoverageBelow`, 60 by default, and a threshold over 100 is accepted and flags every result.
- **Dropped links.** A link whose pull request is not in the input is counted once per issue and pull request in `DroppedLinks`.
- **Errors.** An issue or pull request with no key or given twice, negative lines, a negative or non-finite score or weight, weights whose sum overflows, or a negative or non-finite low-coverage threshold is an error, with no result.

| Basis | Issue percentage |
|---|---|
| `complexity` | (author weight x scores of AI-authored PRs + review weight x scores of AI-reviewed PRs) / all scores, over the scored PRs |
| `complexity_unweighted` | the same over counts, when every score is zero |
| `lines` | the same with lines, when no PR is scored |
| `lines_unweighted` | the same over counts, when every size is zero |
| `none` | no linked PR in the input: the percentage is nil |

The weights are 0.8 and 0.2 blended, so an issue that AI-authored pull requests delivered wholly reads 80%, and setting `Dimension` to authored reads it as 100%. Authored uses 1 and 0, reviewed uses 0 and 1, and custom weights are normalised to sum to 1, so a percentage never exceeds 100 and a pair whose sum overflows is an error. Scores and lines never mix in one issue.

### Policy

| Field | Default |
|---|---|
| `Dimension` | blended |
| `Direction` | both. A chosen direction also leaves out links whose own `Direction` is unset |
| `AuthorWeight`, `ReviewWeight` | 0.8 and 0.2, when both are zero |
| `LinesOnly` | false: scores weigh when any is known |
| `LowCoverageBelow` | 60, when zero |
| `IncludeBlocked` | false: blocked links are left out |
| `MinTier` | none: every tier counts |

### ByRepository and Weekly

- **ByRepository** compares the issues' pull-request keys with each repository's merged keys exactly. A repository no issue reaches is absent.
- **Weekly** keys each issue by `MondayOf` its `ClosedAt`. An issue with no `ClosedAt` is left out.

### JSON

Inputs and results carry `json` tags, and every enum reads and writes its own name, so a stored policy says `"min_tier": "high"` and a result's issues say `"basis": "lines"`. A name an enum does not have is an error wrapping `ErrUnknownEnum`; `"unknown"`, which `Tier` uses for an ungraded link, reads back as `TierUnknown`.

## Before you rely on it

Each limit is pinned by the test named beside it, most harmful first.

### Shares that read higher than the work

- **One AI pull request is enough.** An issue counts as AI-assisted when any of its pull requests is, however small that pull request is (`TestHasSignal_OneAIPullRequestIsEnoughLimit`). The issue's percentage shows how much of the work it was.
- **A mention counts as delivery.** A bare or "see" mention is a low-tier link, unblocked, so it counts unless you set `MinTier` (`TestCompute_AMentionCountsAsDeliveryLimit`).
- **Per-repository rows overlap.** An issue with pull requests in two repositories counts in both rows (`TestByRepository_AnIssueCountsInEveryRepositoryItTouchedLimit`).

### Shares that read lower than the work, or speak for fewer issues

- **Missing pull requests.** A link to a pull request you did not pass leaves its issue unlinked. `DroppedLinks` says how many (`TestCompute_AMissingPullRequestIsDroppedLimit`).
- **Unlinked issues are outside the headline.** Low coverage means the headline speaks for few issues, and `LowCoverage` flags it (`TestCompute_UnlinkedIssuesAreOutsideTheHeadlineLimit`).
