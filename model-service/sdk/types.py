"""Public result types for the PR Work-Type classifier — slug-based, no protos.

The SDK returns plain slug strings
(the taxonomy in `contract/taxonomy.py`) so adopters need no generated code.
"""

from __future__ import annotations

from dataclasses import dataclass
from enum import Enum


class Method(str, Enum):
    """How an axis was decided — the provenance the cascade records per axis."""
    RULE = "rule"            # Layer 0 deterministic rule (bot author / cherry-pick)
    EMBEDDING = "embedding"  # Layer 1 centroid, confident
    LLM = "llm"              # Layer 2 LLM disambiguation
    ABSTAIN = "abstain"      # no confident answer and no LLM available


# Type axis is always resolved to a bucket or this fallback (never null).
UNCATEGORISED = "uncategorised"
# Cherry-picks are classified by origin, not content — a non-taxonomy marker the
# Layer-0 rule emits.
CHERRY_PICK = "cherry-pick"


@dataclass(frozen=True)
class AxisResult:
    bucket: str | None       # a taxonomy slug, UNCATEGORISED/CHERRY_PICK (type), or None (domain)
    method: Method
    score: float | None = None   # top cosine similarity when method == EMBEDDING


@dataclass(frozen=True)
class Classification:
    type: AxisResult
    domain: AxisResult       # domain.bucket is None when the PR has no specialised domain
