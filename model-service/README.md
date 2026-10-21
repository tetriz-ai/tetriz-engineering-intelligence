# PR Work-Type Classifier (model-service)

The Python model service for the [`prworktype`](../prworktype/README.md) engine in
tetriz-engineering-intelligence. It serves the MiniLM embedding and the LLM that the
Go package calls over HTTP. Run the
commands below from this `model-service/` directory.

Classifies a pull request's **type** (what kind of change) and **domain** (which
area) from its title + branch, via a three-layer cascade:

```
Layer 0  rules       bot-author → chore-deps, cherry-pick        (deterministic)
Layer 1  embedding   MiniLM + centroids, confident-or-abstain    (fast path)
Layer 2  LLM         disambiguates what the embedding can't call  (accuracy path)
```

- **type** (always resolved): `feature, bugfix, refactor, chore-deps, test, docs, performance`
- **domain** (nullable): `infra-devops, data-pipeline, observability, security-compliance, ui-design, ai-ml`

Ships in **Python** (library, CSV batch runner, HTTP services) and **Go**
(orchestrator + CSV runner). The model runs in Python (MiniLM); everything else —
rules, routing, storage — is available in both. **No model is bundled you can't
inspect, and no LLM is hard-wired** — you bring your own (hosted or self-hosted).

## Setup (once)

Run everything from this `model-service/` directory. You need Python 3.10+.

```bash
python3 -m venv .venv && . .venv/bin/activate   # a recent Python refuses to install outside one
pip install -r requirements.txt                 # numpy + the MiniLM model library
python build_bundle.py                          # builds the model bundle
```

The install pulls PyTorch and the bundle build downloads MiniLM: together about **1 GB**,
and 10 to 60 minutes the first time. Both are one-off — every way of running below loads
the one bundle this produces.

`build_bundle.py` reads the committed public corpus and writes the bundle to
`artifacts/model-service/pr-classification/v1/`. That one bundle is what every way
below loads — build it once, then pick whichever way suits you. (To stamp
licence + provenance into the bundle, see [`BUILD.md`](BUILD.md).)

## Quick start — classify a CSV

```bash
# CSV needs a `title` column; branch/author_login/files/body are optional
python classify_csv.py --in prs.csv --out results.csv                     # embedding-only
python classify_csv.py --in prs.csv --out results.csv --llm               # full cascade (Gemini)
python classify_csv.py --in prs.csv --out results.csv --llm --db "$DSN"   # + store to Postgres
```

Output: `type_bucket, type_method, type_score, domain_bucket, domain_method`
(method ∈ rule / embedding / llm / abstain). Rows the embedding can't call
confidently escalate to the LLM (or abstain, in embedding-only mode).

## Use it from code (Python SDK)

```python
from sdk import PRWorkTypeClassifier, GeminiProvider

clf = PRWorkTypeClassifier("artifacts/…/v1")                       # embedding-only
clf = PRWorkTypeClassifier(bundle, llm_provider=GeminiProvider())  # full cascade
r = clf.classify("fix: null deref on empty input", "fix/npe")
r.type.bucket    # "bugfix"    r.type.method   # RULE / EMBEDDING / LLM / ABSTAIN
r.domain.bucket  # None        r.type.score    # cosine when method == EMBEDDING
```

`sdk/` — `rules.py` (L0), `embedding.py` (L1, loads the bundle), `llm.py` (L2),
`classifier.py` (cascade). Stateless and self-contained. `python -m sdk.example`
smoke-tests it.

## Bring your own LLM (nothing self-hosted is bundled)

The LLM layer is the `LLMProvider` protocol with two reference adapters — pick one
or write your own:

- **`GeminiProvider`** — your own Gemini key.
- **`OpenAICompatibleProvider`** — any `/chat/completions` endpoint: **self-hosted**
  (Ollama, vLLM, LM Studio) or hosted (OpenAI, Together, Groq, Gemini-compat).

```bash
python classify_csv.py --in prs.csv --out results.csv --llm \
    --provider openai --base-url http://localhost:11434/v1 --model llama3.1   # local
```

## Run it as a service

`server/` + `docker-compose.yml` — a model server (`/embed-classify`,
`/llm-classify`; resident MiniLM + bundle), an orchestrator (`/classify`: rules +
routing + Postgres, degrades to embedding-only if the LLM is down), and Postgres.

```bash
# after Setup (the bundle must exist under artifacts/), then:
export POSTGRES_PASSWORD=…        # choose your own; Postgres refuses to start without it
GEMINI_API_KEY=… docker compose up --build
curl -s localhost:8080/classify -H 'content-type: application/json' \
     -d '{"title":"fix: null deref","branch":"fix/npe"}'
```

`POSTGRES_PASSWORD` is required for **every** `docker compose` command here, including
one that starts a single service — Compose resolves every variable in the file before it
starts anything, so a missing value stops the run with
`required variable POSTGRES_PASSWORD is missing a value` before any container exists.
There is no default on purpose: the password is yours to pick.

## Classify harness artifacts into AIDLC stages

The model server also serves `POST /classify-stage` for the [`aidlc`](../aidlc/README.md) engine:
which lifecycle stage each AI-harness artifact (a skill, agent or command) serves, the stage map
`fold.Facts.StageMap` reads. The Go client is [`aidlc/stageclient`](../aidlc/stageclient/). It is
**Gemini only** (`gemini-3.5-flash`), with the key the server starts with (`GEMINI_API_KEY`); without
one it answers `503`.

```bash
curl -s localhost:8000/classify-stage -H 'content-type: application/json' -d '{"artifacts": [
  {"id": "a1", "name": "review-pr", "type": "HARNESS_ARTIFACT_TYPE_SKILL"},
  {"id": "a2", "name": "pre-commit", "type": "HARNESS_ARTIFACT_TYPE_HOOK"}]}'
# {"results": [{"id": "a1", "outcome": "SUCCEEDED", "bucket": "review", "stage": "AIDLC_STAGE_REVIEW",
#               "votes": ["review", "review", "review"]},
#              {"id": "a2", "outcome": "SKIPPED", "bucket": null, "stage": null, "votes": []}],
#  "failed_calls": 0}
```

Each artifact carries `id` (unique in the request), `name`, `type` (an `aidlc.v1.HarnessArtifactType`
name) and optionally `description` and `body`; a request carries at most 250, and anything else
malformed answers `422`. The skills, agents and commands go to the model three times, each time
shuffled into batches of 25, with the production classifier's prompt (in `stage/prompt.py`). Sample
`s` is shuffled as the production classifier shuffles it, with Go's `math/rand` seeded `s + 1`
(ported bit for bit in `stage/gorand.py`), so the same artifacts in the same order get the
production classifier's exact batches. Labels are then stable from call to call except for rare
variation in the model itself, which answers at temperature 0 but not perfectly deterministically:
across two identical calls on 692 artifacts, one verdict flipped. The
model puts each in one of sixteen buckets: the nine lifecycle stages, six kinds of real work that is
no stage, or `none`. Each result's `outcome` says what came of it:

- `SUCCEEDED`: two of the three samples agree. `bucket` is their answer and `stage` its
  `AidlcStage` name, `AIDLC_STAGE_UNSPECIFIED` for a bucket that is no stage.
- `UNRESOLVED`: all three samples answered and no two agree. No verdict.
- `DEFERRED`: a sample's call failed, its reply did not parse, or it left this artifact out, and the
  votes that came back do not agree. No verdict.
- `SKIPPED`: not a skill, agent or command, so never sent.

`failed_calls` counts the model calls (one per sample per batch of 25) that failed, returned a reply
that did not parse, or answered fewer artifacts than the batch held. A description or body longer
than 50,000 characters answers `422` naming the artifact; `aidlc/stageclient` truncates both to
that length before sending.

`GET /health` says whether this endpoint can answer: its `stage_classifier` field is `"ready"`
when the server started with a `GEMINI_API_KEY` and `"no_key"` when it did not (the endpoint then
answers `503`). The existing `status` and `model_id` fields are unchanged.

Only `SUCCEEDED` belongs in a stage map; leave every other outcome out, which reads as unknown.
**Do not mix organisations in one request**: artifacts share batches, and a batch's neighbours inform
each answer. Descriptions and bodies are redacted (known credential shapes, secret assignments, email
addresses, home-directory names) before they are sent, which is a pattern set, not a proof. Each model
call has a 55-second budget and is billed to your key: three calls per batch of 25.

**The server needs a work-type bundle to start, even for stage classification**: it refuses to start
without `BUNDLE_DIR` pointing at a built bundle (see Setup), whichever endpoints you call.

## Go

The Go client lives at [`../prworktype`](../prworktype/README.md): Layer-0 rules +
routing + Postgres storage **in Go**, calling this model server for the embedding +
LLM layers. `prworktype.Classifier` is the library; `prworktype/cmd/classify` is the
CSV runner (`--llm`, `--db`). The model stays in Python (a Go-native build would need ONNX).

Start this model server first (`POSTGRES_PASSWORD=… docker compose up model-server`, or
`pip install -r server/requirements.txt` then
`BUNDLE_DIR=artifacts/model-service/pr-classification/v1 uvicorn server.model_server:app --port 8000`
— the HTTP services have their own dependency list, which the Setup above does not install), then from this `model-service/` directory:

```bash
# From the REPOSITORY ROOT, not this directory — the Go package lives there.
cd .. && go run ./prworktype/cmd/classify --in prs.csv --out results.csv --model-url http://localhost:8000
```

## Accuracy (honest, independent)

Measured against a **substance-labelled GOLD set** — 594 type PRs (and 245 domain
PRs) from fresh repos with zero training overlap, labelled by what each PR actually
does (body + files), with
a human-QC'd slice. Full numbers in [`datasets/RESULTS.md`](datasets/RESULTS.md).

| axis   | embedding-only        | full cascade                 | gate            | status   |
|--------|-----------------------|------------------------------|-----------------|----------|
| type   | acc 0.705 / F1 0.627  | **acc 0.874 / F1 0.838**     | macro-F1 ≥ 0.80 | **PASS** |
| domain | acc 0.702 / F1 0.610  | **acc 0.841 / F1 0.772**     | accuracy ≥ 0.80 | **PASS** |

Coverage — type: embedding 27% @0.92 / LLM 73% @0.86 · domain: embedding 47% @0.90 / LLM 53% @0.79.

Takeaways: the **embedding is the fast/abstain path, the LLM cascade is the
accuracy path**; both axes pass on the cascade. The type bar is macro-F1 ≥ 0.80 —
0.90 isn't reachable from title+branch alone (that needs serve-time file/body
signal), and 0.80 matches the domain bar. In-distribution numbers mislead — always
eval on an independent, differently-labelled set.

---

## Datasets, build and eval (for maintainers)

The shipped centroid is built from the **committed public corpus** in `datasets/` —
`type_combined.csv` (type) and `domain_clean.csv` (domain) — rows drawn from permissively-licensed
public repos, with no customer or private-organisation data. (Rows do carry each PR's
public GitHub URL — so the public PR number is present — and some branch names include
contributor logins; all of it is already public.) The raw-corpus generation
(GitHub mining + labelling) was done upstream and is **not part of this repo**; the finished corpora,
the GOLD eval set and the leakage-checked splits ship here as data.

Each copied row's source repository, licence and copyright are attributed in
[`datasets/THIRD_PARTY_NOTICES.md`](datasets/THIRD_PARTY_NOTICES.md) (full licence texts in
[`datasets/licenses/`](datasets/licenses/)); the redistribution-rights basis and approved
exceptions are recorded in [`datasets/PROVENANCE.md`](datasets/PROVENANCE.md).

- **`contract/`** — the frozen contract (must not drift between build and serve): the taxonomy, the
  per-axis input recipe (type title-first, domain branch-first), and branch cleaning.
- **`corpus/`** — the shared MiniLM encoder + centroid math (`encoder.py`, used by both the bundle
  builder and the benchmark), the row schema (`schema.py`), the embedding benchmark (`benchmark.py`),
  `manifest_provenance.py` (pins the encoder licence + corpus SPDX licences into the manifest), and
  `attribution.py` (generates the per-source notice schedule and guards it against the corpora).
- **`evalset/`** — the full-cascade evaluator (`cascade.py`): embedding first, LLM on escalation,
  scored against the GOLD set.
- **`datasets/`** — the committed build corpora (`type_combined.csv`, `domain_clean.csv`), the GOLD
  benchmark (`gold_*.csv`), and the leakage-checked splits + benchmark reports.

Build the shippable bundle (centroids + manifest + model) — see [`BUILD.md`](BUILD.md).

Reproduce the eval:

```bash
python -m corpus.benchmark --axis type   --train datasets/type_combined.csv --test datasets/gold_type.csv
python -m evalset.cascade  --axis type   --train datasets/type_combined.csv --gold datasets/gold_type.csv
python -m evalset.cascade  --axis domain --train datasets/domain_clean.csv  --gold datasets/gold_domain.csv
```

### Acceptance gates (portfolio note, module 8)

Public centroid has documented provenance + redistribution rights; manifest pins
hashes + licences (fail-fast on mismatch); immutable non-overlapping train/val/test
with leakage checks; a redistributable benchmark reproduces the published gates;
each layer exposes provenance and the LLM/tracing adapters are replaceable.
