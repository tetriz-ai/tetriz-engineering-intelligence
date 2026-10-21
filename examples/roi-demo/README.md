The AI ROI engine end-to-end with an in-memory `SourceReader` — no database. It builds a small cohort of per-(work-type, band, cohort) medians, runs `airoi.ComputeMeasurement` for the class-corrected, tax-adjusted lift and recovered hours, then derives the read-time dollar block with `airoi.DeriveDollars`. Start here to see the four boundary interfaces and that dollars are derived at read, never stored.

```sh
go run ./examples/roi-demo
```

```text
AI adoption share : 40.8%
Net lift          : 0.333  (gross 0.333, tax 1.00)
Hours recovered   : 203.0 (window 30d)
Per-class:
  bugfix   baseline 12.0h  ai 9.0h  matched_n=18  excluded=false
  feature  baseline 40.0h  ai 24.0h  matched_n=22  excluded=false
Dollars (FTE $75/h, $12k tool spend):
  annual value $185238  net gain $173238  ROI ×15.4  payback 0.8 mo
```
