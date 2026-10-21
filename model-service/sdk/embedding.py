"""Layer 1 — embedding classifier over the centroid bundle. Self-contained: it
reads the bundle (manifest + raw-float32 `.bin` + bundled SentenceTransformer)
directly, with no external dependencies, so the SDK stands alone.

`rank` returns the per-axis candidates ranked by cosine similarity (centroids and
the query are L2-normalised, so a dot product is the cosine). The escalation rule
(score/margin thresholds) lives in the classifier, not here.
"""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from pathlib import Path

import numpy as np
from sentence_transformers import SentenceTransformer

from contract.input_text import INPUT_RECIPE, domain_input, type_input

_ASSEMBLE = {"type": type_input, "domain": domain_input}
_MODEL_SUBDIR = "model"


def _l2(v: np.ndarray) -> np.ndarray:
    n = np.linalg.norm(v, axis=-1, keepdims=True)
    return v / np.where(n < 1e-9, 1.0, n)


@dataclass
class _Axis:
    buckets: list[str]
    matrix: np.ndarray  # (n_buckets, dim), L2-normalised float32


class EmbeddingBundle:
    """Loaded centroid bundle: the encoder + per-axis centroid matrices."""

    def __init__(self, bundle_dir: str | Path) -> None:
        bundle_dir = Path(bundle_dir)
        manifest = json.loads((bundle_dir / "manifest.json").read_text(encoding="utf-8"))
        self.dim = int(manifest["dim"])
        self.model_id = manifest["model_id"]
        # Fail fast if the served recipe drifts from what the centroids were built under.
        if manifest.get("input_recipe") and manifest["input_recipe"] != INPUT_RECIPE:
            raise ValueError("bundle input_recipe != contract INPUT_RECIPE — build/serve mismatch")
        self._axes: dict[str, _Axis] = {}
        for axis, meta in manifest["axes"].items():
            raw = (bundle_dir / f"centroids-{axis}.bin").read_bytes()
            if hashlib.sha256(raw).hexdigest() != meta["sha256"]:
                raise ValueError(f"centroids-{axis}.bin sha256 mismatch — corrupt bundle")
            rows, dim = meta["shape"]
            if dim != self.dim:
                raise ValueError(f"{axis} centroid dim {dim} != manifest dim {self.dim}")
            mat = np.frombuffer(raw, dtype="<f4").reshape(rows, dim).astype("float32")
            self._axes[axis] = _Axis(list(meta["buckets"]), _l2(mat))
        self.model = SentenceTransformer(str(bundle_dir / _MODEL_SUBDIR))
        # dim accessor name varies across sentence-transformers versions; probe instead.
        probe = self.model.encode(["x"], convert_to_numpy=True, show_progress_bar=False)
        if probe.shape[1] != self.dim:
            raise ValueError(f"bundled model dim {probe.shape[1]} != manifest dim {self.dim}")

    def axes(self) -> list[str]:
        return list(self._axes)

    def rank(self, axis: str, title: str, branch: str) -> list[tuple[str, float]]:
        """(bucket, cosine) pairs for one axis, best first."""
        text = _ASSEMBLE[axis](title, branch)
        q = _l2(self.model.encode([text], convert_to_numpy=True, show_progress_bar=False))[0]
        ax = self._axes[axis]
        sims = ax.matrix @ q
        order = np.argsort(-sims)
        return [(ax.buckets[int(i)], float(sims[int(i)])) for i in order]
