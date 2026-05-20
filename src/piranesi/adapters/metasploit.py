from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any

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


class MetasploitParseError(ValueError):
    """Raised when Metasploit JSON cannot be parsed into findings."""


@dataclass(frozen=True)
class MetasploitParseResult:
    findings: list[NormalizedFinding]
    warnings: list[str]
    metadata: dict[str, Any]


def parse_metasploit_json_file(
    input_path: Path,
    *,
    input_sha256: str,
    raw_path: str,
) -> MetasploitParseResult:
    try:
        payload = json.loads(input_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise MetasploitParseError(f"invalid Metasploit JSON: {exc.msg}") from exc
    except OSError as exc:
        raise MetasploitParseError(f"cannot read Metasploit JSON: {exc}") from exc

    if not isinstance(payload, dict):
        raise MetasploitParseError("unsupported Metasploit JSON: expected JSON object")

    warnings: list[str] = []
    findings: list[NormalizedFinding] = []
    records = _records(payload)
    if not records:
        raise MetasploitParseError("empty Metasploit JSON: document contains no evidence records")

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
        raise MetasploitParseError("Metasploit JSON contained no valid evidence records")

    metadata = {
        "format": "metasploit-json",
        "workspace": _as_str(payload.get("workspace")),
        "exported_at": _as_str(payload.get("exported_at")),
        "records": len(records),
        "valid_records": len(findings),
        "malformed_records": len(records) - len(findings),
        "record_types": sorted(
            {
                str(finding.provenance["type"])
                for finding in findings
                if finding.provenance.get("type")
            }
        ),
        "summary": {
            "findings": len({finding.id for finding in findings}),
            "warnings": len(warnings),
        },
    }
    return MetasploitParseResult(findings=findings, warnings=warnings, metadata=metadata)


def _records(payload: dict[str, Any]) -> list[dict[str, Any]]:
    records: list[dict[str, Any]] = []
    for key, record_type in (("vulns", "vuln"), ("loot", "loot"), ("sessions", "session")):
        items = payload.get(key)
        if not isinstance(items, list):
            continue
        for item in items:
            if isinstance(item, dict):
                records.append({**item, "record_type": record_type})
    return records


def _finding_from_record(
    record: dict[str, Any],
    *,
    input_sha256: str,
    raw_path: str,
    record_number: int,
    warnings: list[str],
) -> NormalizedFinding | None:
    record_type = _as_str(record.get("record_type"))
    if record_type == "vuln":
        return _vuln_finding(
            record,
            input_sha256=input_sha256,
            raw_path=raw_path,
            record_number=record_number,
            warnings=warnings,
        )
    if record_type == "loot":
        return _loot_finding(
            record,
            input_sha256=input_sha256,
            raw_path=raw_path,
            record_number=record_number,
            warnings=warnings,
        )
    if record_type == "session":
        return _session_finding(
            record,
            input_sha256=input_sha256,
            raw_path=raw_path,
            record_number=record_number,
            warnings=warnings,
        )
    warnings.append(f"record {record_number}: unsupported record type {record_type!r}")
    return None


def _vuln_finding(
    record: dict[str, Any],
    *,
    input_sha256: str,
    raw_path: str,
    record_number: int,
    warnings: list[str],
) -> NormalizedFinding | None:
    host = _as_str(record.get("host"))
    name = _as_str(record.get("name"))
    if host is None or name is None:
        warnings.append(f"record {record_number}: vulnerability missing host or name")
        return None
    service = _service_context(record.get("service"))
    locator = f"vuln[{record_number}]"
    now = utc_now()
    references = _references(record)
    weakness_ids = sorted(ref for ref in references if ref.startswith(("CVE-", "CWE-")))
    source = _source(record, input_sha256=input_sha256, raw_path=raw_path, locator=locator)
    return NormalizedFinding(
        id=deterministic_finding_id("metasploit", "vuln", host, name, _service_label(service)),
        title=name,
        severity=_map_severity(record.get("risk")),
        confidence="tool-observed",
        description=_as_str(record.get("info")),
        asset=host,
        service=service,
        weakness_ids=weakness_ids,
        references=references,
        tags=_tags("vuln", weakness_ids),
        evidence=[EvidenceSnippet(kind="metasploit-vuln", value=name, locator=locator)],
        source_references=[source],
        affected_instances=[
            AffectedInstance(asset=host, service=service, location=_service_label(service))
        ],
        first_seen=now,
        last_seen=now,
        provenance={"tool": "metasploit", "type": "vuln"},
    )


def _loot_finding(
    record: dict[str, Any],
    *,
    input_sha256: str,
    raw_path: str,
    record_number: int,
    warnings: list[str],
) -> NormalizedFinding | None:
    host = _as_str(record.get("host"))
    name = _as_str(record.get("name")) or _as_str(record.get("ltype"))
    if host is None or name is None:
        warnings.append(f"record {record_number}: loot missing host or name")
        return None
    locator = f"loot[{record_number}]"
    now = utc_now()
    source = _source(record, input_sha256=input_sha256, raw_path=raw_path, locator=locator)
    return NormalizedFinding(
        id=deterministic_finding_id("metasploit", "loot", host, name, _as_str(record.get("path"))),
        title=f"Metasploit loot captured: {name}",
        severity="info",
        confidence="tool-observed",
        description=_as_str(record.get("info")),
        asset=host,
        tags=["metasploit", "metasploit-loot"],
        evidence=_loot_evidence(record, name=name, locator=locator),
        source_references=[source],
        affected_instances=[
            AffectedInstance(
                asset=host,
                location=_as_str(record.get("path")),
                metadata={"ltype": _as_str(record.get("ltype"))},
            )
        ],
        first_seen=now,
        last_seen=now,
        provenance={"tool": "metasploit", "type": "loot"},
    )


def _session_finding(
    record: dict[str, Any],
    *,
    input_sha256: str,
    raw_path: str,
    record_number: int,
    warnings: list[str],
) -> NormalizedFinding | None:
    host = _as_str(record.get("host"))
    session_type = _as_str(record.get("type")) or _as_str(record.get("stype"))
    session_id = _as_str(record.get("id")) or _as_str(record.get("sid")) or str(record_number)
    if host is None or session_type is None:
        warnings.append(f"record {record_number}: session missing host or type")
        return None
    locator = f"session[{session_id}]"
    now = utc_now()
    source = _source(record, input_sha256=input_sha256, raw_path=raw_path, locator=locator)
    title = f"Metasploit session observed: {session_type} on {host}"
    return NormalizedFinding(
        id=deterministic_finding_id("metasploit", "session", host, session_id, session_type),
        title=title,
        severity="info",
        confidence="tool-observed",
        description=_as_str(record.get("via_exploit")),
        asset=host,
        tags=["metasploit", "metasploit-session"],
        evidence=[EvidenceSnippet(kind="metasploit-session", value=title, locator=locator)],
        source_references=[source],
        affected_instances=[
            AffectedInstance(
                asset=host,
                metadata={
                    "session_id": session_id,
                    "platform": _as_str(record.get("platform")),
                    "via_exploit": _as_str(record.get("via_exploit")),
                },
            )
        ],
        first_seen=now,
        last_seen=now,
        provenance={"tool": "metasploit", "type": "session", "session_id": session_id},
    )


def _source(
    record: dict[str, Any],
    *,
    input_sha256: str,
    raw_path: str,
    locator: str,
) -> SourceReference:
    return SourceReference(
        tool="metasploit",
        input_sha256=input_sha256,
        raw_path=raw_path,
        locator=locator,
        metadata={
            "record_type": _as_str(record.get("record_type")),
            "module": _as_str(record.get("module")),
            "path": _as_str(record.get("path")),
        },
    )


def _service_context(value: object) -> ServiceContext | None:
    if not isinstance(value, dict):
        return None
    return ServiceContext(
        port=_as_int(value.get("port")),
        protocol=_as_str(value.get("proto")) or _as_str(value.get("protocol")),
        name=_as_str(value.get("name")),
        product=_as_str(value.get("product")),
        version=_as_str(value.get("version")),
    )


def _service_label(service: ServiceContext | None) -> str | None:
    if service is None:
        return None
    if service.protocol and service.port:
        return f"{service.protocol}/{service.port}"
    return service.name


def _loot_evidence(record: dict[str, Any], *, name: str, locator: str) -> list[EvidenceSnippet]:
    snippets = [
        EvidenceSnippet(
            kind="metasploit-loot",
            value=f"Metasploit loot record {name}",
            locator=locator,
        )
    ]
    content = _as_str(record.get("content"))
    if content:
        snippets.append(
            EvidenceSnippet(
                kind="metasploit-loot-content",
                value=content,
                redacted=True,
                locator=locator,
            )
        )
    return snippets


def _references(record: dict[str, Any]) -> list[str]:
    refs = _string_list(record.get("refs")) or _string_list(record.get("references"))
    return sorted(
        {ref.upper() if ref.lower().startswith(("cve-", "cwe-")) else ref for ref in refs}
    )


def _tags(record_type: str, weakness_ids: list[str]) -> list[str]:
    tags = {"metasploit", f"metasploit-{record_type}"}
    tags.update(item.lower() for item in weakness_ids)
    return sorted(tags)


def _map_severity(value: object) -> Severity:
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
    if isinstance(value, int):
        return str(value)
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


__all__ = ["MetasploitParseError", "MetasploitParseResult", "parse_metasploit_json_file"]
