Folds a ten-person team's raw facts (harness invocations, thirty merged pull requests with their reviews and AI authorship, the tools their sessions used, a declared review policy) into readings with `fold.Fold`, checks the preconditions, and scores them. It prints the profile's summary, each stage's frontier and emerging edge, and the bottleneck with the move that clears it. Start here when your data is events, not counts.

```sh
go run ./examples/fold-team-facts
```

```text
In Code, an agent produces a complete piece of work and a person checks and approves it, and the work is moving toward a way of working where an agent completes a piece of work unattended and a person checks the result afterward. In Test & QA, a person and an assistant work the task out together in conversation. In Review, a person does the work and an assistant helps on request, and the work is moving toward a way of working where an agent produces a complete piece of work and a person checks and approves it. Tech spec has nothing established yet and is moving toward a way of working where a person does the work and an assistant helps on request. Nothing could be measured in Product spec, CI/CD, Deploy and Operate.

product_spec  frontier none              emerging none
tech_spec     frontier none              emerging L1
plan          frontier none              emerging none
code          frontier L3                emerging L4
review        frontier L1                emerging L3
test_qa       frontier L2                emerging none
ci_cd         frontier none              emerging none
deploy        frontier none              emerging none
operate       frontier none              emerging none

- Few tasks are planned with an assistant in plan mode.
  move: Plan with an assistant in plan mode, using a todo list.
```
