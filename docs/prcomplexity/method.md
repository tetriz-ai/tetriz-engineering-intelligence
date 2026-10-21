# PR Complexity: the method

This page explains how `prcomplexity` turns a pull request's diff into a complexity score, and
what the code leaves to the program that calls it. Every number here is the code's, and the
package tests pin them.

## The idea

Lines of code mean different things in different languages and repositories. A 400-line
Terraform change and a 400-line React change carry different review loads, and a repository
that mixes Go and TypeScript has no single "typical PR size".

So a PR is scored against its own repository's recent merged PRs, one language at a time. The
score runs from 1.0 to 10.0. A PR of the repository's typical size for its language scores
about 5.

## Inputs

### The diff, as effective lines of code

`diffsize` reads a git unified diff or GitLab's merge-request diffs JSON and counts only added
lines. For each file:

1. A file matching an exclude glob is dropped first.
2. go-enry flags vendored files by path, and generated files by path and by the added lines,
   and both count in their own totals instead of the language totals.
3. Documentation files (by go-enry's path rules) are counted in their own total, with no
   language.
4. go-enry names the language of every other file, and scc splits its added lines into code,
   comment and blank lines.

The result, `EffectiveLOC`, holds per language the added source lines (scc's code count) and
the number of files changed. A file counts as changed when the diff touches it, including a file
it only removes lines from, so a PR that adds 3 lines to one file and trims 12 others has 13
files. Stored baselines count files the same way.

### The code-only filter

Only languages that hold code count: go-enry's Programming and Markup types, plus Protocol
Buffer. JSON, YAML, Markdown, plain text and unknown languages are dropped. A PR's slice for a
language exists only when that language has at least one added source line.

`IsCodeLanguage` and `CodeSlices` are the one filter, and both the baseline and the scorer go
through them, so a PR is never scored against a distribution it does not contribute to.

## The baseline

A baseline is built from one window of the repository's merged PRs (`BuildBaseline`).

### The window gate

The whole sample must pass two gates, or there is no baseline (`ErrNotEnoughData`):

| Gate | Default | Rule |
|---|---|---|
| Sample size | 30 | strictly more than 30 PRs |
| Coverage | 9500 bps (95%) | strictly more than 95.00% of PRs with a usable diff |

Both comparisons are strict: 30 PRs, or exactly 95.00% coverage, fail. Coverage is
truncated to whole basis points.

A PR has a usable diff when its diff was analysed, or when its diff is the `TOO_LARGE`
sentinel. A `TOO_LARGE` PR is scoreable (it scores 7.0), so it counts toward coverage, but it
has no per-language footprint and adds nothing to the medians. A `NOT_AVAILABLE` PR, and a PR
whose diff was never fetched or analysed, counts in the sample but not in coverage.

### Per-language anchors

For each code language in the admitted sample:

- The sample is every PR that touched the language (SLOC > 0). The median is conditional, so
  a language a repository rarely touches is not dragged toward zero by PRs that skip it.
- The language needs at least 30 such PRs (here the comparison is `>=`).
- The median SLOC and median files must both be above zero.
- Medians are integers: an even count rounds the mean of the two middle values half away from
  zero.
- The log statistics are winsorised at the sorted value at index `floor(0.95 n)` (at least 1):
  `mu = mean(log(min(v, p95) + 1))` and `sigma` is the population standard deviation of the
  same logs.

A language that misses any of these has no anchor. There is no fallback anchor, not
repository-wide and not across repositories.

The log sums run in sample order, so the same PRs in another order can differ in the last bit.

## The score

`ScoreComplexity` applies these steps.

### Before the formula

| Input | Result |
|---|---|
| diff is `NOT_AVAILABLE` | unscoreable |
| diff is `TOO_LARGE` | 7.0 |
| no code slice, or no slice whose language has an anchor | unscoreable |

The `TOO_LARGE` score is a hypothesis: a diff over the caller's size cap is usually inflated by
binary blobs, generated assets or vendored snapshots, and 7.0 is high without overstating it.

### Per-language slice score, cncs formula (the default)

For each anchored slice, each dimension gets a z-score and a sigmoid:

```
z = (log(value + 1) - mu) / sigma
s = clamp(1 + 9 / (1 + exp(-1.702 z)), 1, 10)
```

1.702 is the logistic approximation of the normal CDF. A sigma outside its valid range is
replaced, since a degenerate window (a single-commit repository, uniform PRs) gives a sigma
that would blow up z:

| Dimension | Valid sigma | Fallback |
|---|---|---|
| SLOC | 0.8 to 3.0 | 2.0 |
| Files | 0.3 to 2.0 | 0.8 |

A language whose anchor has `mu` of zero has no log statistics, and the cncs formula skips it.

### Per-language slice score, ratio formula

```
ratio = value / median
s = 1.0          if ratio <= 0.2
s = 5 * ratio    if ratio <= 1.0
s = 10 - 5/ratio otherwise
```

| Value against its language median | s |
|---|---|
| 1/5 or less | 1.0 |
| 1/2 | 2.5 |
| equal | 5.0 |
| 2x | 7.5 |
| 4x | 8.75 |
| 10x | 9.5 |

The curve above the median is hyperbolic on purpose: a slice 40 times the median is harder to
review than one 4 times the median, but not ten times harder.

### Within a slice: the dominant dimension

```
s_L = 0.8 * max(s_sloc, s_files) + 0.2 * min(s_sloc, s_files)
```

A slice is as complex as its heavier dimension, and the other one moderates it, whichever it is.
A 40,000-line single-file change is not demoted by its file count, and in the same way a
many-files, tiny change is promoted by its file count. Against a Go anchor of `mu`/`sigma`
4.36/0.92 for lines and 1.41/0.37 for files, 5 lines over 10 files scores `s_files` 9.90 and
`s_sloc` 1.08, so `s_L` = 0.8 x 9.90 + 0.2 x 1.08 = 8.1.

### Across languages: geometric rank decay

```
sort the slice scores descending: s_1, s_2, s_3, ...
score = round(min(10, s_1 + d s_2 + d^2 s_3 + ...), 1)
```

The hardest language sets the floor, and each next one adds a decaying share: the first
context switch costs more than the third. With `d = 0.3`, PRs whose languages all sit at the
median (each slice 5.0 under the ratio formula) score:

| Languages at the median | Score |
|---|---|
| 1 | 5.0 |
| 2 | 6.5 |
| 3 | 6.95 before rounding, 7.0 |
| 4 | 7.085 before rounding, 7.1 |

The series converges toward 7.14, so many at-median languages never saturate the cap. The cap
fires only when a hard slice meets another hard slice: 8.0 + 0.3 x 8.0 = 10.4, capped at 10.

Rounding is half away from zero, to one decimal place.

## Defaults

| Knob | Default | Where |
|---|---|---|
| Formula | cncs | `ScoreComplexity`'s `formula`; the empty value means cncs, an unknown value is an error |
| Cross-language decay `d` | 0.3 | `DefaultDecay` |
| Sample-size gate | 30 | `DefaultMinSampleSize` |
| Coverage gate | 9500 bps | `DefaultCoverageThresholdBps` |
| Window length | 90 days | the caller's (below) |

## What the caller owns

The package scores a PR against a baseline you give it. Choosing the window, keeping
baselines, and recording outcomes stay with the program that calls it.

### Choosing the window

A host can build one baseline per repository per week, for example each Monday at 00:00 UTC
(the anchor). The window is then the half-open range `(anchor - 90 days, anchor]` of PR open
times, and the sample is the merged PRs opened in it.

Score a PR against the baseline of the anchor on or before the day it was opened. A PR opened
before a repository's first anchor can use the earliest baseline whose window covers it.

Consider holding a repository's first baseline back until the repository is about 30 days old,
so its atypical first merges do not set the norm. The package does not drop bot-authored PRs.

### Pass the sample in a stable order

`BuildBaseline` is deterministic for a given order. Pass PRs ordered by open time, then by ID.

### Recording outcomes

How a result is stored is the host's choice. A policy that works:

- Freeze a merged PR's score, and keep an open or closed PR's score live, recomputing it when
  the PR changes.
- Record an unscoreable merged PR as "not enough data" for good. Keep an unscoreable open PR
  live, so a docs-only PR that later adds code is scored then.
- Retry a window that fails its gate for a while, such as 30 days after the window was created,
  then record it as "not enough data".

### Reading the score as a band

Bands such as High, Medium and Low are a read-side choice and are not part of the package. Bands
can be derived from percentiles of recent scores rather than fixed thresholds.

## Known gaps in the model

- The sample includes bot-authored PRs, which skew the median on repositories with many of
  them.
- Files are counted per language after the generated and vendored files are removed. go-enry
  recognises generated files by path and by the markers of generators it knows, in the added
  lines only. An edit to an existing generated file, whose banner is outside the hunk, counts as
  source unless an exclude glob matches its path. So does a file marked only by another
  generator's banner.
- A file the PR only removes lines from counts toward the files dimension, which the 80/20
  rule can let dominate: 3 added lines in one file plus 12 deletion-only files scores 8.2 where
  the one file alone scores 1.3, against the anchor above.
- There is no dispersion gate: a language with very uneven PR sizes still anchors on its median.
