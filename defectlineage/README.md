# defectlineage

You know how many of your pull requests were bug fixes. You do not know what
keeps breaking.

That is the gap this package closes. A repair-volume number tells you a quarter
of your work was fixes; it cannot tell you whether those bugs concentrate in one
service, one kind of change, or one corner of the codebase — and those are the
only forms of the signal you can act on. Answering them means knowing where each
defect came from, not who cleaned it up.

`defectlineage` takes a merged bug fix, finds the changes most likely to have
introduced the bug it repaired, and stores each one with the evidence behind it.

## Contents

- [What it does](#what-it-does) — the three stages and their entry points.
- [Quick start](#quick-start) — trace one fix, no services.
- [Where it stores](#where-it-stores) — in memory, or PostgreSQL.
- [What gets stored](#what-gets-stored) — the two tables you query.
- [How the walk decides](#how-the-walk-decides) — and when it refuses.
- [The confidence score](#the-confidence-score) — its five terms.
- [Use with tetriz-connectors](#use-with-tetriz-connectors) — the provider half.
- [Running it locally](#running-it-locally) — Postgres and the queue.
- [Before you rely on it](#before-you-rely-on-it) — the limits that matter.

## What it does

Given a merged bug fix, the pipeline reads the diff the fix landed and takes
each region the fix removed or replaced as a **location anchor**. It blames that
anchor against the revision immediately before the fix, because the line the fix
removed does not exist after it. The commit blame names is a **candidate**: a
claim with evidence attached, never a verdict. One fix routinely has several.

The three stages are separable because they cost different things:

| Stage | Entry point | Cost |
|---|---|---|
| Build anchors from the fix's diff | `ExtractUnified`, `ExtractGitLab` | a parse over bytes you hold |
| Blame each anchor back to a culprit | `Walker.Walk` | provider calls — the expensive half |
| Score each candidate | `Combine`, and the component scorers | arithmetic over stored rows |

Separating blame from scoring is deliberate: blame is what you pay a provider
for once, and the formula is what gets recalibrated repeatedly. Changing the
formula costs a re-read, never a re-fetch.

**What it does not do.** It does not fetch. You supply blame answers through
`Source`, the fix's diff through `FixResolver`, and — if you want syntax-aware
scoring — a `Comparator`. It does not map a commit author to a person; it stores
the author's email and leaves identity to you. It has no user interface: it
writes rows, and you query them.

## Quick start

You need Go 1.25.2 or newer. Nothing else: this example uses no database, no
network and no credential.

For the whole pipeline rather than one walk — enqueue, blame, score, then the
rows a user queries — run
[`examples/trace-one-bug-fix`](../examples/trace-one-bug-fix/), which also needs
no services.

```go
package main

import (
	"context"
	"fmt"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	dlv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// history stands in for a provider's blame.
type history struct{}

func (history) Lines(_ context.Context, rev, path string) ([]defectlineage.BlamedLine, error) {
	switch rev {
	case "PARENT": // before the fix, the broken line is here
		return []defectlineage.BlamedLine{{Text: "return a / b", OwnerSHA: "CULPRIT"}}, nil
	case "EARLIER": // before the culprit, it is not
		return []defectlineage.BlamedLine{{Text: "return a", OwnerSHA: "OLDER"}}, nil
	}
	return nil, nil
}

func (history) FirstParent(_ context.Context, sha string) (string, error) {
	return map[string]string{"FIX": "PARENT", "CULPRIT": "EARLIER"}[sha], nil
}

func main() {
	diff := []byte("diff --git a/m.go b/m.go\n--- a/m.go\n+++ b/m.go\n" +
		"@@ -3,1 +3,4 @@\n-return a / b\n+if b == 0 {\n+\treturn 0\n+}\n+return a / b\n")

	anchors := defectlineage.ExtractUnified(diff, defectlineage.Options{})
	fix := &dlv1.PendingLineageBlame{FixCommitSha: "FIX"}
	for _, a := range anchors {
		fix.LocationAnchors = append(fix.LocationAnchors, &dlv1.LineageBlameAnchor{
			Path: a.OldPath, StartLine: int32(a.OldStartLine), RemovedText: a.RemovedText,
		})
	}

	walker, _ := defectlineage.NewWalker(defectlineage.WalkParams{})
	state, results, err := walker.Walk(context.Background(), history{}, fix)
	if err != nil {
		panic(err)
	}
	fmt.Println(state) // DEFECT_LINEAGE_STATE_RESOLVED
	for _, r := range results {
		fmt.Println(r.GetCulpritSha(), r.GetEvidenceType())
		// CULPRIT DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_DIRECT
	}
}
```

## Where it stores

The engine itself stores nothing — the [quick start](#quick-start) above runs
with no database at all. The *pipeline* does, because it is a work queue: it has
to remember which fixes are waiting, which are claimed, and what blame found.

Two stores ship, behind one `Store` interface:

| | `store/memory` | `store/postgres` |
|---|---|---|
| Needs a service | no | PostgreSQL |
| Survives a restart | no | yes |
| Shared between processes | no | yes |
| Use it for | a first run, and tests | anything you intend to keep |

```go
store := memory.New()                    // nothing else needed
store, err := postgres.New(db)           // after loading schema.sql once
```

Both pass the same conformance suite, `store/storetest`, which asserts only
through the interface — so they are interchangeable rather than merely similar,
and a store of your own passes that suite or it is not a `Store`.

Both work lists are leased, blame and scoring alike, so concurrent workers take
disjoint batches and a worker that dies has its work re-offered rather than
stalling it. The in-memory store implements that properly too; what it cannot do
is outlive the process or be shared with another one, and those are the two
reasons the PostgreSQL store exists.

## What gets stored

The PostgreSQL store keeps two tables, created by
[`store/postgres/schema.sql`](store/postgres/schema.sql). They relate only by `(organization_id, bug_fix_pr_id)` and carry no foreign key
between them, so each can be written, re-driven and purged on its own.

**`bugfix_pr_defect_lineage`** holds evidence: one row per (fix, culprit
commit). The culprit commit is the durable key — it always exists, where the
pull request that carried it may not, which is why a squash-merged culprit still
stores a complete row.

```sql
SELECT candidate_commit_sha, candidate_author_email, evidence_type,
       confidence_score, score_evidence->>'astMatchLevel' AS rung
FROM bugfix_pr_defect_lineage
WHERE bug_fix_pr_id = $1 AND deleted_at IS NULL
ORDER BY confidence_score DESC NULLS LAST;
```

`schema.sql` creates the schema rather than migrating one. Re-running it on a
database that already matches does nothing, but it will not add a column to a
table created by an earlier version — that upgrade is yours to write.

**`defect_lineage_pipeline_state`** holds lifecycle: exactly one row per fix,
with its stage and, once terminal, how it ended. This is where you look to find
out whether a fix was traced, and if not, why.

```sql
SELECT terminal_outcome, count(*)
FROM defect_lineage_pipeline_state
WHERE deleted_at IS NULL GROUP BY 1;
```

Neither table filters by a retention window. A candidate whose commit predates a
window is precisely the row this feature exists to record, and a windowed read
would hide it — the signal would under-report silently instead of degrading
honestly.

## How the walk decides

The walk stops on a **positive** match: the removed line's content is absent at
a commit's parent, so that commit introduced it. It never stops on the absence
of an error.

It also hops, up to `WalkParams.HopCap` times (default 3). A commit that only
re-indented a line is its last writer without having authored its behaviour, so
attribution steps past it to the change that did. No provider's blame offers a
whitespace-insensitive mode, so the comparison strips whitespace itself. The
measurement that made this load-bearing rather than a refinement: on one
repository's history a fraction of a percent of lines diverged between the two
readings normally, but the majority diverged immediately after a single
repository-wide reformat.

**It fails closed.** Every case it cannot decide produces no candidate rather
than a guess:

| Situation | Why it refuses |
|---|---|
| The removed text is absent at the anchor revision | There is no line to attribute — the diff and the history disagree |
| The removed text appears more than once at the anchor | Picking one is a coin-flip, and a bare `return nil` or a lone brace is ordinary |
| More than one occurrence survives in a commit's parent | Which one the line descends from is ambiguous, so the ancestry is a guess |
| The path does not exist at the parent | A file created here, or a rename this blame cannot cross — indistinguishable without the owner's diff |
| The hop cap is reached | Resolving to the last commit walked to would be a confident wrong answer |
| The file is generated or vendored | A regenerated stub out-churns the hand-written fix and would out-score it |

A miss is always preferable to a confident wrong author.

**One residual.** When the parent holds exactly one occurrence of the removed
text, the walk treats it as the carried line. If that occurrence is an unrelated
line of the same text, it follows the wrong ancestry. Deciding this needs the
owner commit's own diff, which the walk does not consult; it is the one path
that can yield a wrong culprit rather than a miss.

## The confidence score

Five terms, each in [0,1], combined by weight. `DefaultConfig` is a starting
point to tune, not a claim of correctness.

| Term | What it measures |
|---|---|
| L, lineage strength | How directly blame reached the candidate |
| C, code context | How close the two changes sit in the syntax tree |
| D, deployment | A fixed factor, equal for every candidate of a fix |
| R, recency | The gap from the candidate's commit to the fix's merge, decaying |
| E, corroboration | Signals naming this candidate, plus weaker defect-level ones |

Every score stores its derivation in `score_evidence`, so it is explainable and
re-derivable without re-blaming.

**C needs a parser, and a line to measure at.** `CoverageComparator` is the
default and answers by path alone, marking every same-file verdict `degraded` —
honest about being coverage rather than a measured proximity. For real
syntax-tree proximity, pass [`astcompare.Comparator`](astcompare/) and build
with `-tags astcompare`. It sits behind a tag because tree-sitter is a C
library, not because it is far away: it needs no tetriz-connectors clone, so a
measured rung is available from a plain clone of this repository.

A comparator also needs the line in the culprit's own revision, and the walk
does not supply one: blame reports the position in the revision it was asked
about, which drifts, so `Walk` leaves it at 0 rather than store a drifted
number. Implement [`CulpritLineResolver`](https://pkg.go.dev/github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage#CulpritLineResolver)
on your `ScoringProvider` — one method returning the lines a commit added to a
file, which [`AddedLines`](https://pkg.go.dev/github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage#AddedLines)
parses from that commit's diff — and the pass measures at the added line
matching the text the fix removed. Without it, every candidate the walk produced
is scored `degraded` and the comparator is never asked, so supplying one changes
nothing.

## Use with tetriz-connectors

[tetriz-connectors](https://github.com/tetriz-ai/tetriz-connectors) syncs pull
requests and answers blame queries. It declares three host services it does not
itself provide — a work list, a blame walk and a scorer — and refuses to run an
operation whose host service is missing.

You need a tetriz-connectors clone beside this repository to build against it:
the three services take and return that project's own types, and Go satisfies an
interface only with the identical types, so whoever writes the glue imports it.
Check out the revision the `wiring` module's `go.sum` was tidied against — the
`CONNECTORS_REF` in the Wiring module job of
[`.github/workflows/ci.yml`](../.github/workflows/ci.yml). A newer one can move a
dependency both modules share, and the build then stops with
`updates to go.mod needed`.

The [`wiring`](wiring/) module is that glue, kept here as a working reference —
it supplies all three from this package:

```go
sink, _ := wiring.NewBlameSink(wiring.BlameSinkParams{Store: store, Resolver: resolver})
walker, _ := wiring.NewWalker(nil)
scorer := wiring.NewScorer(wiring.ScorerParams{}) // the syntax-tree comparator

host := sdk.HostServices{BlameSink: sink, LineageWalker: walker, DefectScorer: scorer}
```

It is a separate Go module on purpose, and it is the **only** thing here that
imports tetriz-connectors. Skip it if you do not use that connector; nothing
else in this repository needs it, and the engine builds from a fresh clone with
no C toolchain and no network.

Each opt-in dependency sits behind a build tag, so the default build compiles
none of them and `CGO_ENABLED=0` keeps working:

| Package | Build with | Pulls in | Needs a tetriz-connectors clone |
|---|---|---|---|
| `defectlineage` | nothing | the standard library and pure-Go deps | no |
| [`astcompare`](astcompare/) | `-tags astcompare` | tree-sitter, so a C toolchain | **no** |
| [`queue`](queue/) | `-tags defectlineagequeue` | the AWS SDK | **no** |
| [`wiring`](wiring/) | it is a module of its own | tetriz-connectors | **yes** |

Only `wiring` is a separate module, because only it imports a second repository.
The other two are ordinary packages here, which is why this repository releases
under one version tag rather than one per module.

Writing the glue yourself instead is a reasonable choice — `wiring` is roughly
770 lines against both projects' types, and it is MIT-licensed like the rest.

## Running it locally

You need none of this to try it: `memory.New()` runs the whole pipeline in
process. The stack below is for the durable path.

[`docker-compose.yml`](docker-compose.yml) brings up PostgreSQL
with the schema loaded, and a local AWS stand-in carrying the topic and queue
the enqueue path runs on.

```sh
cd defectlineage && docker compose up -d
```

`provision.sh` creates the topic, the queue, and the subscription that connects
them. The subscription carries a filter policy — `work_type` BUGFIX and `state`
MERGED — so only merged bug fixes are delivered. Filtering there rather than in
the consumer keeps one source of truth for "is this a bug fix", which is why the
consumer enqueues everything it receives instead of re-deciding.

The pipeline runs from your own process against those two services. Nothing in
the compose stack runs the engine.

## Before you rely on it

- **Blame is scoped to one repository.** A fix whose cause lives in another
  repository cannot be traced.
- **A repository that counts commits rather than pull requests produces no row
  for its own fixes.** The fix side requires a pull request; the candidate side
  does not, so a culprit that never had one still stores completely.
- **Deployment evidence is not read.** The D term is a constant, so it shifts
  every score equally and never reorders a fix's candidates.
- **`same_author`, `regression_test_changed` and `incident_or_rollback` are
  always false** unless your own corroboration source sets them. Resolving a
  commit author to a person needs identity data this package does not own.
- **The default scorer degrades.** Without `astcompare.Comparator`, C is file
  coverage and every verdict is marked degraded. Read that flag before trusting
  a same-file rung. A comparator is asked only when the provider implements
  `CulpritLineResolver`; without it the verdict degrades whichever comparator is
  configured.
- **The in-memory store is not durable.** It is for a first run and for tests.
  A restart loses the queue and everything blamed so far.
- **Scores are uncalibrated.** The weights and ladder rungs ship as reasonable
  defaults, not as values validated against labelled data.
