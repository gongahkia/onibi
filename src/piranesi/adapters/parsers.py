from __future__ import annotations

import json
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from piranesi.adapters.models import (
    AdapterDiagnostic,
    AdapterParseResult,
    ExternalRawFinding,
    ExternalTool,
)


def parse_external_tool_file(
    *,
    tool: ExternalTool,
    input_path: Path,
) -> AdapterParseResult:
    payload = json.loads(input_path.read_text(encoding="utf-8"))
    return parse_external_tool_payload(tool=tool, payload=payload, source_path=input_path)


def parse_external_tool_payload(
    *,
    tool: ExternalTool,
    payload: object,
    source_path: Path,
) -> AdapterParseResult:
    result = AdapterParseResult.empty(tool=tool, source_path=source_path)
    result.parsed_at = datetime.now(UTC).isoformat()

    if not isinstance(payload, dict):
        result.diagnostics.append(
            AdapterDiagnostic(
                level="error",
                message="adapter input must be a JSON object",
                context={"type": type(payload).__name__},
            )
        )
        return result

    if tool in {"sarif", "codeql_sarif"}:
        result.findings.extend(_parse_sarif(payload=payload, tool=tool))
    elif tool == "semgrep":
        result.findings.extend(_parse_semgrep(payload=payload))
    elif tool == "trivy":
        result.findings.extend(_parse_trivy(payload=payload))
    elif tool == "zap":
        result.findings.extend(_parse_zap(payload=payload))
    else:
        result.diagnostics.append(
            AdapterDiagnostic(
                level="error",
                message="unsupported adapter tool",
                context={"tool": tool},
            )
        )

    if not result.findings:
        result.diagnostics.append(
            AdapterDiagnostic(
                level="warning",
                message="adapter parsed zero findings",
                context={"tool": tool},
            )
        )
    return result


def _parse_sarif(*, payload: dict[str, Any], tool: ExternalTool) -> list[ExternalRawFinding]:
    findings: list[ExternalRawFinding] = []
    for run in payload.get("runs", []) if isinstance(payload.get("runs"), list) else []:
        if not isinstance(run, dict):
            continue
        rule_map: dict[str, dict[str, Any]] = {}
        tool_payload = run.get("tool")
        if isinstance(tool_payload, dict):
            driver = tool_payload.get("driver")
            if isinstance(driver, dict):
                rules = driver.get("rules", [])
                for rule in rules if isinstance(rules, list) else []:
                    if isinstance(rule, dict):
                        rule_id = _as_str(rule.get("id"))
                        if rule_id is not None:
                            rule_map[rule_id] = rule

        for item in run.get("results", []) if isinstance(run.get("results"), list) else []:
            if not isinstance(item, dict):
                continue
            rule_id = _as_str(item.get("ruleId"))
            rule = rule_map.get(rule_id or "", {})
            message = item.get("message")
            text = None
            if isinstance(message, dict):
                text = _as_str(message.get("text")) or _as_str(message.get("markdown"))
            title = _as_str(rule.get("name")) or _as_str(item.get("message", {}).get("text"))
            if title is None:
                title = rule_id or "SARIF finding"
            level = _as_str(item.get("level"))

            cwe_ids: list[str] = []
            tags = rule.get("properties", {}).get("tags") if isinstance(rule, dict) else None
            if isinstance(tags, list):
                cwe_ids.extend(
                    [tag for tag in tags if isinstance(tag, str) and tag.upper().startswith("CWE-")]
                )

            location = _first_location(item)
            external_id = _as_str(
                item.get("partialFingerprints", {}).get("primaryLocationLineHash")
            )
            findings.append(
                ExternalRawFinding(
                    tool=tool,
                    external_id=external_id,
                    rule_id=rule_id,
                    title=title,
                    description=text,
                    severity=_map_severity(level),
                    cwe_ids=sorted(set(cwe_ids)),
                    category=_as_str(rule.get("shortDescription", {}).get("text")),
                    file_path=location["path"],
                    line=location["line"],
                    column=location["column"],
                    metadata={
                        "help_uri": _as_str(rule.get("helpUri")),
                    },
                )
            )
    return findings


def _parse_semgrep(*, payload: dict[str, Any]) -> list[ExternalRawFinding]:
    findings: list[ExternalRawFinding] = []
    rows = payload.get("results")
    if not isinstance(rows, list):
        return findings

    for row in rows:
        if not isinstance(row, dict):
            continue
        extra = row.get("extra") if isinstance(row.get("extra"), dict) else {}
        metadata = (
            extra.get("metadata")
            if isinstance(extra, dict) and isinstance(extra.get("metadata"), dict)
            else {}
        )
        cwe_ids = _extract_cwes(metadata.get("cwe"))
        start = row.get("start") if isinstance(row.get("start"), dict) else {}
        findings.append(
            ExternalRawFinding(
                tool="semgrep",
                external_id=_as_str(row.get("fingerprint")),
                rule_id=_as_str(row.get("check_id")),
                title=(
                    _as_str(extra.get("message"))
                    or _as_str(row.get("check_id"))
                    or "Semgrep finding"
                ),
                description=_as_str(extra.get("message")),
                severity=_map_severity(_as_str(extra.get("severity"))),
                confidence=None,
                cwe_ids=cwe_ids,
                category=_as_str(metadata.get("category")),
                file_path=_as_str(row.get("path")),
                line=_as_int(start.get("line")),
                column=_as_int(start.get("col")),
                metadata={
                    "semgrep_rule": _as_str(row.get("check_id")),
                },
            )
        )
    return findings


def _parse_trivy(*, payload: dict[str, Any]) -> list[ExternalRawFinding]:
    findings: list[ExternalRawFinding] = []
    results = payload.get("Results")
    if not isinstance(results, list):
        return findings

    for row in results:
        if not isinstance(row, dict):
            continue
        target = _as_str(row.get("Target"))
        vulnerabilities = row.get("Vulnerabilities")
        if not isinstance(vulnerabilities, list):
            continue
        for vuln in vulnerabilities:
            if not isinstance(vuln, dict):
                continue
            findings.append(
                ExternalRawFinding(
                    tool="trivy",
                    external_id=_as_str(vuln.get("VulnerabilityID")),
                    rule_id=_as_str(vuln.get("VulnerabilityID")),
                    title=(
                        _as_str(vuln.get("Title"))
                        or _as_str(vuln.get("VulnerabilityID"))
                        or "Trivy finding"
                    ),
                    description=_as_str(vuln.get("Description")),
                    severity=_map_severity(_as_str(vuln.get("Severity"))),
                    confidence=None,
                    cwe_ids=_extract_cwes(vuln.get("CweIDs")),
                    package_name=_as_str(vuln.get("PkgName")),
                    metadata={
                        "target": target,
                        "installed_version": _as_str(vuln.get("InstalledVersion")),
                        "fixed_version": _as_str(vuln.get("FixedVersion")),
                    },
                )
            )
    return findings


def _parse_zap(*, payload: dict[str, Any]) -> list[ExternalRawFinding]:
    findings: list[ExternalRawFinding] = []
    sites = payload.get("site")
    if not isinstance(sites, list):
        return findings

    for site in sites:
        if not isinstance(site, dict):
            continue
        alerts = site.get("alerts")
        if not isinstance(alerts, list):
            continue
        for alert in alerts:
            if not isinstance(alert, dict):
                continue
            instances = alert.get("instances") if isinstance(alert.get("instances"), list) else []
            first_instance = instances[0] if instances and isinstance(instances[0], dict) else {}
            cwe = _as_str(alert.get("cweid"))
            cwe_ids = [] if cwe in {None, "0"} else [f"CWE-{cwe}"]
            findings.append(
                ExternalRawFinding(
                    tool="zap",
                    external_id=_as_str(alert.get("pluginid")),
                    rule_id=_as_str(alert.get("pluginid")),
                    title=_as_str(alert.get("alert")) or "ZAP alert",
                    description=_as_str(alert.get("desc")),
                    severity=_map_zap_risk(_as_str(alert.get("riskcode"))),
                    cwe_ids=cwe_ids,
                    endpoint=_as_str(first_instance.get("uri")) or _as_str(alert.get("url")),
                    metadata={
                        "method": _as_str(first_instance.get("method")),
                        "confidence": _as_str(alert.get("confidence")),
                    },
                )
            )
    return findings


def _first_location(item: dict[str, Any]) -> dict[str, int | str | None]:
    default = {"path": None, "line": None, "column": None}
    locations = item.get("locations")
    if not isinstance(locations, list) or not locations:
        return default
    first = locations[0]
    if not isinstance(first, dict):
        return default
    physical = first.get("physicalLocation")
    if not isinstance(physical, dict):
        return default
    artifact = physical.get("artifactLocation")
    region = physical.get("region")
    return {
        "path": _as_str(artifact.get("uri")) if isinstance(artifact, dict) else None,
        "line": _as_int(region.get("startLine")) if isinstance(region, dict) else None,
        "column": _as_int(region.get("startColumn")) if isinstance(region, dict) else None,
    }


def _map_zap_risk(risk_code: str | None) -> str:
    mapping = {
        "3": "high",
        "2": "medium",
        "1": "low",
        "0": "informational",
    }
    return mapping.get((risk_code or "").strip(), "medium")


def _map_severity(value: str | None) -> str:
    if value is None:
        return "medium"
    lowered = value.strip().lower()
    if lowered in {"error", "critical"}:
        return "critical"
    if lowered in {"warning", "high"}:
        return "high"
    if lowered in {"medium", "moderate"}:
        return "medium"
    if lowered in {"note", "low"}:
        return "low"
    if lowered in {"none", "informational", "info"}:
        return "informational"
    return "medium"


def _extract_cwes(value: object) -> list[str]:
    if value is None:
        return []
    rows: list[str] = []
    if isinstance(value, str):
        rows = [value]
    elif isinstance(value, list):
        rows = [entry for entry in value if isinstance(entry, str)]
    normalized = []
    for row in rows:
        upper = row.upper().strip()
        if upper.startswith("CWE-"):
            normalized.append(upper)
        elif upper.isdigit() and upper != "0":
            normalized.append(f"CWE-{upper}")
    return sorted(set(normalized))


def _as_str(value: object) -> str | None:
    return value if isinstance(value, str) and value.strip() else None


def _as_int(value: object) -> int | None:
    if isinstance(value, int):
        return value
    if isinstance(value, str) and value.isdigit():
        return int(value)
    return None
