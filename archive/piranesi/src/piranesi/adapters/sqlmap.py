from __future__ import annotations

import json
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

from piranesi.workspace import (
    AffectedInstance,
    EvidenceSnippet,
    NormalizedFinding,
    ServiceContext,
    Severity,
    SourceReference,
    deterministic_finding_id,
    utc_now,
)


class SqlmapParseError(ValueError):
    """Raised when sqlmap artifacts cannot be parsed into findings."""


@dataclass(frozen=True)
class SqlmapParseResult:
    findings: list[NormalizedFinding]
    warnings: list[str]
    metadata: dict[str, Any]


def parse_sqlmap_file(
    input_path: Path,
    *,
    input_sha256: str,
    raw_path: str,
) -> SqlmapParseResult:
    try:
        text = input_path.read_text(encoding="utf-8")
    except OSError as exc:
        raise SqlmapParseError(f"cannot read sqlmap artifact: {exc}") from exc

    if not text.strip():
        raise SqlmapParseError("empty sqlmap artifact: input contains no records")

    warnings: list[str] = []
    records = _json_records(text, warnings=warnings)
    format_name = "sqlmap-json"
    if records is None:
        records = _text_records(text)
        format_name = "sqlmap-log"

    if not records:
        raise SqlmapParseError("sqlmap artifact contained no vulnerability records")

    findings: list[NormalizedFinding] = []
    for index, record in enumerate(records, start=1):
        finding = _finding_from_record(
            record,
            input_sha256=input_sha256,
            raw_path=raw_path,
            record_number=index,
            warnings=warnings,
        )
        if finding is not None:
            findings.append(finding)

    if not findings:
        raise SqlmapParseError("sqlmap artifact contained no valid vulnerability records")

    metadata = {
        "format": format_name,
        "records": len(records),
        "valid_records": len(findings),
        "malformed_records": len(records) - len(findings),
        "summary": {
            "findings": len({finding.id for finding in findings}),
            "warnings": len(warnings),
        },
    }
    return SqlmapParseResult(findings=findings, warnings=warnings, metadata=metadata)


def _json_records(text: str, *, warnings: list[str]) -> list[dict[str, Any]] | None:
    try:
        payload = json.loads(text)
    except json.JSONDecodeError:
        return None
    if not isinstance(payload, dict):
        warnings.append("json artifact: expected top-level object")
        return []
    vulnerabilities = payload.get("vulnerabilities")
    if isinstance(vulnerabilities, list):
        return [
            {**item, "target": payload.get("target"), "dbms": payload.get("dbms")}
            for item in vulnerabilities
            if isinstance(item, dict)
        ]
    if payload.get("parameter") or payload.get("payload"):
        return [payload]
    return []


def _text_records(text: str) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    current: dict[str, Any] = {}
    target_match = re.search(r"testing URL '([^']+)'", text)
    if target_match:
        current["target"] = target_match.group(1)
    for line in text.splitlines():
        parameter_match = re.search(r"Parameter:\s*([^ ]+)\s*\(([^)]+)\)", line)
        if parameter_match:
            if current.get("parameter"):
                records.append(current)
                current = {}
            current["parameter"] = parameter_match.group(1)
            current["place"] = parameter_match.group(2)
            continue
        type_match = re.search(r"Type:\s*(.+)", line)
        if type_match:
            current["type"] = type_match.group(1).strip()
            continue
        payload_match = re.search(r"Payload:\s*(.+)", line)
        if payload_match:
            current["payload"] = payload_match.group(1).strip()
    if current.get("parameter") or current.get("payload"):
        records.append(current)
    return records


def _finding_from_record(
    record: dict[str, Any],
    *,
    input_sha256: str,
    raw_path: str,
    record_number: int,
    warnings: list[str],
) -> NormalizedFinding | None:
    target = _as_str(record.get("url")) or _as_str(record.get("target"))
    parameter = _as_str(record.get("parameter"))
    place = _as_str(record.get("place")) or _as_str(record.get("method"))
    injection_type = _as_str(record.get("type")) or _as_str(record.get("title"))
    if target is None:
        warnings.append(f"record {record_number}: missing target or url")
        return None
    if parameter is None:
        warnings.append(f"record {record_number}: missing injectable parameter")
        return None

    parsed = urlparse(target)
    asset = parsed.hostname or target
    service = _service_context(target)
    title = f"sqlmap reported SQL injection in {place or 'parameter'} {parameter}"
    locator = f"record[{record_number}]"
    now = utc_now()
    source = SourceReference(
        tool="sqlmap",
        input_sha256=input_sha256,
        raw_path=raw_path,
        locator=locator,
        metadata={
            "target": target,
            "parameter": parameter,
            "place": place,
            "type": injection_type,
            "dbms": _as_str(record.get("dbms")),
        },
    )
    return NormalizedFinding(
        id=deterministic_finding_id("sqlmap", target, place, parameter, injection_type),
        title=title,
        severity=_map_severity(record.get("risk")),
        confidence="tool-observed",
        description=injection_type,
        remediation="Use parameterized queries and validate data access paths for this input.",
        asset=asset,
        service=service,
        weakness_ids=_weakness_ids(record),
        references=_references(record),
        tags=["sqlmap", "sql-injection", "cwe-89"],
        evidence=_evidence(record, target=target, parameter=parameter, locator=locator),
        source_references=[source],
        affected_instances=[
            AffectedInstance(
                asset=asset,
                service=service,
                location=target,
                metadata={"parameter": parameter, "place": place, "type": injection_type},
            )
        ],
        first_seen=now,
        last_seen=now,
        provenance={"tool": "sqlmap", "type": "vulnerability", "dbms": _as_str(record.get("dbms"))},
    )


def _service_context(target: str) -> ServiceContext | None:
    parsed = urlparse(target)
    scheme = parsed.scheme or None
    port = parsed.port
    if port is None and scheme == "https":
        port = 443
    elif port is None and scheme == "http":
        port = 80
    if port is None and scheme is None:
        return None
    return ServiceContext(port=port, protocol=scheme, name=scheme)


def _evidence(
    record: dict[str, Any],
    *,
    target: str,
    parameter: str,
    locator: str,
) -> list[EvidenceSnippet]:
    snippets = [
        EvidenceSnippet(
            kind="sqlmap-finding",
            value=f"sqlmap reported injectable parameter {parameter} at {target}",
            locator=locator,
        )
    ]
    payload = _as_str(record.get("payload"))
    if payload:
        snippets.append(
            EvidenceSnippet(
                kind="sqlmap-payload",
                value=payload,
                redacted=True,
                locator=locator,
            )
        )
    return snippets


def _weakness_ids(record: dict[str, Any]) -> list[str]:
    ids = {"CWE-89"}
    cwe = _as_str(record.get("cwe"))
    if cwe:
        ids.add(f"CWE-{cwe.upper().removeprefix('CWE-')}")
    return sorted(ids)


def _references(record: dict[str, Any]) -> list[str]:
    refs = _string_list(record.get("references"))
    return sorted(set(refs))


def _map_severity(value: object) -> Severity:
    if isinstance(value, int):
        if value >= 4:
            return "critical"
        if value >= 3:
            return "high"
        if value >= 2:
            return "medium"
        if value >= 1:
            return "low"
    if isinstance(value, str):
        normalized = value.strip().lower()
        if normalized in {"critical", "high", "medium", "low", "info"}:
            return normalized  # type: ignore[return-value]
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


__all__ = ["SqlmapParseError", "SqlmapParseResult", "parse_sqlmap_file"]
