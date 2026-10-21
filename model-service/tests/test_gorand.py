# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

"""stage.gorand reproduces Go's math/rand Shuffle bit for bit. The expected permutations are what
``rand.New(rand.NewSource(seed)).Shuffle`` gives on Go 1.25-1.27 for the identity order."""

from __future__ import annotations

import pytest

from stage.gorand import GoRand

GO = {
    (5, 1): [2, 0, 1, 4, 3],
    (5, 2): [3, 2, 4, 1, 0],
    (5, 3): [0, 1, 4, 2, 3],
    (25, 1): [0, 14, 10, 6, 5, 16, 21, 20, 12, 19, 3, 23, 17, 11, 7, 4, 18, 2, 1, 13, 8, 9, 24, 22, 15],
    (25, 2): [11, 10, 22, 7, 15, 5, 19, 0, 16, 14, 23, 13, 24, 20, 9, 21, 17, 3, 8, 18, 12, 2, 1, 6, 4],
    (25, 3): [0, 1, 23, 20, 3, 6, 2, 14, 24, 13, 12, 19, 10, 11, 22, 7, 5, 9, 8, 4, 18, 16, 21, 15, 17],
}


@pytest.mark.parametrize(("n", "seed"), sorted(GO))
def test_shuffle_matches_go(n, seed):
    order = list(range(n))
    GoRand(seed).shuffle(order)
    assert order == GO[(n, seed)]


def test_empty_and_single_are_untouched():
    for n in (0, 1):
        order = list(range(n))
        GoRand(1).shuffle(order)
        assert order == list(range(n))
