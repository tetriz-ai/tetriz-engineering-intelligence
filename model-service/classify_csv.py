"""Batch classifier — CSV of PRs in, classifications out (and optionally stored).

The end-to-end entrypoint: read a CSV of PR data, run each row through the full
cascade (Layer-0 rules → embedding → LLM), write the results to a CSV, and
optionally persist them to Postgres.

Input CSV: a `title` column is required; `branch`, `author_login`, `files`
(pipe-separated: "a.go|b.go"), and `body` are optional and improve accuracy.

    # embedding-only (no key): confident rows classified, the rest abstain
    python classify_csv.py --in prs.csv --out results.csv

    # full cascade (rules -> embedding -> LLM), reads GEMINI_API_KEY from env
    python classify_csv.py --in prs.csv --out results.csv --llm

    # also store to Postgres
    python classify_csv.py --in prs.csv --out results.csv --llm --db "$DATABASE_URL"
"""

from __future__ import annotations

import argparse
import csv
import sys
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

from sdk import PRWorkTypeClassifier

_OUT_FIELDS = ("title", "branch", "type_bucket", "type_method", "type_score",
               "domain_bucket", "domain_method")


def _files(cell: str | None) -> list[str] | None:
    if not cell:
        return None
    return [f.strip() for f in cell.split("|") if f.strip()] or None


def _read(path: Path) -> list[dict]:
    with path.open(encoding="utf-8", newline="") as f:
        reader = csv.DictReader(f)
        if not reader.fieldnames or "title" not in reader.fieldnames:
            sys.exit("input CSV must have a 'title' column")
        return list(reader)


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--in", dest="inp", required=True, type=Path)
    p.add_argument("--out", required=True, type=Path)
    p.add_argument("--bundle", default="artifacts/model-service/pr-classification/v1")
    p.add_argument("--llm", action="store_true", help="enable the LLM layer")
    p.add_argument("--provider", choices=["gemini", "openai"], default="gemini",
                   help="LLM backend: gemini (GEMINI_API_KEY) or openai-compatible (--base-url/--model)")
    p.add_argument("--base-url", default=None, help="openai provider: endpoint, e.g. http://localhost:11434/v1")
    p.add_argument("--model", default="gpt-4o-mini", help="openai provider: model id")
    p.add_argument("--topk", type=int, default=0, help="0 = full taxonomy on escalation")
    p.add_argument("--db", default=None, help="DATABASE_URL to also persist results")
    p.add_argument("--workers", type=int, default=4)
    args = p.parse_args()

    provider = None
    if args.llm:
        if args.provider == "openai":
            from sdk import OpenAICompatibleProvider
            if not args.base_url:
                sys.exit("--provider openai needs --base-url (e.g. http://localhost:11434/v1)")
            provider = OpenAICompatibleProvider(base_url=args.base_url, model=args.model)
        else:
            from sdk import GeminiProvider
            provider = GeminiProvider()
    clf = PRWorkTypeClassifier(args.bundle, llm_provider=provider, topk=args.topk)

    store = None
    if args.db:
        from server.storage import Storage
        store = Storage(args.db)

    rows = _read(args.inp)
    print(f"classifying {len(rows)} PRs ({'full cascade' if provider else 'embedding-only'}) …")

    def classify(row: dict):
        return row, clf.classify(
            row.get("title", ""), row.get("branch", "") or "",
            author_login=row.get("author_login", "") or "",
            files=_files(row.get("files")), body=row.get("body") or None,
        )

    results = []
    with ThreadPoolExecutor(max_workers=max(1, args.workers)) as pool:
        for row, c in pool.map(classify, rows):
            results.append({
                "title": row.get("title", ""), "branch": row.get("branch", "") or "",
                "type_bucket": c.type.bucket, "type_method": c.type.method.value, "type_score": c.type.score,
                "domain_bucket": c.domain.bucket or "", "domain_method": c.domain.method.value,
            })
            if store:
                store.save(title=row.get("title", ""), branch=row.get("branch", "") or "",
                           type_bucket=c.type.bucket, type_method=c.type.method.value, type_score=c.type.score,
                           domain_bucket=c.domain.bucket, domain_method=c.domain.method.value)

    args.out.parent.mkdir(parents=True, exist_ok=True)
    with args.out.open("w", encoding="utf-8", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(_OUT_FIELDS))
        w.writeheader()
        w.writerows(results)
    if store:
        store.close()

    print(f"wrote {len(results)} rows -> {args.out}"
          + (f" (+ stored to Postgres)" if store else ""))
    print("type:  ", dict(sorted(Counter(r["type_bucket"] or "(abstain)" for r in results).items())))
    print("domain:", dict(sorted(Counter(r["domain_bucket"] or "(none)" for r in results).items())))


if __name__ == "__main__":
    main()
