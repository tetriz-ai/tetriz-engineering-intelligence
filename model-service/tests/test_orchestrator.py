"""Orchestrator cascade + failure-handling tests.

These drive the HTTP layer with a fake model-server client, so they need no
model, torch or network — only fastapi/pydantic/httpx.
"""

from __future__ import annotations

import httpx
import pytest
from fastapi.testclient import TestClient

from server import orchestrator

CONFIDENT = {"candidates": [{"bucket": "bugfix", "score": 0.91}], "top_score": 0.91, "margin": 0.5}
CONFIDENT_DOM = {"candidates": [{"bucket": "ai-ml", "score": 0.9}], "top_score": 0.9, "margin": 0.5}
UNSURE = {"candidates": [{"bucket": "bugfix", "score": 0.3}, {"bucket": "feature", "score": 0.29}],
          "top_score": 0.3, "margin": 0.01}


class FakeResp:
    def __init__(self, status_code: int, payload: dict):
        self.status_code = status_code
        self._payload = payload

    def json(self) -> dict:
        return self._payload

    def raise_for_status(self) -> None:
        if self.status_code >= 400:
            raise httpx.HTTPStatusError(
                f"status {self.status_code}",
                request=httpx.Request("POST", "http://model-server"),
                response=httpx.Response(self.status_code),
            )


class FakeHTTP:
    """Dispatches /embed-classify and /llm-classify to the supplied handler."""

    def __init__(self, handler):
        self.handler = handler

    def post(self, path, json=None):
        return self.handler(path, json)

    def close(self):
        pass


def client(handler) -> TestClient:
    # No `with`, so the app lifespan never runs — we own _state directly and
    # avoid opening a real httpx.Client to a non-existent model server.
    orchestrator._state["http"] = FakeHTTP(handler)
    orchestrator._state.pop("store", None)
    return TestClient(orchestrator.app)


def test_confident_embedding_both_axes():
    def handler(path, json):
        return FakeResp(200, CONFIDENT if json["axis"] == "type" else CONFIDENT_DOM)

    r = client(handler).post("/classify", json={"title": "fix npe", "branch": "fix/npe"})
    assert r.status_code == 200, r.text
    body = r.json()
    assert body["type"] == {"bucket": "bugfix", "method": "embedding", "score": 0.91}
    assert body["domain"]["bucket"] == "ai-ml" and body["domain"]["method"] == "embedding"


def test_model_server_down_is_502_not_500():
    def handler(path, json):
        raise httpx.ConnectError("connection refused")

    r = client(handler).post("/classify", json={"title": "fix npe"})
    assert r.status_code == 502, r.text


def test_empty_candidates_abstains_not_crashes():
    def handler(path, json):
        return FakeResp(200, {"candidates": [], "top_score": 0.0, "margin": 0.0})

    r = client(handler).post("/classify", json={"title": "x"})
    assert r.status_code == 200, r.text
    assert r.json()["type"] == {"bucket": None, "method": "abstain", "score": None}


def test_llm_unavailable_abstains_not_mislabels():
    # type axis unsure + LLM 503 -> honest abstain, not a fake embedding/llm label.
    def handler(path, json):
        if path == "/embed-classify":
            return FakeResp(200, UNSURE if json["axis"] == "type" else CONFIDENT_DOM)
        return FakeResp(503, {"detail": "no api key"})

    r = client(handler).post("/classify", json={"title": "ambiguous change", "branch": "misc"})
    assert r.status_code == 200, r.text
    assert r.json()["type"]["method"] == "abstain"
    assert r.json()["type"]["bucket"] is None


def test_llm_bucket_outside_taxonomy_is_502():
    def handler(path, json):
        if path == "/embed-classify":
            return FakeResp(200, UNSURE if json["axis"] == "type" else CONFIDENT_DOM)
        return FakeResp(200, {"bucket": "not-a-bucket"})

    r = client(handler).post("/classify", json={"title": "ambiguous", "branch": "misc"})
    assert r.status_code == 502, r.text


def test_rule_fires_for_bot_author():
    def handler(path, json):
        return FakeResp(200, CONFIDENT_DOM)  # only domain reaches the model server

    r = client(handler).post("/classify", json={"title": "bump dep", "author_login": "dependabot[bot]"})
    assert r.status_code == 200, r.text
    assert r.json()["type"] == {"bucket": "chore-deps", "method": "rule", "score": None}


@pytest.mark.parametrize("field,value", [("title", "x" * 2000), ("branch", "b" * 2000)])
def test_oversized_input_is_rejected(field, value):
    r = client(lambda p, j: FakeResp(200, CONFIDENT)).post("/classify", json={"title": "ok", field: value})
    assert r.status_code == 422, r.text
