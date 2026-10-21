# airoi

`airoi` measures how much faster AI-assisted pull requests are than the work they
replaced, and what that speed-up is worth in hours and dollars. It compares AI PRs
against non-AI PRs, corrects for the fact that the two groups rarely do the same
kind of work, discounts for extra rework, and falls back to an honest "not enough
data" when the comparison won't hold.

It is a pure Go library — no database, no model inside it. You supply the numbers
through four small interfaces, and the engine does the math.

## Contents

- [What it measures](#what-it-measures) — the four things it works out, and the honesty rule.
- [Quick start](#quick-start) — install it and make the one call.
- [A worked example](#a-worked-example) — the demo's numbers, start to finish.
- [Definitions](#definitions) — cycle time, ROI and payback, pinned down.
- [Inputs](#inputs) — the five signals each PR must carry.
- [Producing the inputs](#producing-the-inputs) — turning raw PRs into those five signals.
- [The four interfaces](#the-four-interfaces) — where your data plugs in.
- [Self-hosting](#self-hosting) — what you build to run it on your own PRs.
- [Result model](#result-model) — why dollars are worked out at read time, not stored.
- [Reference](#reference) — every setting, its default, and what it decides.
- [Before you rely on it](#before-you-rely-on-it) — the limits, most important first.
- [docs/airoi/method.md](../docs/airoi/method.md) is the full method.
- [Project](../README.md#project) covers versioning, tests, contributing and the license.

## What it measures

Four things, in order:

- **A corrected speed-up.** Within one kind of work, it compares AI and non-AI
  median cycle time inside each complexity band, then weights the bands by the AI
  group's own mix. Comparing like with like means a shift in *what* AI works on
  can't fake a gain.
- **A quality discount.** If AI work needs more review rounds, the speed-up is
  marked down. The discount can only lower a positive result, never raise it.
- **Recovered hours, then dollars.** The cycle time saved becomes engineer hours —
  a fraction of it, capped per change — spread over the AI group and annualised.
  Dollars are worked out when you read the result, from an FTE rate and your tool
  spend.
- **An honest basis.** Every result is labelled with what it was actually measured
  against: your own non-AI baseline (`FROM_YOUR_DATA`); or, once almost all work is
  AI and that baseline is gone, high-quality AI against low-quality AI
  (`WITHIN_ORG_LEVERAGE`); or `INSUFFICIENT_DATA`. A result only ever moves down
  that list, and dollars are shown only on a basis that is a real comparison.

The basis is the point to remember: **read the ROI off the basis, not off the raw
numbers.** On a window too thin to measure, the raw numbers happily report $0 — and
$0 reads as "AI delivered nothing", which is a different claim from "we couldn't
measure it". Only the basis tells the two apart.

## Quick start

You need Go 1.25.2 or later.

```sh
go get github.com/tetriz-ai/tetriz-engineering-intelligence@v0.1.0
```

Here is the whole call. Let the engine pick the basis, and turn hours into dollars
only when that basis is a real comparison. This program compiles and runs as-is.

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/airoi"
)

// reader stands in for your SourceReader over your own PR store. Here it returns
// one healthy class of work; in real use it runs a query.
type reader struct{}

func (reader) LoadBandStats(context.Context, string, time.Time, time.Time, []string, airoi.BandBounds) ([]airoi.BandStat, error) {
	return []airoi.BandStat{
		{WorkType: "feature", Band: airoi.BandM, Cohort: airoi.CohortBaseline, MedianCycleHrs: 40, MedianReviewRounds: 3, N: 30},
		{WorkType: "feature", Band: airoi.BandM, Cohort: airoi.CohortAI, MedianCycleHrs: 24, MedianReviewRounds: 2, N: 22},
	}, nil
}
func (reader) LoadSubjectCounts(context.Context, string, time.Time, time.Time, []string) (airoi.SubjectCounts, error) {
	return airoi.SubjectCounts{AIChangeCount: 22, TotalChangeCount: 52}, nil
}

func main() {
	ctx := context.Background()
	from, to := time.Now().AddDate(0, 0, -30), time.Now()
	cfg := airoi.DefaultConfig()

	// nil, nil = no leverage rung and no prior snapshot: just AI vs the non-AI baseline.
	res, err := airoi.ComputeSubjectCascade(ctx, reader{}, nil, nil, "org-1", from, to, []string{"pr-1"}, to, cfg)
	if err != nil {
		panic(err)
	}

	// Only derive dollars when the basis is a real comparison.
	if res.Basis != airoi.BasisFromYourData && res.Basis != airoi.BasisWithinOrgLeverage {
		fmt.Println("not enough data — no ROI to show")
		return
	}

	spend := 12000.0
	cal := airoi.Calibration{FteRateUSDPerHr: 75, AnnualToolSpendUSD: &spend}
	d := airoi.DeriveDollars(res.Measurement.HoursRecovered, cal, 30)
	fmt.Printf("annual value $%.0f\n", *d.AnnualValueUsd)
}
```

Two runnable copies live in `examples/`: [`roi-demo`](../examples/roi-demo/) on a
healthy cohort, and [`roi-on-a-thin-window`](../examples/roi-on-a-thin-window/) where
the data is too thin and the result is withheld. Run either with `go run ./examples/<name>`.

There is a shorter call, `ComputeMeasurement`, that skips the basis and just compares
AI against non-AI. Use it only once you already know the basis is a real comparison:
on its own it cannot tell a true zero from an unmeasurable window.

## A worked example

These are the numbers [`examples/roi-demo`](../examples/roi-demo/) prints. The figures
are invented fixtures — the demo returns a fixed cohort — not a reading of any real org.

**Input** — one row per (work type × complexity band × group), plus the window totals:

| work-type / band | group | median cycle | review rounds | N |
|---|---|---|---|---|
| feature / M | non-AI | 40h | 3 | 30 |
| feature / M | AI | 24h | 2 | 22 |
| bugfix / S | non-AI | 12h | 2 | 25 |
| bugfix / S | AI | 9h | 2 | 18 |

Window totals: 40 AI changes out of 98. Calibration: $75/h FTE rate, $12,000/yr tool
spend, 30-day window.

**The math:**

1. **Speed-up per bucket** — AI vs non-AI median in the same bucket: feature 40h→24h =
   16h (16/40 = 0.40); bugfix 12h→9h = 3h (3/12 = 0.25).
2. **Weight by the AI group's mix** (feature 22 of 40, bugfix 18 of 40): 0.55×0.40 +
   0.45×0.25 = **0.333**.
3. **Quality discount** — here AI used fewer review rounds, so there is nothing to mark
   down: the discount is 1.00 and the result stays 0.333.
4. **Recovered hours** — saved time × 0.5 (the effort ratio), capped at 8h per change:
   feature 16×0.5 = **8.0h** — this lands exactly on the 8h cap, so a bigger saving would
   recover no more — × 22 = 176h; bugfix 3×0.5 = 1.5h × 18 = 27h; total **203h**.
5. **Dollars** — annual value = 203 × $75 × 365/30 = **$185,238**; net gain = minus the
   $12,000 spend = **$173,238**; ROI **×15.4**; payback **0.8 months**.

**Output:**

```text
AI adoption share : 40.8%
Net lift          : 0.333  (gross 0.333, tax 1.00)
Hours recovered   : 203.0 (window 30d)
Per-class:
  bugfix   baseline 12.0h  ai 9.0h  matched_n=18  excluded=false
  feature  baseline 40.0h  ai 24.0h  matched_n=22  excluded=false
Dollars (FTE $75/h, $12k tool spend):
  annual value $185238  net gain $173238  ROI ×15.4  payback 0.8 mo
```

`excluded=false` means the class had enough data to count toward the headline. A class
with too few PRs prints `excluded=true` — it still shows as a row, but is left out of
the speed-up and the hours.

## Definitions

- **Cycle time** — the engine measures whatever cycle-time median your reader gives it;
  it does not define the term for you. Pick one definition and use it for both groups:
  open→merge, first-commit→merge and ready-for-review→merge give different numbers, and
  the headline moves with the choice. The "40h vs 24h" above is a median cycle time in
  your chosen definition.
- **Net lift** — the corrected, discounted speed-up. 0.333 means AI work finished 33.3%
  faster than the matched non-AI work.
- **Recovered hours** — cycle time saved × the effort ratio (0.5), capped at 8h per
  change, summed over the AI group. The 0.5 says only part of a shorter calendar cycle
  is recovered engineer time; the rest is waiting in a queue or for review. It moves the
  dollars a lot: in this example 0.25 gives about $93k and 1.0 gives about $210k — not a
  clean quarter and double, because the feature class already sits on the 8h cap at 0.5,
  so raising the ratio only grows the uncapped bugfix class.
- **ROI multiple** — annual value ÷ annual tool spend (here 185,238 ÷ 12,000 = **15.4**).
  It is *not* net gain ÷ spend. It answers "value back per dollar spent", before you take
  the dollar out.
- **Payback months** — spend ÷ (annual value ÷ 12): how many months of value repay a
  year of spend (here 0.8).

## Inputs

Each PR carries five signals, passed in as **data** through the interfaces — never as a
code dependency. Three decide which bucket a PR falls in; two are what the engine
measures:

| signal | kind | what it does | where it comes from |
|---|---|---|---|
| **work-type** (`"feature"`, `"bugfix"`, …) | grouping | compares AI vs non-AI within one kind of work | a PR classifier, e.g. the sibling [`prworktype`](../prworktype/README.md) |
| **complexity band** (XS–L) | grouping | compares within the same complexity, so a mix shift can't fake a gain | a complexity scorer, e.g. the sibling [`prcomplexity`](../prcomplexity/README.md) |
| **AI attribution** | grouping | splits the AI group from the non-AI group | your own AI-key set, e.g. the sibling [attribution](../examples/match-one-session/) engine |
| **cycle time** | measured | the AI-vs-non-AI median gap *is* the speed-up | your PR store, as a median |
| **review rounds** | measured | drives the quality discount | your PR store, as a median |

If you size an integration off the three grouping signals alone, the other two will
surprise you when the query won't write itself. Cycle time and review rounds are
first-class inputs, not afterthoughts.

The engine never imports the classifier. The work-type arrives as a plain label on each
`BandStat`, so `airoi` has no code dependency on `prworktype` — they meet only at the
data layer. Any source of work-type labels works; the sibling classifier is just the
obvious one.

## Producing the inputs

The engine assumes the five signals already exist on your PRs. Three have an
open-source engine in this repository; the other two you read from the pull-request
fields that GitHub, GitLab and Bitbucket all expose. Pick one definition for each, and
use the same one for both groups — the engine only needs a median to mean the same
thing on both sides.

- **work-type** — run [`prworktype`](../prworktype/README.md) on the PR, or apply your
  own labels. Either way you get the slug (`"feature"`, `"bugfix"`, …) the engine groups by.
- **complexity band** — score the PR's diff with [`prcomplexity`](../prcomplexity/README.md)
  and bin the score with `BandBounds`, or skip the scorer and bin by the PR's added lines
  of code using the same `BandBounds` cutoffs.
- **AI attribution** — decide which PRs are AI. The [attribution](../examples/match-one-session/)
  engine works it out from coding-session evidence; a plainer rule — an AI commit trailer,
  a label, an author list — works just as well. The result is the set of PR keys you pass
  as `aiPRKeys`.
- **cycle time** — how many hours a PR took. Pick one span and keep it: `merged_at −
  created_at` (opened to merged), `merged_at − first_commit` (first commit to merged), or
  `merged_at − ready_for_review` (marked ready to merged). All three are plain timestamps
  on the pull request.
- **review rounds** — how many times the PR went back for changes. A simple, portable
  count is the number of reviews that requested changes (GitHub's `CHANGES_REQUESTED`
  reviews; the equivalent "changes requested" event on GitLab and Bitbucket). Any
  consistent definition is fine.

Then group these per (work-type, band, AI-or-not) and take the medians — one `BandStat`
row per group. Nothing here needs a private service: it is this repo's own packages plus
the public pull-request API of whichever host you use.

## The four interfaces

The engine is pure; everything that touches data sits behind these:

- **`SourceReader`** — per-(work-type, band, group) medians and counts. Your query.
- **`AICohortProvider`** — which PRs are AI. Optional; you can pass the keys directly.
- **`QualityScoreReader`** — mean quality score per AI PR. Drives the leverage rung.
- **`SnapshotStore`** — stores results and reads the previous one (for the one-way rule).

Calibration — the FTE rate and tool spend — is a plain struct you pass in. No pay or
headcount data ever enters the engine.

## Self-hosting

The one piece you build is the data layer. To run it on your own PRs:

1. **Write a `SourceReader`** over your store — a database, CSVs, anything. Its two
   methods return the `BandStat` rows and window counts from the worked example above: in
   practice, one query that takes the median cycle time and review rounds, grouped by
   work-type, complexity band and group. [`examples/roi-demo`](../examples/roi-demo/) is a
   complete in-memory one to copy.
2. **Produce the five per-PR signals** the query needs — the three it groups by and the
   two it measures. [Producing the inputs](#producing-the-inputs) says where each comes from.
3. **Supply a `Calibration`** — your blended FTE rate and annual tool spend.
4. **Call `ComputeSubjectCascade`**, then look at `res.Basis`: on a real basis
   (`FROM_YOUR_DATA` or `WITHIN_ORG_LEVERAGE`) call `DeriveDollars`; on
   `INSUFFICIENT_DATA` show no dollars.

You own which PRs, labelled how, over which store. The engine owns the math.

## Result model

A `Measurement` holds the measured numbers only — the lift, the recovered hours, the
per-class rows, the work-mix comparison. It **carries no basis.** The same measuring step
produces the numbers on either real basis (the leverage rung just feeds it different
rows), so the label that says which basis they came from lives on the `CascadeResult`
that `ComputeSubjectCascade` returns, not on the `Measurement`. Reaching for a basis on
the `Measurement` is a compile error.

No dollars are stored. `DeriveDollars` and `DeriveClassContributions` work them out when
you read the result. When a figure can't be shown — no spend entered, or a basis with no
real comparison — it is withheld with a stated reason, never shown as $0 or an error.

## Reference

`DefaultConfig()` returns the v1 settings. All six `Config` fields, plus one floor that
lives outside `Config`:

| Setting | Default | What it decides |
|---|---|---|
| `MinBaselineN` | 15 | drop a class from the headline below this many baseline PRs |
| `MinMatchedN` | 10 | drop a class from the headline below this many matched AI PRs |
| `BaselineCollapseAIShare` | 0.75 | AI share at or above this means the baseline has collapsed, so use the leverage rung (0 turns it off) |
| `EffortRatio` | 0.5 | share of the cycle-time saving counted as recovered effort (scales the dollars) |
| `EffortCapHrs` | 8.0 | cap on one change's recovered effort, both directions |
| `BandBounds` | score ≤ 2 / ≤ 4 / ≤ 7; LOC < 50 / < 200 / < 600 | the complexity-band cutoffs — and the lines-of-code fallback — your reader bins by |
| `minBaselineMedianHrs` (not in `Config`) | 0.1 (6 min) | drop a class whose baseline median is under 6 minutes — a stability floor, separate from the min-N checks |

Two are easy to get wrong without noticing:

- **`BandBounds`** is the single source of truth for binning, and you inject it into your
  own reader. Guess different cutoffs and you are comparing different things.
- **`minBaselineMedianHrs`** fires in real data: a class with a 32-second baseline median
  is dropped by this floor, not by the min-N checks, and nothing on the row tells you
  which one dropped it. [docs/airoi/method.md](../docs/airoi/method.md) has the detail.

## Before you rely on it

Most important first:

- **Read the ROI off the basis, not off a bare `Measurement`.** On a thin or collapsed
  window the short call reports annual value $0 and net gain minus the tool spend — which
  reads as "AI delivered nothing". Only `ComputeSubjectCascade`'s `INSUFFICIENT_DATA`
  basis says you simply couldn't measure.
  [examples/roi-on-a-thin-window](../examples/roi-on-a-thin-window/) shows both side by side.
- **Good numbers in, or confident nonsense out.** The engine corrects for work mix and
  quality, but it trusts the medians and counts you feed it. A skewed AI/non-AI split or a
  wrong work-type label skews the headline, and nothing catches that for you.
- **Match the thresholds to your volume.** Below `MinBaselineN` / `MinMatchedN` a class
  drops out; wrong `BandBounds` compares unlike work. Both are in [Reference](#reference).
- **Stand behind your calibration.** The FTE rate, the tool spend and the 0.5 effort ratio
  are yours, and the effort ratio scales the dollars directly — state it when you present a
  number.
