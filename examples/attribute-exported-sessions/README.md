The reference composition, from the files two other tools write. Sessions come from `local-session-extractor export --raw`, one file per machine; the extractor writes no person, so the program pairs each file with one. Pull requests come from tetriz-connectors' JSON-lines sink after a GitHub `fetch_prs` and `fetch_reviews` sync. The program keeps one copy of each record, takes each pull request's `#merged` record as its candidate, and reads submitted reviews for the reviewer side. The fixture is lines from a real sync against the connectors' simulator, with file lists added to PR-79 and PR-81. The abstentions show a branch only the workspace held, thin evidence, a pull request closed without a merge, and a person with no linked account, each with its reason.

```sh
go run ./examples/attribute-exported-sessions
```

```text
6 merged pull requests from 50 connector records
8 sessions in, 4 matched, 4 abstained

SESSION                  SIDE      METHOD                     CONFIDENCE  EVIDENCE      PULL REQUEST
s-named-pr               AUTHORED  DIRECT_PR_REF              1.00        UNSPECIFIED   demo-gh-startup/mobile#PR-110#merged
s-pushed-branch          AUTHORED  BRANCH_MATCH               0.81        UNSPECIFIED   demo-gh-startup/mobile#PR-120#merged
s-shared-edits           AUTHORED  FILE_OVERLAP               0.50        FILE_OVERLAP  demo-gh-startup/mobile#PR-79#merged
s-shared-edits           AUTHORED  FILE_OVERLAP               0.95        FILE_OVERLAP  demo-gh-startup/mobile#PR-81#merged
s-reviewed-pr            REVIEWED  DIRECT_PR_REF              1.00        UNSPECIFIED   demo-gh-startup/mobile#PR-117#merged
s-workspace-branch-only  -         abstained: NO_MATCHING_PR  -           -             -
s-unrelated              -         abstained: NO_MATCHING_PR  -           -             -
s-closed-pr              -         abstained: NO_MATCHING_PR  -           -             -
s-unlinked-person        -         abstained: VCS_NOT_LINKED  -           -             -
```
