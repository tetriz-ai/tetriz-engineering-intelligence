"""Per-axis input-text recipe for the PR-work-type task — the single source
shared by centroid-generation (offline) and the embed-classify endpoint
(online). Frozen public contract; keep byte-identical across build and serve.

The two axes classify orthogonal properties, so they use different input
orderings:

    type   = "{title}. branch: {cleaned_branch}"   (title-first — type verbs
             like feat/fix/docs live in the title and anchor the embedding)
    domain = "branch: {cleaned_branch}. {title}"   (branch-first — the branch
             name carries the strongest domain signal, put it at the front)

Collapsing the two into one shared input measurably degrades domain accuracy, so
the axes are embedded separately. **Build and serve must use this same recipe**
(centroids built title-first but served branch-first would silently emit garbage
cosine scores) — hence one module, wired into both the bundle generator and the
served model through the task spec.

Title is taken **as-is** (whitespace-trimmed only): the conventional-commit verb
(`feat`, `docs`, …) is type signal we keep. Only the branch is cleaned: the
leading person-login segment is dropped, separators are normalised to spaces,
and a bare Jira-ticket branch (no descriptive tail) is dropped entirely.
"""

from __future__ import annotations

import re

from contract.taxonomy import AXIS_DOMAIN, AXIS_TYPE

# Leading branch segment kept iff it is a conventional-commit / area keyword
# (carries type or domain signal); any other leading segment before the first
# "/" is treated as a person login and dropped. The split is between keywords
# (feat/fix/chore/docs/cherry/…) and author logins (e.g. "alice/", "bob/",
# "dev-3/") which carry no work-type signal.
BRANCH_KEEP_PREFIXES = frozenset({
    "feat", "feature", "fix", "bugfix", "bug", "hotfix", "chore", "docs", "doc",
    "refactor", "refac", "refact", "test", "tests", "perf", "performance", "ci",
    "build", "style", "revert", "infra", "release", "cherry", "deps", "dep",
    "revamp", "enhancement", "enhance", "wip", "security", "develop", "proposal",
    "decisions", "decision", "adr", "prd", "design",
})

# A branch that is *only* a Jira ticket id (e.g. "PROJ-123", "PROJ-4") carries
# no semantic signal beyond the ticket number — dropped entirely. A ticket with
# a descriptive tail ("PROJ-42-user-login") is kept and normalised.
_BARE_JIRA_BRANCH = re.compile(r"^[A-Z][A-Z0-9]+-[A-Z]?\d+$")
_SEPARATORS = re.compile(r"[\s/_-]+")


def stripped_title(title: str) -> str:
    """Title as-is, whitespace-trimmed only."""
    return title.strip()


def cleaned_branch(branch: str) -> str:
    """Drop the person-login prefix + bare Jira branches; normalise separators.

    Returns "" when the branch carries no usable signal (empty, or a bare Jira
    ticket id) — the caller decides whether to keep the row title-only or drop it.
    """
    b = branch.strip()
    if not b or _BARE_JIRA_BRANCH.match(b):
        return ""
    if "/" in b:
        head, rest = b.split("/", 1)
        if head.lower() not in BRANCH_KEEP_PREFIXES:  # head is a person login
            b = rest
            if _BARE_JIRA_BRANCH.match(b):  # remainder is itself a bare ticket
                return ""
    return _SEPARATORS.sub(" ", b).strip()


def type_input(title: str, branch: str) -> str:
    """Type axis input — title-first."""
    cb = cleaned_branch(branch)
    return f"{stripped_title(title)}. branch: {cb}" if cb else stripped_title(title)


def domain_input(title: str, branch: str) -> str:
    """Domain axis input — branch-first."""
    cb = cleaned_branch(branch)
    return f"branch: {cb}. {stripped_title(title)}" if cb else stripped_title(title)


# Per-axis ordering descriptor, wired into the task's AxisSpec, pinned into the
# centroid manifest, and re-asserted at load: centroids built under one ordering
# served under another would silently emit garbage cosine scores. `{title}` /
# `{cleaned_branch}` mark the substitution points; the value is the *ordering*,
# the literal recipe the builders above apply.
INPUT_RECIPE = {
    AXIS_TYPE: "{title}. branch: {cleaned_branch}",
    AXIS_DOMAIN: "branch: {cleaned_branch}. {title}",
}
