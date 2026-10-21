What a walk's caps leave out, on one chain: two features merge into `integration`, which `PR-3` promotes to `staging` and `PR-4` promotes on to `main`. With the default limits the walk from `PR-4` reaches both features. With `MaxDepth 1` or `MaxRoutes 1` it stops at `PR-3`, and the result says so three ways: `DepthCapped` or `RouteCapped`, `PR-3` in `Unexpanded` (which `CarriedBy` counts as unresolvable rather than as delivered work), and a count event to the Observer.

```sh
go run ./examples/cap-a-walk
```

```text
default limits
  edges [PR-4->PR-3 PR-3->PR-1 PR-3->PR-2]
  depth capped [] route capped false unexpanded []
  carried [PR-1 PR-2] unresolvable 0
  events []
MaxDepth 1
  edges [PR-4->PR-3]
  depth capped [acme/shop] route capped false unexpanded [PR-3]
  carried [] unresolvable 1
  events [walk_depth_cap_hit{acme/shop}]
MaxRoutes 1
  edges [PR-4->PR-3]
  depth capped [] route capped true unexpanded [PR-3]
  carried [] unresolvable 1
  events [walk_route_cap_hit]
```
