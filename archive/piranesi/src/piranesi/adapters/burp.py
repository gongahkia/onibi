from __future__ import annotations

import base64
import html
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import urlparse
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


class BurpParseError(ValueError):
    """Raised when Burp Suite Pro Issues XML cannot be parsed into findings."""


@dataclass(frozen=True)
class BurpParseResult:
    findings: list[NormalizedFinding]
    warnings: list[str]
    metadata: dict[str, Any]


def parse_burp_xml_file(
    input_path: Path,
    *,
    input_sha256: str,
    raw_path: str,
) -> BurpParseResult:
    try:
        root = DefusedET.parse(input_path).getroot()
    except ParseError as exc:
        raise BurpParseError(f"invalid Burp Issues XML: {exc}") from exc
    except OSError as exc:
        raise BurpParseError(f"cannot read Burp Issues XML: {exc}") from exc

    if root is None or _local_name(root.tag) != "issues":
        raise BurpParseError("unsupported Burp Issues XML: expected <issues> document root")

    issue_elements = [item for item in root if _local_name(item.tag) == "issue"]
    if not issue_elements:
        raise BurpParseError("empty Burp Issues XML: document contains no issue records")

    warnings: list[str] = []
    findings: list[NormalizedFinding] = []
    for index, issue in enumerate(issue_elements, start=1):
        finding = _finding_from_issue(
            issue,
            input_sha256=input_sha256,
            raw_path=raw_path,
            issue_number=index,
            warnings=warnings,
        )
        if finding is not None:
            findings.append(finding)

    if not findings:
        raise BurpParseError("Burp Issues XML contained no valid issue records")

    metadata = {
        "format": "burp-issues-xml",
        "burp_version": root.attrib.get("burpVersion") or root.attrib.get("burp_version"),
        "export_time": root.attrib.get("exportTime") or root.attrib.get("export_time"),
        "records": len(issue_elements),
        "valid_records": len(findings),
        "malformed_records": len(issue_elements) - len(findings),
        "issue_types": sorted(
            {
                str(finding.provenance["burp_type"])
                for finding in findings
                if finding.provenance.get("burp_type")
            }
        ),
        "summary": {
            "findings": len({finding.id for finding in findings}),
            "warnings": len(warnings),
        },
    }
    return BurpParseResult(findings=findings, warnings=warnings, metadata=metadata)


def _finding_from_issue(
    issue: Element,
    *,
    input_sha256: str,
    raw_path: str,
    issue_number: int,
    warnings: list[str],
) -> NormalizedFinding | None:
    title = _text(issue, "name")
    host = _text(issue, "host")
    path = _text(issue, "path")
    location = _text(issue, "location")
    if title is None:
        warnings.append(f"issue {issue_number}: missing name")
        return None
    if host is None and location is None:
        warnings.append(f"issue {issue_number}: missing host and location")
        return None

    now = utc_now()
    issue_type = _text(issue, "type")
    serial_number = _text(issue, "serialNumber")
    severity = _map_severity(_text(issue, "severity"))
    burp_confidence = _text(issue, "confidence")
    asset = _asset_from_host(host) or host or location
    service = _service_context(host)
    locator = f"issue[{serial_number or issue_number}]"
    weakness_ids = _weakness_ids(issue)
    source = SourceReference(
        tool="burp",
        input_sha256=input_sha256,
        raw_path=raw_path,
        locator=locator,
        metadata={
            "serial_number": serial_number,
            "type": issue_type,
            "host": host,
            "path": path,
            "location": location,
            "severity": _text(issue, "severity"),
            "confidence": burp_confidence,
        },
    )
    return NormalizedFinding(
        id=deterministic_finding_id(
            "burp",
            issue_type or title,
            title,
            host,
            path,
            location,
        ),
        title=title,
        severity=severity,
        confidence="tool-observed",
        description=_clean_markup(_text(issue, "issueBackground") or _text(issue, "issueDetail")),
        remediation=_clean_markup(
            _text(issue, "remediationBackground") or _text(issue, "remediationDetail")
        ),
        asset=asset,
        service=service,
        weakness_ids=weakness_ids,
        references=_references(issue),
        tags=_tags(issue_type=issue_type, weakness_ids=weakness_ids),
        evidence=_evidence(
            issue,
            title=title,
            host=host,
            path=path,
            location=location,
            locator=locator,
        ),
        source_references=[source],
        affected_instances=[
            AffectedInstance(
                asset=asset or "unknown",
                service=service,
                location=location or path or host,
                metadata={"burp_type": issue_type, "serial_number": serial_number},
            )
        ],
        first_seen=now,
        last_seen=now,
        provenance={
            "tool": "burp",
            "type": "issue",
            "burp_type": issue_type,
            "burp_confidence": burp_confidence,
            "serial_number": serial_number,
        },
    )


def _evidence(
    issue: Element,
    *,
    title: str,
    host: str | None,
    path: str | None,
    location: str | None,
    locator: str,
) -> list[EvidenceSnippet]:
    rendered_location = location or "".join(part for part in [host, path] if part)
    snippets = [
        EvidenceSnippet(
            kind="burp-issue",
            value=f"Burp reported {title} at {rendered_location or 'not specified'}",
            locator=locator,
        )
    ]
    for index, request_response in enumerate(
        [item for item in issue if _local_name(item.tag) == "requestresponse"],
        start=1,
    ):
        request = _request_response_text(request_response, "request")
        response = _request_response_text(request_response, "response")
        item_locator = f"{locator}/requestresponse[{index}]"
        if request:
            snippets.append(
                EvidenceSnippet(
                    kind="burp-request",
                    value=request,
                    redacted=True,
                    locator=item_locator,
                )
            )
        if response:
            snippets.append(
                EvidenceSnippet(
                    kind="burp-response",
                    value=response,
                    redacted=True,
                    locator=item_locator,
                )
            )
    return snippets


def _request_response_text(request_response: Element, child_name: str) -> str | None:
    child = next((item for item in request_response if _local_name(item.tag) == child_name), None)
    if child is None or child.text is None:
        return None
    raw = child.text.strip()
    if not raw:
        return None
    if (child.attrib.get("base64") or "").lower() == "true":
        try:
            return base64.b64decode(raw, validate=False).decode("utf-8", errors="replace")
        except ValueError:
            return raw
    return html.unescape(raw)


def _service_context(host: str | None) -> ServiceContext | None:
    if not host:
        return None
    parsed = urlparse(host)
    scheme = parsed.scheme or None
    port = parsed.port
    if port is None and scheme == "https":
        port = 443
    elif port is None and scheme == "http":
        port = 80
    if scheme is None and port is None:
        return None
    return ServiceContext(port=port, protocol=scheme, name=scheme)


def _asset_from_host(host: str | None) -> str | None:
    if not host:
        return None
    parsed = urlparse(host)
    if parsed.hostname:
        return parsed.hostname
    stripped = host.removeprefix("http://").removeprefix("https://").split("/", 1)[0]
    return stripped or None


def _references(issue: Element) -> list[str]:
    candidates = [
        _text(issue, "references"),
        _text(issue, "issueBackground"),
        _text(issue, "remediationBackground"),
    ]
    references: set[str] = set()
    for value in candidates:
        if value:
            references.update(
                match.rstrip(".,;") for match in re.findall(r"https?://[^\s<>)\"']+", value)
            )
    return sorted(references)


def _weakness_ids(issue: Element) -> list[str]:
    payload = "\n".join(
        item
        for item in [
            _text(issue, "vulnerabilityClassifications"),
            _text(issue, "issueBackground"),
            _text(issue, "issueDetail"),
        ]
        if item
    )
    ids: set[str] = set()
    for match in re.findall(r"CWE[-\s]*(\d+)", payload, flags=re.IGNORECASE):
        ids.add(f"CWE-{match}")
    return sorted(ids)


def _tags(*, issue_type: str | None, weakness_ids: list[str]) -> list[str]:
    tags = {"burp"}
    if issue_type:
        tags.add(f"burp-type-{issue_type}")
    tags.update(item.lower() for item in weakness_ids)
    return sorted(tags)


def _map_severity(value: str | None) -> Severity:
    normalized = (value or "info").strip().lower()
    if normalized in {"critical", "high", "medium", "low"}:
        return normalized  # type: ignore[return-value]
    return "info"


def _text(element: Element, child_name: str) -> str | None:
    child = next((item for item in element if _local_name(item.tag) == child_name), None)
    if child is None or child.text is None:
        return None
    value = child.text.strip()
    return html.unescape(value) if value else None


def _clean_markup(value: str | None) -> str | None:
    if value is None:
        return None
    without_tags = re.sub(r"<[^>]+>", " ", value)
    return " ".join(html.unescape(without_tags).split()) or None


def _local_name(tag: str) -> str:
    return tag.rsplit("}", 1)[-1]


__all__ = ["BurpParseError", "BurpParseResult", "parse_burp_xml_file"]
