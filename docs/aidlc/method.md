# AIDLC maturity: the method

This page explains how `aidlc` turns one organisation's data into a maturity map, and what the
code leaves to the program that calls it. Every number here is the code's, from ruleset `v1`,
and the package tests pin them.

## The idea

Asking engineers how they use AI gives an answer about intentions. The map reads what the
organisation's own records show instead: who used which AI-harness artifact, which merged pull
requests an AI session authored, who reviewed them, and what the admins enforce and declare.

The map is a grid. Nine lifecycle stages run down it: product spec, tech spec, plan, code,
review, test and QA, CI/CD, deploy and operate. Five levels run across it:

| Level | Name | Who does the work |
|---|---|---|
| L1 | assisted | A person; an assistant helps on request. |
| L2 | conversational | A person and an assistant, in dialogue. |
| L3 | supervised agentic | An agent produces a complete piece; a person checks and approves it. |
| L4 | autonomous | An agent, unattended; a person checks the result afterward. |
| L5 | orchestrated | Agents coordinate, under the organisation's own policy. |

Each stage and level has its own definition in the reader's words (`aidlc/definitions.go`). A
level is a capability claim, so a stage can be met at L3 and not at L2.

The map's one rule is that it never turns missing data into a verdict. A cell it cannot decide
is "not yet visible", with the reason and whose it is to fix.

## Inputs

### Readings

`Score` reads one `Reading` per (signal, stage). A reading carries:

- **Value**, a count or a rate, and **People**, the distinct people behind it.
- **Status**: resolved; no run (nothing produced the signal); row absent (read, but nothing for
  this organisation); or gap-blocked (the gate the rule names is closed, such as no enforced
  practice for the stage).
- **Coverage**: full, partial, declared partial, none or unknown.
- **Remainder**: the members of the population the numerator could not be read over, or the
  population could not enumerate.
- **Bound**: the share of the source covered, used to widen a partial reading.

A reading with no stage is org-level, and the scorer falls back to it for any stage. The seat
roster is one.

Signals are registered in `aidlc/rules/signals.go` with a grain (people or merged changes) and
what a zero can deny. A zero on a signal that only sees an instrumented artifact proves nothing,
because work done in chat leaves no trace there. A zero on a signal that only sees automated
tooling proves nothing either.

### The fold

`fold.Fold` builds the readings from facts the caller supplies. It reads nothing itself.

- **Presence levels** (product spec L1 and L2, tech spec L1, review L1, CI/CD L1, deploy L1,
  operate L2) count the distinct seat holders who invoked a skill, agent or command serving the
  stage, over the roster. Hooks, rules, prompt templates, plugins and permissions are installed
  configuration and never count. A practice artifact whose stage is unknown widens every
  presence level by its users, as an unknown remainder.
- **Merged-change levels** read the window's merged pull requests. A pull request serves a stage
  by its file paths: a `prd/` document for product spec, an `rfc/`, `adr/` or `design-doc/`
  document for tech spec, a test path for test and QA, a pipeline or infrastructure path for
  CI/CD, a monitoring path for operate. The code stage reads work type instead: FEATURE and
  BUGFIX are delivery, and an unknown work type widens the population rather than counting as
  a miss, for code and for test and QA alike. One pull request may serve several stages.
- **AI-authored** means the attribution put the pull request in `AuthoredPRKeys`. A pull
  request with an empty PRKey joins nothing: it counts in the populations its own fields place
  it in, and its AI authorship, turns and reviews are unread. Its author is in neither adoption
  count unless another of their pull requests is AI-authored. **Reviewed**
  means a submitted review that the captured review records do not show as bot-only. A pull
  request whose reviewer list was not captured is unread mass on the reviewed count, never an
  unreviewed merge.
- **Low-turn** (code L4) is an AI delivery pull request whose authoring sessions took one to
  four human turns. A pull request missing from `HumanTurns`, or mapped to zero, has no readable
  count and is unread: the code L4 numerator's remainder, and the test and QA low-turn
  population's.
- **Review automation** (review L3 and L4) reads the captured review records. A pull request
  with no captured record widens the population; one whose source cannot flag a bot is left
  out of both sides.
- **Enforcement** (the L5 cells) reads the admin-pushed practice artifacts live for each
  stage, the people they target, and who among them used one. A stage with no live enforcement
  is gap-blocked.
- **Declared review policy** (the second share of product spec L5 and review L5) is the
  AI-authored pull requests whose declared approval was met, over those the policy covers.
- **Plan** reads planning phases in AI sessions: who planned (L2), planned AI pull requests a
  person reviewed (L3, gated on the tracker having issues in the window), and planned pull
  requests run without a per-step human gate (L4). When the caller marks the planned set
  unread, L3's numerator is unread (every AI pull request its remainder) and L4's population is
  a no-run. A review record whose approvals were not captured leaves its approval unread, for
  plan L3 and the declared review policy alike.
- **Deploy** reads merges into branches whose role is shipped, counting seat-linked authors
  only, the population deploy L2 divides by. Its control is the review rate on non-AI pull
  requests into a shipped branch whose reviewer list was captured. With no shipped role
  anywhere, every deploy signal is a no-run.
- **Declared commands** (test and QA L4, CI/CD L4) read the sessions with tool use as the
  population. The fold reads no command outcomes, so the numerator is all remainder.
- **Operate L1** counts people whose sessions used an observability tool, matched by name as a
  whole token (`temporal` does not match `tempo`). Tool
  inventories are a declared subset, so the level abstains rather than decides.

The fold also returns the org-level reads `Score` takes (`OrgRead`) and the two preconditions.

## The cells

Of the 45 cells, 36 are share rules, 2 are implied floors, 2 are judgments and 5 cannot be
measured.

| Stage | L1 | L2 | L3 | L4 | L5 |
|---|---|---|---|---|---|
| Product spec | seat holders using a product-spec artifact | seat holders typing a spec-drafting command | AI requirement PRs merged after review, over AI requirement PRs | harness-routed requirement PRs, over AI requirement PRs | enforced practice used, and declared policy met |
| Tech spec | seat holders using a design artifact | AI design-doc PRs, over seat-linked design-doc PRs | AI design-doc PRs merged after review, over AI design-doc PRs | harness-routed design-doc PRs, over AI design-doc PRs | not measurable (G6) |
| Plan | judgment | seat holders with a planning phase | planned AI PRs a person reviewed, over AI PRs (gate G2) | planned AI PRs run without a per-step gate, over planned AI PRs | enforced planning practice used |
| Code | AI-authored PRs, over seat-linked delivery PRs | implied floor (G3) | AI delivery PRs a person reviewed, over AI delivery PRs | low-turn AI delivery PRs, over AI delivery PRs | enforced coding practice used |
| Review | seat holders using a review artifact | implied floor | PRs a review bot reviewed, over PRs whose source can flag a bot | PRs a bot cleared with no human review, over bot-reviewed PRs | enforced review practice used, and declared policy met |
| Test & QA | judgment | AI delivery PRs touching tests, over seat-linked delivery PRs | low-turn AI PRs touching tests, over low-turn AI delivery PRs with a file list | declared test command green (gate G1) | enforced testing practice used |
| CI/CD | seat holders using a CI artifact | AI CI PRs, over seat-linked CI PRs | AI CI PRs merged after review, over AI CI PRs | declared CI command green (gate G1) | not measurable (G1) |
| Deploy | seat holders using a deploy artifact | AI PRs into a shipped branch, over seat-linked PRs into one | those merged after review, over AI PRs into a shipped branch | AI PRs deployed from a production branch, over AI PRs (gate G7) | not measurable (G7) |
| Operate | seat holders using an observability tool | seat holders using a debugging artifact | AI monitoring PRs, over seat-linked monitoring PRs | not measurable (G8) | not measurable (G8) |

`aidlc/rules/cells.go` is the authority. Most share cells also carry a people floor of 2: at
least two distinct people did the thing. Cells that compare a review rate carry a control: the
review rate on the same stage's non-AI work must be above zero, or a missing review on AI work
says nothing.

The L5 cells, plan L3 and L4, and deploy measure a narrower stand-in than the level's
definition, because nothing observable carries the definition itself. The profile serves the
stand-in's wording for those cells, so a reader sees what was measured.

## Deciding a share cell

`scoreShare` in `aidlc/scorer.go` runs these steps in order, and the first that applies decides.

1. A gap-blocked numerator or population abstains on the rule's gap.
2. An unresolved numerator or population abstains. A no-run reading that carries a note gives
   it as the reason. A malformed reading abstains too, as row
   absent: a value that is NaN, infinite or negative, a negative people count or remainder, a
   coverage bound outside [0, 1], or a count term that is fractional or beyond 2^53.
3. A declared population remainder with no value abstains.
4. A zero on an instrumented-only signal abstains, unless it comes with unread hits, which
   widen the share instead (step 7). Such a zero never denies: after the band (step 12) it
   abstains on coverage if the widened share straddles a cut, and instrumented-only
   otherwise, after the floors. A non-zero count on these signals is a measured share and
   can read not reached; only a zero never denies.
5. A declared numerator remainder with no value abstains.
6. A numerator above the population's counted value is two readings disagreeing, and abstains
   on coverage. The population's remainder is unenumerated, so it cannot make up the difference.
7. The unread mass is the population remainder plus the numerator remainder. Partial coverage
   widens the numerator's: by the population times the uncovered share when a bound is known,
   and by everything not yet a hit when it is not. Declared-partial, unknown and no coverage
   widen it by everything not yet a hit.
8. A population above zero and under its floor (5 people, or 20 merged pull requests)
   abstains.
9. An empty population abstains: on coverage when some of it was unread, as "no population"
   when none was.
10. The share is an interval: `lo = hits / population` and
    `hi = (hits + unread) / population`, with the unread mass capped at the non-hits.
11. A control that reads zero abstains; one that cannot be read abstains.
12. The band: **established** when `lo > 0.50`; **emerging** when `lo > 0.20` and
    `hi <= 0.50`; **not reached** when `hi <= 0.20`. Otherwise the interval straddles a cut and
    the cell abstains.
13. On the not-reached band only, a zero on an automated-only signal abstains.
14. A second share (product spec L5, review L5) is placed the same way. It
    composes as the lower of the two bands, and a not-reached second share denies the cell.
15. Fewer distinct people than the people floor abstains, unless the cell is already not
    reached (including by step 14). An unknown people count abstains the same way.
16. A context reading (complexity on code L4) is attached, never a gate.

Both cuts are strict. A share of exactly 0.50 is emerging, and exactly 0.20 is not reached.
The emerging cut has no cohort behind it, and a cell it decides says so.

A cell that abstains carries one of eight kinds: gap, row absent, no population, coverage,
population floor, judgment, instrumented only and automated only.

## Closing each stage

A level is downward closed: an organisation doing L4 work can do L3 work. Walking each stage
from L5 down, a met rung awards every closable abstain beneath it as an implied floor (gap,
row absent, no population, coverage). A measured miss, or any other abstain, stops the walk:
so does a malformed reading (it publishes as row absent) and two readings that disagree (it
publishes as coverage), because each is an answer that the input is wrong. An implied floor
carries no band.

A stage's **frontier** is its highest established rung. Its **emerging edge** is the highest
emerging rung above the frontier. Either may be empty, which is a reading too.

**Breadth** says how widely a stage's presence reading is held. Its numerator is the stage's
presence people who are also active engineers, and its denominator the active engineers: the
people with an AI session or a merged pull request in the window. Fewer than 3 people is not
reported. Half or more of the active engineers is organisation-wide; less is a team. Breadth
never changes a verdict.

## Bottlenecks and moves

Five detectors read the scored grid, ranked by impact and then weight.

| Bottleneck | Fires when | Impact |
|---|---|---|
| `review_reach` | Code L3 recorded a share over at least 20 AI delivery pull requests, and even that share's widened upper bound is under 0.50 | High under 0.25, else medium |
| `adoption_reach` | Half or more of the people who merged work have no AI-authored merge | High at 0.70 or more, else medium |
| `rung_<stage>_<level>` | The first not-reached rung on a stage with any met rung | Medium-high from L3, else medium |
| `ladder_<stage>` | A stage with misses and nothing met; the two with the most misses | Medium-high at two or more misses, else medium |
| `gap_<gap>` | The instrumentation gap blocking the most cells | Not shown to the organisation; for whoever runs the measurement |

Each served bottleneck carries a move composed from its numbers: how many more pull requests or
people would clear the line. For merged changes, a new pull request joins both the count and
the total, so the move is the smallest `x` with `(hits + x) / (population + x)` above the cut.
For people, the roster does not grow, so it is the smallest count above the cut over today's
roster. When the cut is 1 or more over merged changes, no `x` clears it, and the move names no
distance. A people share over fewer than 20 people shows no counts, so a small roster's
numbers cannot name individuals.

## Publishing

`CheckPreconditions` withholds a profile on the first failing check:

1. No merged pull request in the 90-day window. Path coverage over nothing is trivially 1.0,
   so this check comes first.
2. Fewer than 0.80 of the merged pull requests carry a file list. Five of the nine stages read
   file paths.

`Score` does not consult them. A withheld profile is still a valid profile.

## Movement and adoption

`DeriveMovement` reads the days the caller stored, one reading per ISO week (its last
published day), over at most 12 weeks ending at the latest published week. A cell row counts
only when a snapshot marks its day published; a withheld day's cells are never read.

- **Moved up**: a cell rises above its held verdict and holds for two consecutive weeks.
- **Moved down**: a cell falls from met to not reached, the week it is measured. A cell that
  stays met but falls from established to emerging produces no point, by design, while its rise
  back is reported as moved up.
- **Lost sight**: a cell that was visible abstains.
- **Coverage gained**: a cell's share interval narrows week over week.
- **Stuck**: a bottleneck present in each of the last 3 or more consecutive weeks.
- **History starts**: the series begins inside the range, or is too short to read.

A week with no published day breaks week-over-week comparisons and stuck runs. A held verdict
carries across abstains and gaps.

`ResolveAdoptionState` compares the active engineers in the last 30 days with the 30 before.
An organisation younger than 60 days is onboarding; a last-30 count of zero is lapsed; then
growing above +20%, contracting below -20%, and steady otherwise. An empty earlier window with
people now is growing.

## Defaults

| Parameter | Value |
|---|---|
| Window | 90 days |
| Established cut | share above 0.50 |
| Emerging cut | share above 0.20 (uncalibrated) |
| Population floor | 5 people, 20 merged pull requests |
| People floor | 2 distinct people on most share cells |
| Path-coverage threshold | 0.80 |
| Breadth floor | 3 people |
| Counts shown | people shares over 20 or more people |
| Movement axis | 12 weeks; promotion holds 2 weeks; stuck from 3 weeks |
| Adoption | onboarding under 60 days; a change of more than 20% |

The ruleset values are hashed (`rs.Hash()`); a released ruleset is never edited, and
`TestHashFrozen` fails an edit. The breadth, count and movement values sit outside the hash.

## What the caller owns

- **The window.** The ruleset says 90 days; the caller chooses the facts that fall inside it.
- **Attribution.** Only session-to-pull-request matches the caller trusts belong in
  `AuthoredPRKeys`. Every AI-authored level reads them.
- **Stage mapping.** `StageMap` says which stage each harness artifact serves.
- **Identity.** `AuthorPersonIDs` maps a pull request's author identity to the person id the
  roster uses, so one person counts once among the active engineers.
- **Storage.** Profiles, cells and bottlenecks are the caller's to keep. Movement reads them
  back.

## Known gaps in the model

With a hand-built ruleset whose cut is one or more (v1's are not), a cell that needs every
merged pull request to qualify names no number of pull requests in its move, because no count
of new ones reaches it. Pinned by `TestScore_TerminatesAtACutOfOne`.

Nine instrumentation gaps leave cells unreadable. Each is named on the cells it blocks.

| Gap | What is missing |
|---|---|
| G1 | Check and build outcomes per run |
| G2 | Planning work read with tracker issues, or a tracker issue in the window |
| G3 | Commits joined to their pull requests |
| G4 | The organisation's enforced practices and declared review policy |
| G5 | Runs that span repositories linked together |
| G6 | Design documents compared with the code they describe |
| G7 | Deploy and rollback events |
| G8 | Incident and alert events |
| G9 | Pull-request file paths from a source |
