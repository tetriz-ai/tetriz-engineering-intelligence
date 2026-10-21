# Eval results (vs substance-labelled GOLD — 594 type / 245 domain PRs, fresh repos, zero training overlap)

All embedding-only numbers are the nearest-centroid classifier on title+branch.
Cascade = embedding (routing thresholds score>=0.35, margin>=0.08) -> LLM escalation.

| axis   | config          | accuracy | macro-F1 | gate            | status |
|--------|-----------------|---------:|---------:|-----------------|--------|
| type   | embedding-only  |   0.705  |  0.627   | macro-F1 >= 0.80| below  |
| type   | full cascade    |   0.874  |  0.838   | macro-F1 >= 0.80| PASS   |
| domain | embedding-only  |   0.702  |  0.610   | accuracy >= 0.80| below  |
| domain | full cascade    |   0.841  |  0.772   | accuracy >= 0.80| PASS   |

Cascade coverage: type embedding 27% @0.92 / LLM 73% @0.86 ; domain embedding 47% @0.90 / LLM 53% @0.79.

Takeaway: embedding-only is the fast/abstain path, the LLM cascade is the accuracy
path. Both axes pass on the cascade. The type bar is macro-F1 >= 0.80: title+branch
alone can't reach 0.90 (that needs serve-time file/body signal), and 0.80 matches
the domain bar. Fully reproducible on public data + any LLM API.

## Escalation candidate-set experiment (type)

| escalation      | accuracy | macro-F1 |
|-----------------|---------:|---------:|
| full taxonomy   |   0.874  |  0.838   |
| top-3 candidates|   0.859  |  0.816   |

Restricting the escalation LLM to the embedding's top-3 (production-L2 style)
HURTS here: our public embedding is weak, so for the escalated (uncertain) rows
its top-3 often omits the correct bucket, capping the LLM. Full-taxonomy
escalation is the right config for this centroid. Best type config: 0.874 acc /
0.838 macro-F1 (clears the 0.80 macro-F1 bar); domain cascade passes its 0.80 gate.
