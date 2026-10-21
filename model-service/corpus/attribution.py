"""Third-party attribution schedule for the public PR corpora.

Every committed corpus row is a PR title + head branch copied from a public
repository under a permissive licence (MIT / Apache-2.0 / BSD-3-Clause / ISC).
Those licences require that a redistribution carry each source's copyright and
licence notice — a repo-level SPDX label alone does not. This module builds the
schedule and bundles the texts those notices need; whether the obligation
attaches to short copied PR-title text is Legal's call (see
``datasets/PROVENANCE.md``).

- ``sources.json`` (committed, under ``datasets/licenses/``) is the provenance
  record: one entry per source repository with its SPDX licence, URL and the
  real upstream copyright notices (plus, for an Apache-2.0 source, the bundled
  NOTICE filename). It is rebuilt by ``refresh`` and is the only part that needs
  the network.
- ``THIRD_PARTY_NOTICES.md`` is generated from ``sources.json`` plus live row
  counts by ``generate`` — human-readable, grouped by licence, pointing at the
  bundled licence texts in ``datasets/licenses/``.
- ``validate`` is the offline guard (run in CI, see tests/test_attribution.py):
  it fails if the corpora reference a source the schedule omits, if a row's
  licence disagrees with the schedule, if a licence has no bundled text, or if
  the committed notices file has drifted from what ``generate`` would write. It
  can also check a built bundle's manifest licence distribution against the
  corpora.

Run everything from the ``model-service`` directory, as BUILD.md does.
"""

from __future__ import annotations

import argparse
import base64
import json
import hashlib
import os
import re
import sys
import unicodedata
import urllib.error
import urllib.request
from collections import Counter
from pathlib import Path

from corpus import manifest_provenance
from corpus.schema import read_rows

# Anchored to model-service/ (this file is corpus/attribution.py) so the tool and
# its tests resolve the corpora from any working directory, not only model-service/.
_MODEL_SERVICE = Path(__file__).resolve().parent.parent
_DATASETS = _MODEL_SERVICE / "datasets"
_LICENSES_DIR = _DATASETS / "licenses"
_SOURCES_JSON = _LICENSES_DIR / "sources.json"
_NOTICES_MD = _DATASETS / "THIRD_PARTY_NOTICES.md"

# The corpora carry lowercase SPDX ids; these are the canonical id + bundled
# licence-text filename each maps to. A corpus licence outside this set is an
# unvetted redistribution term and fails validation rather than passing silently.
_SPDX = {
    "mit": ("MIT", "MIT.txt"),
    "apache-2.0": ("Apache-2.0", "Apache-2.0.txt"),
    "bsd-3-clause": ("BSD-3-Clause", "BSD-3-Clause.txt"),
    "isc": ("ISC", "ISC.txt"),
}

# What a redistribution of copied text owes each family, stated as the permission
# rather than a bare licence name — this is what the schedule header spells out.
_REQUIREMENT = {
    "MIT": "retain the copyright notice and this permission notice",
    "ISC": "retain the copyright notice and this permission notice",
    "BSD-3-Clause": "retain the copyright notice, the conditions and the disclaimer",
    "Apache-2.0": "retain the copyright, NOTICE attributions and a copy of the licence",
}


# The canonical shipped corpora. The leakage-checked splits re-emit these same
# rows partitioned train/val/test, so displayed row counts come from these four
# only — counting the splits too would double every PR.
_CANONICAL = ("type_combined.csv", "domain_clean.csv", "gold_type.csv", "gold_domain.csv")


def _text_file(spdx: str) -> str | None:
    """Bundled licence-text filename for a canonical SPDX id, or None if unknown."""
    return next((f for canonical, f in _SPDX.values() if canonical == spdx), None)


def _md_cell(text: str) -> str:
    """Render an untrusted upstream string as a safe Markdown table cell.

    Upstream LICENSE/NOTICE copyright lines are data, not markup. Drop control and
    format characters (Unicode category C — C0/C1/format/etc.) plus the line and
    paragraph separators, then backslash-escape the Markdown/HTML-structural
    characters, so a crafted line cannot inject a link, image, inline code, table
    break or raw HTML into the generated schedule. (``\\`` is escaped first, so the
    escapes we add are not themselves re-escaped.) Printable text — accents, © —
    is kept.
    """
    text = "".join(
        ch for ch in text
        if unicodedata.category(ch)[0] != "C" and ch not in (" ", " ")
    )
    for ch in ("\\", "`", "|", "<", ">", "[", "]"):
        text = text.replace(ch, "\\" + ch)
    return text


def corpus_inventory(datasets_dir: Path = _DATASETS) -> dict[str, str]:
    """Map every ``public:<owner>/<repo>`` source in the corpora to its SPDX id.

    Scans all CSVs under ``datasets/`` (combined corpora, gold sets and the
    leakage-checked splits), so a source that appears only in a split is still
    accounted for. Raises on a row whose licence is outside the vetted set, or a
    source that carries two different licences across the corpora.
    """
    by_source: dict[str, str] = {}
    for csv_path in sorted(datasets_dir.rglob("*.csv")):
        for row in read_rows(csv_path):
            src = row.source
            if not src.startswith("public:"):
                # Only public-repo rows need upstream attribution; synthetic rows
                # carry their own "generated"/"CC-BY-4.0" licence and no source.
                continue
            lic = row.license.strip().lower()
            if lic not in _SPDX:
                raise ValueError(f"{csv_path.name}: source {src} has unvetted licence {row.license!r}")
            spdx = _SPDX[lic][0]
            prev = by_source.get(src)
            if prev and prev != spdx:
                raise ValueError(f"{src} appears under two licences: {prev} and {spdx}")
            by_source[src] = spdx
    return by_source


def canonical_row_counts(datasets_dir: Path = _DATASETS) -> dict[str, int]:
    """Public-repo rows per source across the canonical corpora only (no splits)."""
    counts: Counter[str] = Counter()
    for name in _CANONICAL:
        for row in read_rows(datasets_dir / name):
            if row.source.startswith("public:"):
                counts[row.source] += 1
    return dict(counts)


_OWNER_REPO_RE = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")


def _owner_repo(source: str) -> str:
    """``public:<owner>/<repo>`` → ``<owner>/<repo>``, rejecting a malformed value.

    The result is interpolated into a GitHub API path in ``refresh``; validating
    it here means a crafted corpus ``source`` (extra segments, ``.``/``..``) fails
    loudly instead of steering a token-authenticated request at an unintended path.
    """
    owner_repo = source.split("public:", 1)[1]
    owner, _, repo = owner_repo.partition("/")
    if not _OWNER_REPO_RE.match(owner_repo) or owner in (".", "..") or repo in (".", ".."):
        raise ValueError(f"malformed source {source!r}: expected 'public:<owner>/<repo>'")
    return owner_repo


# ---------------------------------------------------------------------------
# refresh — rebuild sources.json from the live upstream licences (network).
# ---------------------------------------------------------------------------

def _gh_get(path: str) -> dict | None:
    url = f"https://api.github.com{path}"
    req = urllib.request.Request(url, headers={"Accept": "application/vnd.github+json"})
    token = os.environ.get("GH_TOKEN") or os.environ.get("GITHUB_TOKEN")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        if exc.code == 404:
            return None
        raise


def _copyright_line(text: str) -> str:
    """Collect explicit holder notices; full LICENSE bytes remain authoritative."""
    found = []
    for raw in text.splitlines():
        line = raw.strip()
        match = re.match(r"^(?:copyright\s*(?:\(c\)\s*|©\s*)?|©\s*|\(c\)\s*)(.+)$", line, re.IGNORECASE)
        if not match:
            continue
        holder = match.group(1).strip()
        if any(marker in holder.lower() for marker in ("[yyyy]", "[year]", "<year>", "{yyyy}", "{year}")):
            continue
        if re.match(r"(?:holders?|notice|owners?|law|license)\b", holder, re.IGNORECASE):
            continue
        if line.lower().startswith("(c)") and holder.lower().startswith("you must retain"):
            continue
        found.append(line)
    return "\n".join(found)


def _fetch_notice(owner_repo: str) -> str | None:
    """Fetch an Apache-2.0 source's NOTICE, bundle it, return the bundled filename.

    Apache-2.0 §4(d) wants the NOTICE attributions to travel with a redistribution
    — a link is not a copy — so when a repo ships one we save its body under
    ``licenses/NOTICE-<owner>-<repo>.txt`` and record that name. Returns None when
    the repo has no NOTICE (then the schedule attributes at project level).
    """
    for name in ("NOTICE", "NOTICE.txt", "NOTICE.md"):
        notice = _gh_get(f"/repos/{owner_repo}/contents/{name}")
        if notice is not None:
            body = base64.b64decode(notice.get("content", "")).decode("utf-8", "replace")
            out_name = f"NOTICE-{owner_repo.replace('/', '-')}.txt"
            (_LICENSES_DIR / out_name).write_text(body, encoding="utf-8")
            return out_name
    return None


def refresh() -> dict:
    """Rebuild sources.json (and bundle Apache NOTICE files) from live upstream.

    Network + a GitHub token (``GH_TOKEN``/``GITHUB_TOKEN``) to clear the
    unauthenticated rate limit. Verifies the upstream SPDX against the corpus
    label, records all explicit upstream copyright lines (from LICENSE for MIT/BSD/ISC), and
    for Apache-2.0 bundles the repo's NOTICE where one exists. Maintainer command
    — run it when the corpus source set changes, then commit the result.
    """
    inv = corpus_inventory()
    out: dict[str, dict] = {}
    mismatches: list[str] = []
    unverified: list[str] = []
    for source in sorted(inv):
        owner_repo = _owner_repo(source)
        spdx = inv[source]
        url = f"https://github.com/{owner_repo}"
        copyright = ""
        lic = _gh_get(f"/repos/{owner_repo}/license")
        if lic is None:
            # No GitHub-detected licence to compare against — never silently count
            # this as "verified"; surface it so PROVENANCE's cross-check claim holds.
            unverified.append(source)
        else:
            gh_spdx = (lic.get("license") or {}).get("spdx_id") or ""
            if gh_spdx and gh_spdx.lower() != spdx.lower():
                mismatches.append(f"{source}: corpus={spdx} github={gh_spdx}")
            body = base64.b64decode(lic.get("content", ""))
            copyright = _copyright_line(body.decode("utf-8"))
        entry: dict[str, object] = {"spdx": spdx, "url": url, "copyright": copyright}
        if lic is not None:
            filename = f"LICENSE-{owner_repo.replace('/', '-')}.txt"
            (_LICENSES_DIR / filename).write_bytes(body)
            entry.update(license_file=filename, license_sha=lic["sha"],
                         license_sha256=hashlib.sha256(body).hexdigest(),
                         license_url=lic["html_url"])
        if spdx == "Apache-2.0":
            notice_file = _fetch_notice(owner_repo)
            if notice_file:
                entry["notice_file"] = notice_file
        out[source] = entry
        print(f"  {owner_repo:45s} {spdx:14s} {copyright[:60] or '(attribute to project, see LICENSE)'}", file=sys.stderr)
    if mismatches:
        raise SystemExit("SPDX mismatch between corpus and upstream:\n  " + "\n  ".join(mismatches))
    if unverified:
        print("WARNING: upstream licence not verified (GitHub returned no licence) for:\n  "
              + "\n  ".join(unverified), file=sys.stderr)
    _SOURCES_JSON.write_text(json.dumps(out, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    print(f"wrote {_SOURCES_JSON} ({len(out)} sources)", file=sys.stderr)
    return out


# ---------------------------------------------------------------------------
# generate — render THIRD_PARTY_NOTICES.md from sources.json + live counts.
# ---------------------------------------------------------------------------

def _load_sources() -> dict[str, dict]:
    return json.loads(_SOURCES_JSON.read_text(encoding="utf-8"))


def render_notices(sources: dict[str, dict], counts: dict[str, int]) -> str:
    lines: list[str] = []
    w = lines.append
    w("# Third-party notices — public PR corpora")
    w("")
    w("The datasets under `datasets/` redistribute each training example's **PR title**")
    w("and **head branch** copied from the public repositories listed below. Their licences")
    w("ask that a redistribution carry the source's copyright and licence notice — which a")
    w("repository-level SPDX label alone does not. This schedule names each source's copyright")
    w("and licence, the full licence texts are bundled under `licenses/`, and each Apache-2.0")
    w("NOTICE is bundled alongside. Whether these obligations attach to short copied PR-title")
    w("text is for Legal to determine — see [`PROVENANCE.md`](PROVENANCE.md).")
    w("")
    w("Generated from `datasets/licenses/sources.json` by `python -m corpus.attribution")
    w("generate`; do not edit by hand. Full licence texts are in `datasets/licenses/`.")
    w("")
    by_spdx: dict[str, list[str]] = {}
    for source, meta in sources.items():
        by_spdx.setdefault(meta["spdx"], []).append(source)
    for spdx in sorted(by_spdx):
        text_file = _text_file(spdx)
        rows = sum(counts.get(s, 0) for s in by_spdx[spdx])
        n = len(by_spdx[spdx])
        w(f"## {spdx} — {n} source{'s' if n != 1 else ''}, {rows} rows")
        w("")
        w(f"Redistribution must {_REQUIREMENT[spdx]}. Full text: [`licenses/{text_file}`](licenses/{text_file}).")
        w("")
        w("| Source repository | Copyright | Complete upstream licence |")
        w("|---|---|---|")
        for source in sorted(by_spdx[spdx]):
            meta = sources[source]
            owner_repo = _owner_repo(source)
            upstream = meta.get("copyright")
            if upstream:
                # Upstream LICENSE/NOTICE text — untrusted, neutralise markdown.
                cell = "; ".join(_md_cell(line) for line in upstream.splitlines())
            else:
                # No single authoritative line (Apache-2.0): attribute at project
                # level; where a NOTICE was bundled, point at the bundled copy.
                notice_file = meta.get("notice_file")
                if notice_file:
                    refs = f"LICENSE; NOTICE bundled at [`licenses/{notice_file}`](licenses/{notice_file})"
                else:
                    refs = f"LICENSE at {meta['url']}"
                cell = f"© the {owner_repo} project and its contributors — see its {refs}"
            licence = f"[LICENSE](licenses/{meta['license_file']})"
            if meta.get("notice_file"):
                licence += f"; [NOTICE](licenses/{meta['notice_file']})"
            w(f"| [{owner_repo}]({meta['url']}) | {cell} | {licence} |")
        w("")
    return "\n".join(lines).rstrip() + "\n"


def generate() -> str:
    text = render_notices(_load_sources(), canonical_row_counts())
    _NOTICES_MD.write_text(text, encoding="utf-8")
    print(f"wrote {_NOTICES_MD}", file=sys.stderr)
    return text


# ---------------------------------------------------------------------------
# validate — offline consistency guard (CI).
# ---------------------------------------------------------------------------

def validate(manifest: Path | None = None) -> None:
    """Fail loudly on any gap between the corpora, the schedule and the texts.

    Raises ``SystemExit`` with every problem found. With ``manifest`` it also
    checks a built bundle's provenance licence distribution against the corpora.
    """
    problems: list[str] = []
    # A missing/unreadable corpus or schedule is itself a validation failure —
    # report it as a clean SystemExit, not a raw traceback (validate's contract).
    try:
        inv = corpus_inventory()
        counts = canonical_row_counts()
    except (OSError, ValueError) as exc:
        raise SystemExit(f"attribution validation failed:\n  cannot read corpora: {exc}")
    try:
        sources = _load_sources()
    except (OSError, json.JSONDecodeError) as exc:
        raise SystemExit(f"attribution validation failed:\n  cannot read {_SOURCES_JSON}: {exc}")

    for source in sorted(set(inv) - set(sources)):
        problems.append(f"corpus source not in schedule: {source} — run `refresh`")
    for source in sorted(set(sources) - set(inv)):
        problems.append(f"schedule lists a source no corpus uses: {source} — run `refresh`")
    for source in sorted(set(inv) & set(sources)):
        if inv[source] != sources[source]["spdx"]:
            problems.append(f"{source}: corpus licence {inv[source]} != schedule {sources[source]['spdx']}")

    for spdx in sorted({m["spdx"] for m in sources.values()}):
        text_file = _text_file(spdx)
        if text_file is None or not (_LICENSES_DIR / text_file).is_file():
            problems.append(f"no bundled licence text for {spdx} under {_LICENSES_DIR}/")

    # Every Apache NOTICE the schedule references must actually be bundled.
    for source, meta in sources.items():
        notice_file = meta.get("notice_file")
        if notice_file and not (_LICENSES_DIR / notice_file).is_file():
            problems.append(f"{source}: bundled NOTICE {notice_file} is missing — run `refresh`")

    from corpus import bundle_notices
    try:
        bundle_notices.resources(_DATASETS)
    except (OSError, ValueError) as exc:
        problems.append(f"incomplete licence inventory: {exc}")

    if not _NOTICES_MD.is_file():
        problems.append(f"{_NOTICES_MD} is missing — run `generate`")
    elif _NOTICES_MD.read_text(encoding="utf-8") != render_notices(sources, counts):
        problems.append(f"{_NOTICES_MD} is stale — run `python -m corpus.attribution generate`")

    if manifest is not None:
        problems.extend(_validate_manifest(manifest))

    if problems:
        raise SystemExit("attribution validation failed:\n  " + "\n  ".join(problems))
    print(f"attribution OK: {len(sources)} sources, {sum(counts.values())} rows (canonical corpora)")


def _validate_manifest(manifest_path: Path) -> list[str]:
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        return [f"cannot read manifest {manifest_path}: {exc}"]
    prov = (manifest.get("provenance") or {}).get("corpus") or {}
    out: list[str] = []
    axis_csv = {"type": _DATASETS / "type_combined.csv", "domain": _DATASETS / "domain_clean.csv"}
    for axis, csv_path in axis_csv.items():
        pinned = (prov.get(axis) or {}).get("licenses")
        if pinned is None:
            out.append(f"manifest has no provenance.corpus.{axis}.licenses")
            continue
        # Compare against the same formula manifest_provenance used to WRITE it,
        # so the pin and the check can never drift from two copies of the formula.
        actual = manifest_provenance._corpus_summary(csv_path)["licenses"]
        if dict(sorted(pinned.items())) != actual:
            out.append(f"manifest {axis} licence distribution {pinned} != corpus {actual}")
    from corpus import bundle_notices
    out.extend(bundle_notices.validate(manifest_path.parent, manifest.get("provenance") or {}, _DATASETS))
    for axis, csv_path in axis_csv.items():
        expected_hash = hashlib.sha256(csv_path.read_bytes()).hexdigest()
        if (prov.get(axis) or {}).get("sha256") != expected_hash:
            out.append(f"manifest {axis} corpus sha256 differs from source")
    return out


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__)
    sub = p.add_subparsers(dest="cmd", required=True)
    sub.add_parser("refresh", help="rebuild sources.json from upstream (network)")
    sub.add_parser("generate", help="render THIRD_PARTY_NOTICES.md from sources.json")
    v = sub.add_parser("validate", help="offline consistency guard")
    v.add_argument("--manifest", type=Path, default=None, help="also check a built bundle manifest")
    args = p.parse_args()
    if args.cmd == "refresh":
        refresh()
    elif args.cmd == "generate":
        generate()
    else:
        validate(args.manifest)


if __name__ == "__main__":
    main()
