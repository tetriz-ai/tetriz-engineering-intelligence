"""rank(): cosine ranking of centroids for one axis.

Builds an EmbeddingBundle without its disk/model loader (object.__new__) and
sets a fake encoder, so the real rank() path — text assembly, L2, dot product,
argsort — runs with no model or torch.
"""

from __future__ import annotations

import numpy as np

from sdk.embedding import EmbeddingBundle, _Axis, _l2


class FakeModel:
    """Encodes any text to one fixed query vector."""

    def __init__(self, q: list[float]) -> None:
        self.q = q

    def encode(self, texts, convert_to_numpy=True, show_progress_bar=False):
        return np.array([self.q], dtype="float32")


def test_l2_makes_a_unit_vector():
    assert np.isclose(np.linalg.norm(_l2(np.array([3.0, 4.0]))), 1.0)


def _bundle(axis_buckets, matrix, query):
    b = EmbeddingBundle.__new__(EmbeddingBundle)  # skip the disk/model loader
    b._axes = {"type": _Axis(list(axis_buckets), _l2(np.array(matrix, dtype="float32")))}
    b.model = FakeModel(query)
    return b


def test_rank_orders_buckets_by_cosine_best_first():
    # centroid x -> +x axis, centroid y -> +y axis; query leans +x, so x wins.
    b = _bundle(["x", "y"], [[1.0, 0.0], [0.0, 1.0]], query=[0.9, 0.1])
    ranked = b.rank("type", "add a rate limiter", "feat/rl")

    assert [bucket for bucket, _ in ranked] == ["x", "y"]
    assert ranked[0][1] > ranked[1][1]
    # cosine of the (normalised) query with the +x centroid.
    assert np.isclose(ranked[0][1], 0.9 / np.hypot(0.9, 0.1))


def test_rank_returns_every_bucket():
    b = _bundle(["x", "y", "z"], [[1.0, 0.0], [0.0, 1.0], [-1.0, 0.0]], query=[1.0, 0.0])
    ranked = b.rank("type", "t", "b")
    assert {bucket for bucket, _ in ranked} == {"x", "y", "z"}
