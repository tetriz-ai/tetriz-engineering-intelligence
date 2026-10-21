The reference indexer on SQLite, run the way a scheduled job runs it: put what was fetched, derive the queue, and after a restart the same batch changes nothing. A late pull request makes the promotion that should have carried it stale, its edges are dropped at once, and the next `Derive` replaces its whole edge set. The database lives in a temporary folder the program removes.

```sh
go run ./examples/index-on-sqlite
```

```text
derived 3
edges [PR-4->PR-1 PR-4->PR-3]
replay after restart: stale [] pending []
late PR-2 makes PR-4 stale: late_leaf
edges []
derived 2
edges [PR-4->PR-1 PR-4->PR-2 PR-4->PR-3]
```
