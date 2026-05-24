from __future__ import annotations

import json
import zipfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Literal

from pydantic import ValidationError

from piranesi.detections import DetectionError, load_detections
from piranesi.evidence import EvidenceError, load_evidence_index
from piranesi.objectives import ObjectiveError, load_objectives, load_procedures
from piranesi.report.pentest import REPORT_SCHEMA_VERSION, PentestReport
from piranesi.report.redteam import RED_TEAM_REPORT_SCHEMA_VERSION, RedTeamReport
from piranesi.signing import SigningError, latest_manifest_path, verify_workspace
from piranesi.timeline import TimelineError, load_timeline_events
from piranesi.workspace import (
    NormalizedFinding,
    WorkspaceError,
    file_sha256,
    load_workspace,
    workspace_path,
)

REPORT_QA_SCHEMA_VERSION: Literal["piranesi.report-qa.v1"] = "piranesi.report-qa.v1"

IssueLevel = Literal["error", "warning"]
ReportArtifactKind = Literal[
    "report",
    "handoff-manifest",
    "handoff-draft",
    "archive",
    "pff",
    "artifact",
]

_HIGH_SEVERITIES = {"high", "critical"}
_RETEST_GUIDANCE_KEYS = {
    "retest_guidance",
    "retest_steps",
    "retest_checklist",
    "retest_notes",
    "retest_status",
}
_SENSITIVE_LEVELS = {"sensitive", "secret"}


@dataclass(frozen=True, slots=True)
class ReportQAIssue:
    level: IssueLevel
    code: str
    message: str
    path: str | None = None
    subject: str | None = None

    def as_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "level": self.level,
            "code": self.code,
            "message": self.message,
        }
        if self.path is not None:
            payload["path"] = self.path
        if self.subject is not None:
            payload["subject"] = self.subject
        return payload


@dataclass(frozen=True, slots=True)
class ReportQAArtifact:
    path: str
    sha256: str
    kind: ReportArtifactKind
    covered_by: list[str]

    def as_payload(self) -> dict[str, Any]:
        return {
            "path": self.path,
            "sha256": self.sha256,
            "kind": self.kind,
            "covered_by": self.covered_by,
        }


@dataclass(frozen=True, slots=True)
class ReportQAResult:
    workspace: Path
    artifacts: list[ReportQAArtifact]
    issues: list[ReportQAIssue]

    @property
    def error_count(self) -> int:
        return sum(1 for issue in self.issues if issue.level == "error")

    @property
    def warning_count(self) -> int:
        return sum(1 for issue in self.issues if issue.level == "warning")

    @property
    def valid(self) -> bool:
        return self.error_count == 0

    def as_payload(self) -> dict[str, Any]:
        return {
            "schema_version": REPORT_QA_SCHEMA_VERSION,
            "workspace": str(self.workspace),
            "valid": self.valid,
            "error_count": self.error_count,
            "warning_count": self.warning_count,
            "artifacts": [artifact.as_payload() for artifact in self.artifacts],
            "issues": [issue.as_payload() for issue in self.issues],
        }


@dataclass(frozen=True, slots=True)
class _ArtifactCandidate:
    path: Path
    relative_path: str
    sha256: str
    kind: ReportArtifactKind


def validate_delivery(workspace_root: Path | str) -> ReportQAResult:
    """Validate local report delivery readiness without rewriting artifacts."""
    state = load_workspace(workspace_root)
    issues: list[ReportQAIssue] = []

    evidence_ids, timeline_ids, objective_ids, procedure_ids = _load_reference_sets(
        state.root,
        issues,
    )
    finding_ids = {finding.id for finding in state.findings.findings}

    _check_findings(state.findings.findings, issues)
    _check_finding_source_references(state.root, state.findings.findings, issues)
    _check_evidence_records(state.root, evidence_ids, issues)
    _check_cross_references(
        state.root,
        issues,
        evidence_ids=evidence_ids,
        timeline_ids=timeline_ids,
        finding_ids=finding_ids,
        objective_ids=objective_ids,
        procedure_ids=procedure_ids,
    )

    artifacts = _collect_report_artifacts(state.root)
    if not state.findings.findings:
        issues.append(
            ReportQAIssue(
                level="warning",
                code="workspace-empty",
                message="workspace has no findings to deliver",
            )
        )
    if not artifacts:
        issues.append(
            ReportQAIssue(
                level="warning",
                code="no-report-artifacts",
                message="workspace has no report or handoff artifacts under reports/",
            )
        )

    coverage: dict[str, list[str]] = {artifact.relative_path: [] for artifact in artifacts}
    _check_report_outputs(state.root, artifacts, issues)
    _check_handoff_manifests(state.root, artifacts, coverage, issues)
    _check_archives(artifacts, issues)
    _check_signature_status(state.root, artifacts, coverage, issues)
    _check_optional_guidance(state.root, issues)

    return ReportQAResult(
        workspace=state.root,
        artifacts=[
            ReportQAArtifact(
                path=artifact.relative_path,
                sha256=artifact.sha256,
                kind=artifact.kind,
                covered_by=coverage.get(artifact.relative_path, []),
            )
            for artifact in artifacts
        ],
        issues=sorted(issues, key=lambda issue: (issue.level, issue.code, issue.path or "")),
    )


def render_report_qa_text(result: ReportQAResult) -> str:
    status = "passed" if result.valid else "failed"
    lines = [
        (
            f"delivery QA {status}: {result.error_count} errors, "
            f"{result.warning_count} warnings, {len(result.artifacts)} artifacts"
        )
    ]
    for issue in result.issues:
        location = f" [{issue.path}]" if issue.path else ""
        subject = f" ({issue.subject})" if issue.subject else ""
        lines.append(f"- {issue.level}: {issue.code}{location}{subject}: {issue.message}")
    return "\n".join(lines)


def _load_reference_sets(
    root: Path,
    issues: list[ReportQAIssue],
) -> tuple[set[str], set[str], set[str], set[str]]:
    evidence_ids: set[str] = set()
    timeline_ids: set[str] = set()
    objective_ids: set[str] = set()
    procedure_ids: set[str] = set()
    try:
        evidence_ids = {record.id for record in load_evidence_index(root).evidence}
    except EvidenceError as exc:
        issues.append(
            ReportQAIssue(
                level="error",
                code="evidence-index-invalid",
                message=str(exc),
                path="evidence/index.json",
            )
        )
    try:
        timeline_ids = {event.id for event in load_timeline_events(root)}
    except TimelineError as exc:
        issues.append(
            ReportQAIssue(
                level="error",
                code="timeline-invalid",
                message=str(exc),
                path="timeline/events.jsonl",
            )
        )
    try:
        objective_ids = {objective.id for objective in load_objectives(root).objectives}
    except ObjectiveError as exc:
        issues.append(
            ReportQAIssue(
                level="error",
                code="objectives-invalid",
                message=str(exc),
                path="objectives/objectives.json",
            )
        )
    try:
        procedure_ids = {procedure.id for procedure in load_procedures(root).procedures}
    except ObjectiveError as exc:
        issues.append(
            ReportQAIssue(
                level="error",
                code="procedures-invalid",
                message=str(exc),
                path="procedures/procedures.json",
            )
        )
    return evidence_ids, timeline_ids, objective_ids, procedure_ids


def _check_findings(findings: list[NormalizedFinding], issues: list[ReportQAIssue]) -> None:
    for finding in findings:
        if not finding.evidence and not finding.source_references:
            issues.append(
                ReportQAIssue(
                    level="error",
                    code="finding-missing-evidence",
                    message="finding has no evidence snippets or source references",
                    path="normalized/findings.json",
                    subject=finding.id,
                )
            )
        if finding.severity in _HIGH_SEVERITIES:
            if not _has_text(finding.remediation):
                issues.append(
                    ReportQAIssue(
                        level="error",
                        code="high-finding-missing-remediation",
                        message="high or critical finding is missing remediation guidance",
                        path="normalized/findings.json",
                        subject=finding.id,
                    )
                )
            if not _has_retest_guidance(finding):
                issues.append(
                    ReportQAIssue(
                        level="error",
                        code="high-finding-missing-retest-guidance",
                        message="high or critical finding is missing retest guidance",
                        path="normalized/findings.json",
                        subject=finding.id,
                    )
                )


def _check_finding_source_references(
    root: Path,
    findings: list[NormalizedFinding],
    issues: list[ReportQAIssue],
) -> None:
    for finding in findings:
        for source in finding.source_references:
            try:
                path = workspace_path(root, source.raw_path)
            except WorkspaceError as exc:
                issues.append(
                    ReportQAIssue(
                        level="error",
                        code="finding-source-reference-unsafe",
                        message=str(exc),
                        path="normalized/findings.json",
                        subject=finding.id,
                    )
                )
                continue
            if not path.is_file():
                issues.append(
                    ReportQAIssue(
                        level="error",
                        code="finding-source-reference-missing",
                        message=f"source reference file does not exist: {source.raw_path}",
                        path="normalized/findings.json",
                        subject=finding.id,
                    )
                )


def _check_evidence_records(
    root: Path,
    evidence_ids: set[str],
    issues: list[ReportQAIssue],
) -> None:
    if not evidence_ids:
        return
    try:
        records = load_evidence_index(root).evidence
    except EvidenceError:
        return
    for record in records:
        try:
            path = workspace_path(root, record.raw_path, allowed_roots=("raw",))
        except WorkspaceError as exc:
            issues.append(
                ReportQAIssue(
                    level="error",
                    code="evidence-raw-path-unsafe",
                    message=str(exc),
                    path="evidence/index.json",
                    subject=record.id,
                )
            )
            continue
        if not path.is_file():
            issues.append(
                ReportQAIssue(
                    level="error",
                    code="evidence-raw-file-missing",
                    message=f"evidence raw file does not exist: {record.raw_path}",
                    path="evidence/index.json",
                    subject=record.id,
                )
            )
            continue
        actual = file_sha256(path)
        if actual != record.sha256:
            issues.append(
                ReportQAIssue(
                    level="error",
                    code="evidence-raw-digest-mismatch",
                    message=f"evidence raw file digest mismatch for {record.raw_path}",
                    path="evidence/index.json",
                    subject=record.id,
                )
            )


def _check_cross_references(
    root: Path,
    issues: list[ReportQAIssue],
    *,
    evidence_ids: set[str],
    timeline_ids: set[str],
    finding_ids: set[str],
    objective_ids: set[str],
    procedure_ids: set[str],
) -> None:
    try:
        for event in load_timeline_events(root):
            _check_ids(
                event.evidence_ids,
                evidence_ids,
                issues,
                code="timeline-evidence-reference-missing",
                path="timeline/events.jsonl",
                subject=event.id,
                noun="evidence",
            )
            _check_ids(
                event.finding_ids,
                finding_ids,
                issues,
                code="timeline-finding-reference-missing",
                path="timeline/events.jsonl",
                subject=event.id,
                noun="finding",
            )
            _check_ids(
                event.objective_ids,
                objective_ids,
                issues,
                code="timeline-objective-reference-missing",
                path="timeline/events.jsonl",
                subject=event.id,
                noun="objective",
            )
    except TimelineError:
        pass

    try:
        for objective in load_objectives(root).objectives:
            _check_ids(
                objective.evidence_ids,
                evidence_ids,
                issues,
                code="objective-evidence-reference-missing",
                path="objectives/objectives.json",
                subject=objective.id,
                noun="evidence",
            )
            _check_ids(
                objective.timeline_event_ids,
                timeline_ids,
                issues,
                code="objective-timeline-reference-missing",
                path="objectives/objectives.json",
                subject=objective.id,
                noun="timeline event",
            )
    except ObjectiveError:
        pass

    try:
        for procedure in load_procedures(root).procedures:
            _check_ids(
                procedure.evidence_ids,
                evidence_ids,
                issues,
                code="procedure-evidence-reference-missing",
                path="procedures/procedures.json",
                subject=procedure.id,
                noun="evidence",
            )
            _check_ids(
                procedure.timeline_event_ids,
                timeline_ids,
                issues,
                code="procedure-timeline-reference-missing",
                path="procedures/procedures.json",
                subject=procedure.id,
                noun="timeline event",
            )
            _check_ids(
                procedure.finding_ids,
                finding_ids,
                issues,
                code="procedure-finding-reference-missing",
                path="procedures/procedures.json",
                subject=procedure.id,
                noun="finding",
            )
            _check_ids(
                procedure.objective_ids,
                objective_ids,
                issues,
                code="procedure-objective-reference-missing",
                path="procedures/procedures.json",
                subject=procedure.id,
                noun="objective",
            )
    except ObjectiveError:
        pass

    try:
        detections = load_detections(root)
    except DetectionError:
        return
    for ioc in detections.iocs:
        _check_ids(
            ioc.evidence_ids,
            evidence_ids,
            issues,
            code="ioc-evidence-reference-missing",
            path="detections/detections.json",
            subject=ioc.id,
            noun="evidence",
        )
        _check_ids(
            ioc.timeline_event_ids,
            timeline_ids,
            issues,
            code="ioc-timeline-reference-missing",
            path="detections/detections.json",
            subject=ioc.id,
            noun="timeline event",
        )
        _check_ids(
            ioc.procedure_ids,
            procedure_ids,
            issues,
            code="ioc-procedure-reference-missing",
            path="detections/detections.json",
            subject=ioc.id,
            noun="procedure",
        )
    for note in detections.notes:
        _check_ids(
            note.evidence_ids,
            evidence_ids,
            issues,
            code="detection-note-evidence-reference-missing",
            path="detections/detections.json",
            subject=note.id,
            noun="evidence",
        )
        _check_ids(
            note.timeline_event_ids,
            timeline_ids,
            issues,
            code="detection-note-timeline-reference-missing",
            path="detections/detections.json",
            subject=note.id,
            noun="timeline event",
        )
        _check_ids(
            note.procedure_ids,
            procedure_ids,
            issues,
            code="detection-note-procedure-reference-missing",
            path="detections/detections.json",
            subject=note.id,
            noun="procedure",
        )
        _check_ids(
            note.finding_ids,
            finding_ids,
            issues,
            code="detection-note-finding-reference-missing",
            path="detections/detections.json",
            subject=note.id,
            noun="finding",
        )


def _check_ids(
    references: list[str],
    known: set[str],
    issues: list[ReportQAIssue],
    *,
    code: str,
    path: str,
    subject: str,
    noun: str,
) -> None:
    missing = sorted(set(references) - known)
    for reference in missing:
        issues.append(
            ReportQAIssue(
                level="error",
                code=code,
                message=f"referenced {noun} does not exist: {reference}",
                path=path,
                subject=subject,
            )
        )


def _collect_report_artifacts(root: Path) -> list[_ArtifactCandidate]:
    reports_root = root / "reports"
    if not reports_root.is_dir():
        return []
    artifacts: list[_ArtifactCandidate] = []
    for path in sorted(item for item in reports_root.rglob("*") if item.is_file()):
        relative = path.relative_to(root).as_posix()
        artifacts.append(
            _ArtifactCandidate(
                path=path,
                relative_path=relative,
                sha256=file_sha256(path),
                kind=_artifact_kind(path),
            )
        )
    return artifacts


def _artifact_kind(path: Path) -> ReportArtifactKind:
    name = path.name
    if name.endswith("handoff-manifest.json"):
        return "handoff-manifest"
    if path.suffix.lower() == ".eml":
        return "handoff-draft"
    if path.suffix.lower() == ".zip":
        return "archive"
    if name.endswith(".pff.json"):
        return "pff"
    if path.suffix.lower() in {".json", ".md", ".pdf"}:
        return "report"
    return "artifact"


def _check_report_outputs(
    root: Path,
    artifacts: list[_ArtifactCandidate],
    issues: list[ReportQAIssue],
) -> None:
    for artifact in artifacts:
        if artifact.kind != "report" or artifact.path.suffix.lower() != ".json":
            continue
        payload = _load_json_artifact(artifact.path, issues)
        if payload is None:
            continue
        version = payload.get("schema_version")
        if version == REPORT_SCHEMA_VERSION:
            _check_pentest_report(root, artifact, payload, issues)
        elif version == RED_TEAM_REPORT_SCHEMA_VERSION:
            _check_red_team_report(artifact, payload, issues)


def _check_pentest_report(
    root: Path,
    artifact: _ArtifactCandidate,
    payload: dict[str, Any],
    issues: list[ReportQAIssue],
) -> None:
    try:
        report = PentestReport.model_validate(payload)
    except ValidationError as exc:
        issues.append(
            ReportQAIssue(
                level="error",
                code="report-schema-invalid",
                message=f"invalid pentest report schema: {exc}",
                path=artifact.relative_path,
            )
        )
        return
    for finding in report.findings:
        _check_report_evidence_values(
            finding.evidence,
            issues,
            path=artifact.relative_path,
            subject=finding.id,
        )
        for source in finding.source_references:
            raw_path = source.get("raw_path")
            if not isinstance(raw_path, str):
                continue
            try:
                path = workspace_path(root, raw_path)
            except WorkspaceError:
                continue
            if not path.is_file():
                issues.append(
                    ReportQAIssue(
                        level="error",
                        code="report-source-reference-missing",
                        message=f"report source reference file does not exist: {raw_path}",
                        path=artifact.relative_path,
                        subject=finding.id,
                    )
                )


def _check_red_team_report(
    artifact: _ArtifactCandidate,
    payload: dict[str, Any],
    issues: list[ReportQAIssue],
) -> None:
    try:
        report = RedTeamReport.model_validate(payload)
    except ValidationError as exc:
        issues.append(
            ReportQAIssue(
                level="error",
                code="report-schema-invalid",
                message=f"invalid red-team report schema: {exc}",
                path=artifact.relative_path,
            )
        )
        return
    for record in report.evidence:
        _check_redaction_sensitive_fields(record, issues, path=artifact.relative_path)
    for finding in report.findings:
        _check_report_evidence_values(
            finding.get("evidence"),
            issues,
            path=artifact.relative_path,
            subject=_string_or_none(finding.get("id")),
        )
    detections = report.detections
    for ioc in _list_of_dicts(detections.get("iocs")):
        _check_redaction_sensitive_fields(ioc, issues, path=artifact.relative_path)
    for note in _list_of_dicts(detections.get("notes")):
        _check_redaction_sensitive_fields(note, issues, path=artifact.relative_path)


def _check_report_evidence_values(
    evidence: Any,
    issues: list[ReportQAIssue],
    *,
    path: str,
    subject: str | None,
) -> None:
    for item in _list_of_dicts(evidence):
        if item.get("redacted") is True and item.get("value") != "[redacted]":
            issues.append(
                ReportQAIssue(
                    level="error",
                    code="report-redacted-evidence-leak",
                    message="report includes a redacted evidence value that is not masked",
                    path=path,
                    subject=subject,
                )
            )


def _check_redaction_sensitive_fields(
    record: dict[str, Any],
    issues: list[ReportQAIssue],
    *,
    path: str,
) -> None:
    subject = _string_or_none(record.get("id"))
    sensitivity = record.get("sensitivity")
    for field in ("notes", "body"):
        value = record.get(field)
        if sensitivity in _SENSITIVE_LEVELS and _has_text(value) and value != "[redacted]":
            issues.append(
                ReportQAIssue(
                    level="error",
                    code="report-sensitive-field-leak",
                    message=f"sensitive {field} field is present in report output",
                    path=path,
                    subject=subject,
                )
            )
        if sensitivity == "internal" and _has_text(value):
            issues.append(
                ReportQAIssue(
                    level="error",
                    code="report-internal-note-leak",
                    message=f"internal-only {field} field is present in client handoff output",
                    path=path,
                    subject=subject,
                )
            )


def _check_handoff_manifests(
    root: Path,
    artifacts: list[_ArtifactCandidate],
    coverage: dict[str, list[str]],
    issues: list[ReportQAIssue],
) -> None:
    artifact_by_path = {artifact.relative_path: artifact for artifact in artifacts}
    manifests = [artifact for artifact in artifacts if artifact.kind == "handoff-manifest"]
    for manifest in manifests:
        payload = _load_json_artifact(manifest.path, issues)
        if payload is None:
            continue
        if payload.get("schema_version") != "piranesi.handoff-manifest.v1":
            issues.append(
                ReportQAIssue(
                    level="error",
                    code="handoff-manifest-schema-invalid",
                    message="unsupported handoff manifest schema_version",
                    path=manifest.relative_path,
                )
            )
            continue
        if payload.get("sensitive_content_embedded") is not False:
            issues.append(
                ReportQAIssue(
                    level="error",
                    code="handoff-sensitive-content-embedded",
                    message="handoff manifest does not clearly exclude embedded sensitive content",
                    path=manifest.relative_path,
                )
            )
        draft = payload.get("draft")
        if isinstance(draft, dict):
            _check_handoff_reference(
                root,
                manifest,
                artifact_by_path,
                coverage,
                issues,
                reference=draft,
                label="draft",
            )
        artifact_refs = payload.get("artifacts")
        if not isinstance(artifact_refs, list):
            issues.append(
                ReportQAIssue(
                    level="error",
                    code="handoff-manifest-artifacts-invalid",
                    message="handoff manifest artifacts must be an array",
                    path=manifest.relative_path,
                )
            )
            continue
        for reference in artifact_refs:
            if not isinstance(reference, dict):
                issues.append(
                    ReportQAIssue(
                        level="error",
                        code="handoff-manifest-artifact-invalid",
                        message="handoff manifest artifact reference must be an object",
                        path=manifest.relative_path,
                    )
                )
                continue
            _check_handoff_reference(
                root,
                manifest,
                artifact_by_path,
                coverage,
                issues,
                reference=reference,
                label="artifact",
            )


def _check_handoff_reference(
    root: Path,
    manifest: _ArtifactCandidate,
    artifact_by_path: dict[str, _ArtifactCandidate],
    coverage: dict[str, list[str]],
    issues: list[ReportQAIssue],
    *,
    reference: dict[Any, Any],
    label: str,
) -> None:
    relative = reference.get("path")
    expected = reference.get("sha256")
    if not isinstance(relative, str) or not relative:
        issues.append(
            ReportQAIssue(
                level="error",
                code="handoff-reference-path-missing",
                message=f"handoff {label} reference is missing path",
                path=manifest.relative_path,
            )
        )
        return
    if not isinstance(expected, str) or not expected:
        issues.append(
            ReportQAIssue(
                level="error",
                code="handoff-reference-digest-missing",
                message=f"handoff {label} reference is missing sha256",
                path=manifest.relative_path,
                subject=relative,
            )
        )
        return
    try:
        path = workspace_path(root, relative, allowed_roots=("reports",))
    except WorkspaceError as exc:
        issues.append(
            ReportQAIssue(
                level="error",
                code="handoff-reference-unsafe",
                message=str(exc),
                path=manifest.relative_path,
                subject=relative,
            )
        )
        return
    if not path.is_file():
        issues.append(
            ReportQAIssue(
                level="error",
                code="handoff-reference-missing",
                message=f"handoff {label} file does not exist: {relative}",
                path=manifest.relative_path,
                subject=relative,
            )
        )
        return
    actual = file_sha256(path)
    if actual != expected:
        issues.append(
            ReportQAIssue(
                level="error",
                code="handoff-reference-digest-mismatch",
                message=f"handoff {label} digest mismatch for {relative}",
                path=manifest.relative_path,
                subject=relative,
            )
        )
        return
    if relative in artifact_by_path:
        coverage.setdefault(relative, []).append(manifest.relative_path)


def _check_archives(artifacts: list[_ArtifactCandidate], issues: list[ReportQAIssue]) -> None:
    for artifact in artifacts:
        if artifact.kind != "archive":
            continue
        try:
            with zipfile.ZipFile(artifact.path) as archive:
                names = set(archive.namelist())
                if "archive-manifest.json" not in names:
                    issues.append(
                        ReportQAIssue(
                            level="error",
                            code="archive-manifest-missing",
                            message="handoff archive is missing archive-manifest.json",
                            path=artifact.relative_path,
                        )
                    )
                    continue
                payload = json.loads(archive.read("archive-manifest.json").decode("utf-8"))
                entries = payload.get("entries") if isinstance(payload, dict) else None
                if not isinstance(entries, list):
                    issues.append(
                        ReportQAIssue(
                            level="error",
                            code="archive-manifest-entries-invalid",
                            message="archive manifest entries must be an array",
                            path=artifact.relative_path,
                        )
                    )
                    continue
                for entry in entries:
                    _check_archive_entry(archive, names, artifact.relative_path, entry, issues)
        except (zipfile.BadZipFile, UnicodeDecodeError, json.JSONDecodeError) as exc:
            issues.append(
                ReportQAIssue(
                    level="error",
                    code="archive-invalid",
                    message=f"invalid handoff archive: {exc}",
                    path=artifact.relative_path,
                )
            )


def _check_archive_entry(
    archive: zipfile.ZipFile,
    names: set[str],
    archive_path: str,
    entry: Any,
    issues: list[ReportQAIssue],
) -> None:
    if not isinstance(entry, dict):
        issues.append(
            ReportQAIssue(
                level="error",
                code="archive-manifest-entry-invalid",
                message="archive manifest entry must be an object",
                path=archive_path,
            )
        )
        return
    relative = entry.get("path")
    expected = entry.get("sha256")
    if not isinstance(relative, str) or not relative:
        issues.append(
            ReportQAIssue(
                level="error",
                code="archive-manifest-entry-path-missing",
                message="archive manifest entry is missing path",
                path=archive_path,
            )
        )
        return
    candidate = Path(relative)
    if candidate.is_absolute() or any(part in {"", ".", ".."} for part in candidate.parts):
        issues.append(
            ReportQAIssue(
                level="error",
                code="archive-manifest-entry-unsafe",
                message=f"archive manifest entry path is unsafe: {relative}",
                path=archive_path,
                subject=relative,
            )
        )
        return
    if relative not in names:
        issues.append(
            ReportQAIssue(
                level="error",
                code="archive-manifest-entry-missing",
                message=f"archive manifest references missing entry: {relative}",
                path=archive_path,
                subject=relative,
            )
        )
        return
    if not isinstance(expected, str) or not expected:
        issues.append(
            ReportQAIssue(
                level="error",
                code="archive-manifest-entry-digest-missing",
                message=f"archive manifest entry is missing sha256: {relative}",
                path=archive_path,
                subject=relative,
            )
        )
        return
    actual = _sha256_bytes(archive.read(relative))
    if actual != expected:
        issues.append(
            ReportQAIssue(
                level="error",
                code="archive-manifest-entry-digest-mismatch",
                message=f"archive manifest entry digest mismatch: {relative}",
                path=archive_path,
                subject=relative,
            )
        )


def _check_signature_status(
    root: Path,
    artifacts: list[_ArtifactCandidate],
    coverage: dict[str, list[str]],
    issues: list[ReportQAIssue],
) -> None:
    manifest_path = latest_manifest_path(root)
    if manifest_path is None:
        issues.append(
            ReportQAIssue(
                level="warning",
                code="unsigned-delivery",
                message="no chain-of-custody manifest found under signatures/",
            )
        )
        return
    relative_manifest = manifest_path.relative_to(root).as_posix()
    try:
        verification = verify_workspace(root, manifest_path=manifest_path)
    except SigningError as exc:
        issues.append(
            ReportQAIssue(
                level="error",
                code="signature-verification-error",
                message=str(exc),
                path=relative_manifest,
            )
        )
        return
    if not verification.ok:
        for failure in verification.failures:
            issues.append(
                ReportQAIssue(
                    level="error",
                    code="signature-verification-failed",
                    message=failure.message,
                    path=relative_manifest,
                    subject=failure.path,
                )
            )
    manifest_payload = _load_json_artifact(manifest_path, issues, relative_path=relative_manifest)
    if manifest_payload is None:
        return
    manifest_artifacts = manifest_payload.get("artifacts")
    if not isinstance(manifest_artifacts, list):
        issues.append(
            ReportQAIssue(
                level="error",
                code="signature-manifest-artifacts-invalid",
                message="signature manifest artifacts must be an array",
                path=relative_manifest,
            )
        )
        return
    manifest_digests: dict[str, str] = {}
    for entry in manifest_artifacts:
        if not isinstance(entry, dict):
            continue
        path = entry.get("path")
        sha256 = entry.get("sha256")
        if isinstance(path, str) and isinstance(sha256, str):
            manifest_digests[path] = sha256
    for artifact in artifacts:
        expected = manifest_digests.get(artifact.relative_path)
        if expected is None:
            issues.append(
                ReportQAIssue(
                    level="error",
                    code="report-artifact-not-signed",
                    message="report artifact is not covered by latest chain-of-custody manifest",
                    path=artifact.relative_path,
                )
            )
            continue
        if expected != artifact.sha256:
            issues.append(
                ReportQAIssue(
                    level="error",
                    code="report-artifact-signature-stale",
                    message="report artifact digest differs from latest chain-of-custody manifest",
                    path=artifact.relative_path,
                )
            )
            continue
        coverage.setdefault(artifact.relative_path, []).append(relative_manifest)


def _check_optional_guidance(root: Path, issues: list[ReportQAIssue]) -> None:
    try:
        procedures = load_procedures(root).procedures
    except ObjectiveError:
        return
    for procedure in procedures:
        if procedure.technique_id is None and procedure.tactic is not None:
            issues.append(
                ReportQAIssue(
                    level="warning",
                    code="procedure-missing-attack-technique",
                    message="procedure has a tactic but no ATT&CK technique id",
                    path="procedures/procedures.json",
                    subject=procedure.id,
                )
            )


def _has_retest_guidance(finding: NormalizedFinding) -> bool:
    for key in _RETEST_GUIDANCE_KEYS:
        value = finding.provenance.get(key)
        if _has_text(value):
            return True
        if isinstance(value, list) and value:
            return True
    return finding.remediation is not None and "retest" in finding.remediation.lower()


def _has_text(value: object) -> bool:
    return isinstance(value, str) and bool(value.strip())


def _load_json_artifact(
    path: Path,
    issues: list[ReportQAIssue],
    *,
    relative_path: str | None = None,
) -> dict[str, Any] | None:
    display_path = relative_path or path.as_posix()
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        issues.append(
            ReportQAIssue(
                level="error",
                code="json-artifact-invalid",
                message=f"invalid JSON artifact: {exc.msg}",
                path=display_path,
            )
        )
        return None
    if not isinstance(payload, dict):
        issues.append(
            ReportQAIssue(
                level="error",
                code="json-artifact-not-object",
                message="JSON artifact must be an object",
                path=display_path,
            )
        )
        return None
    return payload


def _list_of_dicts(value: Any) -> list[dict[str, Any]]:
    if not isinstance(value, list):
        return []
    return [item for item in value if isinstance(item, dict)]


def _string_or_none(value: object) -> str | None:
    return value if isinstance(value, str) else None


def _sha256_bytes(data: bytes) -> str:
    import hashlib

    return hashlib.sha256(data).hexdigest()


__all__ = [
    "REPORT_QA_SCHEMA_VERSION",
    "ReportQAArtifact",
    "ReportQAIssue",
    "ReportQAResult",
    "render_report_qa_text",
    "validate_delivery",
]
