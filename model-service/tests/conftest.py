"""Stub the heavy encoder dependency so the model-math tests need no torch.

encoder.py and embedding.py import sentence_transformers at module load. The
model-layer tests exercise the pure centroid/ranking math with a fake encoder,
so a lightweight stand-in keeps them fast and dependency-light. A real
sentence_transformers (if installed) is left untouched.
"""

from __future__ import annotations

import sys
import types

if "sentence_transformers" not in sys.modules:
    _st = types.ModuleType("sentence_transformers")

    class _SentenceTransformer:  # pragma: no cover - never instantiated in tests
        def __init__(self, *args, **kwargs) -> None: ...

    _st.SentenceTransformer = _SentenceTransformer
    sys.modules["sentence_transformers"] = _st
