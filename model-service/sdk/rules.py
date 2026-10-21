"""Layer 0 — deterministic type-axis rules.

Two empirically-precise rules, type axis only:
  - a known dependency-bot author (dependabot / renovate) → chore-deps
  - a cherry-pick marker in the title or head branch → CHERRY_PICK (classified by
    origin, not content)

Everything else abstains (returns None) so the embedding/LLM layers decide. There
are no domain rules — domain always escalates past Layer 0.
"""

from __future__ import annotations

from sdk.types import CHERRY_PICK

_BOT_AUTHORS = frozenset({"dependabot[bot]", "renovate[bot]"})
# Plain substring match (the validated rule); "cp" is excluded — it matches cpu/copy.
_CHERRY_MARKERS = ("cherry-pick", "cherrypick")


def classify_type(title: str, branch: str, author_login: str = "") -> str | None:
    """Return a type slug if a Layer-0 rule fires, else None (escalate).

    The bot rule is checked first — the higher-precision signal.
    """
    if author_login.strip().lower() in _BOT_AUTHORS:
        return "chore-deps"
    text = f"{title} {branch}".lower()
    if any(marker in text for marker in _CHERRY_MARKERS):
        return CHERRY_PICK
    return None
