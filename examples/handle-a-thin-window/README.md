Builds a baseline from a young repository's 12 merged PRs, which the default gates reject with an error wrapping `ErrNotEnoughData`, so the host records the window's PRs as unscoreable. It then shows that the zero `BaselineOptions` is an error, and builds a baseline with lowered gates, starting from `DefaultBaselineOptions()`. Lower gates admit smaller windows, at the cost of noisier anchors.

```sh
go run ./examples/handle-a-thin-window
```

```text
default gates: no baseline (12 PRs, coverage 9166 bps)
new PR: <nil> false
zero options: prcomplexity: MinSampleSize 0 below 1
lowered gates: Go anchor from 11 PRs, median 62 SLOC in 2 files
new PR: 7.6 true
```
