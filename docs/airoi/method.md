# AI ROI: the method

This page explains how `airoi` turns pull-request cohort aggregates into an AI ROI measurement,
and what it leaves to the program that calls it. Every number here is the code's, and the
package tests pin them.

## The idea

A team wants one defensible number: how much faster is AI-assisted work than the work it
replaced, and what is that worth. The naive answer — blend every AI PR's cycle time against
every non-AI PR's — is biased, because the two cohorts rarely do the same mix of work. If AI is
used mostly on small changes, a blended average flatters it for free.

So the comparison is held within work type and within complexity band, and only then
re-weighted by the AI cohort's own band mix. A mix shift can no longer fake a win: a class that
AI did more of contributes more weight, but each class is still AI-vs-baseline at the same
complexity.

## Inputs

Each PR the engine measures carries five signals, supplied as data through `SourceReader`, never
as a code dependency:

| Signal | Role |
|---|---|
| work type (`"feature"`, `"bugfix"`, …) | the grouping key the comparison runs *within* |
| complexity band (XS–L) | the second grouping key, so AI meets baseline at the same complexity |
| AI attribution (which PRs are AI) | splits the AI cohort from the baseline |
| cycle time | the measured quantity — the AI-vs-baseline delta is the lift |
| review rounds | the quality signal the tax is derived from |

The first three decide which cell a change falls in; the last two are what each cell measures.
The reader returns one `BandStat` per non-empty (work type, band, cohort) cell — the cohort's
median cycle time, its median review rounds, and the change count `N` — plus the window's
subject counts.

### Bands are the caller's, with the engine's cutoffs

`BandBounds` carries the complexity-band cutoffs the reader bins by: score ≤ 2 is XS, ≤ 4 is S,
≤ 7 is M, else L, with a code-only lines-of-code fallback of < 50 / < 200 / < 600. They are the
single source of truth, injected into the reader rather than hard-coded there, so the baseline
and the AI cohort are always binned the same way. Get them wrong and every comparison is between
different things.

## The measurement

`Measure` turns one subject's frame into a `Measurement` (`ComputeMeasurement` is the convenience
that reads the frame first). It holds the measured quantities only — no dollars, and no basis.

### Class correction

For each work type, over the *matched* bands (where both cohorts have changes):

- The baseline and AI cycle times are the band medians, each weighted by the AI cohort's change
  count in that band. An unmatched band, and `BandUnknown`, contribute no weight.
- `baseline_n` and `matched_n` are the counts over those matched bands — the compared
  population. `ai_change_count` is the full AI cohort over every band, the volume the headline
  credits the measured per-change saving to.
- A class below the min-n floor is kept as a row but dropped from the headline (`MinNExcluded`).
- A class whose baseline median sits under the numerical-stability floor (6 minutes, near-instant
  bot auto-merges) is dropped from the headline too — `delta / baseline` is unbounded as the
  baseline approaches zero. This is a separate exclusion from min-n, and nothing on the row
  distinguishes the two.

The gross lift is each included class's `delta / baseline_median`, weighted by its share of the
AI cohort. A slower class (negative delta) legitimately pulls the figure down.

### The quality tax

A deflator (≤ 1.0) from review rounds: `(baseline_rounds + 1) / (ai_rounds + 1)`, aggregated
over the matched bands, AI-count weighted, capped at 1.0. More AI rework discounts the lift; it
can never inflate it. Below the min-n floor the tax is the neutral 1.0. The tax applies to a
positive lift and a positive hours figure on each one's own sign, so a negative result is never
flattered toward zero.

### Recovered hours

Per included class, the per-change effort is `delta × EffortRatio`, symmetrically capped at
`EffortCapHrs`, credited to the class's full AI change count. `EffortRatio` (0.5) encodes that
only part of a calendar cycle-time reduction is recovered engineer effort — the rest is queueing
and review wait. It scales a class's effort linearly up to the cap: in the worked example 0.25 →
~$93k and 1.0 → ~$210k annual value (not a clean quarter-and-double — the feature class is already
pinned at the 8h cap at the default 0.5, so raising the ratio grows only the uncapped bugfix class).

## The basis cascade

`Measure` does not decide whether its comparison is trustworthy. `ComputeSubjectCascade` does, and
it is the only call that produces a basis.

Rungs, strongest to weakest:

| Basis | Comparison | Numeric |
|---|---|---|
| `FROM_YOUR_DATA` | AI vs the org's own non-AI baseline | yes |
| `WITHIN_ORG_LEVERAGE` | high-quality AI vs low-quality AI, when the non-AI baseline has collapsed | yes |
| `INSUFFICIENT_DATA` | neither rung is measurable this window | no |

`WITHIN_ORG_LEVERAGE` is preferred when the AI share clears `BaselineCollapseAIShare` (0.75) and
its frame is usable; otherwise `FROM_YOUR_DATA` when its frame is usable; otherwise
`INSUFFICIENT_DATA`. A frame is usable when at least one class clears the min-n floor and sits
above the baseline-median floor.

The same `Measure` and the same `Measurement` type serve both numeric rungs — the leverage rung
just feeds a different frame (high-quality AI as the "AI" cohort, low-quality AI as the
"baseline"). That is why the type carries no basis: only the cascade knows which rung produced
it. The basis rides out on `CascadeResult`, and in a production deployment on the snapshot and the
read response — never on the `Measurement`.

### The one-way rule

Among numeric rungs a subject only ever descends: a collapsed cohort does not credibly return
within a window, so the basis is clamped to at least the prior snapshot's. A non-numeric prior
(`COMPUTING`, `INSUFFICIENT_DATA`) imposes no floor.

## Dollars

`DeriveDollars` turns recovered hours into dollars at read, from a `Calibration` (FTE rate +
annual tool spend). It is never stored.

- annual value = `hours_recovered × fte_rate × 365 / window_days`
- net gain = annual value − spend
- ROI multiple = annual value ÷ spend (not net gain ÷ spend)
- payback months = spend ÷ (annual value ÷ 12)

A figure is withheld with a stated reason rather than rendered as infinity, an error or a bare
`$0`: spend never entered withholds net gain / ROI / payback; a deliberate $0 spend withholds ROI
and payback (division by zero); a non-positive annual value withholds payback.

`DeriveDollars` takes no basis. Deriving dollars from a `Measurement` on a non-numeric basis is
the one mistake the type cannot stop you making — guard the call behind the cascade's basis. See
[examples/roi-on-a-thin-window](../../examples/roi-on-a-thin-window/).

## Defaults

| Knob | Default | Field |
|---|---|---|
| Min baseline sample | 15 | `Config.MinBaselineN` |
| Min matched AI sample | 10 | `Config.MinMatchedN` |
| Baseline-collapse AI share | 0.75 | `Config.BaselineCollapseAIShare` (0 disables the leverage rung) |
| Effort ratio | 0.5 | `Config.EffortRatio` |
| Per-change effort cap | 8.0 h | `Config.EffortCapHrs` (symmetric) |
| Band cutoffs | score ≤ 2/4/7, LOC < 50/200/600 | `Config.BandBounds` |
| Baseline-median floor | 0.1 h (6 min) | `minBaselineMedianHrs`, not in `Config` |

Start from `DefaultConfig()` and change the field you need.

## What the caller owns

The engine is pure: it owns the methodology math over a frame you supply. Everything that
touches data stays behind the four interfaces.

### The aggregates

`SourceReader` is the one piece every adopter builds — one query that takes `PERCENTILE_CONT(0.5)`
of cycle time and review rounds, grouped by work type, complexity band and cohort, plus the
window's subject counts. Define cycle time once and hold it fixed: open→merge, first-commit→merge
and ready-for-review→merge give different numbers, and the headline moves with the choice.

### The optional rungs

`QualityScoreReader` drives the `WITHIN_ORG_LEVERAGE` rung; `SnapshotStore` persists measurements
and supplies the prior rung for the one-way rule. Both are legally `nil` — pass them and the
cascade gains the leverage rung and the clamp; omit them and it is AI-vs-non-AI with no history.

### The calibration

The FTE rate and tool spend are yours, supplied as a plain struct. No compensation or headcount
data enters the engine.

## Known gaps in the model

- The engine trusts the medians and counts it is fed. A biased cohort split or a wrong work-type
  label biases the headline; the class correction fixes mix, not attribution error.
- A `Measurement` on its own cannot be read safely — without the basis from the cascade, a $0 on a
  collapsed or unmeasurable window is indistinguishable from a real zero result.
- Nothing on a `ClassRow` says whether it was excluded by the min-n floor or by the
  baseline-median floor; both surface only as `MinNExcluded`.
- The dollar model is a single linear `EffortRatio` on cycle-time delta. It does not separate
  recovered engineer effort from reduced queueing and review wait beyond that one ratio.
