"""PR Work-Type classifier SDK — a self-contained rules → embedding → LLM cascade.

    from sdk import PRWorkTypeClassifier, GeminiProvider
    clf = PRWorkTypeClassifier("artifacts/model-service/pr-classification/v1")   # embedding-only
    clf = PRWorkTypeClassifier(bundle, llm_provider=GeminiProvider())          # full cascade
    r = clf.classify("fix: null deref on empty input", "fix/npe")
    r.type.bucket, r.type.method, r.domain.bucket

The model-bearing names (PRWorkTypeClassifier and the LLM providers) load lazily,
so importing a pure submodule — ``from sdk import rules`` or ``from sdk.types
import Method`` — does not pull in sentence-transformers / torch. A caller that
only orchestrates over HTTP (the orchestrator service) therefore stays light.
"""

from sdk.types import AxisResult, Classification, Method

__all__ = [
    "PRWorkTypeClassifier",
    "Classification",
    "AxisResult",
    "Method",
    "LLMProvider",
    "GeminiProvider",
    "OpenAICompatibleProvider",
    "disambiguate",
]

_LAZY = {
    "PRWorkTypeClassifier": ("sdk.classifier", "PRWorkTypeClassifier"),
    "GeminiProvider": ("sdk.llm", "GeminiProvider"),
    "LLMProvider": ("sdk.llm", "LLMProvider"),
    "OpenAICompatibleProvider": ("sdk.llm", "OpenAICompatibleProvider"),
    "disambiguate": ("sdk.llm", "disambiguate"),
}


def __getattr__(name: str):
    """Import the model-bearing names on first use (PEP 562), not at package import."""
    target = _LAZY.get(name)
    if target is None:
        raise AttributeError(f"module {__name__!r} has no attribute {name!r}")
    import importlib

    return getattr(importlib.import_module(target[0]), target[1])
