from __future__ import annotations

import json
from collections import Counter
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from piranesi.workspace import (
    AUDIT_LOG_FILE,
    FINDINGS_FILE,
    WORKSPACE_FILE,
    AuditEvent,
    NormalizedFinding,
    WorkspaceState,
    append_audit_event,
    file_sha256,
    load_workspace,
    save_workspace,
    utc_now,
)

RETEST_SCHEMA_VERSION: Literal["piranesi.retest.v1"] = "piranesi.retest.v1"
RetestStatus = Literal["new", "open", "closed", "changed", "regressed", "ambiguous"]


class RetestError(ValueError):
    """Raised when retest comparison cannot be completed."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class RetestFinding(_StrictModel):
    finding_id: str
    status: RetestStatus
    title: str
    asset: str | None = None
    baseline_id: str | None = None
    current_id: str | None = None
    matched_by: Literal["id", "fallback", "none", "ambiguous"]
    details: dict[str, Any] = Field(default_factory=dict)


class RetestResult(_StrictModel):
    schema_version: Literal["piranesi.retest.v1"] = RETEST_SCHEMA_VERSION
    generated_at: str
    baseline_workspace: str
    current_workspace: str
    baseline_digest: str
    current_digest: str
    summary: dict[str, int]
    findings: list[RetestFinding]
    ambiguous_matches: list[dict[str, Any]] = Field(default_factory=list)


def compare_workspaces(
    baseline_root: Path | str,
    current_root: Path | str,
) -> RetestResult:
    baseline = load_workspace(baseline_root)
    current = load_workspace(current_root)
    result = build_retest_result(baseline, current)
    annotate_current_workspace(current, result)
    return result


def build_retest_result(baseline: WorkspaceState, current: WorkspaceState) -> RetestResult:
    baseline_by_id = {finding.id: finding for finding in baseline.findings.findings}
    current_by_id = {finding.id: finding for finding in current.findings.findings}
    used_baseline: set[str] = set()
    results: list[RetestFinding] = []
    ambiguous: list[dict[str, Any]] = []

    for current_finding in sorted(current.findings.findings, key=lambda item: item.id):
        baseline_finding = baseline_by_id.get(current_finding.id)
        if baseline_finding is not None:
            used_baseline.add(baseline_finding.id)
            results.append(_classify_matched(baseline_finding, current_finding, matched_by="id"))
            continue

        fallback_candidates = _fallback_candidates(current_finding, baseline.findings.findings)
        unused_candidates = [
            candidate for candidate in fallback_candidates if candidate.id not in used_baseline
        ]
        if len(unused_candidates) == 1:
            baseline_finding = unused_candidates[0]
            used_baseline.add(baseline_finding.id)
            results.append(
                _classify_matched(baseline_finding, current_finding, matched_by="fallback")
            )
        elif len(unused_candidates) > 1:
            candidate_ids = [candidate.id for candidate in unused_candidates]
            ambiguous.append(
                {
                    "current_id": current_finding.id,
                    "candidate_baseline_ids": candidate_ids,
                    "reason": "multiple fallback candidates matched",
                }
            )
            results.append(
                RetestFinding(
                    finding_id=current_finding.id,
                    status="ambiguous",
                    title=current_finding.title,
                    asset=current_finding.asset,
                    current_id=current_finding.id,
                    matched_by="ambiguous",
                    details={"candidate_baseline_ids": candidate_ids},
                )
            )
        else:
            results.append(
                RetestFinding(
                    finding_id=current_finding.id,
                    status="new",
                    title=current_finding.title,
                    asset=current_finding.asset,
                    current_id=current_finding.id,
                    matched_by="none",
                )
            )

    for baseline_finding in sorted(baseline.findings.findings, key=lambda item: item.id):
        if baseline_finding.id in used_baseline or baseline_finding.id in current_by_id:
            continue
        results.append(
            RetestFinding(
                finding_id=baseline_finding.id,
                status="closed",
                title=baseline_finding.title,
                asset=baseline_finding.asset,
                baseline_id=baseline_finding.id,
                matched_by="none",
            )
        )

    summary = Counter(item.status for item in results)
    return RetestResult(
        generated_at=utc_now(),
        baseline_workspace=str(baseline.root),
        current_workspace=str(current.root),
        baseline_digest=_workspace_digest(baseline.root),
        current_digest=_workspace_digest(current.root),
        summary={status: summary.get(status, 0) for status in _status_order()},
        findings=sorted(results, key=lambda item: (item.status, item.finding_id)),
        ambiguous_matches=ambiguous,
    )


def annotate_current_workspace(current: WorkspaceState, result: RetestResult) -> None:
    status_by_id = {
        item.current_id: item.status
        for item in result.findings
        if item.current_id is not None and item.status != "closed"
    }
    updated: list[NormalizedFinding] = []
    for finding in current.findings.findings:
        status = status_by_id.get(finding.id)
        if status is None:
            updated.append(finding)
            continue
        provenance = {**finding.provenance, "retest_status": status}
        finding_status = (
            status if status in {"new", "open", "changed", "regressed"} else finding.status
        )
        updated.append(
            finding.model_copy(
                update={
                    "status": finding_status,
                    "provenance": provenance,
                }
            )
        )
    save_workspace(
        WorkspaceState(
            root=current.root,
            workspace=current.workspace,
            findings=current.findings.model_copy(update={"findings": updated}),
        )
    )


def render_retest_markdown(result: RetestResult) -> str:
    lines = [
        "# Piranesi Retest Diff",
        "",
        f"Generated: {result.generated_at}",
        f"Baseline: {result.baseline_workspace}",
        f"Current: {result.current_workspace}",
        "",
        "## Summary",
        "",
    ]
    for status, count in result.summary.items():
        lines.append(f"- {status}: {count}")
    lines.extend(["", "## Findings", ""])
    for finding in result.findings:
        lines.append(
            f"- {finding.status}: {finding.title} "
            f"(baseline={finding.baseline_id or '-'}, current={finding.current_id or '-'})"
        )
    lines.append("")
    return "\n".join(lines)


def write_retest_output(result: RetestResult, output_path: Path) -> Path:
    output_path.parent.mkdir(parents=True, exist_ok=True)
    if output_path.suffix.lower() == ".md":
        output_path.write_text(render_retest_markdown(result), encoding="utf-8")
    else:
        output_path.write_text(
            json.dumps(result.model_dump(mode="json"), indent=2, sort_keys=True) + "\n",
            encoding="utf-8",
        )
    return output_path


def _classify_matched(
    baseline: NormalizedFinding,
    current: NormalizedFinding,
    *,
    matched_by: Literal["id", "fallback"],
) -> RetestFinding:
    if baseline.status == "closed" and current.status != "closed":
        status: RetestStatus = "regressed"
    elif _finding_signature(baseline) != _finding_signature(current):
        status = "changed"
    else:
        status = "open"
    return RetestFinding(
        finding_id=current.id,
        status=status,
        title=current.title,
        asset=current.asset,
        baseline_id=baseline.id,
        current_id=current.id,
        matched_by=matched_by,
        details={
            "baseline_status": baseline.status,
            "current_status": current.status,
        },
    )


def _fallback_candidates(
    current: NormalizedFinding,
    baseline_findings: list[NormalizedFinding],
) -> list[NormalizedFinding]:
    current_key = _fallback_key(current)
    return [finding for finding in baseline_findings if _fallback_key(finding) == current_key]


def _fallback_key(finding: NormalizedFinding) -> tuple[Any, ...]:
    service = finding.service
    return (
        finding.asset,
        finding.title.strip().lower(),
        service.protocol if service is not None else None,
        service.port if service is not None else None,
        tuple(sorted(finding.weakness_ids)),
    )


def _finding_signature(finding: NormalizedFinding) -> dict[str, Any]:
    return {
        "title": finding.title,
        "severity": finding.severity,
        "confidence": finding.confidence,
        "asset": finding.asset,
        "service": finding.service.model_dump(mode="json") if finding.service else None,
        "evidence": [
            item.model_dump(mode="json")
            for item in sorted(
                finding.evidence,
                key=lambda evidence: (evidence.kind, evidence.value),
            )
        ],
    }


def append_retest_audit(current: WorkspaceState, result: RetestResult, output_path: Path) -> None:
    try:
        output_reference = str(output_path.resolve(strict=False).relative_to(current.root))
    except ValueError:
        output_reference = str(output_path.resolve(strict=False))
    append_audit_event(
        current,
        AuditEvent(
            timestamp=utc_now(),
            command="retest",
            input_path=str(result.baseline_workspace),
            input_sha256=result.baseline_digest,
            output_path=output_reference,
            output_sha256=file_sha256(output_path) if output_path.is_file() else None,
            summary={
                "baseline_digest": result.baseline_digest,
                "current_digest": result.current_digest,
                "statuses": result.summary,
            },
        ),
    )


def _workspace_digest(root: Path) -> str:
    import hashlib

    digest = hashlib.sha256()
    for relative in (WORKSPACE_FILE, FINDINGS_FILE, AUDIT_LOG_FILE):
        path = root / relative
        if path.is_file():
            digest.update(relative.encode("utf-8"))
            digest.update(file_sha256(path).encode("utf-8"))
    return digest.hexdigest()


def _status_order() -> tuple[RetestStatus, ...]:
    return ("new", "open", "closed", "changed", "regressed", "ambiguous")


__all__ = [
    "RetestError",
    "RetestFinding",
    "RetestResult",
    "append_retest_audit",
    "build_retest_result",
    "compare_workspaces",
    "render_retest_markdown",
    "write_retest_output",
]
