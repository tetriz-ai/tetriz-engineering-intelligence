A repository's promotion edges, the first program in the `branchlineage` README's Usage section: nine merged pull requests where features merge into `develop`, `develop` is promoted to `release` and to `main`, and `release` is promoted to `main`. `Derive` says which pull requests each promotion carried, so `PR-11` carries `PR-10`, the promotion of `develop` to `release`, which carries three features. `PR-12`'s earliest commit came after `PR-10` merged, so its window opens there and it carries only `PR-3`.

```sh
go run ./examples/derive-promotion-edges
```

```text
PR-10 develop->release carried [PR-1 PR-2 PR-4]
PR-11 release->main carried [PR-10]
PR-12 develop->main carried [PR-3]
```
