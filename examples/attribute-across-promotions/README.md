Three engines joined on pull-request keys, none importing another: a feature merged into `develop`, then `develop` promoted to `main`. `vcs.Record` has the same fields as `attribution`'s record, so one fetch feeds both. `attribution` matches the session to the feature, `branchlineage` finds the promotion that carried it to `main`, and the `aidlc` fold counts AI pull requests merged into a shipped branch: 0 from the direct matches, and 1 once the promotion is credited.

```sh
go run ./examples/attribute-across-promotions
```

```text
attribution: session s1 produced acme/shop#PR-7#merged
branchlineage: acme/shop#PR-8#merged carried it to main: [acme/shop#PR-8#merged acme/shop#PR-7#merged]
aidlc: AI pull requests shipped to main, direct only: 0
aidlc: AI pull requests shipped to main, with lineage: 1
```
