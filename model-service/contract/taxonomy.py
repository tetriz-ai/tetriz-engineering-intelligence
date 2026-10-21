"""The two PR-work-type axes and their bucket slugs.

These slugs are the *endpoint contract* — what the model endpoint returns. A
centroid is built per slug, so this list, the centroid bundle, and any consumer
enum must move in lock-step: adding a bucket means a new slug *and* a centroid
regeneration.

The ``type`` axis is always resolved to one of these 7 buckets (an
``UNCATEGORISED`` fallback is set by the LLM layer, never here). The ``domain``
axis is nullable — the embedding layer ranks all 6, and the consumer/LLM decides
whether the signal is strong enough to assign one.

This module is the frozen public contract for the PR Work-Type corpus and SDK.
It is lifted verbatim from the source task; keep it byte-identical across the
corpus generator and the served model so build and serve share one label space.
"""

from __future__ import annotations

# Axis keys used throughout the request/response contract.
AXIS_TYPE = "type"
AXIS_DOMAIN = "domain"

# type axis — what kind of change (always set for a merged PR).
TYPE_BUCKETS: tuple[str, ...] = (
    "feature",
    "bugfix",
    "refactor",
    "chore-deps",
    "test",
    "docs",
    "performance",
)

# domain axis — which area of the system (nullable; null = cross-cutting).
DOMAIN_BUCKETS: tuple[str, ...] = (
    "infra-devops",
    "data-pipeline",
    "observability",
    "security-compliance",
    "ui-design",
    "ai-ml",
)
