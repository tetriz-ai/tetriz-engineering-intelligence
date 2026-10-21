Why the ROI value is read off the basis cascade, not off a bare `Measurement`. One thin window — a single work type whose baseline and AI samples both sit under `DefaultConfig`'s min-n floor — is run two ways. `ComputeMeasurement` + `DeriveDollars` (the simple, basis-free path) prints a net gain of −$12,000 against an annual value of $0: "AI cost us the tool spend and returned nothing" and "we could not measure anything" are indistinguishable, because a `Measurement` carries no basis. `ComputeSubjectCascade` reports `INSUFFICIENT_DATA`, so an honest reader derives no dollars at all. Guard `DeriveDollars` behind a numeric basis.

```sh
go run ./examples/roi-on-a-thin-window
```

```text
-- ComputeMeasurement + DeriveDollars (basis-free: cannot withhold) --
  hours recovered  0.0
  annual value     $0
  net gain         $-12000
  payback          WITHHELD
-- ComputeSubjectCascade (basis first) --
  basis            INSUFFICIENT_DATA
  dollars          not derived (non-numeric basis)
```
