Builds a baseline from the 60 baseline PRs of a synthetic corpus and scores its 9 held-out PRs with both formulas. A dash marks an unscoreable PR. CI checks every score below, and [gencorpus](gencorpus/main.go) generates [testdata/corpus.json](testdata/corpus.json) from a fixed seed, so it holds no real code.

```sh
go run ./examples/score-held-out-prs
```

```text
baseline: 60 PRs, coverage 9833 bps
  Go         52 PRs, median 72 SLOC in 4 files
  TypeScript 39 PRs, median 44 SLOC in 3 files
held-out PR                cncs  ratio
held-small-go               1.3    1.2
held-large-go               9.8    9.1
held-go-and-ts              8.9    8.4
held-typical-ts             5.4    5.1
held-python-only              -      -
held-docs-and-config          -      -
held-gitlab-go-vendored     5.4    5.3
held-too-large              7.0    7.0
held-not-available            -      -
```
