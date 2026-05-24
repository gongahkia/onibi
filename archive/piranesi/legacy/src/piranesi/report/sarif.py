from __future__ import annotations

import re
from pathlib import Path
from typing import TYPE_CHECKING

from piranesi.report.cwe import cwe_reporting_descriptor

if TYPE_CHECKING:
    from piranesi.models import SourceLocation, TaintStep
    from piranesi.report.renderer import CombinedFinding, PiranesiReport

_SARIF_SCHEMA_URI = (
    "https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/schemas/sarif-schema-2.1.0.json"
)
_SRCROOT_URI_BASE_ID = "%SRCROOT%"
_SEVERITY_LEVELS = {
    "critical": "error",
    "high": "error",
    "medium": "warning",
    "low": "note",
    "info": "note",
    "informational": "note",
    "note": "note",
}
_HUNK_PATTERN = re.compile(
    r"^@@ -(?P<old_start>\d+)(?:,(?P<old_length>\d+))? "
    r"\+(?P<new_start>\d+)(?:,(?P<new_length>\d+))? @@"
)


def generate_sarif(report: PiranesiReport) -> dict[str, object]:
    target_root = Path(report.target).resolve(strict=False)
    rules: dict[str, dict[str, object]] = {}
    results = []

    for finding in report.findings:
        rules.setdefault(
            finding.cwe,
            cwe_reporting_descriptor(finding.cwe, fallback=finding.title),
        )
        results.append(_result_for_finding(finding, target_root=target_root))

    return {
        "$schema": _SARIF_SCHEMA_URI,
        "version": "2.1.0",
        "runs": [
            {
                "tool": {
                    "driver": {
                        "name": "Piranesi",
                        "informationUri": "https://github.com/gongahkia/piranesi",
                        "version": report.appendix.piranesi_version,
                        "rules": list(rules.values()),
                    }
                },
                "originalUriBaseIds": {
                    _SRCROOT_URI_BASE_ID: {
                        "uri": _directory_uri(target_root),
                        "description": {"text": "Analyzed project root"},
                    }
                },
                "results": results,
                "properties": {
                    "target": report.target,
                    "generatedAt": report.generated_at,
                },
            }
        ],
    }


def _result_for_finding(
    finding: CombinedFinding,
    *,
    target_root: Path,
) -> dict[str, object]:
    result: dict[str, object] = {
        "ruleId": finding.cwe,
        "level": _severity_to_level(finding.severity),
        "message": {
            "text": finding.exploit_payload or f"Confirmed {finding.title.lower()} exploit."
        },
        "locations": [
            _sarif_location(
                finding.source_location,
                target_root=target_root,
                message="Taint source",
            )
        ],
        "properties": _result_properties(finding),
    }

    sink_location = _sarif_location(
        finding.sink_location,
        target_root=target_root,
        message="Taint sink",
    )
    result["relatedLocations"] = [sink_location]

    if finding.taint_path:
        result["codeFlows"] = [
            {
                "threadFlows": [
                    {
                        "locations": [
                            _thread_flow_location(step, target_root=target_root, index=index)
                            for index, step in enumerate(finding.taint_path)
                        ]
                    }
                ]
            }
        ]

    fix = _sarif_fix(finding, target_root=target_root)
    if fix is not None:
        result["fixes"] = [fix]

    return result


def _result_properties(finding: CombinedFinding) -> dict[str, object]:
    properties: dict[str, object] = {
        "findingId": finding.finding_id,
        "severity": finding.severity.lower(),
        "confidence": finding.confidence,
        "verificationMethod": finding.verification_method,
        "relatedCves": list(finding.related_cves),
    }

    regulatory: dict[str, object] = {}
    if finding.regulatory_obligations:
        regulatory["obligations"] = [
            obligation.model_dump(mode="json") for obligation in finding.regulatory_obligations
        ]
    if finding.legal_risk_tier is not None:
        regulatory["riskTier"] = finding.legal_risk_tier
    if finding.legal_memo_markdown is not None:
        regulatory["memoMarkdown"] = finding.legal_memo_markdown
    if regulatory:
        properties["regulatory"] = regulatory

    return properties


def _thread_flow_location(
    step: TaintStep,
    *,
    target_root: Path,
    index: int,
) -> dict[str, object]:
    payload: dict[str, object] = {
        "location": _sarif_location(
            step.location,
            target_root=target_root,
            message=_thread_flow_message(step),
        ),
        "kinds": ["taint"],
        "executionOrder": index,
    }
    state = {
        "operation": {"text": step.operation},
        "taintState": {"text": step.taint_state},
    }
    if step.through_function is not None:
        state["throughFunction"] = {"text": step.through_function}
    if step.sanitizer_applied is not None:
        state["sanitizerApplied"] = {"text": step.sanitizer_applied}
    payload["state"] = state
    return payload


def _thread_flow_message(step: TaintStep) -> str:
    details = [step.operation, f"state={step.taint_state}"]
    if step.through_function is not None:
        details.append(f"via {step.through_function}")
    if step.sanitizer_applied is not None:
        details.append(f"sanitizer={step.sanitizer_applied}")
    return " | ".join(details)


def _sarif_location(
    location: SourceLocation,
    *,
    target_root: Path,
    message: str | None = None,
) -> dict[str, object]:
    payload: dict[str, object] = {
        "physicalLocation": {
            "artifactLocation": _artifact_location(location.file, target_root=target_root),
            "region": _region(location),
        }
    }
    if message is not None:
        payload["message"] = {"text": message}
    return payload


def _artifact_location(path: str, *, target_root: Path) -> dict[str, str]:
    artifact_path = Path(path)
    if artifact_path.is_absolute():
        resolved_path = artifact_path.resolve(strict=False)
        try:
            relative_path = resolved_path.relative_to(target_root)
        except ValueError:
            return {"uri": resolved_path.as_uri()}
        return {"uri": relative_path.as_posix(), "uriBaseId": _SRCROOT_URI_BASE_ID}
    return {"uri": artifact_path.as_posix()}


def _region(location: SourceLocation) -> dict[str, int]:
    region = {
        "startLine": location.line,
        "startColumn": location.column,
    }
    if location.end_line is not None:
        region["endLine"] = location.end_line
    if location.end_column is not None:
        region["endColumn"] = location.end_column
    return region


def _severity_to_level(severity: str) -> str:
    return _SEVERITY_LEVELS.get(severity.lower(), "warning")


def _sarif_fix(
    finding: CombinedFinding,
    *,
    target_root: Path,
) -> dict[str, object] | None:
    if finding.patch_diff is None:
        return None
    artifact_changes = _artifact_changes_from_diff(finding.patch_diff, target_root=target_root)
    if not artifact_changes:
        return None
    fix: dict[str, object] = {"artifactChanges": artifact_changes}
    if finding.patch_explanation is not None:
        fix["description"] = {"text": finding.patch_explanation}
    return fix


def _artifact_changes_from_diff(
    patch_diff: str,
    *,
    target_root: Path,
) -> list[dict[str, object]]:
    lines = patch_diff.splitlines()
    artifact_changes: list[dict[str, object]] = []
    current_path: str | None = None
    current_replacements: list[dict[str, object]] = []
    old_path: str | None = None

    index = 0
    while index < len(lines):
        line = lines[index]

        if line.startswith("--- "):
            old_path = _normalize_diff_path(line[4:].strip())
            index += 1
            continue

        if line.startswith("+++ "):
            if current_path is not None and current_replacements:
                artifact_changes.append(
                    {
                        "artifactLocation": _artifact_location(
                            current_path,
                            target_root=target_root,
                        ),
                        "replacements": current_replacements,
                    }
                )
            new_path = _normalize_diff_path(line[4:].strip())
            current_path = old_path if new_path == "/dev/null" else new_path
            current_replacements = []
            index += 1
            continue

        match = _HUNK_PATTERN.match(line)
        if match is None:
            index += 1
            continue

        old_start = int(match.group("old_start"))
        old_length = int(match.group("old_length") or "1")
        inserted_lines: list[str] = []

        index += 1
        while index < len(lines):
            hunk_line = lines[index]
            if (
                hunk_line.startswith("@@ ")
                or hunk_line.startswith("--- ")
                or hunk_line.startswith("diff --git ")
            ):
                break
            if hunk_line.startswith((" ", "+")):
                inserted_lines.append(hunk_line[1:])
            index += 1

        replacement: dict[str, object] = {
            "deletedRegion": _replacement_region(old_start, old_length)
        }
        if inserted_lines:
            replacement["insertedContent"] = {"text": "\n".join(inserted_lines) + "\n"}
        current_replacements.append(replacement)

    if current_path is not None and current_replacements:
        artifact_changes.append(
            {
                "artifactLocation": _artifact_location(current_path, target_root=target_root),
                "replacements": current_replacements,
            }
        )

    return artifact_changes


def _replacement_region(start_line: int, line_count: int) -> dict[str, int]:
    normalized_start = max(start_line, 1)
    if line_count <= 0:
        return {
            "startLine": normalized_start,
            "startColumn": 1,
            "endLine": normalized_start,
            "endColumn": 1,
        }
    return {
        "startLine": normalized_start,
        "startColumn": 1,
        "endLine": normalized_start + line_count - 1,
        "endColumn": 1,
    }


def _normalize_diff_path(path: str) -> str:
    normalized = path.split("\t", maxsplit=1)[0]
    if normalized.startswith(("a/", "b/")):
        return normalized[2:]
    return normalized


def _directory_uri(path: Path) -> str:
    uri = path.resolve(strict=False).as_uri()
    return uri if uri.endswith("/") else f"{uri}/"


__all__ = ["generate_sarif"]
