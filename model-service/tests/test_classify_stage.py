# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

"""/classify-stage against a fake provider: batching and the per-sample shuffle, the
strict-majority vote (including a split), skipped types, malformed replies, the request cap and
the 503 without a key. No model, torch, key or network."""

from __future__ import annotations

import json
from pathlib import Path

import pytest
from fastapi.testclient import TestClient

from server import model_server
from stage import classify as stage
from stage.prompt import BUCKETS, RESPONSE_SCHEMA, SYSTEM_PROMPT, stage_for_bucket

SKILL = "HARNESS_ARTIFACT_TYPE_SKILL"


class FakeProvider:
    """Answers each call with ``reply(user)`` — a string, or an exception to raise — and records
    every call's content and options."""

    def __init__(self, reply):
        self.reply = reply
        self.calls: list[tuple[str, str, dict]] = []

    def complete(self, system: str, user: str, **options) -> str:
        self.calls.append((system, user, options))
        out = self.reply(user)
        if isinstance(out, Exception):
            raise out
        return out


def rows(user: str) -> list[dict]:
    return json.loads(user)


def by_name(table: dict[str, str]):
    return lambda user: json.dumps([{"id": r["id"], "bucket": table[r["name"]]} for r in rows(user)])


def per_call(*answers):
    """Call i assigns every artifact ``answers[i]``, or raises it."""
    it = iter(answers)

    def reply(user: str):
        a = next(it)
        return a if isinstance(a, Exception) else json.dumps([{"id": r["id"], "bucket": a} for r in rows(user)])

    return reply


def post(provider, artifacts):
    # No `with`: the lifespan (which needs a bundle) never runs; the test owns _state.
    model_server._state.clear()
    if provider is not None:
        model_server._state["llm"] = provider
    return TestClient(model_server.app).post("/classify-stage", json={"artifacts": artifacts})


def art(i, name=None, type_=SKILL, **extra):
    return {"id": str(i), "name": name or f"artifact-{i}", "type": type_, **extra}


@pytest.fixture(autouse=True)
def _one_call_at_a_time(monkeypatch):
    # Serial calls, so per_call's i-th answer is the i-th sample's.
    monkeypatch.setattr(stage, "CONCURRENCY", 1)
    yield
    model_server._state.clear()


def test_classifies_practice_artifacts_and_skips_other_types():
    provider = FakeProvider(by_name({"review-pr": "review", "sales-deck": "x_gtm", "ship-it": "deploy"}))
    r = post(provider, [
        art("a", "review-pr"),
        art("b", "sales-deck", "HARNESS_ARTIFACT_TYPE_AGENT"),
        art("c", "ship-it", "HARNESS_ARTIFACT_TYPE_COMMAND"),
        art("d", "pre-commit", "HARNESS_ARTIFACT_TYPE_HOOK"),
        art("e", "mystery", "NOT_A_TYPE"),
    ])

    assert r.status_code == 200, r.text
    body = r.json()
    assert body["failed_calls"] == 0
    got = {x["id"]: (x["outcome"], x["bucket"], x["stage"], x["votes"]) for x in body["results"]}
    assert got == {
        "a": ("SUCCEEDED", "review", "AIDLC_STAGE_REVIEW", ["review"] * 3),
        # An exclusion bucket is a definitive "serves no stage".
        "b": ("SUCCEEDED", "x_gtm", "AIDLC_STAGE_UNSPECIFIED", ["x_gtm"] * 3),
        "c": ("SUCCEEDED", "deploy", "AIDLC_STAGE_DEPLOY", ["deploy"] * 3),
        "d": ("SKIPPED", None, None, []),
        "e": ("SKIPPED", None, None, []),
    }
    assert [x["id"] for x in body["results"]] == ["a", "b", "c", "d", "e"]
    # Three samples of one batch; skipped artifacts are never sent.
    assert [sorted(r["name"] for r in rows(u)) for _, u, _ in provider.calls] == [
        ["review-pr", "sales-deck", "ship-it"]
    ] * 3


def test_each_call_carries_the_classifier_settings():
    provider = FakeProvider(by_name({"plan-sprint": "plan"}))
    post(provider, [art(1, "plan-sprint")])

    system, user, options = provider.calls[0]
    assert system == SYSTEM_PROMPT
    assert rows(user) == [{"id": 0, "name": "plan-sprint"}]
    assert options == {
        "model": "gemini-3.5-flash",
        "max_output_tokens": 8192,
        "temperature": 0.0,
        "response_json_schema": RESPONSE_SCHEMA,
        "timeout_s": 55.0,
    }


def test_batches_of_25_reshuffled_per_sample():
    names = {f"n{i:02d}": "code" for i in range(60)}
    provider = FakeProvider(by_name(names))
    r = post(provider, [art(i, n) for i, n in enumerate(names)])

    assert all(x["outcome"] == "SUCCEEDED" for x in r.json()["results"])
    assert [len(rows(u)) for _, u, _ in provider.calls] == [25, 25, 10] * 3
    # Every artifact goes out once per sample, and the batches differ between samples.
    batches = [frozenset(x["name"] for x in rows(u)) for _, u, _ in provider.calls]
    for s in range(3):
        assert set().union(*batches[3 * s : 3 * s + 3]) == set(names)
    assert len(set(batches)) == len(batches)


def test_repeat_requests_send_the_same_batches():
    # Sample s is shuffled with seed s + 1, so the same input in the same order is batched the
    # same way every time, and the samples still differ from one another.
    names = [f"n{i:02d}" for i in range(60)]
    table = dict.fromkeys(names, "code")
    first, second = FakeProvider(by_name(table)), FakeProvider(by_name(table))
    post(first, [art(i, n) for i, n in enumerate(names)])
    post(second, [art(i, n) for i, n in enumerate(names)])

    assert [u for _, u, _ in first.calls] == [u for _, u, _ in second.calls]
    first_batches = [rows(u)[0]["name"] for _, u, _ in first.calls[::3]]
    assert len(set(first_batches)) == 3


@pytest.mark.parametrize(
    ("answers", "outcome", "bucket", "votes"),
    [
        (("review", "code", "review"), "SUCCEEDED", "review", ["review", "code", "review"]),
        # Every sample voted and no bucket holds a strict majority: a split.
        (("review", "code", "plan"), "UNRESOLVED", None, ["review", "code", "plan"]),
        # Two agreeing votes of three samples win even when the third call failed.
        (("review", RuntimeError("down"), "review"), "SUCCEEDED", "review", ["review", "review"]),
        # A failed call and a split: no verdict, and not a definitive one either.
        (("review", RuntimeError("down"), "code"), "DEFERRED", None, ["review", "code"]),
    ],
)
def test_the_strict_majority_vote(answers, outcome, bucket, votes):
    r = post(FakeProvider(per_call(*answers)), [art(1)])

    x = r.json()["results"][0]
    assert (x["outcome"], x["bucket"], x["votes"]) == (outcome, bucket, votes)
    assert x["stage"] == (stage_for_bucket(bucket) if bucket else None)
    assert r.json()["failed_calls"] == sum(isinstance(a, Exception) for a in answers)


@pytest.mark.parametrize(
    "bad",
    [
        "not json",
        '{"id": 0, "bucket": "code"}',  # an object, not an array
        '[{"id": 0, "bucket": "coding"}]',  # off-vocabulary bucket
        '[{"id": "0", "bucket": "code"}]',  # id is not an integer
        '["code"]',  # rows are not objects
        '[{"id": 0, "bucket": "code"}, {"id": 0, "bucket": "plan"}]',  # a repeated id
    ],
)
def test_a_malformed_reply_costs_that_sample_its_votes(bad):
    good = json.dumps([{"id": 0, "bucket": "code"}])
    replies = iter([good, bad, good])
    r = post(FakeProvider(lambda _u: next(replies)), [art(1)])

    assert r.json()["failed_calls"] == 1
    assert r.json()["results"][0]["outcome"] == "SUCCEEDED"
    assert r.json()["results"][0]["votes"] == ["code", "code"]


def test_a_reply_that_leaves_artifacts_out_counts_as_a_failed_call():
    # Sample 1 answers only the first artifact of its batch; the other loses that vote.
    calls = iter(range(3))

    def reply(user: str) -> str:
        n = next(calls)
        answered = rows(user)[:1] if n == 1 else rows(user)
        return json.dumps([{"id": r["id"], "bucket": "code"} for r in answered])

    r = post(FakeProvider(reply), [art(1, "a"), art(2, "b")])

    assert r.json()["failed_calls"] == 1
    assert sorted(len(x["votes"]) for x in r.json()["results"]) == [2, 3]
    assert all(x["outcome"] == "SUCCEEDED" for x in r.json()["results"])


# Made once with this Go program (Go 1.27.1), which batches 60 artifacts "0".."59" the way the
# production classifier does, and saved as fixtures/go_batches_60.json:
#
#     for s := 0; s < 3; s++ {
#         order := []string{"0", ..., "59"}
#         r := rand.New(rand.NewSource(int64(s) + 1))
#         r.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
#         // batches: order[0:25], order[25:50], order[50:60]
#     }
GO_BATCHES = json.loads((Path(__file__).parent / "fixtures" / "go_batches_60.json").read_text())


def test_batches_are_the_production_classifiers():
    provider = FakeProvider(lambda u: json.dumps([{"id": r["id"], "bucket": "code"} for r in rows(u)]))
    post(provider, [art(i, f"n{i}") for i in range(60)])

    sent = [[r["name"][1:] for r in rows(u)] for _, u, _ in provider.calls]
    assert [sent[3 * s : 3 * s + 3] for s in range(3)] == GO_BATCHES


def test_an_over_long_body_is_422_naming_the_artifact():
    r = post(FakeProvider(lambda _u: "[]"), [art(1), art("long-one", body="x" * 50_001)])

    assert r.status_code == 422
    assert "artifact 'long-one': body is longer than 50000 characters" in r.text


def test_a_body_at_the_cap_is_accepted():
    r = post(FakeProvider(lambda u: json.dumps([{"id": 0, "bucket": "code"}])), [art(1, body="x" * 50_000)])

    assert r.status_code == 200


def test_an_id_outside_the_batch_is_ignored():
    reply = json.dumps([{"id": 0, "bucket": "plan"}, {"id": 7, "bucket": "code"}])
    r = post(FakeProvider(lambda _u: reply), [art(1)])

    assert r.json()["results"][0]["votes"] == ["plan"] * 3


def test_description_and_body_are_sent_redacted():
    provider = FakeProvider(lambda _u: json.dumps([{"id": 0, "bucket": "code"}]))
    post(provider, [art(1, "deploy-helper", description="mail ops@example.com", body="token=abc123 cd /Users/jdoe/x")])

    assert rows(provider.calls[0][1]) == [
        {"id": 0, "name": "deploy-helper", "description": "mail [redacted]", "body": "[redacted] cd ~/x"}
    ]


def test_no_key_is_503():
    assert post(None, [art(1)]).status_code == 503


def test_a_malformed_request_is_422_before_the_key_check():
    assert post(None, [{"id": "1", "type": SKILL}]).status_code == 422


def test_duplicate_ids_are_422():
    assert post(FakeProvider(lambda _u: "[]"), [art(1), art(1)]).status_code == 422


def test_more_than_250_artifacts_is_422():
    provider = FakeProvider(lambda _u: "[]")
    assert post(provider, [art(i) for i in range(251)]).status_code == 422
    assert provider.calls == []


def test_an_empty_request_makes_no_call():
    provider = FakeProvider(lambda _u: "[]")
    r = post(provider, [])

    assert r.json() == {"results": [], "failed_calls": 0}
    assert provider.calls == []


@pytest.mark.parametrize(
    ("votes", "winner"),
    [([], None), (["a"], None), (["a", "a"], "a"), (["a", "b", "a"], "a"), (["a", "b", "c"], None)],
)
def test_majority_is_measured_against_the_configured_samples(votes, winner):
    assert stage.majority(votes, 3) == winner


def test_every_stage_bucket_names_an_aidlc_stage():
    assert [stage_for_bucket(b) for b in BUCKETS[:9]] == [
        "AIDLC_STAGE_PRODUCT_SPEC", "AIDLC_STAGE_TECH_SPEC", "AIDLC_STAGE_PLAN", "AIDLC_STAGE_CODE",
        "AIDLC_STAGE_REVIEW", "AIDLC_STAGE_TEST_QA", "AIDLC_STAGE_CI_CD", "AIDLC_STAGE_DEPLOY",
        "AIDLC_STAGE_OPERATE",
    ]
    assert {stage_for_bucket(b) for b in BUCKETS[9:]} == {"AIDLC_STAGE_UNSPECIFIED"}


@pytest.mark.parametrize(("llm", "state"), [(object(), "ready"), (None, "no_key")])
def test_health_says_whether_the_stage_classifier_can_answer(llm, state):
    model_server._state.clear()
    if llm is not None:
        model_server._state["llm"] = llm
    r = TestClient(model_server.app).get("/health")

    assert r.status_code == 200
    assert r.json() == {"status": "ok", "model_id": None, "stage_classifier": state}


class _FakeBundle:
    model_id = "fake-bundle"

    def __init__(self, bundle_dir):
        self.bundle_dir = bundle_dir


@pytest.mark.parametrize(("key", "state"), [("test-key-not-real", "ready"), (None, "no_key")])
def test_health_reflects_the_key_loaded_at_startup(monkeypatch, key, state):
    # Through the real lifespan: the provider is built from GEMINI_API_KEY at startup, or not.
    pytest.importorskip("google.genai")
    monkeypatch.setattr(model_server, "EmbeddingBundle", _FakeBundle)
    monkeypatch.setenv("BUNDLE_DIR", "/nonexistent-bundle")
    if key is None:
        monkeypatch.delenv("GEMINI_API_KEY", raising=False)
    else:
        monkeypatch.setenv("GEMINI_API_KEY", key)
    model_server._state.clear()
    with TestClient(model_server.app) as client:
        r = client.get("/health")

    assert r.status_code == 200
    assert r.json() == {"status": "ok", "model_id": "fake-bundle", "stage_classifier": state}
