#!/usr/bin/env bash
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT
# Local equivalent of the checked-in workflow; requires no GitHub Actions.
set -euo pipefail
cd "$(dirname "$0")/.."
root=$PWD
jobs=${*:-"test offline tagged wiring lint vulncheck examples python"}
for job in $jobs; do
  case "$job" in test|offline|tagged|wiring|lint|vulncheck|examples|python) ;;
    *) echo "unknown job: $job" >&2; exit 2 ;;
  esac
done
need_db() {
  if [ -z "${DEFECTLINEAGE_TEST_DSN:-}" ]; then
    echo 'Set DEFECTLINEAGE_TEST_DSN to a dedicated test PostgreSQL database; skipping its tests is not a CI pass.' >&2
    exit 1
  fi
}
for job in $jobs; do
  echo "=== $job"
  case "$job" in
    test)
      need_db
      for version in ${CI_GO_VERSIONS:-go1.25.2 go1.26.10 go1.27.3}; do
        export GOTOOLCHAIN=$version
        go version
        go build ./...
        go vet ./...
        go test -race -count=1 ./...
      done
      ;;
    offline)
      for version in ${CI_GO_VERSIONS:-go1.25.2 go1.26.10 go1.27.3}; do
        GOTOOLCHAIN=$version CGO_ENABLED=0 DEFECTLINEAGE_TEST_DSN= go build ./...
        GOTOOLCHAIN=$version CGO_ENABLED=0 DEFECTLINEAGE_TEST_DSN= go test -count=1 ./...
      done
      ;;
    tagged)
      export GOTOOLCHAIN=go1.25.2
      for entry in astcompare:defectlineage/astcompare defectlineagequeue:defectlineage/queue; do
        tag=${entry%%:*}; directory=${entry#*:}
        go build -tags "$tag" ./...
        go vet -tags "$tag" "./$directory/..."
        go test -tags "$tag" -race -count=1 "./$directory/..."
      done
      ;;
    wiring)
      need_db
      # Keep the single workflow pin as the shared contract for both runners.
      expected=$(sed -n 's/^      CONNECTORS_REF: //p' .github/workflows/ci.yml)
      if ! [[ "$expected" =~ ^[a-f0-9]{40}$ ]]; then
        echo 'CONNECTORS_REF must be one full commit SHA in .github/workflows/ci.yml' >&2; exit 1
      fi
      sibling="$root/../tetriz-connectors"
      test -f "$sibling/go.mod" || { echo "missing sibling clone: $sibling" >&2; exit 1; }
      test "$(git -C "$sibling" rev-parse HEAD)" = "$expected" || {
        echo "sibling connectors must be at $expected" >&2; exit 1;
      }
      test -z "$(git -C "$sibling" status --porcelain)" || {
        echo 'sibling connectors has uncommitted changes' >&2; exit 1;
      }
      (cd defectlineage/wiring
       export GOTOOLCHAIN=go1.26.0
       go build -mod=readonly ./...
       go vet -mod=readonly ./...
       go test -mod=readonly -tags astcompare -race -count=1 ./...)
      ;;
    lint)
      GOTOOLCHAIN=go1.27.3 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run ./...
      ;;
    vulncheck)
      GOTOOLCHAIN=go1.27.3 go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
      ;;
    examples) GOTOOLCHAIN=go1.27.3 scripts/run-examples.sh ;;
    python)
      python=${PYTHON:-python3}
      (cd model-service; PYTHONPATH=. "$python" -m pytest tests/ -q)
      ;;
  esac
  echo "=== $job: passed"
done
