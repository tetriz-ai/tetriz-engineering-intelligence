"""Model-layer math: L2 normalisation and centroid building.

Uses a fake encoder (fixed embedding per text), so it needs numpy but no model
or torch — the centroid math is exercised directly.
"""

from __future__ import annotations

import numpy as np
import pytest

from corpus.encoder import build_centroids, normalise


class FakeModel:
    """Returns the mapped embedding for each example text."""

    def __init__(self, vectors: dict[str, list[float]]) -> None:
        self.vectors = vectors

    def encode(self, texts, convert_to_numpy=True, show_progress_bar=False):
        return np.array([self.vectors[t] for t in texts], dtype="float32")


def test_normalise_makes_unit_rows_and_leaves_zero_rows():
    out = normalise(np.array([[3.0, 4.0], [0.0, 0.0]]))
    assert np.isclose(np.linalg.norm(out[0]), 1.0)
    assert np.allclose(out[1], [0.0, 0.0])  # a near-zero row is left as-is, not divided


def test_build_centroids_are_unit_length_in_bucket_order():
    model = FakeModel({"a1": [2.0, 0.0], "a2": [1.0, 0.0], "b1": [0.0, 5.0]})
    cents = build_centroids(model, {"alpha": ["a1", "a2"], "beta": ["b1"]})

    assert cents.buckets == ["alpha", "beta"]  # row order follows the dict's key order
    assert cents.matrix.dtype == np.dtype("float32")
    for row in cents.matrix:
        assert np.isclose(np.linalg.norm(row), 1.0)
    # alpha's examples both point along +x, beta's along +y.
    assert np.allclose(cents.matrix[0], [1.0, 0.0])
    assert np.allclose(cents.matrix[1], [0.0, 1.0])


def test_build_centroids_rejects_a_bucket_with_no_examples():
    with pytest.raises(ValueError):
        build_centroids(FakeModel({}), {"alpha": []})
