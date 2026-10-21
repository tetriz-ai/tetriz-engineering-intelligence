# Releasing without GitHub Actions

Keep Actions disabled and use `main` as the repository's default branch. This is primarily a
Go library: its release is a validated source tag, not a downloadable CLI binary. The optional
Python model service has a separate build procedure and is not needed to use the Go engines.

## Local validation

`scripts/ci.sh` is the local equivalent of the checked-in CI workflow. By default it runs every
job and fails if a required database or the pinned sibling connector source is absent. It
starts or deletes no containers. Provision a dedicated PostgreSQL database first, then set
`DEFECTLINEAGE_TEST_DSN` to it. Never use a production database for these tests.

Clone `tetriz-connectors` beside this checkout. Its full commit must match `CONNECTORS_REF` in
`.github/workflows/ci.yml`; the nested wiring module has a local replacement for that sibling.
The workflow resolves the sibling under the current repository owner, so source-org
validation and the eventual public import do not depend on a hard-coded private org.
When refreshing a connector pin, use the fixed connector commit, run `go mod tidy` inside
`defectlineage/wiring` against it, review `go.mod`/`go.sum`, and run the wiring job. An old commit
from a deleted repository cannot satisfy this check. After a squash merge or fresh public
import, update the pin to the resulting connector commit before the release gate. Do not claim wiring passed when that
checkout or revision is unavailable.

Prepare a Python environment outside this clean checkout with the light dependencies used by
the workflow; these tests need no trained model or live API calls:

```sh
python3 -m venv /absolute/path/to/engineering-ci-venv
/absolute/path/to/engineering-ci-venv/bin/pip install fastapi pydantic httpx pytest numpy google-genai==1.75.0
export PYTHON=/absolute/path/to/engineering-ci-venv/bin/python
export DEFECTLINEAGE_TEST_DSN='host=127.0.0.1 port=5432 user=postgres password=local-only dbname=engineering_intelligence_test sslmode=disable'
scripts/ci.sh
```

The gate builds, vets and race-tests the root at Go 1.25.2, 1.26.10 and 1.27.3 (the current pinned matrix), then verifies the
CGO-free path, both build-tagged packages, the wiring module (Go 1.26.0), pinned lint and
vulnerability tools, all examples and the light Python tests. It requires a C compiler for the
race detector, SQLite and tree-sitter tests. `scripts/ci.sh wiring` or other named jobs can be
run independently. `CI_GO_VERSIONS` is an explicit matrix override for development; retain the
full default matrix for release evidence. Record exact versions, command output and commit IDs.

## Source release

After all local jobs pass on the final clean commit on `main`, resolve any pending changelog
date and record that exact commit. Push `main`, set the destination's default branch to `main`,
and create/push the stable `v0.1.0` tag at the validated commit. A maintainer can create a draft
GitHub Release using `gh release create v0.1.0 --repo tetriz-ai/tetriz-engineering-intelligence
--verify-tag --draft --notes-file /absolute/path/to/release-notes.txt`. Inspect it before
publishing. GitHub's generated source archives are appropriate for this library; do not imply
there are CLI executables. Verify `go get` from an independent clean module after the repository
and tag are publicly readable.

The wiring submodule remains a sibling-source integration; its local `replace` directives
are for building from source and are not a promise that `go get` installs its host adapters
without that checkout. Validate the documented source layout when testing the public import.

## Optional model-service image

Use the separate [bundle build guide](../model-service/BUILD.md) and
[model-service README](../model-service/README.md). `docker compose -f
model-service/docker-compose.yml build` builds locally and does not upload to GHCR. The compose
ports bind to localhost. Do not treat a source release as a public hosted model-service launch
or expose it directly to the internet; service hardening is a separate release decision.
A GHCR package is optional for this library. If one is later published, include source and
third-party notices, scan and test the exact image, and explicitly make the package public;
repository visibility does not set package visibility.
