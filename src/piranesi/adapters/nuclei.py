from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from piranesi.workspace import (
    AffectedInstance,
    Confidence,
    EvidenceSnippet,
    NormalizedFinding,
    ServiceContext,
    Severity,
    SourceReference,
    deterministic_finding_id,
    utc_now,
)


class NucleiParseError(ValueError):
    """Raised when nuclei JSONL cannot be parsed into workspace findings."""


@dataclass(frozen=True)
class NucleiParseResult:
    findings: list[NormalizedFinding]
    warnings: list[str]
    metadata: dict[str, Any]


def parse_nuclei_jsonl_file(
    input_path: Path,
    *,
    input_sha256: str,
    raw_path: str,
) -> NucleiParseResult:
    try:
        lines = input_path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        raise NucleiParseError(f"cannot read nuclei JSONL: {exc}") from exc

    warnings: list[str] = []
    findings: list[NormalizedFinding] = []
    valid_records = 0
    template_ids: set[str] = set()
    malformed_lines = 0

    for line_number, line in enumerate(lines, start=1):
        stripped = line.strip()
        if not stripped:
            continue
        try:
            payload = json.loads(stripped)
        except json.JSONDecodeError as exc:
            malformed_lines += 1
            warnings.append(f"line {line_number}: invalid JSON ({exc.msg})")
            continue
        if not isinstance(payload, dict):
            malformed_lines += 1
            warnings.append(f"line {line_number}: expected JSON object")
            continue
        finding = _finding_from_record(
            payload,
            input_sha256=input_sha256,
            raw_path=raw_path,
            line_number=line_number,
            warnings=warnings,
        )
        if finding is None:
            malformed_lines += 1
            continue
        valid_records += 1
        template_ids.add(str(payload.get("template-id") or finding.title))
        findings.append(finding)

    if not lines or (valid_records == 0 and malformed_lines == 0):
        raise NucleiParseError("empty nuclei JSONL: input contains no records")
    if valid_records == 0:
        raise NucleiParseError("nuclei JSONL contained no valid records")

    metadata = {
        "format": "jsonl",
        "records": valid_records + malformed_lines,
        "valid_records": valid_records,
        "malformed_records": malformed_lines,
        "templates": sorted(template_ids),
        "summary": {
            "findings": len({finding.id for finding in findings}),
            "warnings": len(warnings),
        },
    }
    return NucleiParseResult(findings=findings, warnings=warnings, metadata=metadata)


def _finding_from_record(
    payload: dict[str, Any],
    *,
    input_sha256: str,
    raw_path: str,
    line_number: int,
    warnings: list[str],
) -> NormalizedFinding | None:
    template_id = _as_str(payload.get("template-id"))
    raw_info = payload.get("info")
    info: dict[str, Any] = raw_info if isinstance(raw_info, dict) else {}
    title = _as_str(info.get("name")) or template_id
    matched_at = _as_str(payload.get("matched-at")) or _as_str(payload.get("url"))
    if template_id is None:
        warnings.append(f"line {line_number}: missing template-id")
        return None
    if title is None:
        warnings.append(f"line {line_number}: missing nuclei finding name")
        return None
    if matched_at is None:
        warnings.append(f"line {line_number}: missing matched-at or url")
        return None

    now = utc_now()
    asset = _as_str(payload.get("host")) or _as_str(payload.get("ip"))
    service = _service_context(payload)
    severity = _map_severity(_as_str(info.get("severity")))
    source = SourceReference(
        tool="nuclei",
        input_sha256=input_sha256,
        raw_path=raw_path,
        locator=f"jsonl:{line_number}",
        metadata={
            "template_id": template_id,
            "template_path": _as_str(payload.get("template-path")),
            "matched_at": matched_at,
            "type": _as_str(payload.get("type")),
            "timestamp": _as_str(payload.get("timestamp")),
            "matcher_status": payload.get("matcher-status"),
        },
    )
    finding = NormalizedFinding(
        id=deterministic_finding_id("nuclei", template_id, matched_at),
        title=title,
        severity=severity,
        confidence=_confidence_from_match_status(payload.get("matcher-status")),
        description=_as_str(info.get("description")),
        remediation=_as_str(info.get("remediation")),
        asset=asset,
        service=service,
        weakness_ids=_weakness_ids(info.get("classification")),
        references=_string_list(info.get("reference")),
        tags=sorted({"nuclei", template_id, *_string_list(info.get("tags"))}),
        evidence=_evidence(payload, template_id=template_id, matched_at=matched_at),
        source_references=[source],
        affected_instances=[
            AffectedInstance(
                asset=asset or matched_at,
                service=service,
                location=matched_at,
                metadata={"template_id": template_id},
            )
        ],
        first_seen=now,
        last_seen=now,
        provenance={
            "tool": "nuclei",
            "template_id": template_id,
            "type": _as_str(payload.get("type")),
        },
    )
    return finding


def _service_context(payload: dict[str, Any]) -> ServiceContext | None:
    port = _as_int(payload.get("port"))
    scheme = _as_str(payload.get("scheme"))
    if port is None and scheme is None:
        return None
    return ServiceContext(port=port, protocol=scheme, name=scheme)


def _evidence(
    payload: dict[str, Any],
    *,
    template_id: str,
    matched_at: str,
) -> list[EvidenceSnippet]:
    snippets = [
        EvidenceSnippet(
            kind="nuclei-match",
            value=f"nuclei matched template {template_id} at {matched_at}",
            locator=matched_at,
        )
    ]
    for value in _string_list(payload.get("extracted-results")):
        snippets.append(EvidenceSnippet(kind="nuclei-extractor", value=value, locator=matched_at))
    request = _as_str(payload.get("request"))
    if request:
        snippets.append(
            EvidenceSnippet(
                kind="nuclei-request",
                value=request,
                redacted=True,
                locator=matched_at,
            )
        )
    response = _as_str(payload.get("response"))
    if response:
        snippets.append(
            EvidenceSnippet(
                kind="nuclei-response",
                value=response,
                redacted=True,
                locator=matched_at,
            )
        )
    curl_command = _as_str(payload.get("curl-command"))
    if curl_command:
        snippets.append(
            EvidenceSnippet(
                kind="nuclei-curl",
                value=curl_command,
                redacted=True,
                locator=matched_at,
            )
        )
    return snippets


def _weakness_ids(classification: object) -> list[str]:
    if not isinstance(classification, dict):
        return []
    weakness_ids: list[str] = []
    for key in ("cwe-id", "cve-id"):
        for value in _string_list(classification.get(key)):
            weakness_ids.append(value.upper())
    return sorted(set(weakness_ids))


def _map_severity(value: str | None) -> Severity:
    normalized = (value or "info").strip().lower()
    if normalized in {"critical", "high", "medium", "low"}:
        return normalized  # type: ignore[return-value]
    return "info"


def _confidence_from_match_status(value: object) -> Confidence:
    if value is True:
        return "confirmed"
    if value is False:
        return "low"
    return "high"


def _string_list(value: object) -> list[str]:
    if value is None:
        return []
    if isinstance(value, str):
        return [value] if value else []
    if isinstance(value, list):
        return [item for item in value if isinstance(item, str) and item]
    return []


def _as_str(value: object) -> str | None:
    if isinstance(value, str) and value:
        return value
    return None


def _as_int(value: object) -> int | None:
    if isinstance(value, int):
        return value
    if isinstance(value, str):
        try:
            return int(value)
        except ValueError:
            return None
    return None


__all__ = ["NucleiParseError", "NucleiParseResult", "parse_nuclei_jsonl_file"]
