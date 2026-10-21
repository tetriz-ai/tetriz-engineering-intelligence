"""Public-corpus row schema + CSV read.

One row per labelled example: ``title``, ``branch``, ``bucket`` are the signal the
build and eval read; ``source``, ``source_url``, ``license`` carry provenance for
the redistribution audit and MUST NOT contain any customer org name, internal PR
number, or engineer login.
"""

from __future__ import annotations

import csv
from dataclasses import dataclass
from pathlib import Path

# Column order is stable — it is what the committed CSVs and their sha256 pin.
FIELDNAMES: tuple[str, ...] = ("source", "source_url", "license", "bucket", "title", "branch")


@dataclass(frozen=True)
class Row:
    source: str        # "public:<owner>/<repo>" | "synthetic:<generator-version>"
    source_url: str    # PR/commit URL for public rows; "" for synthetic
    license: str       # SPDX id (public) | "CC-BY-4.0" / "generated" (synthetic)
    bucket: str        # one taxonomy slug for this axis
    title: str         # signal — PR title as-is
    branch: str        # signal — head branch, raw

    def __post_init__(self) -> None:
        # A row with neither a title nor a branch assembles to empty text and
        # cannot train a centroid — reject at construction, not silently at build.
        if not self.title.strip() and not self.branch.strip():
            raise ValueError("row has neither title nor branch signal")
        if not self.bucket.strip():
            raise ValueError("row has no bucket label")


def read_rows(path: Path) -> list[Row]:
    """Read a public-corpus CSV. utf-8 explicit: titles carry em-dashes."""
    with path.open(encoding="utf-8", newline="") as f:
        reader = csv.DictReader(f)
        missing = set(FIELDNAMES) - set(reader.fieldnames or ())
        if missing:
            raise ValueError(f"{path.name}: missing columns {sorted(missing)}")
        return [Row(**{k: (r.get(k) or "") for k in FIELDNAMES}) for r in reader]
