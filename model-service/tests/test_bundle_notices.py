"""Exercise real bundle assembly with a deterministic, offline encoder."""
import json
import sys

import numpy as np
import pytest

import build_bundle
from corpus import attribution, manifest_provenance
from sdk import embedding


def test_all_copyright_holders_are_retained():
    body = "Copyright 2000 First\nCopyright 2001 Second\nCopyright [yyyy] Template\nPermission notice\n"
    assert attribution._copyright_line(body).splitlines() == ["Copyright 2000 First", "Copyright 2001 Second"]


@pytest.fixture
def built_bundle(tmp_path, monkeypatch):
    class FakeEncoder:
        def __init__(self, *args, **kwargs):
            pass

        def encode(self, texts, **kwargs):
            return np.tile(np.arange(1, 385, dtype="float32"), (len(texts), 1))

        def save(self, path):
            from pathlib import Path
            dest = Path(path)
            dest.mkdir(parents=True)
            (dest / "offline-test-model.txt").write_text("fixture, not real weights\n")

    monkeypatch.setattr(build_bundle, "SentenceTransformer", FakeEncoder)
    monkeypatch.setattr(embedding, "SentenceTransformer", FakeEncoder)
    monkeypatch.setattr(sys, "argv", ["build_bundle.py", "--out", str(tmp_path), "--data-dir", str(attribution._DATASETS)])
    build_bundle.main()
    return tmp_path / build_bundle.BUNDLE_SUBPATH


def test_builder_packages_notices_and_preserves_runtime_contract(built_bundle):
    manifest = json.loads((built_bundle / "manifest.json").read_text())
    assert "notice_files" in manifest["provenance"]
    assert (built_bundle / manifest["provenance"]["attribution"]).is_file()
    assert (built_bundle / "datasets/licenses/encoder/LICENSE.txt").is_file()
    assert not (built_bundle / "dataset.csv").exists()
    assert attribution._validate_manifest(built_bundle / "manifest.json") == []
    loaded = embedding.EmbeddingBundle(built_bundle)
    assert loaded.axes() == ["type", "domain"]
    assert len(loaded.rank("type", "fix typo", "fix/typo")) == 7
    for axis, filename in [("type", "type_combined.csv"), ("domain", "domain_clean.csv")]:
        import hashlib
        assert manifest["provenance"]["corpus"][axis]["sha256"] == hashlib.sha256((attribution._DATASETS / filename).read_bytes()).hexdigest()


@pytest.mark.parametrize("relative", ["datasets/THIRD_PARTY_NOTICES.md", "datasets/licenses/LICENSE-expressjs-express.txt", "datasets/licenses/encoder/LICENSE.txt"])
def test_missing_packaged_notices_are_rejected(built_bundle, relative):
    (built_bundle / relative).unlink()
    assert attribution._validate_manifest(built_bundle / "manifest.json")


def test_tampered_notices_cannot_be_reapproved_by_rehashing_manifest(built_bundle):
    import hashlib
    path = built_bundle / "datasets/licenses/encoder/LICENSE.txt"
    path.write_text("not a license")
    manifest_path = built_bundle / "manifest.json"
    manifest = json.loads(manifest_path.read_text())
    manifest["provenance"]["notice_files"]["datasets/licenses/encoder/LICENSE.txt"] = hashlib.sha256(path.read_bytes()).hexdigest()
    manifest_path.write_text(json.dumps(manifest))
    assert attribution._validate_manifest(manifest_path)


def test_missing_notice_inventory_is_rejected(built_bundle):
    path = built_bundle / "manifest.json"
    manifest = json.loads(path.read_text())
    del manifest["provenance"]["notice_files"]
    path.write_text(json.dumps(manifest))
    assert attribution._validate_manifest(path)


def test_refresh_preserves_complete_upstream_license(tmp_path, monkeypatch):
    import base64
    import hashlib
    raw = b"Copyright 2000 First\nCopyright 2001 Second\n\nAll permission terms remain.\n"
    monkeypatch.setattr(attribution, "corpus_inventory", lambda: {"public:example/project": "MIT"})
    monkeypatch.setattr(attribution, "_LICENSES_DIR", tmp_path)
    monkeypatch.setattr(attribution, "_SOURCES_JSON", tmp_path / "sources.json")
    monkeypatch.setattr(attribution, "_gh_get", lambda path: {"license": {"spdx_id": "MIT"}, "content": base64.b64encode(raw).decode(), "sha": "upstream-blob", "html_url": "https://github.com/example/project/blob/main/LICENSE"})
    meta = attribution.refresh()["public:example/project"]
    assert (tmp_path / meta["license_file"]).read_bytes() == raw
    assert meta["license_sha256"] == hashlib.sha256(raw).hexdigest()
    assert meta["copyright"].splitlines() == ["Copyright 2000 First", "Copyright 2001 Second"]


@pytest.mark.parametrize("mode", ["missing", "edited", "linked", "removed-hash"])
def test_packaged_notice_tampering_fails(built_bundle, tmp_path, mode):
    path = built_bundle / "datasets/licenses/LICENSE-expressjs-express.txt"
    if mode == "missing":
        path.unlink()
    elif mode == "edited":
        path.write_text("truncated copyright notice")
    elif mode == "linked":
        outside = tmp_path / "outside-license.txt"
        outside.write_bytes(path.read_bytes())
        path.unlink()
        path.symlink_to(outside)
    else:
        manifest_path = built_bundle / "manifest.json"
        manifest = json.loads(manifest_path.read_text())
        del manifest["provenance"]["notice_files"]["datasets/licenses/LICENSE-expressjs-express.txt"]
        manifest_path.write_text(json.dumps(manifest))
    assert attribution._validate_manifest(built_bundle / "manifest.json")


def test_notice_install_preserves_conflicting_local_files(tmp_path):
    from corpus import bundle_notices
    path = tmp_path / "datasets/THIRD_PARTY_NOTICES.md"
    path.parent.mkdir()
    path.write_text("user material")
    with pytest.raises(ValueError, match="existing notice differs"):
        bundle_notices.install(tmp_path, attribution._DATASETS)
    assert path.read_text() == "user material"
    assert not (tmp_path / "datasets/licenses").exists()


def test_copyright_parser_excludes_license_prose():
    text = "Copyright 2024 Example Project\ncopyright notice that is included in or attached to the work\n(c) You must retain, in the Source form of any Derivative Works\nCopyright (c) Tokio Contributors\nCopyright OpenJS Foundation and other contributors\nCOPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM\n"
    assert attribution._copyright_line(text).splitlines() == ["Copyright 2024 Example Project", "Copyright (c) Tokio Contributors", "Copyright OpenJS Foundation and other contributors"]


def test_refresh_parser_preserves_reviewed_source_headers():
    sources = json.loads(attribution._SOURCES_JSON.read_text())
    for source, meta in sources.items():
        body = (attribution._LICENSES_DIR / meta["license_file"]).read_text()
        assert attribution._copyright_line(body) == meta["copyright"], source


@pytest.mark.parametrize("notice", ["(c) Example Contributors", "Copyright npm contributors"])
def test_yearless_lowercase_holder_notices_are_retained(notice):
    assert attribution._copyright_line(notice) == notice


@pytest.mark.parametrize("kind", ["file", "directory", "symlink", "dangling-symlink"])
def test_builder_refuses_old_training_export_before_loading_model(tmp_path, monkeypatch, kind):
    out = tmp_path / build_bundle.BUNDLE_SUBPATH
    out.mkdir(parents=True)
    legacy = out / "dataset.csv"
    outside = tmp_path / "outside.txt"
    if kind == "file":
        legacy.write_text("previous training export")
    elif kind == "directory":
        legacy.mkdir()
    else:
        if kind == "symlink":
            outside.write_text("preserve outside")
        legacy.symlink_to(outside)
    sentinel = out / "centroids-type.bin"
    sentinel.write_bytes(b"previous centroid")

    def unexpected_model_load(*args, **kwargs):
        pytest.fail("legacy export must be refused before loading a model")

    monkeypatch.setattr(build_bundle, "SentenceTransformer", unexpected_model_load)
    monkeypatch.setattr(sys, "argv", ["build_bundle.py", "--out", str(tmp_path)])
    with pytest.raises(ValueError, match="dataset.csv"):
        build_bundle.main()
    assert sentinel.read_bytes() == b"previous centroid"
    if kind == "file":
        assert legacy.read_text() == "previous training export"
    elif kind == "directory":
        assert legacy.is_dir()
    else:
        assert legacy.is_symlink()
    if kind == "symlink":
        assert outside.read_text() == "preserve outside"
    assert not (out / "manifest.json").exists()


def test_bundle_validation_rejects_reintroduced_training_export(built_bundle):
    (built_bundle / "dataset.csv").write_text("axis,bucket,text\n")
    assert any("dataset.csv" in error for error in attribution._validate_manifest(built_bundle / "manifest.json"))
