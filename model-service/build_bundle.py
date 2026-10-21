"""Build the centroid bundle this classifier runs on — self-contained, no
external services needed.

Run it once from this model-service/ directory:

    python build_bundle.py

It reads the two committed corpora (datasets/type_combined.csv and
datasets/domain_clean.csv), turns each labelled PR into the per-axis input text,
embeds every example with the MiniLM model, averages the examples in each bucket
into one centroid, and writes the bundle the SDK and servers load:

    artifacts/model-service/pr-classification/v1/
        centroids-type.bin      one averaged vector per type bucket
        centroids-domain.bin    one averaged vector per domain bucket
        manifest.json           model name, size, input recipe, per-axis checksums
        model/                  the MiniLM model itself (so serve-time loads the
                                exact model that built the centroids)
        datasets/               corpus and encoder licence/provenance notices

The MiniLM model (all-MiniLM-L6-v2, Apache-2.0) downloads from Hugging Face on
first run and is then cached. The builder includes the corpus/encoder notices
and stamps licences, provenance and training-input hashes automatically (BUILD.md).
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
from pathlib import Path

from sentence_transformers import SentenceTransformer

from contract.input_text import INPUT_RECIPE, domain_input, type_input
from contract.taxonomy import AXIS_DOMAIN, AXIS_TYPE, DOMAIN_BUCKETS, TYPE_BUCKETS
from corpus import manifest_provenance
from corpus.encoder import EMBEDDING_DIM, MODEL_NAME, MODEL_REVISION, build_centroids

BUNDLE_SUBPATH = "model-service/pr-classification/v1"  # the bundle's version/path

# axis -> (its bucket order, how to assemble a row's input, which CSV holds it)
_AXES = {
    AXIS_TYPE: (TYPE_BUCKETS, type_input, "type_combined.csv"),
    AXIS_DOMAIN: (DOMAIN_BUCKETS, domain_input, "domain_clean.csv"),
}


def _load_rows(path: Path, assemble) -> dict[str, list[str]]:
    """Group a corpus CSV by bucket, assembling each row's input text."""
    by_bucket: dict[str, list[str]] = {}
    with path.open(encoding="utf-8") as f:
        for row in csv.DictReader(f):
            by_bucket.setdefault(row["bucket"], []).append(assemble(row["title"], row["branch"]))
    return by_bucket


def main() -> None:
    p = argparse.ArgumentParser(description="Build the centroid bundle (no external services needed).")
    p.add_argument("--out", type=Path, default=Path("artifacts"),
                   help="artifact root; the bundle is written under <out>/" + BUNDLE_SUBPATH)
    p.add_argument("--data-dir", type=Path, default=Path("datasets"),
                   help="dir holding type_combined.csv + domain_clean.csv")
    args = p.parse_args()

    out_dir = args.out / BUNDLE_SUBPATH
    legacy_export = out_dir / "dataset.csv"
    if legacy_export.exists() or legacy_export.is_symlink():
        raise ValueError("bundle contains legacy dataset.csv; use a fresh --out directory "
                         "or move the old bundle aside before rebuilding")

    print(f"loading model {MODEL_NAME} (first run downloads it) …")
    model = SentenceTransformer(MODEL_NAME, revision=MODEL_REVISION)
    # Probe the output size by encoding one string — the accessor method name
    # varies across sentence-transformers versions, so don't rely on it.
    dim = int(model.encode(["x"], convert_to_numpy=True, show_progress_bar=False).shape[1])
    if dim != EMBEDDING_DIM:
        raise ValueError(f"model size {dim} != expected {EMBEDDING_DIM}")

    out_dir.mkdir(parents=True, exist_ok=True)
    manifest: dict = {
        "version": BUNDLE_SUBPATH,
        "model_id": MODEL_NAME,
        "model_revision": MODEL_REVISION,
        "dim": dim,
        "input_recipe": dict(INPUT_RECIPE),
        "axes": {},
    }

    for axis, (bucket_order, assemble, filename) in _AXES.items():
        by_bucket = _load_rows(args.data_dir / filename, assemble)
        total = sum(len(v) for v in by_bucket.values())
        print(f"{axis}: {total} rows across {len(bucket_order)} buckets")
        missing = [b for b in bucket_order if b not in by_bucket]
        if missing:
            raise ValueError(f"corpus is missing examples for buckets: {missing}")
        # Build in canonical bucket order so the centroid row index is stable.
        ac = build_centroids(model, {b: by_bucket[b] for b in bucket_order})
        buckets, matrix = ac.buckets, ac.matrix
        raw = matrix.tobytes(order="C")
        (out_dir / f"centroids-{axis}.bin").write_bytes(raw)
        manifest["axes"][axis] = {
            "buckets": buckets,
            "shape": [int(matrix.shape[0]), int(matrix.shape[1])],
            "sha256": hashlib.sha256(raw).hexdigest(),
        }

    (out_dir / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    model.save(str(out_dir / "model"))
    manifest_provenance.augment(out_dir, args.data_dir / "type_combined.csv", args.data_dir / "domain_clean.csv")

    print(f"\nwrote bundle to {out_dir}")
    for axis, meta in manifest["axes"].items():
        print(f"  {axis}: {meta['shape'][0]} buckets, sha256={meta['sha256'][:12]}…")
    print("  included corpus/encoder licences, notices and input hashes; see BUILD.md")


if __name__ == "__main__":
    main()
