Runs the whole defect-lineage pipeline over one bug fix, in process, with no database and no network: enqueue, blame, score, then read the rows a user would query.

The history is deliberate. The line the fix repaired was written by `CULPRIT`, then re-indented by `REFORMAT`. A reformat is the line's last writer without having authored its behaviour, so blame alone would charge the wrong commit — the walk steps past it, which is what `BLAME_AFTER_HOPS` records.

The comparator here answers by path alone, so the code-context term is file coverage and the score is marked `degraded`. Pass `wiring.ASTComparator` for a measured syntax-tree rung; it needs a C toolchain, which is why it lives in a separate module.

```sh
go run ./examples/trace-one-bug-fix
```

The confidence re-derives from the five weighted terms: L 0.70 × 0.35 + C 0.30 × 0.20 + D 0.60 × 0.20 + R 0.709 × 0.15 + E 0 × 0.10 = 0.531.

```text
blamed: claimed=1 resolved=1
scored: candidates=1 degraded=1 fixes_resolved=1

candidate CULPRIT by dev@example.com
  reached by:  BLAME_AFTER_HOPS
  confidence:  0.531
  context:     same_file (degraded: true)
  age at fix:  31 days

fix pr-42: RESOLVED / SCORED_OK
```
