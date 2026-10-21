Counts the effective lines of code a two-file diff adds, with a lock file left out by an exclude glob. It is the same program as the "Count a diff's lines" section of the [diffsize README](../../diffsize/README.md#count-a-diffs-lines). The output is the Python file's source, comment and blank lines, its file count, and the excluded files.

```sh
go run ./examples/count-loc
```

```text
2 1 1 1 [poetry.lock]
```
