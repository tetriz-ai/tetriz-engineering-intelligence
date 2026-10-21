The engine embedded in your own worker, with no runner: a map of sessions, `IsPending` to pick the batch, `Engine.Attribute`, and `Reduce` to merge each result into what is stored. When a pull request merges, the worker raises a re-attribution request on each session whose start date falls in `ReattributionWindow`, by incrementing its `ReattributionSeq`. Only those sessions reopen, and the one that pushed the branch now matches.

```sh
go run ./examples/reattribute-on-merge
```

```text
before the merge:
  long-before        abstained: SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR
  pushed-the-branch  abstained: SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR
  unrelated-recent   abstained: SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR

acme/widgets#9 merged: re-attribution requested for [pushed-the-branch unrelated-recent]
pending now: [pushed-the-branch unrelated-recent]
serviced 2 requests, withdrew 0 matches

after the merge:
  long-before        abstained: SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR
  pushed-the-branch  SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH acme/widgets#9 0.72
  unrelated-recent   abstained: SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR
```
