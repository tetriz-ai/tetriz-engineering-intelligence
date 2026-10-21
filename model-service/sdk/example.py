"""Runnable example + smoke test for the PR Work-Type SDK.

Runs embedding-only (no API key needed). Pass a GeminiProvider for the full
cascade. Usage:  python -m sdk.example [bundle_dir]
"""

from __future__ import annotations

import sys

from sdk import PRWorkTypeClassifier

_SAMPLES = [
    ("fix: null pointer deref on empty input", "fix/npe-empty-input", ""),
    ("feat(api): add pagination to /users", "feat/users-pagination", ""),
    ("chore(deps): bump requests to 2.32.0", "renovate/requests", "renovate[bot]"),
    ("perf: cache compiled regex in hot path", "perf/regex-cache", ""),
    ("docs: fix typo in README quickstart", "docs/readme-typo", ""),
    ("Add gitleaks secret scanning to CI", "ci/gitleaks", ""),
    ("cherry-pick: backport auth fix to 2.x", "backport-2.x", ""),
]


def main() -> None:
    bundle = sys.argv[1] if len(sys.argv) > 1 else "artifacts/model-service/pr-classification/v1"
    clf = PRWorkTypeClassifier(bundle)  # embedding-only
    print(f"loaded bundle: {bundle} (embedding-only; pass a provider for the full cascade)\n")
    for title, branch, author in _SAMPLES:
        r = clf.classify(title, branch, author_login=author)
        t, d = r.type, r.domain
        print(f"- {title[:44]:46} -> type={t.bucket or '(abstain)'} [{t.method.value}"
              f"{'' if t.score is None else f' {t.score}'}]"
              f"  domain={d.bucket or '(none)'} [{d.method.value}]")


if __name__ == "__main__":
    main()
