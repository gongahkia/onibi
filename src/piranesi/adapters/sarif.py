from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from piranesi.workspace import (
    AffectedInstance,
    EvidenceSnippet,
    NormalizedFinding,
    Severity,
    SourceReference,
    deterministic_finding_id,
    utc_now,
)


class SarifParseError(ValueError):
    """Raised when SARIF JSON cannot be parsed into findings."""


@dataclass(frozen=True)
class SarifParseResult:
    findings: list[NormalizedFinding]
    warnings: list[str]
    metadata: dict[str, Any]


def parse_sarif_file(
    input_path: Path,
    *,
    input_sha256: str,
    raw_path: str,
) -> SarifParseResult:
    try:
        payload = json.loads(input_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise SarifParseError(f"invalid SARIF JSON: {exc.msg}") from exc
    except OSError as exc:
        raise SarifParseError(f"cannot read SARIF JSON: {exc}") from exc

    if not isinstance(payload, dict):
        raise SarifParseError("unsupported SARIF: expected JSON object")
    version = _as_str(payload.get("version"))
    if version != "2.1.0":
        raise SarifParseError(f"unsupported SARIF version {version!r}; expected '2.1.0'")
    runs = payload.get("runs")
    if not isinstance(runs, list) or not runs:
        raise SarifParseError("empty SARIF: document contains no runs")

    warnings: list[str] = []
    findings: list[NormalizedFinding] = []
    result_count = 0
    for run_index, run in enumerate(runs, start=1):
        if not isinstance(run, dict):
            warnings.append(f"run {run_index}: expected object")
            continue
        rules = _rules_by_id(run)
        tool_name = _tool_name(run)
        results = run.get("results")
        if not isinstance(results, list):
            continue
        for result_index, result in enumerate(results, start=1):
            result_count += 1
            if not isinstance(result, dict):
                warnings.append(f"run {run_index} result {result_index}: expected object")
                continue
            finding = _finding_from_result(
                result,
                rules=rules,
                tool_name=tool_name,
                input_sha256=input_sha256,
                raw_path=raw_path,
                run_index=run_index,
                result_index=result_index,
                warnings=warnings,
            )
            if finding is not None:
                findings.append(finding)

    if result_count == 0:
        raise SarifParseError("empty SARIF: document contains no result records")
    if not findings:
        raise SarifParseError("SARIF contained no valid result records")

    metadata = {
        "format": "sarif",
        "sarif_version": version,
        "runs": len(runs),
        "records": result_count,
        "valid_records": len(findings),
        "malformed_records": result_count - len(findings),
        "rules": sorted(
            {
                str(finding.provenance["rule_id"])
                for finding in findings
                if finding.provenance.get("rule_id")
            }
        ),
        "summary": {
            "findings": len({finding.id for finding in findings}),
            "warnings": len(warnings),
        },
    }
    return SarifParseResult(findings=findings, warnings=warnings, metadata=metadata)


def _finding_from_result(
    result: dict[str, Any],
    *,
    rules: dict[str, dict[str, Any]],
    tool_name: str | None,
    input_sha256: str,
    raw_path: str,
    run_index: int,
    result_index: int,
    warnings: list[str],
) -> NormalizedFinding | None:
    rule_id = _as_str(result.get("ruleId"))
    if rule_id is None:
        warnings.append(f"run {run_index} result {result_index}: missing ruleId")
        return None
    rule = rules.get(rule_id, {})
    location = _primary_location(result)
    asset = location.get("uri") if location else None
    message = _message_text(result.get("message"))
    title = _as_str(rule.get("name")) or rule_id
    now = utc_now()
    locator = f"run[{run_index}]/result[{result_index}]"
    source = SourceReference(
        tool="sarif",
        input_sha256=input_sha256,
        raw_path=raw_path,
        locator=locator,
        metadata={
            "tool_name": tool_name,
            "rule_id": rule_id,
            "level": _as_str(result.get("level")),
            "uri": asset,
            "start_line": location.get("start_line") if location else None,
        },
    )
    weakness_ids = _weakness_ids(rule, result)
    return NormalizedFinding(
        id=deterministic_finding_id(
            "sarif",
            tool_name,
            rule_id,
            asset,
            location.get("start_line") if location else None,
            message,
        ),
        title=title,
        severity=_map_severity(result, rule),
        confidence="tool-observed",
        description=_message_text(rule.get("fullDescription")) or message,
        remediation=_message_text(rule.get("help")),
        asset=asset,
        weakness_ids=weakness_ids,
        references=_references(rule),
        tags=_tags(tool_name=tool_name, rule_id=rule_id, weakness_ids=weakness_ids),
        evidence=_evidence(message=message, location=location, locator=locator),
        source_references=[source],
        affected_instances=[
            AffectedInstance(
                asset=asset or "unknown",
                location=_location_label(location),
                metadata={"rule_id": rule_id, "tool_name": tool_name},
            )
        ],
        first_seen=now,
        last_seen=now,
        provenance={
            "tool": "sarif",
            "type": "result",
            "sarif_tool": tool_name,
            "rule_id": rule_id,
        },
    )


def _rules_by_id(run: dict[str, Any]) -> dict[str, dict[str, Any]]:
    driver = _driver(run)
    rules = driver.get("rules") if driver else None
    if not isinstance(rules, list):
        return {}
    indexed: dict[str, dict[str, Any]] = {}
    for rule in rules:
        if isinstance(rule, dict) and isinstance(rule.get("id"), str):
            indexed[rule["id"]] = rule
    return indexed


def _tool_name(run: dict[str, Any]) -> str | None:
    driver = _driver(run)
    if not driver:
        return None
    return _as_str(driver.get("name"))


def _driver(run: dict[str, Any]) -> dict[str, Any] | None:
    tool = run.get("tool")
    if not isinstance(tool, dict):
        return None
    driver = tool.get("driver")
    return driver if isinstance(driver, dict) else None


def _primary_location(result: dict[str, Any]) -> dict[str, Any] | None:
    locations = result.get("locations")
    if not isinstance(locations, list):
        return None
    for location in locations:
        if not isinstance(location, dict):
            continue
        physical = location.get("physicalLocation")
        if not isinstance(physical, dict):
            continue
        artifact = physical.get("artifactLocation")
        region = physical.get("region")
        uri = artifact.get("uri") if isinstance(artifact, dict) else None
        start_line = region.get("startLine") if isinstance(region, dict) else None
        return {
            "uri": uri if isinstance(uri, str) else None,
            "start_line": start_line if isinstance(start_line, int) else None,
        }
    return None


def _evidence(
    *,
    message: str | None,
    location: dict[str, Any] | None,
    locator: str,
) -> list[EvidenceSnippet]:
    details = message or "SARIF result"
    location_label = _location_label(location)
    if location_label:
        details = f"{details} at {location_label}"
    return [EvidenceSnippet(kind="sarif-result", value=details, locator=locator)]


def _location_label(location: dict[str, Any] | None) -> str | None:
    if not location:
        return None
    uri = location.get("uri")
    start_line = location.get("start_line")
    if isinstance(uri, str) and isinstance(start_line, int):
        return f"{uri}:{start_line}"
    return uri if isinstance(uri, str) else None


def _weakness_ids(rule: dict[str, Any], result: dict[str, Any]) -> list[str]:
    ids: set[str] = set()
    for value in [*_tags_from_properties(rule), *_tags_from_properties(result)]:
        upper = value.upper()
        if upper.startswith(("CWE-", "CVE-")):
            ids.add(upper)
    return sorted(ids)


def _references(rule: dict[str, Any]) -> list[str]:
    refs: set[str] = set()
    help_uri = _as_str(rule.get("helpUri"))
    if help_uri:
        refs.add(help_uri)
    return sorted(refs)


def _tags(*, tool_name: str | None, rule_id: str, weakness_ids: list[str]) -> list[str]:
    tags = {"sarif", f"sarif-rule-{rule_id}"}
    if tool_name:
        tags.add(f"sarif-tool-{tool_name.lower()}")
    tags.update(item.lower() for item in weakness_ids)
    return sorted(tags)


def _map_severity(result: dict[str, Any], rule: dict[str, Any]) -> Severity:
    security_severity = _security_severity(result) or _security_severity(rule)
    if security_severity is not None:
        if security_severity >= 9.0:
            return "critical"
        if security_severity >= 7.0:
            return "high"
        if security_severity >= 4.0:
            return "medium"
        if security_severity > 0.0:
            return "low"
    level = _as_str(result.get("level")) or _default_rule_level(rule)
    return {
        "error": "high",
        "warning": "medium",
        "note": "low",
        "none": "info",
    }.get((level or "none").lower(), "info")


def _security_severity(payload: dict[str, Any]) -> float | None:
    properties = payload.get("properties")
    if not isinstance(properties, dict):
        return None
    value = properties.get("security-severity")
    if isinstance(value, int | float):
        return float(value)
    if isinstance(value, str):
        try:
            return float(value)
        except ValueError:
            return None
    return None


def _default_rule_level(rule: dict[str, Any]) -> str | None:
    configuration = rule.get("defaultConfiguration")
    if not isinstance(configuration, dict):
        return None
    return _as_str(configuration.get("level"))


def _tags_from_properties(payload: dict[str, Any]) -> list[str]:
    properties = payload.get("properties")
    if not isinstance(properties, dict):
        return []
    tags = properties.get("tags")
    if not isinstance(tags, list):
        return []
    return [item for item in tags if isinstance(item, str) and item]


def _message_text(value: object) -> str | None:
    if not isinstance(value, dict):
        return None
    text = _as_str(value.get("text")) or _as_str(value.get("markdown"))
    return text


def _as_str(value: object) -> str | None:
    if isinstance(value, str) and value:
        return value
    return None


__all__ = ["SarifParseError", "SarifParseResult", "parse_sarif_file"]
