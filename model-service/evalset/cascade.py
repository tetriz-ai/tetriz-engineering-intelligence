"""Full-cascade eval: embedding first, escalate low-confidence rows to the LLM.

Mirrors the served flow (minus the Layer-0 rules, whose cherry-pick output maps
to an OUT-of-taxonomy bucket): the centroid predicts; if its top score and margin
clear the routing thresholds it is accepted (fast path); otherwise the axis
escalates to the LLM. Reports overall accuracy/macro-F1 vs gold, plus the
coverage split (how much the embedding answers vs the LLM) and each path's own
accuracy — so you can see where the accuracy actually comes from.

Thresholds are the routing constants: score >= 0.35 AND
margin >= 0.08 to accept the embedding; else escalate.
"""

from __future__ import annotations

import argparse
import json
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

from sentence_transformers import SentenceTransformer

from corpus.encoder import MODEL_NAME, MODEL_REVISION, build_centroids, normalise

from contract.input_text import domain_input, type_input
from contract.taxonomy import DOMAIN_BUCKETS, TYPE_BUCKETS
from corpus.schema import read_rows
from sdk.llm import disambiguate

_AXES = {"type": (TYPE_BUCKETS, type_input), "domain": (DOMAIN_BUCKETS, domain_input)}
MIN_SCORE = 0.35
MIN_MARGIN = 0.08


def _macro(y_true, y_pred, labels) -> dict:
    f1s = []
    for L in labels:
        tp = sum(1 for t, p in zip(y_true, y_pred) if t == L and p == L)
        fp = sum(1 for t, p in zip(y_true, y_pred) if t != L and p == L)
        fn = sum(1 for t, p in zip(y_true, y_pred) if t == L and p != L)
        pr = tp / (tp + fp) if (tp + fp) else 0.0
        rc = tp / (tp + fn) if (tp + fn) else 0.0
        f1s.append(2 * pr * rc / (pr + rc) if (pr + rc) else 0.0)
    acc = sum(1 for t, p in zip(y_true, y_pred) if t == p) / max(len(y_true), 1)
    return {"accuracy": round(acc, 4), "macro_f1": round(sum(f1s) / len(labels), 4)}


def run(train_csv: Path, gold_csv: Path, axis: str, provider, *, workers: int = 5, topk: int = 0) -> dict:
    buckets, assemble = _AXES[axis]
    train = read_rows(train_csv)
    gold = read_rows(gold_csv)

    by = {b: [] for b in buckets}
    for r in train:
        by[r.bucket].append(assemble(r.title, r.branch))
    model = SentenceTransformer(MODEL_NAME, revision=MODEL_REVISION)
    cen = build_centroids(model, by)

    emb = normalise(model.encode([assemble(r.title, r.branch) for r in gold],
                                 convert_to_numpy=True, show_progress_bar=False, batch_size=256))
    sims = emb @ cen.matrix.T

    preds: list[str] = [""] * len(gold)
    path: list[str] = [""] * len(gold)
    escalate: list[int] = []
    for i in range(len(gold)):
        row = sorted(((float(sims[i][j]), cen.buckets[j]) for j in range(len(cen.buckets))), reverse=True)
        top_score, top_bucket = row[0]
        margin = top_score - (row[1][0] if len(row) > 1 else 0.0)
        if top_score >= MIN_SCORE and margin >= MIN_MARGIN:
            preds[i], path[i] = top_bucket, "embedding"
        else:
            path[i] = "llm"
            escalate.append(i)

    # LLM-resolve escalated rows (in parallel)
    def _top_buckets(i: int, k: int) -> list[str]:
        order = sorted(range(len(cen.buckets)), key=lambda j: sims[i][j], reverse=True)
        return [cen.buckets[j] for j in order[:k]]

    def _llm(i: int) -> tuple[int, str]:
        g = gold[i]
        emb_top = _top_buckets(i, 1)[0]
        # topk==0: give the LLM the full taxonomy; topk>0: production-L2 style, the
        # embedding's top-k. The SDK's disambiguate builds the prompt from the frozen
        # bucket definitions and returns a chosen slug (or None on an unusable reply).
        cands = _top_buckets(i, topk) if topk > 0 else list(buckets)
        pick = disambiguate(provider, axis, g.title, g.branch, cands, allow_null=False)
        return i, pick if pick is not None else emb_top  # fall back to embedding top-1

    if escalate:
        with ThreadPoolExecutor(max_workers=max(1, workers)) as pool:
            for i, b in pool.map(_llm, escalate):
                preds[i] = b

    gold_labels = [r.bucket for r in gold]
    overall = _macro(gold_labels, preds, list(buckets))
    emb_idx = [i for i in range(len(gold)) if path[i] == "embedding"]
    llm_idx = escalate
    return {
        "axis": axis,
        "gold_rows": len(gold),
        "overall": overall,
        "coverage": {
            "embedding": len(emb_idx),
            "llm": len(llm_idx),
            "embedding_pct": round(100 * len(emb_idx) / max(len(gold), 1), 1),
        },
        "embedding_path_acc": round(sum(1 for i in emb_idx if preds[i] == gold_labels[i]) / max(len(emb_idx), 1), 4),
        "llm_path_acc": round(sum(1 for i in llm_idx if preds[i] == gold_labels[i]) / max(len(llm_idx), 1), 4),
    }


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--train", required=True, type=Path)
    p.add_argument("--gold", required=True, type=Path)
    p.add_argument("--axis", required=True, choices=["type", "domain"])
    p.add_argument("--workers", type=int, default=5)
    p.add_argument("--topk", type=int, default=0, help="0=full taxonomy on escalation; k>0=restrict LLM to embedding's top-k")
    args = p.parse_args()

    from sdk.llm import GeminiProvider
    res = run(args.train, args.gold, args.axis, GeminiProvider(), workers=args.workers, topk=args.topk)
    print(json.dumps(res, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
