"""Pure SDK tests — Layer-0 rules, result types, and the lazy package loader.

These touch no model: importing sdk.rules / sdk.types must not pull torch, which
is exactly what the lazy sdk/__init__ guarantees. A regression there would make
this module fail to import.
"""

from __future__ import annotations

import pytest

import sdk
from sdk import rules
from sdk.types import CHERRY_PICK, Method


@pytest.mark.parametrize("title,branch,author,want", [
    ("", "", "dependabot[bot]", "chore-deps"),
    ("", "", "renovate[bot]", "chore-deps"),
    ("", "", "  Dependabot[bot] ", "chore-deps"),
    ("Cherry-pick #42", "", "", CHERRY_PICK),
    ("", "cherrypick/hotfix", "", CHERRY_PICK),
    ("add login", "feat/login", "alice", None),
])
def test_classify_type(title, branch, author, want):
    assert rules.classify_type(title, branch, author) == want


def test_method_values():
    assert Method.ABSTAIN.value == "abstain"
    assert {m.value for m in Method} == {"rule", "embedding", "llm", "abstain"}


def test_lazy_loader_rejects_unknown_without_importing_the_model():
    # Must raise cleanly (not import sdk.classifier/torch) for an unknown name.
    with pytest.raises(AttributeError):
        _ = sdk.NoSuchName


def test_lazy_names_are_mapped():
    assert {"PRWorkTypeClassifier", "GeminiProvider", "OpenAICompatibleProvider",
            "LLMProvider", "disambiguate"} <= set(sdk._LAZY)
