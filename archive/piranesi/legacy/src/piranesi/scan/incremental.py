from __future__ import annotations

import json
from collections.abc import Mapping
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
from typing import Any

from piranesi.scan.transpile import collect_transpilable_files

_MANIFEST_FILENAME = "_manifest.json"


@dataclass(frozen=True, slots=True)
class ManifestEntry:
    sha256: str
    mtime: int


@dataclass(frozen=True, slots=True)
class FileManifest:
    target_dir: Path
    files: dict[str, ManifestEntry]

    @classmethod
    def from_dict(cls, payload: Mapping[str, Any]) -> FileManifest:
        raw_target_dir = payload.get("target_dir")
        raw_files = payload.get("files")
        if not isinstance(raw_target_dir, str) or not isinstance(raw_files, Mapping):
            raise ValueError("invalid manifest payload")

        files: dict[str, ManifestEntry] = {}
        for relative_path, raw_entry in raw_files.items():
            if not isinstance(relative_path, str) or not isinstance(raw_entry, Mapping):
                raise ValueError("invalid manifest entry")
            raw_hash = raw_entry.get("sha256")
            raw_mtime = raw_entry.get("mtime")
            if not isinstance(raw_hash, str) or not isinstance(raw_mtime, int):
                raise ValueError("invalid manifest entry payload")
            files[relative_path] = ManifestEntry(sha256=raw_hash, mtime=raw_mtime)

        return cls(
            target_dir=Path(raw_target_dir).resolve(strict=False),
            files=files,
        )

    def to_dict(self) -> dict[str, Any]:
        return {
            "target_dir": str(self.target_dir),
            "files": {
                relative_path: {
                    "sha256": entry.sha256,
                    "mtime": entry.mtime,
                }
                for relative_path, entry in sorted(self.files.items())
            },
        }


@dataclass(frozen=True, slots=True)
class IncrementalResult:
    added: set[Path]
    modified: set[Path]
    deleted: set[Path]
    unchanged: set[Path]

    @property
    def changed_files(self) -> set[Path]:
        return {*self.added, *self.modified}

    @property
    def has_changes(self) -> bool:
        return bool(self.added or self.modified or self.deleted)


def build_manifest(target_dir: Path) -> FileManifest:
    normalized_target = target_dir.resolve(strict=False)
    files: dict[str, ManifestEntry] = {}
    for path in collect_transpilable_files(normalized_target):
        stat = path.stat()
        files[path.relative_to(normalized_target).as_posix()] = ManifestEntry(
            sha256=_hash_file(path),
            mtime=stat.st_mtime_ns,
        )
    return FileManifest(target_dir=normalized_target, files=files)


def write_manifest(target_dir: Path, output_dir: Path) -> FileManifest:
    manifest = build_manifest(target_dir)
    normalized_output = output_dir.resolve(strict=False)
    normalized_output.mkdir(parents=True, exist_ok=True)
    (normalized_output / _MANIFEST_FILENAME).write_text(
        json.dumps(manifest.to_dict(), indent=2, sort_keys=True),
        encoding="utf-8",
    )
    return manifest


def load_manifest(
    output_dir: Path,
    *,
    expected_target_dir: Path | None = None,
) -> FileManifest | None:
    manifest_path = output_dir.resolve(strict=False) / _MANIFEST_FILENAME
    if not manifest_path.exists():
        return None

    try:
        payload = json.loads(manifest_path.read_text(encoding="utf-8"))
        manifest = FileManifest.from_dict(payload)
    except (OSError, ValueError, TypeError, json.JSONDecodeError):
        return None

    if expected_target_dir is not None:
        normalized_target = expected_target_dir.resolve(strict=False)
        if manifest.target_dir != normalized_target:
            return None

    return manifest


def diff_manifests(
    previous_manifest: FileManifest | None,
    current_manifest: FileManifest,
) -> IncrementalResult:
    if previous_manifest is None or previous_manifest.target_dir != current_manifest.target_dir:
        added = {Path(relative_path) for relative_path in current_manifest.files}
        return IncrementalResult(
            added=added,
            modified=set(),
            deleted=set(),
            unchanged=set(),
        )

    previous_files = previous_manifest.files
    current_files = current_manifest.files
    previous_paths = {Path(relative_path) for relative_path in previous_files}
    current_paths = {Path(relative_path) for relative_path in current_files}
    shared_paths = previous_paths & current_paths

    added = current_paths - previous_paths
    deleted = previous_paths - current_paths
    modified: set[Path] = set()
    unchanged: set[Path] = set()
    for relative_path in shared_paths:
        previous_entry = previous_files[relative_path.as_posix()]
        current_entry = current_files[relative_path.as_posix()]
        if (
            previous_entry.sha256 != current_entry.sha256
            or previous_entry.mtime != current_entry.mtime
        ):
            modified.add(relative_path)
        else:
            unchanged.add(relative_path)

    return IncrementalResult(
        added=added,
        modified=modified,
        deleted=deleted,
        unchanged=unchanged,
    )


def _hash_file(path: Path) -> str:
    digest = sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()
