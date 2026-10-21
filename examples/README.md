# Examples

## attribution

| You want | Example |
|---|---|
| The smallest complete program, the README's quick start | [match-one-session](match-one-session/) |
| To see the signature scanner on its own | [scan-text-for-ai-signatures](scan-text-for-ai-signatures/) |
| Your own metrics from the engine's events | [count-engine-events](count-engine-events/) |
| Attribution on a schedule, with results kept in SQLite | [run-batches-on-sqlite](run-batches-on-sqlite/) |
| The engine in your own worker, re-attributing when a pull request merges | [reattribute-on-merge](reattribute-on-merge/) |
| A session extractor's export joined to a connector's pull requests | [attribute-exported-sessions](attribute-exported-sessions/) |

## prcomplexity and diffsize

| You want | Example |
|---|---|
| To score one PR against a stored baseline | [score-one-pr](score-one-pr/) |
| To score a real diff, with exclude globs | [score-a-diff](score-a-diff/) |
| To build a baseline from a window of PRs and score new ones | [score-held-out-prs](score-held-out-prs/) |
| To build a PR Complexity baseline from a window too thin for one, and set your own gates | [handle-a-thin-window](handle-a-thin-window/) |
| To count the effective lines of code a diff adds | [count-loc](count-loc/) |

## airoi

| You want | Example |
|---|---|
| To turn AI-vs-non-AI cohort medians into recovered hours and dollars, the `airoi` README's quick start | [roi-demo](roi-demo/) |
| To see why the ROI is read off the basis cascade, not a bare measurement, on thin data | [roi-on-a-thin-window](roi-on-a-thin-window/) |

## defectlineage

| You want | Example |
|---|---|
| The whole pipeline over one bug fix, no database, no network | [trace-one-bug-fix](trace-one-bug-fix/) |

## aidlc

| You want | Example |
|---|---|
| The smallest AIDLC maturity program, the `aidlc` README's quick start | [score-maturity-readings](score-maturity-readings/) |
| A team's raw facts folded into a complete maturity profile | [fold-team-facts](fold-team-facts/) |
| To withhold a profile when the window cannot support one | [withhold-a-thin-window](withhold-a-thin-window/) |
| Week-over-week movement and the adoption state | [track-movement](track-movement/) |

## branchlineage

| You want | Example |
|---|---|
| Each promotion credited with the AI work it carried, the `branchlineage` README's quick start | [project-ai-attribution](project-ai-attribution/) |
| A repository's promotion edges, the first `branchlineage` Usage program | [derive-promotion-edges](derive-promotion-edges/) |
| A repository's promotion flow suggested, checked and resolved | [resolve-a-merge-flow](resolve-a-merge-flow/) |
| A walk with a depth cap and a route budget, and what each left out | [cap-a-walk](cap-a-walk/) |
| To see how much of a branch the merge path explains, and where history is missing | [check-coverage](check-coverage/) |
| Promotion edges kept in SQLite, with replay and a late pull request | [index-on-sqlite](index-on-sqlite/) |
| Attribution, lineage and the AIDLC fold joined on pull-request keys | [attribute-across-promotions](attribute-across-promotions/) |

## issuelinkage and issueattribution

| You want | Example |
|---|---|
| A pull request linked to the issues it names, the `issuelinkage` README's quick start | [link-a-pr-to-issues](link-a-pr-to-issues/) |
| Links kept in SQLite while pull requests and issues arrive in either order | [link-late-issues-on-sqlite](link-late-issues-on-sqlite/) |
| The share of completed issues AI-assisted pull requests delivered, the `issueattribution` README's quick start | [measure-issue-ai-share](measure-issue-ai-share/) |
| Linkage, AI signatures, PR complexity and issue attribution joined on pull-request keys | [attribute-issues-to-ai](attribute-issues-to-ai/) |

Run them all with `scripts/run-examples.sh`, from the repository root. Every example runs offline, against fixed data in its own folder, with no account and no credential. The runner compares each example's standard output with the last block of its README, with nothing normalised.
