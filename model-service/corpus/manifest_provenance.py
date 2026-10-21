"""Augment a built centroid bundle's manifest with provenance + licenses.

The generator pins version/model/dim/recipe/per-axis sha256. The OSS acceptance
criteria additionally require the manifest to pin **licenses** — for the encoder
and for the corpus the centroids were averaged from. This reads the built
manifest + the training corpora and adds a `provenance` block: encoder licence,
frozen taxonomy, and per-axis row counts + SPDX licence distribution. It only
adds keys; it never rewrites the generator's own fields.
"""

from __future__ import annotations

import argparse
import json
import hashlib
from collections import Counter
from datetime import datetime, timezone
from pathlib import Path

from contract.taxonomy import DOMAIN_BUCKETS, TYPE_BUCKETS
from corpus.schema import read_rows
from corpus import bundle_notices

# sentence-transformers/all-MiniLM-L6-v2 is Apache-2.0.
_ENCODER_LICENSE = "Apache-2.0"
_ENCODER_SOURCE = "sentence-transformers/all-MiniLM-L6-v2"


def _corpus_summary(path: Path) -> dict:
    rows = read_rows(path)
    return {
        "rows": len(rows),
        "filename": path.name,
        "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
        "licenses": dict(sorted(Counter(r.license for r in rows).items())),
        "bucket_counts": dict(sorted(Counter(r.bucket for r in rows).items())),
    }


def augment(bundle_dir: Path, type_corpus: Path, domain_corpus: Path) -> dict:
    manifest_path = bundle_dir / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if type_corpus.resolve().parent != domain_corpus.resolve().parent:
        raise ValueError("training corpora and their notice inventory must share one directory")
    encoder = json.loads((type_corpus.parent / "licenses/encoder/source.json").read_text())
    if manifest.get("model_revision") != encoder["revision"] or manifest.get("model_id") != "all-MiniLM-L6-v2":
        raise ValueError("encoder identity differs from the bundled licence provenance")
    notice_files = bundle_notices.install(bundle_dir, type_corpus.parent)
    manifest["provenance"] = {
        "built_at": datetime.now(timezone.utc).isoformat(),
        "encoder": {"model_id": manifest.get("model_id"), "source": _ENCODER_SOURCE, "license": _ENCODER_LICENSE,
                    "revision": encoder["revision"], "attribution": "datasets/licenses/encoder/source.json"},
        "taxonomy": {"type": list(TYPE_BUCKETS), "domain": list(DOMAIN_BUCKETS), "frozen": True},
        "corpus": {"type": _corpus_summary(type_corpus), "domain": _corpus_summary(domain_corpus)},
        # Pointer to the per-source copyright/licence schedule the distribution stands for.
        "attribution": "datasets/THIRD_PARTY_NOTICES.md",
        "notice_files": notice_files,
        "redistribution": "See the packaged datasets/PROVENANCE.md for rights scope and review status. "
                          "Training CSVs remain in the source repository; the runtime bundle omits dataset.csv.",
    }
    manifest_path.write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    return manifest["provenance"]


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--bundle", required=True, type=Path, help="bundle dir holding manifest.json")
    p.add_argument("--type-corpus", required=True, type=Path)
    p.add_argument("--domain-corpus", required=True, type=Path)
    args = p.parse_args()
    prov = augment(args.bundle, args.type_corpus, args.domain_corpus)
    print(json.dumps(prov, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
