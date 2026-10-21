Joins four packages on pull-request keys. `issuelinkage` links pull requests to Linear issues, the `attribution` signature scanner marks AI-authored descriptions, `prcomplexity` scores each pull request and `issueattribution` measures the issues. The link that only mentions `ENG-3` under a Background heading is left out, so `ENG-3` counts as unlinked, not as AI-assisted work.

```sh
go run ./examples/attribute-issues-to-ai
```

```text
link acme/web#PR-21 -> ENG-1 fixes blocked=false
link acme/web#PR-22 -> ENG-2 implements blocked=false
link acme/web#PR-23 -> ENG-3 references_context blocked=true
pr acme/web#PR-21 ai=true complexity=9.3
pr acme/web#PR-22 ai=false complexity=3.0
pr acme/web#PR-23 ai=true complexity=6.0
AI-assisted: 1 of 2 linked issues, coverage 50%, low=true
ENG-1 linked=true ai=true
ENG-2 linked=true ai=false
ENG-3 linked=false ai=false
ENG-4 linked=false ai=false
```
