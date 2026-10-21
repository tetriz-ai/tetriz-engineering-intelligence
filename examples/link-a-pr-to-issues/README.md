Links one pull request to the Jira issues it names, in memory, and prints its row, the same program as the [issuelinkage README's quick start](../../issuelinkage/README.md#quick-start). `PROJ-7` follows "Fixes" and is in the branch name, so it is a direct, high-tier closing link, and `PROJ-9` follows "see", so it is a reference. `PROJ-3` is mentioned only under a Background heading, so its link carries the `negative_section` reason.

```sh
go run ./examples/link-a-pr-to-issues
```

```text
processed
10003 reference/low references_context closes=false blocking=[negative_section]
10007 direct/high fixes closes=true blocking=[]
10009 reference/low reference_only closes=false blocking=[]
```
