Checks the preconditions for two windows that should not publish. A window with no merged pull request fails `PRECONDITION_KEY_NO_SEATED_ACTIVITY`. A window where 7 of 10 pull requests carry no file list fails on path coverage.

```sh
go run ./examples/withhold-a-thin-window
```

```text
quiet window
  PRECONDITION_KEY_NO_SEATED_ACTIVITY  passed=false 0 merged pull requests in the last 90 days
  PRECONDITION_KEY_PATH_COVERAGE       passed=true  1.00 of 0 merged pull requests carry a file list; a profile needs at least 0.80
  withheld by PRECONDITION_KEY_NO_SEATED_ACTIVITY
thin window
  PRECONDITION_KEY_NO_SEATED_ACTIVITY  passed=true  10 merged pull requests in the last 90 days
  PRECONDITION_KEY_PATH_COVERAGE       passed=false 0.30 of 10 merged pull requests carry a file list; a profile needs at least 0.80
  withheld by PRECONDITION_KEY_PATH_COVERAGE
```
