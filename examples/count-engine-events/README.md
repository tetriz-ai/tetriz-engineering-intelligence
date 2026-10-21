A custom `Observer`, the engine's extension point for metrics, passed in `Params` rather than registered anywhere. It counts every event by its labels and keeps the explanatory ones. One session clears the branch gate, one is refused, and one person's candidate store fails, so the Observer hears why that session was left pending.

```sh
go run ./examples/count-engine-events
```

```text
3 sessions in, 2 results

2  anchor_effect{effect=unmeasurable,ide_type=kiro,source=engine}
1  anchor_effect{effect=unmeasurable,ide_type=other,source=engine}
1  branch_gate_evaluations{ide_type=IDE_TYPE_KIRO,org_id=my-org,outcome=corroborated}
1  branch_gate_evaluations{ide_type=IDE_TYPE_KIRO,org_id=my-org,outcome=no_evidence}
1  person_skipped{reason=candidate_fetch_error}
1  signal_hits{method=SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH}

person_skipped person=grace reason=candidate_fetch_error error="listing authored candidates: candidate store unavailable"
```
