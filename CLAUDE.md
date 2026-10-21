## What this is

`attribution` links AI coding sessions to the pull requests they produced, or abstains with a
reason. `prcomplexity` scores a pull request against its repository's recent merged PRs, from the
per-language sizes `diffsize` takes from a diff. `aidlc` scores an organisation's AI development
lifecycle maturity from facts the caller folds into readings. `branchlineage` records which pull
requests each promotion carried and credits promotions with attributed work. `issuelinkage` links
pull requests to tracker issues with evidence, and `issueattribution` measures the share of
completed issues AI-assisted pull requests delivered. `attribution`, `prcomplexity`, `diffsize`,
`aidlc`, `branchlineage`, `issuelinkage` and `issueattribution` fetch nothing and call no network.

## Layout

- `attribution/` is the engine, with `runner/` (stores in `memory/`, `sqlite/`), `adapter/` and
  `signature/`. `gen/` (apart from `doc.go`) and `internal/exportpb/` are generated from `proto/`.
- `prcomplexity/` scores and builds baselines from `diffsize/`'s sizes. Each has a `README.md`, and
  `docs/prcomplexity/method.md` is the method. Engines never import each other.
- `aidlc/` scores maturity (`scorer.go`, `closure.go`, `bottleneck.go`, `profile.go`; served copy
  in `copy.go`, `render.go`, `coverage_items.go`). `aidlc/fold/` builds readings from raw facts,
  `aidlc/rules/` is the hashed ruleset, and `proto/aidlc/v1/` generates `gen/aidlc/v1/`. Its
  `README.md` is the guide and `docs/aidlc/method.md` the method. `aidlc/stageclient/` builds
  `fold.Facts.StageMap` from model-service's `/classify-stage` over HTTP, kept out of `aidlc/` and
  `aidlc/fold/` so they stay network-free.
- `branchlineage/` derives promotion edges (`derive.go`, `history.go`), invalidates them
  (`invalidate.go`), walks and projects them (`walk.go`, `project.go`, `carried.go`), resolves
  flows (`flow.go`, `detect.go`, `validate.go`) and reports coverage (`coverage_signal.go`).
  `store/memory` and `store/sqlite` keep edges incrementally and pass `store/storetest`.
  `adapter/vcs` reads connector records. Scenario data is in `branchlineage/testdata/scenarios/`.
- `issuelinkage/` extracts candidates (`extract.go`, stage 1), resolves and folds them into links
  (`process.go`, stage 2) and runs over a `Store` (`run.go`, `store.go`). `store/memory` and
  `store/sqlite` share their rules in `store/internal/plan` and pass `store/storetest`.
  `adapter/vcs` reads connector records. `issueattribution/` is one file, `attribution.go`.
- `defectlineage/` traces a bug fix to the change that introduced it: anchors from its diff
  (`anchor.go`), the blame hop walk (`walk.go`), the confidence formula (`scoring.go`,
  `corroboration.go`) and the pipeline (`drain.go`, `scorepass.go`, `sweep.go`). `store/memory`
  and `store/postgres` pass `store/storetest`'s conformance suite, and a new store must too;
  the schema is `store/postgres/schema.sql`.
  `defectlineage/wiring/` is a SEPARATE module holding every dependency the root refuses —
  tetriz-connectors' three host services, the C-based tree-sitter comparator and the queue
  consumer — so the root keeps building with `CGO_ENABLED=0`.
- `examples/`: one program per folder, indexed in `examples/README.md`. Tests sit beside code.
- API: `go doc ./<pkg>`; each package README lists its entry points.

## Commands

```sh
go build ./...
go vet ./...
go test -race -count=1 ./...
GOTOOLCHAIN=go1.27.3 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run ./...
GOTOOLCHAIN=go1.27.3 go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
scripts/run-examples.sh
```

`scripts/ci.sh` runs these checks locally without GitHub Actions; see `docs/releasing.md`
for its required PostgreSQL, sibling connector revision and Python environment. It tests
Go 1.25.2 (the floor), 1.26.10 and 1.27.3; lint, vulnerability checks and examples run on 1.27.3.

## Contract to keep

- No evidence means an abstention (`attribution/engine_attribute_test.go`). Workspace-only
  evidence never carries a gated editor's branch match (`TestMatchBranchGated`, `signals_test.go`).
- In `attribution/behaviour_test.go`: `TestOnlyTheEnumNameIsGatedDeclined` (only `IDE_TYPE_KIRO`
  is gated), `TestAnUnmergedCandidateDecaysFromItsOpenDate` (a confidence stays in [0, 1]) and
  `TestALeftPendingPersonIsReportedWithTheError` (`Attribute` errors only on a worker panic, and a
  person whose fetch fails stays pending and reaches the Observer).
- The reducer never withdraws a match this run did not fetch:
  `TestMergePRAttribution_UnexaminedCandidateIsNeverWithdrawn` in `attribution/merge_test.go`.
- Both stores follow one set of rules, one writer each (`attribution/runner/runner_test.go`).
  Upstream editor names and provenance classes: `attribution/adapter/contract_pins_test.go`.
- In `prcomplexity/`: scores are pinned bit for bit (`complexity_golden_test.go`,
  `scoring_golden_test.go`, the `score-held-out-prs` README). Baseline gates are strict at the
  window and `>=` per language, so leave `Median`'s rounding (`TestBuildBaseline_StrictGates`).
  A `TOO_LARGE` diff scores 7.0 and counts toward coverage, not the medians (`TestScoreComplexity`,
  `TestBuildBaseline_TooLargeCountsTowardCoverage`). Invalid input is an error with a nil result,
  never a NaN or zero (`TestScoreComplexity_InvalidInputIsAnError`).
- In `diffsize/diffsize_test.go`: each scc alias counts one comment and one code line, and a new
  alias needs a row (`TestSccAliases_CountOneCommentAndOneCodeLine`). A GitLab entry flagged
  `too_large` or `collapsed` wraps `ErrDiffExcluded` unless an exclude glob matches its path,
  which excludes it (`TestAnalyzeDiffWithExcludes_GlobCoversAGitLabExcludedDiff`), and invalid
  input is an error with a nil result (`TestAnalyzeDiff_GitLabExcludedDiffIsAnError` and the
  other `...IsAnError` tests). A deletion-only file counts in `FilesChanged`, the rule stored
  baselines are built on (`TestAnalyzeDiff_DeletionOnlyFileCountsInFilesChangedDeclined`).
- In `aidlc/`: the `v1` ruleset is frozen (`TestHashFrozen` in `rules/registry_test.go`), so a
  rule change is a new version. An undecidable cell abstains, never a zero
  (`TestFold_EmptyFactsAbstainNeverZero`, `TestFixtureB_AbstainPaths`). Both cuts are strict and
  closure stops at a measured miss (`boundaries_test.go`). Review reach reads code L3's own
  share (`TestReviewReach_ReadsTheAIDeliveryPopulation`), and a merged-change move toward a cut
  of one names no distance (`TestScore_TerminatesAtACutOfOne`). Every served sentence is pinned
  by `TestRenderedPages` against `aidlc/testdata/pages/`; regenerate with
  `AIDLC_UPDATE_PAGES=1 go test ./aidlc -run TestRenderedPages` and read the diff.
- In `branchlineage/`: the deriver's rules are pinned by `TestDerive_Rules` and the scenario
  numbers by `TestScenarios`. After any sequence of `Put` and `Derive`, a store's edges equal a
  full derivation (`RandomBatchesConvergeToAFullDerivation` in `store/storetest`). A capped walk
  says what it cut, and a cut node is never carried work (`TestWalk_DepthCap`,
  `TestCarriedBy_CapStoppedNodesAreNotWork`). `MaxDepth` and `MaxRoutes` are used as given
  (`TestWalk_LimitsAreTheCallers`). The detector's output always validates
  (`TestDetectFlow_OutputAlwaysValidates`). Coverage counts a promotion that carried nothing,
  such as a release cut with no pull request into it (`TestComputeCoverage_ReleaseCutWithNoHotfix`).
- In `issuelinkage/`: the keyword nearest a reference sets its intent, and another reference between
  them cancels it (`TestExtract`). The strongest evidence wins, and a context-heading mention
  blocks a link only when it is the only evidence (`TestProcess_Combine`). The closer is the newest
  merged claimer, ties to the smaller key, never an unmerged one (`TestElectCloser`). A replay
  writes the same rows, `FirstSeenAt` is kept, and a nil native list keeps the stored one
  (`AReplayChangesNothing`, `FirstSeenIsKeptAcrossRuns`, `ANilNativeListKeepsTheStoredOne` in
  `store/storetest`).
- In `issueattribution/`: blocked links are left out by default (`TestCompute_LinkFloor`),
  coverage is nil with no issues (`TestCompute_HeadlineAndCoverage`), a link to a missing pull
  request is counted in `DroppedLinks` (`TestCompute_DroppedLinks`), and invalid input is an error
  with a nil result (`TestCompute_InvalidInputIsAnError`).
- A known limit is a `...Declined` or `...Limit` test in README "Before you rely on it". Pre-v1 API changes are
  allowed, each in CHANGELOG.md.

## Making a change

- Write the test first: it fails before the change and passes after. A new case goes in its
  test's table. Never silence the linter inline: turn a rule off in `.golangci.yml`, with a comment.
- A behaviour change adds an `[Unreleased]` line to CHANGELOG.md. A new package before v0.1.0 is
  tagged goes under `[0.1.0]` Added instead. A new example needs none, since the runner is its test.
- The runner runs the first `sh` block of each `examples/<name>/README.md` from the repo root and
  compares stdout with the last `text` block exactly. Sort matches and read results by session id.
  An abstention carries today's time, so anchor dates on today when one is printed, as
  `run-batches-on-sqlite` does. Otherwise a fixed date is fine, as in `match-one-session`.
- A new example copies `scan-text-for-ai-signatures`: `main.go` opens `// Command <name> ...` and
  its `go run` line (no license header), input sits inline in `main.go`, or in the example's own
  `testdata/` read from the repo root when it is large, and the
  README is a paragraph, the `sh` command and the `text` output. Index it under its package's
  heading, after the example it builds on, otherwise last under that heading.
- `TestEveryREADMEProgramRuns` (`attribution/quickstart_test.go`) runs each root README program
  against its `text` block. Byte for byte,
  `TestQuickStartIsOneProgram` holds the Quick start to `examples/match-one-session/main.go` and
  `attribution/example_test.go`'s `Example`, and `prcomplexity/readme_test.go` holds its quick start
  to `examples/score-one-pr/` and "Count a diff's lines" to `examples/count-loc/`, and
  `aidlc/readme_test.go` holds its README programs to `examples/score-maturity-readings/`
  (and `ExampleScore`), `withhold-a-thin-window/`, `track-movement/` and fold's `Example`.
  `branchlineage/readme_test.go` holds its quick start to `examples/project-ai-attribution/` and
  "Derive promotion edges" to `examples/derive-promotion-edges/`, and its other Usage programs to
  `branchlineage/readme_example_test.go`. `issuelinkage/readme_test.go` and
  `issueattribution/readme_test.go` hold their quick starts to `examples/link-a-pr-to-issues/` and
  `examples/measure-issue-ai-share/`, and their Usage programs to each `readme_example_test.go`.
  match-one-session, score-one-pr, count-loc, score-maturity-readings, withhold-a-thin-window,
  track-movement, project-ai-attribution, derive-promotion-edges, link-a-pr-to-issues and
  measure-issue-ai-share have no `go run` line,
  because a README copies them byte for byte; every other
  example's main.go has one. The other README blocks in prcomplexity and diffsize match Example
  functions in `prcomplexity/readme_example_test.go` and `diffsize/readme_example_test.go`. Only a
  new exported entry point
  gets a row in a "Choosing an entry point" table (root README, `prcomplexity/README.md`,
  `aidlc/README.md`, `branchlineage/README.md`, `issuelinkage/README.md` or
  `issueattribution/README.md`).

## Do not

- Do not edit generated `.pb.go` files. Change the `.proto` file and run `buf generate`.
- Do not move the Go floor without a dependency that needs it. scc v3.7.0 requires 1.25.2.
- Do not change `modernc.org/libc` on its own: it must match what `modernc.org/sqlite` requires.
- Do not name a source file `coverage.*`: `.gitignore` drops it silently (hence
  `aidlc/coverage_items.go`). Do not edit `aidlc/testdata/pages/` by hand; regenerate them.
- Do not edit `examples/score-held-out-prs/testdata/corpus.json` by hand. Rerun
  `go run ./examples/score-held-out-prs/gencorpus`. The runner fails a stale corpus.
