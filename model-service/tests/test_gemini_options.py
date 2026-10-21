# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

"""GeminiProvider.complete's keyword options: the PR work-type call is unchanged, and the stage
call gets its own ceiling, temperature, schema and budget. A stub client stands in for the SDK's
network call; the SDK itself must be installed (the test skips without it)."""

from __future__ import annotations

from types import SimpleNamespace

import pytest

from sdk.llm import GeminiProvider

types = pytest.importorskip("google.genai.types")


class _Models:
    def __init__(self):
        self.configs = []

    def generate_content(self, *, model, contents, config):
        self.configs.append(config)
        return SimpleNamespace(text="{}")


def _provider(model="gemini-3.5-flash"):
    p = object.__new__(GeminiProvider)
    p._model = model
    p._client = SimpleNamespace(models=_Models())
    return p


def test_the_default_call_is_the_pr_work_type_one():
    p = _provider()
    p.complete("sys", "user")
    cfg = p._client.models.configs[0]
    assert cfg.max_output_tokens == 256
    assert cfg.response_mime_type == "application/json"
    assert cfg.response_json_schema is None
    assert cfg.temperature is None
    assert cfg.thinking_config.thinking_level == types.ThinkingLevel.MINIMAL
    assert cfg.http_options is None


def test_stage_options_reach_the_config():
    p = _provider()
    schema = {"type": "array", "items": {"type": "string"}}
    p.complete("sys", "user", max_output_tokens=8192, temperature=0.0, response_json_schema=schema, timeout_s=55)
    cfg = p._client.models.configs[0]
    assert cfg.max_output_tokens == 8192
    assert cfg.temperature == 0.0
    assert cfg.response_json_schema == schema
    assert 54_000 <= cfg.http_options.timeout <= 55_000


def test_a_per_call_model_overrides_the_providers():
    p = _provider()
    calls = []
    p._client.models.generate_content = lambda **kw: calls.append(kw) or SimpleNamespace(text="{}")
    p.complete("sys", "user", model="gemini-2.5-flash")
    assert calls[0]["model"] == "gemini-2.5-flash"


def test_a_2x_model_gets_temperature_and_a_zero_thinking_budget():
    p = _provider("gemini-2.5-flash")
    p.complete("sys", "user")
    p.complete("sys", "user", temperature=0.4)
    default, chosen = p._client.models.configs
    assert default.temperature == 0.0
    assert chosen.temperature == 0.4
    assert chosen.thinking_config.thinking_budget == 0
    assert chosen.thinking_config.thinking_level is None
