"""Model server — the two model-bearing endpoints.

  POST /embed-classify  {title, branch, axis}  -> ranked candidates + top score + margin
  POST /llm-classify    {axis, title, branch, candidates, allow_null, files, body} -> {bucket}
  POST /classify-stage  {artifacts: [{id, name, type, description?, body?}]} -> each artifact's
                        AIDLC lifecycle stage by a three-sample vote (see stage/)

Holds the resident SBERT + centroid bundle (loaded once at startup) and the LLM
provider, so callers don't each load a 90 MB model. The orchestrator applies the
routing thresholds and rules — this server only serves the two model layers.

Request fields are length-capped so one oversized payload cannot exhaust memory
or be forwarded whole into the LLM prompt.

Env: BUNDLE_DIR (required), GEMINI_API_KEY (optional — /llm-classify and /classify-stage 503
without it).
"""

from __future__ import annotations

import os
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field, field_validator, model_validator

from sdk import llm
from sdk.embedding import EmbeddingBundle
from stage import classify as stage

# Field caps — a PR title/branch is short by nature; these only bound a
# pathological payload, not real input.
MAX_TITLE = 1000
MAX_BRANCH = 512
MAX_AXIS = 16
MAX_BODY = 20_000
MAX_FILES = 1000
MAX_CANDIDATES = 64
MAX_ARTIFACTS = 250
MAX_ARTIFACT_ID = 256
MAX_ARTIFACT_NAME = 1000
MAX_ARTIFACT_TYPE = 64
MAX_ARTIFACT_TEXT = 50_000

_state: dict = {}


@asynccontextmanager
async def lifespan(app: FastAPI):
    bundle_dir = os.environ.get("BUNDLE_DIR")
    if not bundle_dir:
        raise RuntimeError("BUNDLE_DIR not set")
    _state["bundle"] = EmbeddingBundle(bundle_dir)  # loads SBERT + centroids once
    _state["llm"] = llm.GeminiProvider() if os.environ.get("GEMINI_API_KEY") else None
    yield
    _state.clear()


app = FastAPI(title="pr-work-type model server", lifespan=lifespan)


class EmbedReq(BaseModel):
    title: str = Field(max_length=MAX_TITLE)
    branch: str = Field(default="", max_length=MAX_BRANCH)
    axis: str = Field(max_length=MAX_AXIS)  # "type" | "domain"


class Candidate(BaseModel):
    bucket: str
    score: float


class EmbedResp(BaseModel):
    candidates: list[Candidate]
    top_score: float
    margin: float


class LLMReq(BaseModel):
    axis: str = Field(max_length=MAX_AXIS)
    title: str = Field(max_length=MAX_TITLE)
    branch: str = Field(default="", max_length=MAX_BRANCH)
    candidates: list[str] = Field(max_length=MAX_CANDIDATES)
    allow_null: bool = False
    files: list[str] | None = Field(default=None, max_length=MAX_FILES)
    body: str | None = Field(default=None, max_length=MAX_BODY)


class LLMResp(BaseModel):
    bucket: str | None


@app.get("/health")
def health() -> dict:
    return {
        "status": "ok",
        "model_id": _state["bundle"].model_id if "bundle" in _state else None,
        # Whether /classify-stage (and /llm-classify) can answer: the Gemini key was loaded.
        "stage_classifier": "ready" if _state.get("llm") is not None else "no_key",
    }


@app.post("/embed-classify", response_model=EmbedResp)
def embed_classify(req: EmbedReq) -> EmbedResp:
    if req.axis not in _state["bundle"].axes():
        raise HTTPException(400, f"unknown axis {req.axis!r}")
    ranked = _state["bundle"].rank(req.axis, req.title, req.branch)
    top = ranked[0][1]
    margin = top - (ranked[1][1] if len(ranked) > 1 else 0.0)
    return EmbedResp(
        candidates=[Candidate(bucket=b, score=round(s, 4)) for b, s in ranked],
        top_score=round(top, 4), margin=round(margin, 4),
    )


@app.post("/llm-classify", response_model=LLMResp)
def llm_classify(req: LLMReq) -> LLMResp:
    if _state.get("llm") is None:
        raise HTTPException(503, "LLM provider not configured (set GEMINI_API_KEY)")
    bucket = llm.disambiguate(
        _state["llm"], req.axis, req.title, req.branch, req.candidates,
        allow_null=req.allow_null, files=req.files, body=req.body,
    )
    return LLMResp(bucket=bucket)


class StageArtifact(BaseModel):
    id: str = Field(min_length=1, max_length=MAX_ARTIFACT_ID)  # the caller's id, unique per request
    name: str = Field(min_length=1, max_length=MAX_ARTIFACT_NAME)
    type: str = Field(max_length=MAX_ARTIFACT_TYPE)  # an aidlc.v1.HarnessArtifactType name
    description: str = ""
    body: str = ""

    @model_validator(mode="after")
    def _text_within_cap(self) -> StageArtifact:
        # Checked here rather than with Field(max_length) so the 422 names the artifact, not just
        # its position in the list.
        for field_name in ("description", "body"):
            if len(getattr(self, field_name)) > MAX_ARTIFACT_TEXT:
                raise ValueError(
                    f"artifact {self.id!r}: {field_name} is longer than {MAX_ARTIFACT_TEXT} characters"
                )
        return self


class StageReq(BaseModel):
    artifacts: list[StageArtifact] = Field(max_length=MAX_ARTIFACTS)

    @field_validator("artifacts")
    @classmethod
    def _unique_ids(cls, artifacts: list[StageArtifact]) -> list[StageArtifact]:
        if len({a.id for a in artifacts}) != len(artifacts):
            raise ValueError("artifact ids must be unique within a request")
        return artifacts


class StageResult(BaseModel):
    id: str
    outcome: str  # SUCCEEDED | UNRESOLVED | DEFERRED | SKIPPED
    bucket: str | None = None
    stage: str | None = None  # an aidlc.v1.AidlcStage name, only when SUCCEEDED
    votes: list[str] = []


class StageResp(BaseModel):
    results: list[StageResult]
    failed_calls: int


@app.post("/classify-stage", response_model=StageResp)
def classify_stage(req: StageReq) -> StageResp:
    if _state.get("llm") is None:
        raise HTTPException(503, "LLM provider not configured (set GEMINI_API_KEY)")
    result = stage.classify(
        _state["llm"],
        [stage.Artifact(a.id, a.name, a.type, a.description, a.body) for a in req.artifacts],
    )
    return StageResp(
        results=[StageResult(id=v.id, outcome=v.outcome, bucket=v.bucket, stage=v.stage, votes=v.votes)
                 for v in result.verdicts],
        failed_calls=result.failed_calls,
    )
