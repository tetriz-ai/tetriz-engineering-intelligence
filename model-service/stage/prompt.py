# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

"""The AIDLC stage classifier's prompt, reply schema, and bucket -> AidlcStage mapping.

The prompt is the production classifier's, word for word: at temperature 0 any edit moves its
answers, so it is kept verbatim. The model sorts each harness artifact into one of sixteen buckets: the
nine lifecycle stages, six exclusion buckets (real work that is no lifecycle stage), and ``none``.
A stage bucket maps to its ``aidlc.v1.AidlcStage`` name; every other bucket maps to
``AIDLC_STAGE_UNSPECIFIED``, the definitive "serves no stage".
"""

from __future__ import annotations

import json
from typing import Any

from stage.redact import redact

STAGE_BUCKETS: dict[str, str] = {
    "product_spec": "AIDLC_STAGE_PRODUCT_SPEC",
    "tech_spec": "AIDLC_STAGE_TECH_SPEC",
    "plan": "AIDLC_STAGE_PLAN",
    "code": "AIDLC_STAGE_CODE",
    "review": "AIDLC_STAGE_REVIEW",
    "test_qa": "AIDLC_STAGE_TEST_QA",
    "ci_cd": "AIDLC_STAGE_CI_CD",
    "deploy": "AIDLC_STAGE_DEPLOY",
    "operate": "AIDLC_STAGE_OPERATE",
}
EXCLUSION_BUCKETS: tuple[str, ...] = (
    "x_domain",
    "x_gtm",
    "x_generic_agent",
    "x_meta_harness",
    "x_harness_internal",
    "x_persona",
    "none",
)
# Declaration order, which the schema's enum keeps.
BUCKETS: tuple[str, ...] = (*STAGE_BUCKETS, *EXCLUSION_BUCKETS)
NO_STAGE = "AIDLC_STAGE_UNSPECIFIED"


def stage_for_bucket(bucket: str) -> str:
    """The AidlcStage name a bucket maps to; an exclusion bucket serves no stage."""
    return STAGE_BUCKETS.get(bucket, NO_STAGE)


SYSTEM_PROMPT = """\
You classify an engineering organisation's AI-harness artifacts (skills, slash-commands, agents) by the software-lifecycle stage each artifact serves. Each input carries the artifact's name and, when available, the opening of its body — judge from everything you are given. Classify each artifact into exactly one bucket.

The nine lifecycle stages:

- `product_spec` — turning a need into an agreed statement of what to build: PRDs, product specs, brainstorming, requirements, UX research, and visual/UI design work.
- `tech_spec` — deciding how to build it, written down before building: RFCs, ADRs, architecture and technical design documents.
- `plan` — turning agreed work into an ordered, sized set of tasks: issue creation, task breakdown, estimation, sprint or milestone planning.
- `code` — making the change: writing or refactoring application code, migrations, infrastructure-as-code, scripts; executing an implementation plan.
- `review` — deciding whether a change or document is fit to land: code review, PR review, review checklists and panels, and opening or describing a pull request.
- `test_qa` — establishing that the change works: writing or running tests, QA passes, reproducing bugs to verify a fix.
- `ci_cd` — turning a merged change into a build that can ship: pipelines, build configuration, build failures.
- `deploy` — putting the build in front of users: releases, rollouts, hotfix promotion, environment promotion.
- `operate` — keeping it working once it is live: production debugging, monitoring, alerts, incidents, on-call work.

The six exclusion buckets — work that is real but is NOT a lifecycle stage:

- `x_domain` — the organisation's own business domain (freight tendering, lending, payments operations, …): domain knowledge, not software process.
- `x_gtm` — go-to-market work: sales, marketing, growth, customer success, outreach, competitor and market research, pitch and sales material, pricing and landing-page experiments, investor- or partner-facing documents.
- `x_generic_agent` — generic agent or orchestration plumbing serving no single stage: dispatchers, multi-agent coordinators, subagent fan-out, scheduling of agent runs, generic worker loops, queue drains.
- `x_meta_harness` — work on the harness itself: creating, editing or reloading skills, rules, hooks, agents or plugins; prompt tooling; agent memory; harness configuration.
- `x_harness_internal` — harness-internal utilities: context compaction, session start/resume/end, worktree housekeeping, transcript handling.
- `x_persona` — persona, voice, tone or communication-style definitions that do no lifecycle work themselves.

And `none` — bare tool names, generic utilities, freeform noise, or anything genuinely unclassifiable.

Nine rules that are load-bearing:

1. A business-domain name is never a stage. Counting domain skills as lifecycle vocabulary reads domain investment as process maturity. A business-domain or go-to-market name keeps its `x_domain` or `x_gtm` bucket even when it carries a lifecycle word — review, test, plan, pipeline, deploy, monitoring, runbook, audit, validity — because there the word names the domain's own process (a loan review, a claims test case, a sales pipeline, a pricing-page A/B test, a document-validity check on customer paperwork), not software work.
2. Visual/UI "design" names are `product_spec`, never `tech_spec`. The word "design" alone must not award technical-design vocabulary.
3. A telemetry or event-record name — something that *describes an occurrence* rather than *names a capability someone invokes* (e.g. a file-opened record, a session-started event) — is `none`. An event about coding is not a coding skill.
4. Classify a writing or authoring skill by the *action it performs*, not the *topic it operates on*. A skill that writes god-scripts is `code` (the action is authoring code), not `operate` (its topic); a skill that writes migrations or Terraform is `code`, not `deploy`. Let the verb decide the stage, not the noun it acts on.
5. Reviewing, critiquing, grading or approving a software-lifecycle artifact — a PRD, a design doc, a plan, code, a pull request — is `review`; which lifecycle artifact is reviewed does not decide the stage. Opening, describing, updating or stacking a pull request is also `review`: it starts the review stage rather than authoring code. Rules 1 and 7 outrank this one: reviewing a sales deck, a pitch or a business document stays `x_gtm`/`x_domain`, and reviewing or auditing the harness's own agents, skills, hooks or prompts stays `x_meta_harness`.
6. An agent named for an engineering role (a frontend engineer, a backend developer, a temporal developer) does that role's lifecycle work and takes its stage — usually `code`. `x_persona` is only for definitions of voice, tone or character that perform no lifecycle work; `x_generic_agent` is only for role-less dispatch, scheduling or fan-out plumbing.
7. Managing the session itself — saving or resuming context, compaction, ending a session, creating or cleaning worktrees — is `x_harness_internal`. Creating, editing, reloading or configuring skills, rules, hooks, agents, plugins or agent memory is `x_meta_harness`. Neither is `code`, even though both touch files.
8. Generic utilities are `none` unless the body ties them to a stage: document-format skills (Word, spreadsheet, PDF, slides), drawing and diagramming, summarising, recapping, note-taking, translation, knowledge-graph building. Raw tool and command names — a shell, a file reader, a fetcher, `grep`, a CLI flag, an MCP tool identifier, a bare vendor name like `github` — are `none` too: a tool is not a capability that serves a stage.
9. A bare name with no body that fits several stages equally is `none`, never a guess. An unclassifiable artifact widens an interval; a guessed stage awards a level.

Answer with a JSON array only, one object per input, in this exact shape:
`[{"id": <input id>, "bucket": "<one of the buckets above>"}]`
No prose, no markdown fences, no explanation.

Classify these artifacts:"""

# The reply: one assignment per input, every bucket bound to the vocabulary. Sent as a raw JSON
# Schema with its keys in sorted order, byte for byte the schema the reference caller sends: the
# model writes properties in schema order, so "bucket" comes before "id", and reordering them
# measurably changes the answers at temperature 0.
RESPONSE_SCHEMA: dict[str, Any] = {
    "items": {
        "properties": {
            "bucket": {
                "description": "Exactly one bucket from the bound vocabulary.",
                "enum": list(BUCKETS),
                "type": "string",
            },
            "id": {"description": "The artifact's id, echoed verbatim from the input batch.", "type": "integer"},
        },
        "required": ["id", "bucket"],
        "type": "object",
    },
    "type": "array",
}


def build_content(items: list[dict[str, str]]) -> str:
    """One batch's user content: a JSON array of rows keyed by batch-local id.

    ``name`` is always sent; ``body`` only when present, redacted first, as the production
    classifier sends it. ``description`` is this service's addition, sent the same way when given.
    """
    rows: list[dict[str, Any]] = []
    for i, it in enumerate(items):
        row: dict[str, Any] = {"id": i, "name": it["name"]}
        if it.get("description"):
            row["description"] = redact(it["description"])
        if it.get("body"):
            row["body"] = redact(it["body"])
        rows.append(row)
    return json.dumps(rows)


def parse_reply(reply: str, batch_size: int) -> dict[int, str]:
    """The batch-local id -> bucket the reply assigns.

    A reply that is not a JSON array of ``{id: int, bucket: <known bucket>}`` objects raises
    ``ValueError``: like a failed call, it costs that batch its votes for the sample. So does an id
    that appears twice, which would otherwise let one row's answer stand in for another's. An id
    outside the batch is ignored.
    """
    rows = json.loads(reply)
    if not isinstance(rows, list):
        raise ValueError("reply is not a JSON array")
    out: dict[int, str] = {}
    for row in rows:
        if not isinstance(row, dict):
            raise ValueError("reply row is not an object")
        rid, bucket = row.get("id"), row.get("bucket")
        if not isinstance(rid, int) or isinstance(rid, bool) or bucket not in BUCKETS:
            raise ValueError(f"reply row {row!r} is not an id with a known bucket")
        if rid in out:
            raise ValueError(f"reply repeats id {rid}")
        if 0 <= rid < batch_size:
            out[rid] = bucket
    return out
