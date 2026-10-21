How a repository's promotion flow is found and kept: `DetectFlow` suggests `develop->main` from three merges against one back-merge, and `ValidateFlow` confirms a person could accept it as it stands. The derived edges prove both directions, and `ResolveFlow` refuses `main->develop` because it leaves the tracked branch. A person's confirmed flow is used as given, with `Observed` showing what the edges prove beside it.

```sh
go run ./examples/resolve-a-merge-flow
```

```text
suggested develop->main from 3 merges
confirmable: true
edge pairs [develop->main main->develop]
inferred [develop->main] terminals [main]
confirmed [develop->main release->main] observed [develop->main]
```
