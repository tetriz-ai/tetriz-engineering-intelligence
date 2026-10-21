"""PRWorkTypeClassifier — the full per-axis cascade as one library call.

    clf = PRWorkTypeClassifier("path/to/bundle", llm_provider=GeminiProvider())
    result = clf.classify(title="fix: null deref", branch="fix/npe")
    result.type.bucket    # "bugfix"
    result.domain.bucket  # None (no specialised domain)

The full per-axis cascade, as one in-process library call:
  type   : Layer 0 rule → Layer 1 embedding (if confident) → Layer 2 LLM
  domain : Layer 1 embedding (if confident) → Layer 2 LLM (nullable)

Without an LLM provider it runs embedding-only with honest abstention. Escalation
gives the LLM the FULL taxonomy by default (topk=0): on this public centroid that
beat restricting to the embedding's top-k (see datasets/RESULTS.md).
"""

from __future__ import annotations

from pathlib import Path

from sdk import llm, rules
from sdk.embedding import EmbeddingBundle
from sdk.types import AxisResult, Classification, Method


class PRWorkTypeClassifier:
    def __init__(
        self, bundle_dir: str | Path, *, llm_provider: llm.LLMProvider | None = None,
        min_score: float = 0.35, min_margin: float = 0.08, topk: int = 0,
    ) -> None:
        self._bundle = EmbeddingBundle(Path(bundle_dir))
        self._llm = llm_provider
        self._min_score = min_score
        self._min_margin = min_margin
        self._topk = topk

    def _candidates(self, ranked: list[tuple[str, float]]) -> list[str]:
        return [b for b, _ in (ranked[: self._topk] if self._topk > 0 else ranked)]

    def _resolve(self, axis: str, title: str, branch: str, *, allow_null: bool,
                 files: list[str] | None, body: str | None) -> AxisResult:
        ranked = self._bundle.rank(axis, title, branch)
        top_bucket, top_score = ranked[0]
        margin = top_score - (ranked[1][1] if len(ranked) > 1 else 0.0)
        if top_score >= self._min_score and margin >= self._min_margin:
            return AxisResult(top_bucket, Method.EMBEDDING, round(top_score, 4))
        # escalate
        if self._llm is not None:
            pick = llm.disambiguate(self._llm, axis, title, branch, self._candidates(ranked),
                                    allow_null=allow_null, files=files, body=body)
            if pick is not None:
                return AxisResult(pick, Method.LLM)
            if allow_null:                       # domain: LLM judged "no domain"
                return AxisResult(None, Method.LLM)
            # type: LLM couldn't land — best-effort embedding top-1
            return AxisResult(top_bucket, Method.EMBEDDING, round(top_score, 4))
        # no LLM → honest abstention (embedding wasn't confident)
        return AxisResult(None, Method.ABSTAIN)

    def classify(self, title: str, branch: str = "", *, author_login: str = "",
                 files: list[str] | None = None, body: str | None = None) -> Classification:
        rule = rules.classify_type(title, branch, author_login)
        type_res = (AxisResult(rule, Method.RULE) if rule is not None
                    else self._resolve("type", title, branch, allow_null=False, files=files, body=body))
        domain_res = self._resolve("domain", title, branch, allow_null=True, files=files, body=body)
        return Classification(type=type_res, domain=domain_res)
