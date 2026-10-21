The engine inside its batch runner, over the SQLite reference store, for attribution that runs on a schedule and keeps its results. Three sessions match by a different signal each, one matches nothing, and one belongs to a person with no linked vendor account. A second run has nothing to do, because every session is settled or waiting out its cooldown.

```sh
go run ./examples/run-batches-on-sqlite
```

```text
first run: 5 sessions, 3 matched, 2 abstained
second run: 0 sessions

SESSION            OUTCOME         PULL REQUEST                CONFIDENCE
names-its-pr       DIRECT_PR_REF   acme/widgets#PR-101#merged  1.00
shares-a-branch    BRANCH_MATCH    acme/widgets#PR-102#merged  0.85
shares-its-edits   FILE_OVERLAP    acme/widgets#PR-102#merged  0.67
shares-its-edits   FILE_OVERLAP    acme/widgets#PR-103#merged  1.00
matches-nothing    NO_MATCHING_PR  -                           -
person-not-linked  VCS_NOT_LINKED  -                           -
```
