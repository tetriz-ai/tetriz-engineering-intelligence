"""Type-axis benchmark: build centroids from a train split, score a test split.

Faithful to serve time — it builds centroids through the same ``corpus.encoder``
(the exact MiniLM averaging + L2 the bundle and the SDK use) and assembles every
row through the frozen ``type_input`` recipe. Reports per-bucket precision/recall/F1,
macro-F1, and accuracy, and checks macro-F1 against the published gate (>= 0.80).

Self-contained: run it from the model-service/ directory (see the invocation in the README).
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path

from sentence_transformers import SentenceTransformer

from corpus.encoder import MODEL_NAME, MODEL_REVISION, build_centroids, normalise

from contract.input_text import domain_input, type_input
from contract.taxonomy import DOMAIN_BUCKETS, TYPE_BUCKETS
from corpus.schema import read_rows

_AXES = {
    "type": (TYPE_BUCKETS, type_input),
    "domain": (DOMAIN_BUCKETS, domain_input),
}


def _assemble_by_bucket(path: Path, axis: str) -> dict[str, list[str]]:
    """Ordered {bucket: [assembled text]} over the axis's buckets (canonical order)."""
    buckets, assemble = _AXES[axis]
    by_bucket: dict[str, list[str]] = {b: [] for b in buckets}
    for r in read_rows(path):
        by_bucket[r.bucket].append(assemble(r.title, r.branch))
    return by_bucket


def _flat(path: Path, axis: str) -> tuple[list[str], list[str]]:
    """(assembled texts, gold buckets) for a split, in file order."""
    _, assemble = _AXES[axis]
    texts, gold = [], []
    for r in read_rows(path):
        texts.append(assemble(r.title, r.branch))
        gold.append(r.bucket)
    return texts, gold


def macro_f1(y_true: list[str], y_pred: list[str], labels: list[str]) -> dict:
    per: dict[str, dict] = {}
    for lab in labels:
        tp = sum(1 for t, p in zip(y_true, y_pred) if t == lab and p == lab)
        fp = sum(1 for t, p in zip(y_true, y_pred) if t != lab and p == lab)
        fn = sum(1 for t, p in zip(y_true, y_pred) if t == lab and p != lab)
        prec = tp / (tp + fp) if (tp + fp) else 0.0
        rec = tp / (tp + fn) if (tp + fn) else 0.0
        f1 = 2 * prec * rec / (prec + rec) if (prec + rec) else 0.0
        per[lab] = {
            "precision": round(prec, 3),
            "recall": round(rec, 3),
            "f1": round(f1, 3),
            "support": sum(1 for t in y_true if t == lab),
        }
    macro = round(sum(v["f1"] for v in per.values()) / len(labels), 4)
    acc = round(sum(1 for t, p in zip(y_true, y_pred) if t == p) / max(len(y_true), 1), 4)
    return {"macro_f1": macro, "accuracy": acc, "per_bucket": per}


def run(train_csv: Path, test_csv: Path, axis: str = "type", gate: float = 0.80) -> dict:
    buckets, _ = _AXES[axis]
    labelled = _assemble_by_bucket(train_csv, axis)
    print(f"loading encoder {MODEL_NAME} …")
    model = SentenceTransformer(MODEL_NAME, revision=MODEL_REVISION)

    print(f"building {axis} centroids from train …")
    centroids = build_centroids(model, labelled)

    texts, gold = _flat(test_csv, axis)
    print(f"scoring {len(texts)} test rows …")
    emb = normalise(model.encode(texts, convert_to_numpy=True, show_progress_bar=False))
    sims = emb @ centroids.matrix.T  # cosine (both L2-normalised)
    pred = [centroids.buckets[int(i)] for i in sims.argmax(axis=1)]

    result = macro_f1(gold, pred, list(buckets))
    result["axis"] = axis
    result["train_rows"] = sum(len(v) for v in labelled.values())
    result["test_rows"] = len(texts)
    # published gate metric differs by axis: type on macro-F1, domain on accuracy.
    gate_metric = "macro_f1" if axis == "type" else "accuracy"
    result["gate"] = {"metric": gate_metric, "threshold": gate}
    result["passes_gate"] = result[gate_metric] >= gate
    return result


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--train", required=True, type=Path)
    p.add_argument("--test", required=True, type=Path)
    p.add_argument("--axis", choices=["type", "domain"], default="type")
    # published gates: type macro-F1 >= 0.80, domain accuracy >= 0.80. 0.80 (not 0.90)
    # on type because title+branch alone can't reach 0.90 (needs file/body signal).
    p.add_argument("--gate", type=float, default=None)
    p.add_argument("--out", type=Path, default=None)
    args = p.parse_args()

    gate = args.gate if args.gate is not None else 0.80  # both axes: 0.80 (see note above)
    result = run(args.train, args.test, args.axis, gate)
    print("\n== type-axis benchmark ==")
    print(json.dumps(result, indent=2, sort_keys=True))
    if args.out:
        args.out.write_text(json.dumps(result, indent=2, sort_keys=True) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
