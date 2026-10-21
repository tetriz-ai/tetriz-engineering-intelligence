"""Copy the reviewed attribution inventory into a self-contained bundle."""
from __future__ import annotations

import hashlib
import json
from pathlib import Path


_FAMILY_FILES = ("MIT.txt", "Apache-2.0.txt", "BSD-3-Clause.txt", "ISC.txt")
_ENCODER_FILES = ("LICENSE.txt", "README.md", "source.json")


def _read_regular(path: Path) -> bytes:
    if path.is_symlink() or not path.is_file():
        raise ValueError(f"missing or linked notice file: {path}")
    return path.read_bytes()


def _filename(value: str) -> str:
    if not isinstance(value, str) or not value or Path(value).name != value or "\\" in value or value in (".", ".."):
        raise ValueError("notice inventory requires a plain filename")
    return value


def resources(datasets: Path) -> dict[str, bytes]:
    """Return required bundle-relative paths and exact reviewed source bytes."""
    licenses = datasets / "licenses"
    if datasets.is_symlink() or licenses.is_symlink() or (licenses / "encoder").is_symlink():
        raise ValueError("notice directories must not be symlinks")
    files = {"datasets/" + name: _read_regular(datasets / name)
             for name in ("THIRD_PARTY_NOTICES.md", "PROVENANCE.md")}
    source_bytes = _read_regular(licenses / "sources.json")
    sources = json.loads(source_bytes)
    names = {"sources.json", *_FAMILY_FILES}
    for source, meta in sources.items():
        name = _filename(meta.get("license_file"))
        raw = _read_regular(licenses / name)
        if hashlib.sha256(raw).hexdigest() != meta.get("license_sha256"):
            raise ValueError(f"source licence hash mismatch: {source}")
        names.add(name)
        if meta.get("notice_file"):
            names.add(_filename(meta["notice_file"]))
    for name in sorted(names):
        files["datasets/licenses/" + name] = _read_regular(licenses / name)
    for name in _ENCODER_FILES:
        files["datasets/licenses/encoder/" + name] = _read_regular(licenses / "encoder" / name)
    return files


def install(bundle: Path, datasets: Path) -> dict[str, str]:
    files = resources(datasets)
    # Preflight all destinations before writing. Never follow a linked directory
    # or overwrite differing local material when augmenting an existing bundle.
    for relative, raw in files.items():
        path = bundle / relative
        if bundle.is_symlink() or path.is_symlink():
            raise ValueError(f"linked notice destination: {path}")
        for parent in path.parents:
            if parent == bundle:
                break
            if parent.is_symlink() or (parent.exists() and not parent.is_dir()):
                raise ValueError(f"invalid notice directory: {parent}")
        if path.exists() and _read_regular(path) != raw:
            raise ValueError(f"existing notice differs: {path}; use a fresh bundle directory")
    for relative, raw in files.items():
        path = bundle / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        if not path.exists():
            with path.open("xb") as file:
                file.write(raw)
    return {name: hashlib.sha256(raw).hexdigest() for name, raw in sorted(files.items())}


def validate(bundle: Path, provenance: dict, datasets: Path) -> list[str]:
    problems = []
    legacy_export = bundle / "dataset.csv"
    if legacy_export.exists() or legacy_export.is_symlink():
        problems.append("runtime bundle must not contain dataset.csv; rebuild in a fresh directory")
    try:
        expected = resources(datasets)
        pinned = provenance.get("notice_files")
        hashes = {name: hashlib.sha256(raw).hexdigest() for name, raw in expected.items()}
        if pinned != hashes:
            problems.append("manifest notice inventory differs from reviewed source files")
        if provenance.get("attribution") != "datasets/THIRD_PARTY_NOTICES.md":
            problems.append("manifest attribution must resolve inside the bundle")
        for name, raw in expected.items():
            path = bundle / name
            for parent in path.parents:
                if parent.is_symlink():
                    raise ValueError(f"linked notice directory: {path}")
                if parent == bundle:
                    break
            if _read_regular(path) != raw:
                problems.append(f"packaged notice differs from source: {name}")
    except (OSError, ValueError, TypeError) as exc:
        problems.append(f"cannot validate packaged notices: {exc}")
    return problems
