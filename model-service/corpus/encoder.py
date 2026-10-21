"""The MiniLM encoder name + centroid math — self-contained (no external services).

`build_bundle.py` and the eval scripts (`corpus/benchmark.py`, `evalset/cascade.py`)
all build centroids the same way through this module, so build-time and eval-time use
one identical embedding surface. `normalise` is the L2 normalisation that makes a dot
product equal cosine similarity; `build_centroids` averages each bucket's example
embeddings into one L2-normalised centroid.
"""

from __future__ import annotations

from dataclasses import dataclass

import numpy as np
from sentence_transformers import SentenceTransformer

# The embed-classify encoder: MiniLM-L6-v2, 384-d, Apache-2.0.
MODEL_NAME = "all-MiniLM-L6-v2"
# Pin the exact Hugging Face snapshot so a rebuilt bundle is reproducible and a
# silently-updated upstream model cannot shift the centroids. This is a permalink
# to one commit: https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2/tree/1110a243fdf4706b3f48f1d95db1a4f5529b4d41
MODEL_REVISION = "1110a243fdf4706b3f48f1d95db1a4f5529b4d41"
EMBEDDING_DIM = 384


def normalise(vectors: np.ndarray) -> np.ndarray:
    """L2-normalise row-wise so a dot product is the cosine similarity.
    A near-zero row is left as-is (scaling it would be pure floating-point noise)."""
    norms = np.linalg.norm(vectors, axis=1, keepdims=True)
    return vectors / np.where(norms < 1e-9, 1.0, norms)


@dataclass(frozen=True)
class AxisCentroids:
    """Per-axis centroid matrix plus the bucket order its rows correspond to."""

    buckets: list[str]
    matrix: np.ndarray  # (len(buckets), dim), float32, L2-normalised rows


def build_centroids(model: SentenceTransformer, labelled: dict[str, list[str]]) -> AxisCentroids:
    """Average each bucket's example embeddings into one L2-normalised centroid.

    Row index follows ``labelled``'s key order — callers pass an ordered dict keyed
    in the axis's canonical bucket order so the row index is stable. A bucket with no
    examples is an error (a missing row would silently drop it from every ranking)."""
    buckets = list(labelled.keys())
    rows: list[np.ndarray] = []
    for bucket in buckets:
        examples = labelled[bucket]
        if not examples:
            raise ValueError(f"bucket {bucket!r} has no examples — cannot build a centroid")
        emb = model.encode(examples, convert_to_numpy=True, show_progress_bar=False)
        # Normalise each example before averaging so every example counts equally,
        # then normalise the mean so the centroid itself is unit-length.
        rows.append(normalise(emb).mean(axis=0))
    return AxisCentroids(buckets=buckets, matrix=normalise(np.vstack(rows)).astype("float32"))
