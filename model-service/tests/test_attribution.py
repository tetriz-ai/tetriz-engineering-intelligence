"""Offline guard for the third-party attribution schedule (finding #6).

The corpora redistribute copied PR text under MIT/Apache-2.0/BSD-3-Clause/ISC,
so the committed schedule, the bundled licence texts and the live corpora must
never drift apart. These run in CI's Python job (no network): they assert the
committed artifacts are internally consistent, and exercise the manifest check
on an in-memory bundle manifest.
"""

from __future__ import annotations

import json

import pytest

from corpus import attribution


def test_committed_schedule_is_in_sync():
    # The whole guard: corpora vs sources.json vs THIRD_PARTY_NOTICES.md vs the
    # bundled licence texts. Raises SystemExit listing every drift if any.
    attribution.validate()


def test_every_corpus_licence_is_vetted_and_has_text():
    inv = attribution.corpus_inventory()
    assert inv, "no public sources found in the corpora"
    for source, spdx in inv.items():
        text_file = attribution._text_file(spdx)
        assert text_file and (attribution._LICENSES_DIR / text_file).is_file(), f"{source}: no text for {spdx}"


def test_bundled_apache_notices_exist():
    # Every schedule-referenced NOTICE must be committed (§4(d) wants a copy, not a link).
    sources = json.loads(attribution._SOURCES_JSON.read_text(encoding="utf-8"))
    referenced = [(s, m["notice_file"]) for s, m in sources.items() if m.get("notice_file")]
    assert referenced, "expected at least one bundled Apache NOTICE"
    for source, notice_file in referenced:
        assert (attribution._LICENSES_DIR / notice_file).is_file(), f"{source}: missing {notice_file}"


def test_owner_repo_rejects_malformed_source():
    assert attribution._owner_repo("public:tiangolo/fastapi") == "tiangolo/fastapi"
    for bad in ("public:evil/../etc", "public:no-slash", "public:a/b/c", "public:../x"):
        with pytest.raises(ValueError):
            attribution._owner_repo(bad)


def test_md_cell_neutralises_markdown():
    out = attribution._md_cell("[x](http://e)|`<b>`\x07")
    for ch in ("[", "]", "|", "`", "<", ">"):
        assert f"\\{ch}" in out
    assert "\x07" not in out  # control char stripped


def test_schedule_covers_exactly_the_corpus_sources():
    inv = attribution.corpus_inventory()
    sources = json.loads(attribution._SOURCES_JSON.read_text(encoding="utf-8"))
    assert set(sources) == set(inv), "sources.json and the corpora disagree — run `refresh`"
    for source, spdx in inv.items():
        assert sources[source]["spdx"] == spdx


def test_generate_is_deterministic():
    sources = json.loads(attribution._SOURCES_JSON.read_text(encoding="utf-8"))
    counts = attribution.canonical_row_counts()
    once = attribution.render_notices(sources, counts)
    assert once == attribution.render_notices(sources, counts)
    assert once == attribution._NOTICES_MD.read_text(encoding="utf-8")


def _complete_manifest(tmp_path):
    from corpus import manifest_provenance
    from corpus.encoder import MODEL_NAME, MODEL_REVISION
    path = tmp_path / "manifest.json"
    path.write_text(json.dumps({"model_id": MODEL_NAME, "model_revision": MODEL_REVISION}))
    manifest_provenance.augment(tmp_path, attribution._DATASETS / "type_combined.csv", attribution._DATASETS / "domain_clean.csv")
    return path


def test_manifest_matching_distribution_passes(tmp_path):
    assert attribution._validate_manifest(_complete_manifest(tmp_path)) == []


def test_manifest_wrong_distribution_is_caught(tmp_path):
    path = _complete_manifest(tmp_path)
    manifest = json.loads(path.read_text())
    for axis in ("type", "domain"):
        manifest["provenance"]["corpus"][axis]["licenses"] = {"mit": 1}
    path.write_text(json.dumps(manifest))
    problems = attribution._validate_manifest(path)
    assert len(problems) == 2 and all("distribution" in p for p in problems)


def test_manifest_missing_provenance_is_caught(tmp_path):
    manifest = tmp_path / "manifest.json"
    manifest.write_text(json.dumps({"model_id": "x"}), encoding="utf-8")
    assert attribution._validate_manifest(manifest), "a manifest without provenance must fail"
