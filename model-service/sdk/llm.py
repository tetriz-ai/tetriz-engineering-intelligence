"""Layer 2 — LLM disambiguation. Given the escalated axis's candidate buckets
plus the richer PR context (files, body), the LLM picks one (or null for domain).

Backend-agnostic: the classifier depends only on the `LLMProvider` protocol, so
any model works. A `GeminiProvider` adapter is included; adopters can pass their
own. Definitions are the methodology's, verbatim.
"""

from __future__ import annotations

import json
import os
import re
from typing import Protocol

from contract.taxonomy import DOMAIN_BUCKETS, TYPE_BUCKETS

_JSON = re.compile(r"\{.*\}", re.DOTALL)

_TYPE_DEFS = """\
- feature: new user-facing functionality or behaviour change
- bugfix: fixing incorrect behaviour, crash, or data integrity issue
- refactor: code restructuring without behaviour change
- chore-deps: dependency updates, version bumps, lock file changes
- test: adding or modifying tests with no production code change
- docs: documentation, comments, README changes only
- performance: changes whose primary goal is latency, throughput, or memory reduction"""

_DOMAIN_DEFS = """\
- infra-devops: CI/CD, deployment, containers, IaC, provisioning
- data-pipeline: data ingestion, transformation, ETL, migrations, warehouse
- observability: logging, metrics, tracing, alerting, dashboards
- security-compliance: auth, encryption, secrets, access control, compliance, audit
- ui-design: frontend visual changes, CSS, design tokens, component styling
- ai-ml: ML training, inference, model serving, embeddings — NOT merely calling an LLM API"""

_KEY = {"type": "work_type", "domain": "work_domain"}
_DEFS = {"type": _TYPE_DEFS, "domain": _DOMAIN_DEFS}


class LLMProvider(Protocol):
    def complete(self, system: str, user: str) -> str: ...


def _system(axis: str, allow_null: bool) -> str:
    key = _KEY[axis]
    null = f' or {{"{key}": null}} if none of the candidates fit' if allow_null else ""
    return (
        f"You are a pull request classifier. From the candidate {axis}s in the message, select the "
        f"single best one using these definitions. Weigh changed files and body over the title. "
        f'Respond with ONE JSON object only, no prose or code fences: {{"{key}": "<exact candidate>"}}{null}.'
        f"\n\nDefinitions:\n{_DEFS[axis]}"
    )


def _user(title: str, branch: str, candidates: list[str], allow_null: bool,
          files: list[str] | None, body: str | None) -> str:
    lines = [f"Title: {title}", f"Branch: {branch}"]
    if files:
        lines.append(f"Changed files: {', '.join(files[:15])}")
    if body:
        lines.append(f"Body excerpt: {' '.join(body.split()[:120])}")
    choose = "choose exactly one, or null if none fit" if allow_null else "choose exactly one"
    lines.append(f"Candidates ({choose}): {', '.join(candidates)}")
    return "\n".join(lines)


def disambiguate(
    provider: LLMProvider, axis: str, title: str, branch: str, candidates: list[str],
    *, allow_null: bool = False, files: list[str] | None = None, body: str | None = None,
) -> str | None:
    """Return the chosen candidate slug, or None (abstain / no-domain / unusable reply)."""
    try:
        reply = provider.complete(_system(axis, allow_null), _user(title, branch, candidates, allow_null, files, body))
        m = _JSON.search(reply or "")
        if not m:
            return None
        val = json.loads(m.group(0)).get(_KEY[axis])
    except Exception:
        return None
    if val is None:
        return None
    val = str(val).strip()
    return val if val in candidates else None


class GeminiProvider:
    """google-genai adapter. Bounds thinking (3.x thinking_level / 2.x thinking_budget)
    so the tiny JSON answer is not eaten by reasoning; retries transient 5xx.
    """

    _MAX_RETRIES = 4

    def __init__(self, *, model: str = "gemini-3.5-flash", api_key: str | None = None) -> None:
        from google import genai
        key = api_key or os.environ.get("GEMINI_API_KEY")
        if not key:
            raise RuntimeError("GEMINI_API_KEY not set (or pass api_key=)")
        self._client = genai.Client(api_key=key)
        self._model = model

    def complete(
        self, system: str, user: str, *, model: str | None = None, max_output_tokens: int = 256,
        temperature: float | None = None,
        response_json_schema: dict | None = None, timeout_s: float | None = None,
    ) -> str:
        """One reply. The keyword options default to the PR work-type call; another task sets
        its own output ceiling, a temperature sent to 3.x models too, a raw JSON Schema for the reply, and a
        budget every attempt (retries included) must finish inside."""
        import time
        from google.genai import types
        from google.genai.errors import ServerError

        model = model or self._model
        vm = re.search(r"gemini-(\d+)", model.lower())
        major = int(vm.group(1)) if vm else 3
        cfg = types.GenerateContentConfig(
            system_instruction=system, max_output_tokens=max_output_tokens, response_mime_type="application/json",
        )
        if response_json_schema is not None:
            cfg.response_json_schema = response_json_schema
        if major >= 3:
            cfg.thinking_config = types.ThinkingConfig(thinking_level=types.ThinkingLevel.MINIMAL)
            if temperature is not None:
                cfg.temperature = temperature
        else:
            cfg.temperature = 0.0 if temperature is None else temperature
            cfg.thinking_config = types.ThinkingConfig(thinking_budget=0)
        deadline = None if timeout_s is None else time.monotonic() + timeout_s
        last: Exception | None = None
        for attempt in range(self._MAX_RETRIES):
            if deadline is not None:
                remaining = deadline - time.monotonic()
                if remaining < 1:
                    break
                cfg.http_options = types.HttpOptions(timeout=int(remaining * 1000))
            try:
                return self._client.models.generate_content(model=model, contents=user, config=cfg).text or ""
            except ServerError as e:
                last = e
                pause = min(2 ** attempt, 8)
                if deadline is not None and deadline - time.monotonic() - pause < 1:
                    break
                time.sleep(pause)
        raise last if last else RuntimeError("gemini call failed")


class OpenAICompatibleProvider:
    """Any OpenAI-compatible /chat/completions endpoint — so adopters can self-host
    (Ollama, vLLM, LM Studio, …) or use a hosted API (OpenAI, Together, Groq, or
    Gemini's OpenAI-compat endpoint) without us shipping a model.

        # local (Ollama):
        OpenAICompatibleProvider(base_url="http://localhost:11434/v1", model="llama3.1")
        # hosted:
        OpenAICompatibleProvider(base_url="https://api.openai.com/v1", model="gpt-4o-mini")

    api_key falls back to $OPENAI_API_KEY; omit it for keyless local servers.
    """

    def __init__(self, *, base_url: str, model: str, api_key: str | None = None, timeout: float = 60) -> None:
        self._url = base_url.rstrip("/") + "/chat/completions"
        self._model = model
        self._key = api_key if api_key is not None else os.environ.get("OPENAI_API_KEY", "")
        self._timeout = timeout

    def complete(self, system: str, user: str) -> str:
        import httpx
        headers = {"Content-Type": "application/json"}
        if self._key:
            headers["Authorization"] = f"Bearer {self._key}"
        payload = {
            "model": self._model,
            "temperature": 0,
            "max_tokens": 256,
            "messages": [
                {"role": "system", "content": system},
                {"role": "user", "content": user},
            ],
        }
        r = httpx.post(self._url, json=payload, headers=headers, timeout=self._timeout)
        r.raise_for_status()
        return r.json()["choices"][0]["message"].get("content") or ""


__all__ = ["LLMProvider", "GeminiProvider", "OpenAICompatibleProvider",
           "disambiguate", "TYPE_BUCKETS", "DOMAIN_BUCKETS"]
