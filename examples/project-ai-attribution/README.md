The `branchlineage` README's quick start, on the nine-pull-request repository with four pull requests an AI coding session produced. Of the four merges into `main`, only `PR-5` is attributed directly, and with lineage `PR-11` (which carried `PR-1` and `PR-2`) and `PR-12` (which carried `PR-3`) are credited too, making 3 of 4. `PR-10`, the promotion of `develop` to `release`, is credited but merged into `release`, so the count leaves it out, and `CarriedBy` lists what each promotion delivered.

```sh
go run ./examples/project-ai-attribution
```

```text
credit PR-10 via [PR-10 PR-1]
credit PR-11 via [PR-11 PR-10 PR-1]
credit PR-12 via [PR-12 PR-3]
merges into main credited with AI work: 1 of 4 directly, 3 of 4 with lineage
landed PR-1 by [PR-11]
landed PR-2 by [PR-11]
landed PR-3 by [PR-12]
landed PR-4 by [PR-11]
```
