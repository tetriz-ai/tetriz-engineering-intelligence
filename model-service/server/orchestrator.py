"""Orchestrator — the classification API.

  POST /classify {title, branch, author_login?, files?, body?} -> Classification

Composes the cascade over the model server's two endpoints:
  type   : Layer 0 rule → /embed-classify (confident?) → /llm-classify
  domain : /embed-classify (confident?) → /llm-classify (nullable)
Applies the routing thresholds here (the model server only serves the layers),
and persists each result to Postgres when DATABASE_URL is set (else stateless).

A model server that is unreachable, errors, or returns no candidates surfaces as
a 502 (never a bare 500); request fields are length-capped so one oversized
payload cannot exhaust memory.

Env: MODEL_SERVER_URL (default http://model-server:8000), DATABASE_URL (optional),
     MIN_SCORE / MIN_MARGIN (routing thresholds), TOPK (0 = full taxonomy on escalation).
"""

from __future__ import annotations

import os
from contextlib import asynccontextmanager

import httpx
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

from contract import taxonomy
from sdk import rules
from sdk.types import CHERRY_PICK, UNCATEGORISED, Method

MODEL_URL = os.environ.get("MODEL_SERVER_URL", "http://model-server:8000")
MIN_SCORE = float(os.environ.get("MIN_SCORE", "0.35"))
MIN_MARGIN = float(os.environ.get("MIN_MARGIN", "0.08"))
TOPK = int(os.environ.get("TOPK", "0"))

# Request-field caps. A PR title/branch is short by nature; the caps exist only
# to stop a pathological payload (a 10 MB "title") from exhausting memory or
# being forwarded whole into the LLM prompt — not to constrain real input.
MAX_TITLE = 1000
MAX_BRANCH = 512
MAX_LOGIN = 256
MAX_BODY = 20_000
MAX_FILES = 1000
MAX_FILE_LEN = 512

# Buckets the LLM may legitimately return per axis — the server's answer is
# checked against the frozen taxonomy so a drifted model cannot inject one.
_VALID = {
    "type": set(taxonomy.TYPE_BUCKETS) | {UNCATEGORISED, CHERRY_PICK},
    "domain": set(taxonomy.DOMAIN_BUCKETS),
}

_state: dict = {}


@asynccontextmanager
async def lifespan(app: FastAPI):
    _state["http"] = httpx.Client(base_url=MODEL_URL, timeout=60)
    dsn = os.environ.get("DATABASE_URL")
    if dsn:
        from server.storage import Storage
        _state["store"] = Storage(dsn)
    yield
    _state["http"].close()
    if _state.get("store"):
        _state["store"].close()


app = FastAPI(title="pr-work-type orchestrator", lifespan=lifespan)


class ClassifyReq(BaseModel):
    title: str = Field(max_length=MAX_TITLE)
    branch: str = Field(default="", max_length=MAX_BRANCH)
    author_login: str = Field(default="", max_length=MAX_LOGIN)
    files: list[str] | None = Field(default=None, max_length=MAX_FILES)
    body: str | None = Field(default=None, max_length=MAX_BODY)


class AxisOut(BaseModel):
    bucket: str | None
    method: str
    score: float | None = None


class ClassifyResp(BaseModel):
    type: AxisOut
    domain: AxisOut


def _embed(axis: str, title: str, branch: str) -> dict:
    try:
        r = _state["http"].post("/embed-classify", json={"axis": axis, "title": title, "branch": branch})
        r.raise_for_status()
        return r.json()
    except httpx.HTTPError as e:
        raise HTTPException(status_code=502, detail=f"model server embed-classify failed: {e}") from e


def _llm(axis: str, title: str, branch: str, candidates: list[str], allow_null: bool,
         files, body) -> tuple[str | None, bool]:
    # Returns (pick, available). available is False only for a deliberate 503 (the
    # LLM layer is not configured) — the caller then abstains honestly. Any other
    # failure is surfaced as a 502 rather than silently swallowed into a wrong label.
    try:
        r = _state["http"].post("/llm-classify", json={
            "axis": axis, "title": title, "branch": branch, "candidates": candidates,
            "allow_null": allow_null, "files": files, "body": body,
        })
        if r.status_code == 503:
            return None, False
        r.raise_for_status()
        return r.json().get("bucket"), True
    except httpx.HTTPError as e:
        raise HTTPException(status_code=502, detail=f"model server llm-classify failed: {e}") from e


def _resolve(axis: str, title: str, branch: str, *, allow_null: bool, files, body) -> AxisOut:
    emb = _embed(axis, title, branch)
    cands = emb.get("candidates") or []
    if not cands:
        # The model server returned nothing to rank — an honest abstention, not a crash.
        return AxisOut(bucket=None, method=Method.ABSTAIN.value)
    top = cands[0]
    if emb.get("top_score", 0.0) >= MIN_SCORE and emb.get("margin", 0.0) >= MIN_MARGIN:
        return AxisOut(bucket=top["bucket"], method=Method.EMBEDDING.value, score=top["score"])
    slugs = [c["bucket"] for c in (cands[:TOPK] if TOPK > 0 else cands)]
    pick, available = _llm(axis, title, branch, slugs, allow_null, files, body)
    if not available:
        # The LLM layer is unconfigured — abstain rather than mislabel the
        # embedding's unsure guess as a confident one.
        return AxisOut(bucket=None, method=Method.ABSTAIN.value)
    if pick is not None:
        if pick not in _VALID[axis]:
            raise HTTPException(status_code=502, detail=f"model server returned {pick!r} outside the {axis} taxonomy")
        return AxisOut(bucket=pick, method=Method.LLM.value)
    if allow_null:
        return AxisOut(bucket=None, method=Method.LLM.value)
    # type: the LLM declined though asked to — best-effort to the embedding's top,
    # labelled embedding and carrying its (sub-threshold) score.
    return AxisOut(bucket=top["bucket"], method=Method.EMBEDDING.value, score=top["score"])


@app.get("/health")
def health() -> dict:
    return {"status": "ok"}


@app.post("/classify", response_model=ClassifyResp)
def classify(req: ClassifyReq) -> ClassifyResp:
    rule = rules.classify_type(req.title, req.branch, req.author_login)
    type_out = (AxisOut(bucket=rule, method=Method.RULE.value) if rule is not None
                else _resolve("type", req.title, req.branch, allow_null=False, files=req.files, body=req.body))
    domain_out = _resolve("domain", req.title, req.branch, allow_null=True, files=req.files, body=req.body)

    if _state.get("store"):
        _state["store"].save(
            title=req.title, branch=req.branch,
            type_bucket=type_out.bucket, type_method=type_out.method, type_score=type_out.score,
            domain_bucket=domain_out.bucket, domain_method=domain_out.method,
        )
    return ClassifyResp(type=type_out, domain=domain_out)
