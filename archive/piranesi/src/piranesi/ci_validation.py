from __future__ import annotations

import json
import zipfile
from pathlib import Path
from typing import Any

from pydantic import ValidationError

from piranesi.pff import load_and_validate_pff_file
from piranesi.report.pentest import REPORT_SCHEMA_VERSION, PentestReport
from piranesi.report.redteam import RED_TEAM_REPORT_SCHEMA_VERSION, RedTeamReport

RED_TEAM_ARCHIVE_SCHEMA_VERSION = "piranesi.red-team-archive.v1"


class CiValidationError(ValueError):
    """Raised when CI artifact validation fails."""


def validate_pff_artifact(path: Path | str) -> dict[str, Any]:
    artifact = Path(path)
    document = load_and_validate_pff_file(artifact)
    findings = document.get("findings")
    return {
        "path": str(artifact),
        "schema_version": document["schema_version"],
        "findings": len(findings) if isinstance(findings, list) else 0,
        "valid": True,
    }


def validate_report_bundle(path: Path | str) -> dict[str, Any]:
    bundle_path = Path(path)
    if bundle_path.is_dir():
        return _validate_report_directory(bundle_path)
    if bundle_path.suffix == ".zip":
        archive = _validate_red_team_archive(bundle_path)
        return {
            "path": str(bundle_path),
            "archives": [archive],
            "reports": [],
            "valid": True,
        }
    if bundle_path.suffix == ".json":
        report = _validate_report_json(bundle_path, bundle_path.read_text(encoding="utf-8"))
        return {
            "path": str(bundle_path),
            "archives": [],
            "reports": [report],
            "valid": True,
        }
    raise CiValidationError(f"unsupported report bundle path: {bundle_path}")


def _validate_report_directory(path: Path) -> dict[str, Any]:
    report_paths = sorted(item for item in path.rglob("*.json") if item.is_file())
    archive_paths = sorted(item for item in path.rglob("*.zip") if item.is_file())
    reports = _validate_report_json_paths(report_paths)
    archives = [_validate_red_team_archive(item) for item in archive_paths]
    if not reports and not archives:
        raise CiValidationError(f"no JSON reports or report archives found under {path}")
    return {
        "path": str(path),
        "archives": archives,
        "reports": reports,
        "valid": True,
    }


def _validate_report_json_paths(report_paths: list[Path]) -> list[dict[str, Any]]:
    reports: list[dict[str, Any]] = []
    for path in report_paths:
        body = path.read_text(encoding="utf-8")
        schema_version = _json_schema_version(path, body)
        if schema_version == "piranesi.pff.v0":
            continue
        reports.append(_validate_report_json(path, body))
    return reports


def _json_schema_version(path: Path, body: str) -> str | None:
    try:
        payload = json.loads(body)
    except json.JSONDecodeError as exc:
        raise CiValidationError(f"{path}: invalid JSON: {exc.msg}") from exc
    if not isinstance(payload, dict):
        raise CiValidationError(f"{path}: JSON artifact must be an object")
    version = payload.get("schema_version")
    return version if isinstance(version, str) else None


def _validate_report_json(path: Path, body: str) -> dict[str, Any]:
    try:
        payload = json.loads(body)
    except json.JSONDecodeError as exc:
        raise CiValidationError(f"{path}: invalid JSON report: {exc.msg}") from exc
    if not isinstance(payload, dict):
        raise CiValidationError(f"{path}: report JSON must be an object")
    version = payload.get("schema_version")
    try:
        if version == REPORT_SCHEMA_VERSION:
            finding_count = len(PentestReport.model_validate(payload).findings)
        elif version == RED_TEAM_REPORT_SCHEMA_VERSION:
            finding_count = len(RedTeamReport.model_validate(payload).findings)
        else:
            raise CiValidationError(f"{path}: unsupported report schema_version {version!r}")
    except ValidationError as exc:
        raise CiValidationError(f"{path}: invalid report schema: {exc}") from exc
    return {
        "path": str(path),
        "schema_version": version,
        "findings": finding_count,
    }


def _validate_red_team_archive(path: Path) -> dict[str, Any]:
    try:
        with zipfile.ZipFile(path) as archive:
            names = set(archive.namelist())
            if "archive-manifest.json" not in names:
                raise CiValidationError(f"{path}: missing archive-manifest.json")
            manifest = _load_archive_manifest(path, archive.read("archive-manifest.json"))
            entries = manifest.get("entries")
            if not isinstance(entries, list):
                raise CiValidationError(f"{path}: archive manifest entries must be an array")
            missing: list[str] = []
            for entry in entries:
                if not isinstance(entry, dict):
                    raise CiValidationError(f"{path}: archive manifest entry must be an object")
                entry_path = entry.get("path")
                if not isinstance(entry_path, str) or not entry_path:
                    raise CiValidationError(f"{path}: archive manifest entry missing path")
                _validate_relative_archive_path(path, entry_path)
                if entry_path not in names:
                    missing.append(entry_path)
            if missing:
                joined = ", ".join(missing[:5])
                raise CiValidationError(
                    f"{path}: archive manifest references missing entries: {joined}"
                )
    except zipfile.BadZipFile as exc:
        raise CiValidationError(f"{path}: invalid ZIP archive") from exc
    return {
        "path": str(path),
        "schema_version": RED_TEAM_ARCHIVE_SCHEMA_VERSION,
        "entries": len(entries),
    }


def _load_archive_manifest(path: Path, body: bytes) -> dict[str, Any]:
    try:
        payload = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise CiValidationError(f"{path}: invalid archive manifest JSON") from exc
    if not isinstance(payload, dict):
        raise CiValidationError(f"{path}: archive manifest must be an object")
    version = payload.get("schema_version")
    if version != RED_TEAM_ARCHIVE_SCHEMA_VERSION:
        raise CiValidationError(f"{path}: unsupported archive schema_version {version!r}")
    return payload


def _validate_relative_archive_path(path: Path, entry_path: str) -> None:
    candidate = Path(entry_path)
    if candidate.is_absolute() or any(part in {"", ".", ".."} for part in candidate.parts):
        raise CiValidationError(f"{path}: unsafe archive path {entry_path!r}")


__all__ = [
    "RED_TEAM_ARCHIVE_SCHEMA_VERSION",
    "CiValidationError",
    "validate_pff_artifact",
    "validate_report_bundle",
]
