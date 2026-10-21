Turns a unified diff into per-language sizes with `diffsize`, then scores them with both formulas, the way a host scores a real pull request. The diff in [testdata/change.patch](testdata/change.patch) changes Go, TypeScript, a Markdown file and a lock file. The lock file is dropped by an exclude glob, the Markdown lines are counted as documentation, and only Go and TypeScript reach the score.

```sh
go run ./examples/score-a-diff
```

```text
excluded: [web/yarn.lock]
docs lines: 2
Go          22 SLOC in 2 files (code: true)
TypeScript   5 SLOC in 1 files (code: true)
cncs: 2.9 (scoreable: true)
ratio: 2.8 (scoreable: true)
```
