The coverage signal on a git-flow repository: `PR-3` promotes 210 lines of `develop` and carried features with 200, inside the 0.5 to 2 times band. `release/2.0` was cut with its content already on it, so `PR-5` promotes 1,400 lines against a carried 15-line fix. `ComputeCoverage` lists that release as unexplained, the warning that history is missing.

```sh
go run ./examples/check-coverage
```

```text
carriers 2 of 3 merges into main (0.67)
explained 1 of 2 (0.50)
warning: PR-5 promoted 1400 lines, but the pull requests it carried account for only 15 (out_of_band)
```
