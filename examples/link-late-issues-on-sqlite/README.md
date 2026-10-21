Keeps links in a SQLite file, in a temporary directory, while a pull request names `OPS-12` a day before its issue arrives. Its row waits as partially processed, and the next run links it and re-arms the issue. The issue's own text names no pull request, so its row ends `not_found` with no references of its own, and putting the same records again changes nothing.

```sh
go run ./examples/link-late-issues-on-sqlite
```

```text
day 1: processed 1
  pr acme/api#PR-8 partially_processed waiting:OPS-12(not_found)
day 2: processed 2
  pr acme/api#PR-8 processed linked:20012
  issue 20012 to_be_reprocessed
day 3: processed 1
  pr acme/api#PR-8 processed linked:20012
  issue 20012 not_found
day 4: processed 0
  pr acme/api#PR-8 processed linked:20012
  issue 20012 not_found
```
