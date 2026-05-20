from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any
from xml.etree.ElementTree import Element, ParseError

from defusedxml import ElementTree as DefusedET

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


class NessusParseError(ValueError):
    """Raised when Nessus XML cannot be parsed into findings."""


@dataclass(frozen=True)
class NessusParseResult:
    findings: list[NormalizedFinding]
    warnings: list[str]
    metadata: dict[str, Any]


def parse_nessus_file(
    input_path: Path,
    *,
    input_sha256: str,
    raw_path: str,
) -> NessusParseResult:
    try:
        root = DefusedET.parse(input_path).getroot()
    except ParseError as exc:
        raise NessusParseError(f"invalid Nessus XML: {exc}") from exc
    except OSError as exc:
        raise NessusParseError(f"cannot read Nessus XML: {exc}") from exc

    if root is None or _local_name(root.tag) != "NessusClientData_v2":
        raise NessusParseError("unsupported Nessus XML: expected NessusClientData_v2 root")

    report_hosts = [
        item
        for report in _children(root, "Report")
        for item in _children(report, "ReportHost")
    ]
    if not report_hosts:
        raise NessusParseError("empty Nessus XML: document contains no ReportHost records")

    warnings: list[str] = []
    findings: list[NormalizedFinding] = []
    report_items = 0
    for host_index, host in enumerate(report_hosts, start=1):
        asset = host.attrib.get("name") or _host_property(host, "host-ip")
        if not asset:
            warnings.append(f"host {host_index}: missing ReportHost name and host-ip property")
            continue
        for item_index, item in enumerate(_children(host, "ReportItem"), start=1):
            report_items += 1
            finding = _finding_from_report_item(
                item,
                asset=asset,
                input_sha256=input_sha256,
                raw_path=raw_path,
                host_index=host_index,
                item_index=item_index,
                warnings=warnings,
            )
            if finding is not None:
                findings.append(finding)

    if not findings:
        raise NessusParseError("Nessus XML contained no valid ReportItem records")

    metadata = {
        "format": "nessus-v2",
        "policy_name": _text(root, "PolicyName"),
        "report_hosts": len(report_hosts),
        "records": report_items,
        "valid_records": len(findings),
        "malformed_records": report_items - len(findings),
        "plugin_ids": sorted(
            {
                str(finding.provenance["plugin_id"])
                for finding in findings
                if finding.provenance.get("plugin_id")
            }
        ),
        "summary": {
            "findings": len({finding.id for finding in findings}),
            "warnings": len(warnings),
        },
    }
    return NessusParseResult(findings=findings, warnings=warnings, metadata=metadata)


def _finding_from_report_item(
    item: Element,
    *,
    asset: str,
    input_sha256: str,
    raw_path: str,
    host_index: int,
    item_index: int,
    warnings: list[str],
) -> NormalizedFinding | None:
    plugin_id = item.attrib.get("pluginID") or item.attrib.get("plugin_id")
    title = item.attrib.get("pluginName") or _text(item, "plugin_name")
    if not plugin_id:
        warnings.append(f"host {host_index} item {item_index}: missing pluginID")
        return None
    if not title:
        warnings.append(f"host {host_index} item {item_index}: missing pluginName")
        return None

    service = _service_context(item)
    locator = f"host[{asset}]/plugin[{plugin_id}]"
    now = utc_now()
    source = SourceReference(
        tool="nessus",
        input_sha256=input_sha256,
        raw_path=raw_path,
        locator=locator,
        metadata={
            "plugin_id": plugin_id,
            "plugin_name": title,
            "plugin_family": item.attrib.get("pluginFamily"),
            "severity": item.attrib.get("severity"),
            "risk_factor": _text(item, "risk_factor"),
        },
    )
    weakness_ids = _weakness_ids(item)
    return NormalizedFinding(
        id=deterministic_finding_id("nessus", asset, plugin_id, service.port if service else None),
        title=title,
        severity=_map_severity(item.attrib.get("severity"), _text(item, "risk_factor")),
        confidence="tool-observed",
        description=_text(item, "synopsis") or _text(item, "description"),
        remediation=_text(item, "solution"),
        asset=asset,
        service=service,
        weakness_ids=weakness_ids,
        references=_references(item),
        tags=_tags(plugin_id=plugin_id, weakness_ids=weakness_ids),
        evidence=_evidence(item, title=title, locator=locator),
        source_references=[source],
        affected_instances=[
            AffectedInstance(
                asset=asset,
                service=service,
                location=_location(item),
                metadata={"plugin_id": plugin_id},
            )
        ],
        first_seen=now,
        last_seen=now,
        provenance={
            "tool": "nessus",
            "type": "report_item",
            "plugin_id": plugin_id,
            "plugin_family": item.attrib.get("pluginFamily"),
        },
    )


def _service_context(item: Element) -> ServiceContext | None:
    port = _as_int(item.attrib.get("port"))
    protocol = item.attrib.get("protocol") or None
    name = item.attrib.get("svc_name") or None
    if port is None and protocol is None and name is None:
        return None
    return ServiceContext(port=port, protocol=protocol, name=name)


def _location(item: Element) -> str | None:
    port = item.attrib.get("port")
    protocol = item.attrib.get("protocol")
    if port and protocol:
        return f"{protocol}/{port}"
    return port or protocol


def _evidence(item: Element, *, title: str, locator: str) -> list[EvidenceSnippet]:
    snippets = [
        EvidenceSnippet(
            kind="nessus-report-item",
            value=f"Nessus reported {title}",
            locator=locator,
        )
    ]
    plugin_output = _text(item, "plugin_output")
    if plugin_output:
        snippets.append(
            EvidenceSnippet(
                kind="nessus-plugin-output",
                value=plugin_output,
                redacted=True,
                locator=locator,
            )
        )
    return snippets


def _weakness_ids(item: Element) -> list[str]:
    ids: set[str] = set()
    for cwe in _texts(item, "cwe"):
        normalized = cwe.upper().removeprefix("CWE-")
        ids.add(f"CWE-{normalized}")
    for cve in _texts(item, "cve"):
        ids.add(cve.upper())
    return sorted(ids)


def _references(item: Element) -> list[str]:
    refs: set[str] = set()
    for child_name in ("see_also", "xref"):
        for value in _texts(item, child_name):
            refs.update(part.strip() for part in value.splitlines() if part.strip())
    return sorted(refs)


def _tags(*, plugin_id: str, weakness_ids: list[str]) -> list[str]:
    tags = {"nessus", f"nessus-plugin-{plugin_id}"}
    tags.update(item.lower() for item in weakness_ids)
    return sorted(tags)


def _map_severity(raw_severity: str | None, risk_factor: str | None) -> Severity:
    severity_map: dict[str, Severity] = {
        "0": "info",
        "1": "low",
        "2": "medium",
        "3": "high",
        "4": "critical",
    }
    if raw_severity in severity_map:
        return severity_map[raw_severity]
    normalized = (risk_factor or "").strip().lower()
    if normalized in {"critical", "high", "medium", "low"}:
        return normalized  # type: ignore[return-value]
    return "info"


def _host_property(host: Element, name: str) -> str | None:
    properties = next(iter(_children(host, "HostProperties")), None)
    if properties is None:
        return None
    for tag in _children(properties, "tag"):
        if tag.attrib.get("name") == name and tag.text:
            return tag.text.strip()
    return None


def _children(element: Element, child_name: str) -> list[Element]:
    return [child for child in element if _local_name(child.tag) == child_name]


def _text(element: Element, child_name: str) -> str | None:
    values = _texts(element, child_name)
    return values[0] if values else None


def _texts(element: Element, child_name: str) -> list[str]:
    values: list[str] = []
    for child in _children(element, child_name):
        if child.text and child.text.strip():
            values.append(child.text.strip())
    return values


def _as_int(value: object) -> int | None:
    if isinstance(value, int):
        return value
    if isinstance(value, str):
        try:
            return int(value)
        except ValueError:
            return None
    return None


def _local_name(tag: str) -> str:
    return tag.rsplit("}", 1)[-1]


__all__ = ["NessusParseError", "NessusParseResult", "parse_nessus_file"]
