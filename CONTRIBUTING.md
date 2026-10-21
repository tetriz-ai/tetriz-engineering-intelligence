# Contributing

## Before a change

Open an issue that says what you hit. Most changes here alter what gets attributed or scored, and
the reason matters more than the diff.

## Building and testing

The toolchain floor is Go 1.25.2. The local gate tests the floor and Go 1.26/1.27.
GitHub Actions is optional and can remain disabled: see [local validation and releases](docs/releasing.md).

```sh
go build ./...
go vet ./...
go test -race -count=1 ./...
GOTOOLCHAIN=go1.27.3 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run ./...
GOTOOLCHAIN=go1.27.3 go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
scripts/run-examples.sh
```

CI runs the same commands. Lint and the vulnerability check run on the go1.27.3 toolchain.
Their versions are pinned in the command itself, here and in `.github/workflows/ci.yml`, and
no Dependabot ecosystem covers that form, so they are bumped by hand — in both files at once.
The vulnerability check needs network access to the Go vulnerability database.
`scripts/run-examples.sh` runs every example and compares its output with the example's README.

## The bar for a pull request

- Every pull request needs a test. A behaviour change carries a test that fails before the change
  and passes after it. Name that test in the pull request.
- An attribution change says what gets attributed that did not before, or what stops being
  attributed. A change that widens or narrows the matched population without saying so is the
  hardest kind to review, because the tests still pass.
- A behaviour change adds a line under `[Unreleased]` in CHANGELOG.md.
- The lint config is not silenced inline. A rule that must be off is off in `.golangci.yml`, with
  a comment saying why.

This library will not take a signal that attributes on evidence the session did not produce, a
match that cannot explain itself, or a default that replaces an abstention with a guess.

## Commit style

One change per commit, with a subject line in the imperative mood ("Add", "Fix") under 72
characters.

## Licensing of contributions

No sign-off line and no separately signed agreement are needed. By opening a pull request you
agree that your contribution is released under the project's [LICENSE](LICENSE).
