from __future__ import annotations

import json
import shutil
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from hashlib import sha256
from pathlib import Path
from typing import Any

from piranesi import __version__
from piranesi.config import PiranesiConfig, config_hash
from piranesi.scan.cpg_graph import PiranesiCPG, deserialize_cpg, serialize_cpg

_CACHE_ROOT_PARTS = (".piranesi-cache", "cpg")
_CPG_FILENAME = "cpg.msgpack"
_CHECKSUM_FILENAME = "cpg.msgpack.sha256"
_METADATA_FILENAME = "metadata.json"


@dataclass(frozen=True, slots=True)
class CacheEntryInfo:
    project_hash: str
    path: Path
    size_bytes: int
    last_accessed: str | None
    metadata: dict[str, Any]


def cache_root(project_root: Path) -> Path:
    resolved = project_root.resolve(strict=False)
    return resolved.joinpath(*_CACHE_ROOT_PARTS)


def compute_cache_key(
    project_root: Path,
    config: PiranesiConfig,
    *,
    piranesi_version: str = __version__,
    joern_version: str,
) -> str:
    hasher = sha256()
    hasher.update(str(project_root.resolve(strict=False)).encode("utf-8"))
    hasher.update(piranesi_version.encode("utf-8"))
    hasher.update(joern_version.encode("utf-8"))
    hasher.update(config_hash(config).encode("utf-8"))
    return hasher.hexdigest()[:16]


def entry_dir(project_root: Path, project_hash: str) -> Path:
    return cache_root(project_root) / project_hash


def load_cached_cpg(
    project_root: Path,
    config: PiranesiConfig,
    *,
    joern_version: str,
    piranesi_version: str = __version__,
) -> tuple[str, PiranesiCPG | None]:
    project_hash = compute_cache_key(
        project_root,
        config,
        joern_version=joern_version,
        piranesi_version=piranesi_version,
    )
    cache_dir = entry_dir(project_root, project_hash)
    metadata = _load_metadata(cache_dir)
    if metadata is None:
        return project_hash, None

    cpg_path = cache_dir / _CPG_FILENAME
    checksum_path = cache_dir / _CHECKSUM_FILENAME
    if not cpg_path.exists() or not checksum_path.exists():
        return project_hash, None

    expected_checksum = checksum_path.read_text(encoding="utf-8").strip()
    actual_checksum = sha256(cpg_path.read_bytes()).hexdigest()
    if actual_checksum != expected_checksum:
        return project_hash, None

    cpg = deserialize_cpg(cpg_path)
    if (
        cpg.version != piranesi_version
        or cpg.joern_version != joern_version
        or cpg.config_hash != config_hash(config)
    ):
        return project_hash, None

    cpg.touch()
    _write_metadata(
        cache_dir,
        _metadata_payload(cpg, checksum=expected_checksum),
    )
    return project_hash, cpg


def write_cached_cpg(
    project_root: Path,
    config: PiranesiConfig,
    cpg: PiranesiCPG,
    *,
    joern_version: str,
    piranesi_version: str = __version__,
) -> str:
    project_hash = compute_cache_key(
        project_root,
        config,
        joern_version=joern_version,
        piranesi_version=piranesi_version,
    )
    cache_dir = entry_dir(project_root, project_hash)
    cache_dir.mkdir(parents=True, exist_ok=True)
    cpg.version = piranesi_version
    cpg.joern_version = joern_version
    cpg.config_hash = config_hash(config)
    cpg.project_root = str(project_root.resolve(strict=False))
    cpg.touch()
    checksum = serialize_cpg(cpg, cache_dir / _CPG_FILENAME)
    (cache_dir / _CHECKSUM_FILENAME).write_text(checksum, encoding="utf-8")
    _write_metadata(cache_dir, _metadata_payload(cpg, checksum=checksum))
    enforce_cache_limit(cache_root(project_root), config.scan.cpg_cache_max_mb * 1024 * 1024)
    return project_hash


def invalidate_cache(project_root: Path, *, project_hash: str | None = None) -> None:
    target = (
        entry_dir(project_root, project_hash)
        if project_hash is not None
        else cache_root(project_root)
    )
    shutil.rmtree(target, ignore_errors=True)


def cache_info(project_root: Path) -> dict[str, Any]:
    root = cache_root(project_root)
    entries = list(_iter_cache_entries(root))
    total_size = sum(entry.size_bytes for entry in entries)
    return {
        "cache_root": root,
        "entry_count": len(entries),
        "total_size_bytes": total_size,
        "entries": entries,
    }


def clear_cache(
    project_root: Path,
    *,
    stale_days: int | None = None,
) -> int:
    root = cache_root(project_root)
    parent = root.parent
    if not root.exists():
        return 0
    if stale_days is None:
        shutil.rmtree(root, ignore_errors=True)
        if parent.exists() and not any(parent.iterdir()):
            parent.rmdir()
        return 1

    removed = 0
    cutoff = datetime.now(UTC) - timedelta(days=stale_days)
    for entry in _iter_cache_entries(root):
        if entry.last_accessed is None:
            shutil.rmtree(entry.path, ignore_errors=True)
            removed += 1
            continue
        try:
            last_accessed = datetime.fromisoformat(entry.last_accessed)
        except ValueError:
            shutil.rmtree(entry.path, ignore_errors=True)
            removed += 1
            continue
        if last_accessed < cutoff:
            shutil.rmtree(entry.path, ignore_errors=True)
            removed += 1
    if root.exists() and not any(root.iterdir()):
        root.rmdir()
    if parent.exists() and not any(parent.iterdir()):
        parent.rmdir()
    return removed


def enforce_cache_limit(cache_root_dir: Path, max_bytes: int) -> None:
    entries = list(_iter_cache_entries(cache_root_dir))
    entries.sort(key=lambda entry: entry.last_accessed or "")
    total = sum(entry.size_bytes for entry in entries)
    while total > max_bytes and entries:
        entry = entries.pop(0)
        shutil.rmtree(entry.path, ignore_errors=True)
        total -= entry.size_bytes


def _iter_cache_entries(root: Path) -> list[CacheEntryInfo]:
    if not root.exists():
        return []
    entries: list[CacheEntryInfo] = []
    for project_dir in root.iterdir():
        if not project_dir.is_dir():
            continue
        metadata = _load_metadata(project_dir)
        if metadata is None:
            shutil.rmtree(project_dir, ignore_errors=True)
            continue
        size_bytes = sum(path.stat().st_size for path in project_dir.rglob("*") if path.is_file())
        entries.append(
            CacheEntryInfo(
                project_hash=project_dir.name,
                path=project_dir,
                size_bytes=size_bytes,
                last_accessed=metadata.get("last_accessed")
                if isinstance(metadata.get("last_accessed"), str)
                else None,
                metadata=metadata,
            )
        )
    return entries


def _metadata_payload(cpg: PiranesiCPG, *, checksum: str) -> dict[str, Any]:
    return {
        "version": cpg.version,
        "joern_version": cpg.joern_version,
        "config_hash": cpg.config_hash,
        "project_root": cpg.project_root,
        "function_count": len(cpg.functions),
        "call_edge_count": len(cpg.call_edges),
        "taint_flow_count": len(cpg.taint_flows),
        "file_count": len(cpg.file_hashes),
        "created_at": cpg.created_at,
        "updated_at": cpg.updated_at,
        "last_accessed": cpg.last_accessed,
        "checksum": checksum,
    }


def _load_metadata(cache_dir: Path) -> dict[str, Any] | None:
    metadata_path = cache_dir / _METADATA_FILENAME
    if not metadata_path.exists():
        return None
    try:
        payload = json.loads(metadata_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    return payload if isinstance(payload, dict) else None


def _write_metadata(cache_dir: Path, payload: dict[str, Any]) -> None:
    (cache_dir / _METADATA_FILENAME).write_text(
        json.dumps(payload, indent=2, sort_keys=True),
        encoding="utf-8",
    )


__all__ = [
    "CacheEntryInfo",
    "cache_info",
    "cache_root",
    "clear_cache",
    "compute_cache_key",
    "enforce_cache_limit",
    "entry_dir",
    "invalidate_cache",
    "load_cached_cpg",
    "write_cached_cpg",
]
