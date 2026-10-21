# Building the shippable centroid bundle

The bundle (`centroids-type.bin`, `centroids-domain.bin`, `manifest.json`,
`model/`) is a **generated artifact** — gitignored, released separately, never
committed. Rebuild it from the committed corpus:

```bash
# From model-service/, after installing requirements.txt:
# Build from the two committed corpora; first use downloads the pinned encoder.
# The builder also copies notices and stamps provenance automatically.
python build_bundle.py

# Verify input hashes, licence distribution and actual packaged notice files.
python -m corpus.attribution validate \
    --manifest artifacts/model-service/pr-classification/v1/manifest.json
```

`build_bundle.py` is self-contained — it reads `datasets/type_combined.csv` and
`datasets/domain_clean.csv`, embeds each PR with MiniLM, averages the examples in
each bucket into a centroid, and writes the bundle. See its module docstring for
the details.

The bundle then carries: per-axis centroids (7×384 type, 6×384 domain) + sha256,
`model_id` / `dim` / `input_recipe`, and a `provenance` block (encoder licence
Apache-2.0, corpus SPDX licence distribution, frozen taxonomy, and an
`attribution` pointer to the packaged per-source notice schedule, input CSV hashes,
and a hash inventory of all packaged notices). The SDK loads it
through `sdk/embedding.py`, which fails fast on any checksum / size / recipe
mismatch.

## Corpus provenance & notices

The bundle contains `datasets/THIRD_PARTY_NOTICES.md`, `datasets/PROVENANCE.md`,
all source-specific LICENSE/NOTICE files and the encoder licence/model card under
`datasets/licenses/encoder/`. A pointer to an external repository is not enough.
`corpus.manifest_provenance` can also package and stamp an existing bundle using
`--bundle`, `--type-corpus` and `--domain-corpus`; all inputs and their notice
inventory must live in the same dataset directory. Use a fresh bundle directory
when notice contents change; conflicting existing notice files are preserved.

The licence *distribution* the manifest stamps is a headcount; the *permissions*
behind it are the per-source copyright and licence schedule in
[`datasets/THIRD_PARTY_NOTICES.md`](datasets/THIRD_PARTY_NOTICES.md), with the
full licence texts in [`datasets/licenses/`](datasets/licenses/) and the
redistribution-rights statement in
[`datasets/PROVENANCE.md`](datasets/PROVENANCE.md). The schedule is generated
from `datasets/licenses/sources.json`, never hand-edited:

```bash
# rebuild sources.json from each source repo's real LICENSE/NOTICE — network +
# a GitHub token; run only when the corpus source set changes, then commit.
GH_TOKEN=$(gh auth token) python -m corpus.attribution refresh

# re-render the schedule from sources.json (offline, deterministic)
python -m corpus.attribution generate

# the offline guard CI runs: corpora ↔ sources.json ↔ schedule ↔ licence texts
python -m corpus.attribution validate
```

## Corpora used

- **type**: `datasets/type_combined.csv` — rule-labelled + LLM-recovered abstentions
  (best on gold: 0.838 macro-F1 in the cascade).
- **domain**: `datasets/domain_clean.csv` — LLM-labelled, out-of-fold cross-checked
  agreements.

## Serving

The centroid is the fast/abstain path (~27% coverage on public data @ ~0.92); the
LLM cascade (`evalset/cascade.py` shows the routing) carries the rest. Ship the
cascade, not the centroid alone — see `datasets/RESULTS.md`.

## Rebuilding without a runtime training CSV

The authoritative build inputs stay in the source repository:
`model-service/datasets/type_combined.csv` and `model-service/datasets/domain_clean.csv`.
Check out the source revision recorded for a release, install the pinned
`requirements.txt`, and run the commands above from `model-service/`. The encoder
revision is pinned in `corpus/encoder.py`; the generated manifest records that
revision and the filename/SHA-256 of each input CSV. Compare those hashes before
rebuilding. Identical inputs identify the training population; floating-point
results can still depend on the numeric stack and hardware.

Runtime bundles omit `dataset.csv`. The builder reads the original source CSVs
above directly; no generated training-text export is required to build centroids.
Keep the required licence/provenance inventory with the input directory. Use
`--data-dir /path/to/datasets` to select a reviewed corpus with the same schema,
examples for every taxonomy bucket, and its own complete notice inventory.

If reusing an old output directory containing `dataset.csv`, the builder refuses
before loading the model or changing output. Choose a fresh `--out` directory or
move the old bundle aside; existing files are not deleted automatically. Bundle
validation also rejects a leftover `dataset.csv`. Source-corpus redistribution
and model-artifact distribution remain distinct approval scopes; omitting the
runtime text copy does not remove the publicly committed source data or notices.
