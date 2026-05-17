from __future__ import annotations

import getpass
import json
import platform
import re
import socket
import sys
import zipfile
from dataclasses import dataclass, field
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from pydantic import BaseModel, ConfigDict, Field

from piranesi import __version__
from piranesi.preflight import PreflightMode, build_preflight_report

_SENSITIVE_KEY_RE = re.compile(
    r"(?i)(api[_-]?key|authorization|cookie|credential|password|passwd|secret|session|token)"
)
_ASSIGNMENT_RE = re.compile(
    r"(?i)(?P<prefix>\b(?:api[_-]?key|authorization|cookie|credential|password|passwd|secret|session(?:id)?|token)\b\s*[=:]\s*)(?P<value>[^\s,;\"']+)"
)
_BEARER_RE = re.compile(r"(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{8,}")
_LONG_TOKEN_RE = re.compile(r"\b(?:sk|gho|ghp|xoxb|xoxp)-[A-Za-z0-9._-]{8,}\b")
_HOST_LIKE_RE = re.compile(r"\b(?:\d{1,3}\.){3}\d{1,3}\b")


class SupportBundleEntry(BaseModel):
    model_config = ConfigDict(extra="forbid")

    path: str
    source: str
    redacted: bool = True
    kind: str


class SupportBundleManifest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = 1
    generated_at: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    piranesi_version: str
    python: str
    platform: str
    project_root: str
    include_report_artifacts: bool
    preflight_mode: PreflightMode
    entries: list[SupportBundleEntry] = Field(default_factory=list)
    redaction: dict[str, object]


@dataclass(slots=True)
class SupportBundleOptions:
    output: Path
    project_root: Path = Path(".")
    config_path: Path | None = None
    report_path: Path | None = None
    include_report_artifacts: bool = False
    log_paths: list[Path] = field(default_factory=list)
    preflight_mode: PreflightMode = "workbench"


def create_support_bundle(options: SupportBundleOptions) -> SupportBundleManifest:
    output = options.output.expanduser().resolve(strict=False)
    if output.suffix.lower() != ".zip":
        output = output.with_suffix(".zip")
    output.parent.mkdir(parents=True, exist_ok=True)

    project_root = options.project_root.expanduser().resolve(strict=False)
    sensitive_values = _sensitive_values(project_root)
    entries: list[SupportBundleEntry] = []

    manifest = SupportBundleManifest(
        piranesi_version=__version__,
        python=sys.version.split()[0],
        platform=f"{platform.system()} {platform.release()}".strip(),
        project_root=_redact_text(str(project_root), sensitive_values=sensitive_values),
        include_report_artifacts=options.include_report_artifacts,
        preflight_mode=options.preflight_mode,
        entries=entries,
        redaction={
            "enabled": True,
            "guarantee": "best-effort",
            "redacted_values": sorted(sensitive_values.keys()),
        },
    )

    with zipfile.ZipFile(output, mode="w", compression=zipfile.ZIP_DEFLATED) as archive:
        _write_json(
            archive,
            "preflight.json",
            build_preflight_report(mode=options.preflight_mode).model_dump(mode="json"),
            entries,
            source="runtime preflight",
            kind="preflight",
            sensitive_values=sensitive_values,
        )
        _write_json(
            archive,
            "environment.json",
            _environment_payload(),
            entries,
            source="runtime environment",
            kind="environment",
            sensitive_values=sensitive_values,
        )
        _maybe_add_config(archive, options, entries, sensitive_values)
        _maybe_add_report(archive, options, entries, sensitive_values)
        _add_logs(archive, options, entries, sensitive_values)
        _write_text(
            archive,
            "README.txt",
            _readme_text(),
            entries,
            source="generated instructions",
            kind="readme",
            sensitive_values=sensitive_values,
        )
        manifest.entries = list(entries)
        _write_json(
            archive,
            "manifest.json",
            manifest.model_dump(mode="json"),
            entries=[],
            source="support bundle manifest",
            kind="manifest",
            sensitive_values=sensitive_values,
        )
    return manifest


def redact_payload(value: Any, *, sensitive_values: dict[str, str] | None = None) -> Any:
    sensitive_values = sensitive_values or _sensitive_values(Path.cwd())
    if isinstance(value, dict):
        redacted: dict[str, Any] = {}
        for key, nested in value.items():
            key_text = str(key)
            if _SENSITIVE_KEY_RE.search(key_text):
                redacted[key_text] = "[REDACTED]"
            else:
                redacted[key_text] = redact_payload(nested, sensitive_values=sensitive_values)
        return redacted
    if isinstance(value, list):
        return [redact_payload(item, sensitive_values=sensitive_values) for item in value]
    if isinstance(value, str):
        return _redact_text(value, sensitive_values=sensitive_values)
    return value


def _maybe_add_config(
    archive: zipfile.ZipFile,
    options: SupportBundleOptions,
    entries: list[SupportBundleEntry],
    sensitive_values: dict[str, str],
) -> None:
    config_path = options.config_path or options.project_root / "piranesi.toml"
    config_path = config_path.expanduser().resolve(strict=False)
    if not config_path.is_file():
        return
    _write_text(
        archive,
        "config/piranesi.toml.redacted",
        _safe_read_text(config_path),
        entries,
        source=str(config_path),
        kind="config",
        sensitive_values=sensitive_values,
    )


def _maybe_add_report(
    archive: zipfile.ZipFile,
    options: SupportBundleOptions,
    entries: list[SupportBundleEntry],
    sensitive_values: dict[str, str],
) -> None:
    report_path = _resolve_report_path(options.report_path)
    if report_path is None:
        return
    try:
        payload = json.loads(report_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        payload = {"path": str(report_path), "error": "could not parse report JSON"}
    _write_json(
        archive,
        "report/report-metadata.json",
        _report_metadata(report_path, payload),
        entries,
        source=str(report_path),
        kind="report_metadata",
        sensitive_values=sensitive_values,
    )
    if options.include_report_artifacts:
        _write_json(
            archive,
            f"report/artifacts/{report_path.name}",
            payload,
            entries,
            source=str(report_path),
            kind="report_artifact",
            sensitive_values=sensitive_values,
        )
        markdown = report_path.with_suffix(".md")
        if markdown.is_file():
            _write_text(
                archive,
                f"report/artifacts/{markdown.name}",
                _safe_read_text(markdown),
                entries,
                source=str(markdown),
                kind="report_artifact",
                sensitive_values=sensitive_values,
            )


def _add_logs(
    archive: zipfile.ZipFile,
    options: SupportBundleOptions,
    entries: list[SupportBundleEntry],
    sensitive_values: dict[str, str],
) -> None:
    seen: set[Path] = set()
    candidates = [path.expanduser().resolve(strict=False) for path in options.log_paths]
    report_path = _resolve_report_path(options.report_path)
    if report_path is not None:
        for name in ("scan.log", "piranesi.log"):
            candidates.append(report_path.parent / name)
        debug_dir = report_path.parent / "debug"
        if debug_dir.is_dir():
            candidates.extend(sorted(debug_dir.glob("*.json")))
    for path in candidates:
        if path in seen or not path.is_file():
            continue
        seen.add(path)
        _write_text(
            archive,
            f"logs/{path.name}.redacted",
            _safe_read_text(path),
            entries,
            source=str(path),
            kind="log",
            sensitive_values=sensitive_values,
        )


def _write_json(
    archive: zipfile.ZipFile,
    path: str,
    payload: Any,
    entries: list[SupportBundleEntry],
    *,
    source: str,
    kind: str,
    sensitive_values: dict[str, str],
) -> None:
    redacted = redact_payload(payload, sensitive_values=sensitive_values)
    archive.writestr(path, json.dumps(redacted, indent=2, sort_keys=True) + "\n")
    if entries is not None:
        entries.append(SupportBundleEntry(path=path, source=source, kind=kind))


def _write_text(
    archive: zipfile.ZipFile,
    path: str,
    text: str,
    entries: list[SupportBundleEntry],
    *,
    source: str,
    kind: str,
    sensitive_values: dict[str, str],
) -> None:
    archive.writestr(path, _redact_text(text, sensitive_values=sensitive_values))
    entries.append(SupportBundleEntry(path=path, source=source, kind=kind))


def _resolve_report_path(path: Path | None) -> Path | None:
    if path is None:
        return None
    candidate = path.expanduser().resolve(strict=False)
    if candidate.is_dir():
        for name in ("host-report.json", "fleet-report.json", "report.json"):
            report = candidate / name
            if report.is_file():
                return report
        return None
    return candidate if candidate.is_file() else None


def _report_metadata(report_path: Path, payload: Any) -> dict[str, Any]:
    if not isinstance(payload, dict):
        return {"path": str(report_path), "type": "unknown"}
    findings = payload.get("findings")
    return {
        "path": str(report_path),
        "file_name": report_path.name,
        "type": _report_type(report_path),
        "schema_version": payload.get("schema_version"),
        "generated_at": payload.get("generated_at"),
        "target": payload.get("target"),
        "summary": payload.get("summary") or payload.get("executive_summary"),
        "finding_count": len(findings) if isinstance(findings, list) else None,
    }


def _report_type(report_path: Path) -> str:
    return {
        "host-report.json": "host",
        "fleet-report.json": "fleet",
        "report.json": "source",
    }.get(report_path.name, "unknown")


def _environment_payload() -> dict[str, Any]:
    return {
        "piranesi_version": __version__,
        "python": sys.version,
        "executable": sys.executable,
        "platform": {
            "system": platform.system(),
            "release": platform.release(),
            "machine": platform.machine(),
            "python_implementation": platform.python_implementation(),
        },
        "argv": list(sys.argv),
    }


def _sensitive_values(project_root: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    for label, value in {
        "project_root": str(project_root),
        "user": getpass.getuser(),
        "hostname": socket.gethostname(),
    }.items():
        if value:
            values[value] = f"[REDACTED_{label.upper()}]"
    return values


def _redact_text(value: str, *, sensitive_values: dict[str, str]) -> str:
    redacted = value
    for raw, replacement in sorted(
        sensitive_values.items(), key=lambda item: len(item[0]), reverse=True
    ):
        if raw:
            redacted = redacted.replace(raw, replacement)
    redacted = _BEARER_RE.sub("Bearer [REDACTED]", redacted)
    redacted = _ASSIGNMENT_RE.sub(r"\g<prefix>[REDACTED]", redacted)
    redacted = _LONG_TOKEN_RE.sub("[REDACTED]", redacted)
    redacted = _HOST_LIKE_RE.sub("[REDACTED_IP]", redacted)
    return redacted


def _safe_read_text(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8", errors="replace")
    except OSError as exc:
        return f"could not read {path}: {exc}\n"


def _readme_text() -> str:
    return """Piranesi support bundle

This archive is generated locally for bug reports and community support.
It applies best-effort redaction to obvious secrets, local usernames, hostnames,
IP addresses, and configured paths. Review the files before sharing them.

Report artifacts are included only when explicitly requested.
"""


__all__ = [
    "SupportBundleEntry",
    "SupportBundleManifest",
    "SupportBundleOptions",
    "create_support_bundle",
    "redact_payload",
]
