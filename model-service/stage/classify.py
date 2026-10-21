# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

"""Classify harness artifacts into lifecycle stages by a strict-majority vote over samples.

The vote is the production classifier's. Each sample shuffles the eligible artifacts and sends
them in batches of ``BATCH_SIZE``, so an artifact's batch neighbours differ between samples:
batch composition is the dominant source of variance, and identical repeats would fake stability.
Sample ``s`` shuffles with ``rand.New(rand.NewSource(s + 1)).Shuffle`` from Go's math/rand,
ported bit for bit in ``stage.gorand``: the production classifier's rule and generator. The same
artifacts in the same order therefore get the production classifier's exact batches, and a repeat
request gets the same batches and stable labels.
An artifact's verdict is the bucket a strict
majority of the configured samples agree on. Without one it gets no verdict:

  - ``UNRESOLVED`` when every sample voted and none holds a majority;
  - ``DEFERRED`` when a sample's batch failed (provider error, malformed reply) and the votes
    that did arrive hold no majority.

A failed batch costs only its own votes in that sample. Only authored-practice types (skills,
agents, commands) are sent; any other type is ``SKIPPED``. Absent a verdict, a caller leaves the
artifact out of its stage map: absent is "unknown", while ``AIDLC_STAGE_UNSPECIFIED`` is a
definitive "serves no stage".
"""

from __future__ import annotations

import logging
import threading
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass, field
from typing import Any, Protocol

from stage.gorand import GoRand
from stage.prompt import RESPONSE_SCHEMA, SYSTEM_PROMPT, build_content, parse_reply, stage_for_bucket

logger = logging.getLogger(__name__)

# Sent with every call, so the model is explicit rather than the provider's default.
MODEL = "gemini-3.5-flash"
SAMPLES = 3
BATCH_SIZE = 25
# Calls in flight at once for one request.
CONCURRENCY = 4
# One model call's budget, retries included: a hung call costs its batch's votes in that sample
# after this long rather than holding up the request.
CALL_TIMEOUT_S = 55.0
MAX_OUTPUT_TOKENS = 8_192

# The authored-practice types, as aidlc.v1.HarnessArtifactType names; kept equal to
# aidlc/fold's practiceArtifactTypes. Installed configuration (hooks, rules, instruction files,
# plugins, prompt templates) never reaches a presence level, so it is never classified.
PRACTICE_TYPES = frozenset(
    {
        "HARNESS_ARTIFACT_TYPE_SKILL",
        "HARNESS_ARTIFACT_TYPE_AGENT",
        "HARNESS_ARTIFACT_TYPE_COMMAND",
    }
)

SUCCEEDED = "SUCCEEDED"
UNRESOLVED = "UNRESOLVED"
DEFERRED = "DEFERRED"
SKIPPED = "SKIPPED"


class StageProvider(Protocol):
    """The provider call this task makes: sdk.llm.GeminiProvider.complete with its options."""

    def complete(self, system: str, user: str, **options: Any) -> str: ...


@dataclass(frozen=True)
class Artifact:
    id: str
    name: str
    type: str
    description: str = ""
    body: str = ""


@dataclass
class Verdict:
    id: str
    outcome: str
    bucket: str | None = None
    stage: str | None = None
    votes: list[str] = field(default_factory=list)


@dataclass
class Classification:
    verdicts: list[Verdict]
    # Model calls (one per sample per batch) that failed, returned an unusable reply, or answered
    # fewer artifacts than their batch held.
    failed_calls: int = 0


def majority(votes: list[str], samples: int) -> str | None:
    """The bucket more than half of ``samples`` voted for, or None.

    The bar is the configured sample count, not the votes that arrived: two agreeing votes of
    three samples win even when the third never came, and one vote alone never does.
    """
    counts: dict[str, int] = {}
    best, best_n = None, 0
    for vote in votes:
        counts[vote] = counts.get(vote, 0) + 1
        if counts[vote] > best_n:
            best, best_n = vote, counts[vote]
    return best if best_n > samples // 2 else None


def classify(provider: StageProvider, artifacts: list[Artifact]) -> Classification:
    """One verdict per artifact, in request order."""
    eligible = [a for a in artifacts if a.type in PRACTICE_TYPES]
    # artifact id -> sample -> bucket, so the votes read back in sample order.
    votes: dict[str, dict[int, str]] = {a.id: {} for a in eligible}
    lock = threading.Lock()
    failed = 0

    def run_batch(sample: int, batch: list[Artifact]) -> None:
        nonlocal failed
        content = build_content([{"name": a.name, "description": a.description, "body": a.body} for a in batch])
        try:
            reply = provider.complete(
                SYSTEM_PROMPT,
                content,
                model=MODEL,
                max_output_tokens=MAX_OUTPUT_TOKENS,
                temperature=0.0,
                response_json_schema=RESPONSE_SCHEMA,
                timeout_s=CALL_TIMEOUT_S,
            )
            assigned = parse_reply(reply, len(batch))
        except Exception as exc:
            with lock:
                failed += 1
            logger.warning("classify-stage batch dropped for this sample: %s", type(exc).__name__)
            return
        with lock:
            for local_id, bucket in assigned.items():
                votes[batch[local_id].id][sample] = bucket
            # A reply that leaves artifacts out still counts what it did answer, but the ones it
            # dropped lost a vote just as a failed call's would.
            if len(assigned) < len(batch):
                failed += 1

    jobs: list[tuple[int, list[Artifact]]] = []
    for sample in range(SAMPLES):
        order = list(eligible)
        GoRand(sample + 1).shuffle(order)
        jobs.extend((sample, order[i : i + BATCH_SIZE]) for i in range(0, len(order), BATCH_SIZE))
    if jobs:
        with ThreadPoolExecutor(max_workers=CONCURRENCY) as pool:
            list(pool.map(lambda job: run_batch(*job), jobs))

    verdicts: list[Verdict] = []
    for a in artifacts:
        if a.type not in PRACTICE_TYPES:
            verdicts.append(Verdict(id=a.id, outcome=SKIPPED))
            continue
        cast = [bucket for _, bucket in sorted(votes[a.id].items())]
        bucket = majority(cast, SAMPLES)
        if bucket is not None:
            verdicts.append(Verdict(a.id, SUCCEEDED, bucket, stage_for_bucket(bucket), cast))
        elif len(cast) < SAMPLES:
            verdicts.append(Verdict(a.id, DEFERRED, votes=cast))
        else:
            verdicts.append(Verdict(a.id, UNRESOLVED, votes=cast))
    return Classification(verdicts=verdicts, failed_calls=failed)
