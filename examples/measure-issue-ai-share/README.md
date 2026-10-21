Measures how many completed issues AI-assisted pull requests delivered, from links and per-pull-request signals you already have, in the same program as the [issueattribution README's quick start](../../issueattribution/README.md#quick-start). An issue counts as AI-assisted when any linked pull request is. The percentage beside it weighs the pull requests by complexity score, or by lines when none is scored.

```sh
go run ./examples/measure-issue-ai-share
```

```text
AI-assisted: 2 of 3 linked issues (67%)
coverage: 3 of 4 issues linked (75%), low=false
PROJ-1 ai=true 60% by complexity
PROJ-2 ai=false 0% by complexity
PROJ-3 ai=true 20% by lines
PROJ-4 no linked pull request
```
