from __future__ import annotations

import re
from dataclasses import dataclass, field
from datetime import UTC, datetime
from hashlib import sha256
from pathlib import Path

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi import __version__
from piranesi.models import CandidateFinding, ConfirmedFinding, ReachabilityResult, SourceLocation
from piranesi.report.cwe import cwe_title, extract_cwe_id

_WHITESPACE_RE = re.compile(r"\s+")


class Finding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    stable_fingerprint: str
    vuln_class: str
    severity: str
    confidence: float
    source_location: SourceLocation
    sink_location: SourceLocation
    source_type: str
    source_parameter: str | None = None
    sink_type: str
    sink_api: str
    taint_path_length: int
    taint_operations: list[str] = Field(default_factory=list)

    @classmethod
    def from_candidate(cls, candidate: CandidateFinding) -> Finding:
        return cls(
            id=candidate.id,
            stable_fingerprint=stable_fingerprint(candidate),
            vuln_class=candidate.vuln_class,
            severity=candidate.severity,
            confidence=candidate.confidence,
            source_location=candidate.source.location,
            sink_location=candidate.sink.location,
            source_type=candidate.source.source_type,
            source_parameter=candidate.source.parameter_name,
            sink_type=candidate.sink.sink_type,
            sink_api=candidate.sink.api_name,
            taint_path_length=len(candidate.taint_path),
            taint_operations=[step.operation for step in candidate.taint_path],
        )


class BaselineArtifact(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = 1
    created_at: str
    piranesi_version: str
    source_path: str
    findings: list[Finding] = Field(default_factory=list)


class DetectArtifact(BaseModel):
    model_config = ConfigDict(extra="forbid")

    findings: list[CandidateFinding] = Field(default_factory=list)
    reachability: ReachabilityResult | None = None
    suppression_lifecycle: dict[str, object] | None = None


class VerifyArtifact(BaseModel):
    model_config = ConfigDict(extra="forbid")

    findings: list[ConfirmedFinding] = Field(default_factory=list)
    attempts: list[object] = Field(default_factory=list)


class ChangedFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    stable_fingerprint: str
    baseline: Finding
    current: Finding
    changed_fields: list[str] = Field(default_factory=list)


@dataclass(frozen=True, slots=True)
class DiffResult:
    new: list[Finding]
    fixed: list[Finding]
    unchanged: list[Finding]
    changed: list[ChangedFinding] = field(default_factory=list)

    @property
    def existing(self) -> list[Finding]:
        return self.unchanged


def load_findings(artifact_path: Path) -> list[Finding]:
    resolved_path = _resolve_artifact_path(artifact_path)
    try:
        payload = resolved_path.read_text(encoding="utf-8")
    except OSError as exc:
        raise ValueError(f"failed to read findings artifact {resolved_path}: {exc}") from exc

    baseline_artifact = _try_load_model(payload, BaselineArtifact)
    if baseline_artifact is not None:
        return list(baseline_artifact.findings)

    detect_artifact = _try_load_model(payload, DetectArtifact)
    if detect_artifact is not None:
        return [Finding.from_candidate(candidate) for candidate in detect_artifact.findings]

    verify_artifact = _try_load_model(payload, VerifyArtifact)
    if verify_artifact is not None:
        return [
            Finding.from_candidate(confirmed.finding.finding)
            for confirmed in verify_artifact.findings
        ]

    raise ValueError(
        "unsupported findings artifact. Expected a baseline JSON file, "
        "detect.json, verify.json, or a results directory containing one of those files."
    )


def build_baseline_artifact(results_path: Path) -> BaselineArtifact:
    return BaselineArtifact(
        created_at=_utc_now(),
        piranesi_version=__version__,
        source_path=str(results_path),
        findings=load_findings(results_path),
    )


def diff_findings(baseline: list[Finding], current: list[Finding]) -> DiffResult:
    baseline_by_fingerprint = _findings_by_fingerprint(baseline)
    current_by_fingerprint = _findings_by_fingerprint(current)

    new: list[Finding] = []
    fixed: list[Finding] = []
    unchanged: list[Finding] = []
    changed: list[ChangedFinding] = []

    for fingerprint in sorted(baseline_by_fingerprint.keys() | current_by_fingerprint.keys()):
        baseline_bucket = list(baseline_by_fingerprint.get(fingerprint, []))
        current_bucket = list(current_by_fingerprint.get(fingerprint, []))
        paired = min(len(baseline_bucket), len(current_bucket))

        for index in range(paired):
            baseline_finding = baseline_bucket[index]
            current_finding = current_bucket[index]
            changed_fields = _changed_fields(baseline_finding, current_finding)
            if changed_fields:
                changed.append(
                    ChangedFinding(
                        stable_fingerprint=fingerprint,
                        baseline=baseline_finding,
                        current=current_finding,
                        changed_fields=changed_fields,
                    )
                )
            else:
                unchanged.append(current_finding)

        if len(current_bucket) > paired:
            new.extend(current_bucket[paired:])
        if len(baseline_bucket) > paired:
            fixed.extend(baseline_bucket[paired:])

    return DiffResult(
        new=sorted(new, key=_finding_sort_key),
        fixed=sorted(fixed, key=_finding_sort_key),
        unchanged=sorted(unchanged, key=_finding_sort_key),
        changed=sorted(changed, key=_changed_sort_key),
    )


def render_diff(diff_result: DiffResult) -> str:
    sections = [
        _render_section("NEW", "+", diff_result.new),
        _render_changed_section(diff_result.changed),
        _render_section("FIXED", "-", diff_result.fixed),
        _render_section("EXISTING", "=", diff_result.existing),
        (
            "Summary: "
            f"{len(diff_result.new)} new, "
            f"{len(diff_result.changed)} changed, "
            f"{len(diff_result.fixed)} fixed, "
            f"{len(diff_result.existing)} existing"
        ),
    ]
    return "\n\n".join(sections)


def render_diff_markdown(diff_result: DiffResult) -> str:
    lines = [
        "## Baseline Diff",
        "",
        (
            f"- New: **{len(diff_result.new)}**"
            f" | Changed: **{len(diff_result.changed)}**"
            f" | Fixed: **{len(diff_result.fixed)}**"
            f" | Existing: **{len(diff_result.existing)}**"
        ),
        "",
        "### New Findings",
        *_render_markdown_findings(diff_result.new),
        "",
        "### Changed Findings",
        *_render_markdown_changed(diff_result.changed),
        "",
        "### Fixed Findings",
        *_render_markdown_findings(diff_result.fixed),
    ]
    return "\n".join(lines).rstrip() + "\n"


def diff_result_payload(diff_result: DiffResult) -> dict[str, object]:
    return {
        "summary": {
            "new": len(diff_result.new),
            "changed": len(diff_result.changed),
            "fixed": len(diff_result.fixed),
            "existing": len(diff_result.existing),
            "new_by_severity": _severity_counts(diff_result.new),
        },
        "new": [finding.model_dump(mode="json") for finding in diff_result.new],
        "changed": [
            {
                "stable_fingerprint": finding.stable_fingerprint,
                "changed_fields": list(finding.changed_fields),
                "baseline": finding.baseline.model_dump(mode="json"),
                "current": finding.current.model_dump(mode="json"),
            }
            for finding in diff_result.changed
        ],
        "fixed": [finding.model_dump(mode="json") for finding in diff_result.fixed],
        "existing": [finding.model_dump(mode="json") for finding in diff_result.existing],
    }


def new_findings_at_or_above(diff_result: DiffResult, *, minimum_severity: str) -> list[Finding]:
    threshold = _severity_rank(minimum_severity)
    return [finding for finding in diff_result.new if _severity_rank(finding.severity) >= threshold]


def stable_fingerprint(finding: CandidateFinding) -> str:
    material = "|".join(
        [
            _normalize_text(finding.vuln_class),
            _normalize_path(finding.source.location.file),
            _normalize_text(finding.source.parameter_name or finding.source.source_type),
            _normalize_text(finding.source.location.snippet),
            _normalize_path(finding.sink.location.file),
            _normalize_text(finding.sink.api_name or finding.sink.sink_type),
            _normalize_text(finding.sink.location.snippet),
            str(len(finding.taint_path)),
            ",".join(_normalize_text(step.operation) for step in finding.taint_path),
        ]
    )
    return sha256(material.encode("utf-8")).hexdigest()[:16]


def _try_load_model[T: BaseModel](payload: str, model_type: type[T]) -> T | None:
    try:
        return model_type.model_validate_json(payload)
    except ValidationError:
        return None


def _resolve_artifact_path(artifact_path: Path) -> Path:
    if artifact_path.is_dir():
        for candidate_name in ("detect.json", "verify.json"):
            candidate_path = artifact_path / candidate_name
            if candidate_path.exists():
                return candidate_path
        raise ValueError(
            f"no findings artifact found in {artifact_path}. Expected detect.json or verify.json."
        )

    if artifact_path.exists():
        return artifact_path

    raise ValueError(f"findings artifact {artifact_path} does not exist")


def _findings_by_fingerprint(findings: list[Finding]) -> dict[str, list[Finding]]:
    indexed: dict[str, list[Finding]] = {}
    for finding in findings:
        indexed.setdefault(finding.stable_fingerprint, []).append(finding)
    for bucket in indexed.values():
        bucket.sort(key=_finding_sort_key)
    return indexed


def _finding_sort_key(finding: Finding) -> tuple[str, str, int, int, str]:
    return (
        extract_cwe_id(finding.vuln_class),
        finding.sink_location.file,
        finding.sink_location.line,
        finding.sink_location.column,
        finding.id,
    )


def _render_section(title: str, marker: str, findings: list[Finding]) -> str:
    lines = [f"{title} ({len(findings)}):"]
    if not findings:
        lines.append("  (none)")
        return "\n".join(lines)

    lines.extend(f"  {marker} {_finding_summary(finding)}" for finding in findings)
    return "\n".join(lines)


def _render_changed_section(findings: list[ChangedFinding]) -> str:
    lines = [f"CHANGED ({len(findings)}):"]
    if not findings:
        lines.append("  (none)")
        return "\n".join(lines)
    for finding in findings:
        details = ", ".join(finding.changed_fields)
        lines.append(f"  ~ {_finding_summary(finding.current)} [{details}]")
    return "\n".join(lines)


def _finding_summary(finding: Finding) -> str:
    cwe = extract_cwe_id(finding.vuln_class)
    title = cwe_title(cwe, fallback=finding.vuln_class)
    sink = finding.sink_api or finding.sink_type
    location = f"{_display_path(finding.sink_location.file)}:{finding.sink_location.line}"
    return f"{cwe} {title} in {location} -> {sink}"


def _render_markdown_findings(findings: list[Finding]) -> list[str]:
    if not findings:
        return ["- None"]
    return [f"- {_finding_summary(finding)}" for finding in findings]


def _render_markdown_changed(findings: list[ChangedFinding]) -> list[str]:
    if not findings:
        return ["- None"]
    lines: list[str] = []
    for finding in findings:
        details = ", ".join(finding.changed_fields)
        lines.append(f"- {_finding_summary(finding.current)} ({details})")
    return lines


def _changed_fields(baseline: Finding, current: Finding) -> list[str]:
    changes: list[str] = []
    if baseline.severity.lower() != current.severity.lower():
        changes.append(f"severity:{baseline.severity.lower()}->{current.severity.lower()}")
    if round(baseline.confidence, 4) != round(current.confidence, 4):
        changes.append(f"confidence:{baseline.confidence:.2f}->{current.confidence:.2f}")
    return changes


def _changed_sort_key(finding: ChangedFinding) -> tuple[str, str, int, int, str]:
    return _finding_sort_key(finding.current)


def _severity_counts(findings: list[Finding]) -> dict[str, int]:
    counts: dict[str, int] = {}
    for finding in findings:
        key = finding.severity.lower()
        counts[key] = counts.get(key, 0) + 1
    return dict(sorted(counts.items(), key=lambda item: _severity_rank(item[0]), reverse=True))


def _severity_rank(severity: str) -> int:
    normalized = severity.lower()
    if normalized == "low":
        return 0
    if normalized == "medium":
        return 1
    if normalized == "high":
        return 2
    if normalized == "critical":
        return 3
    return -1


def _normalize_text(value: str) -> str:
    collapsed = _WHITESPACE_RE.sub(" ", value.strip())
    return collapsed.lower()


def _normalize_path(value: str) -> str:
    parts = Path(value).parts
    if not parts:
        return value
    trimmed = parts[-4:] if len(parts) > 4 else parts
    return "/".join(trimmed).lower()


def _display_path(value: str) -> str:
    parts = Path(value).parts
    if not parts:
        return value
    trimmed = parts[-4:] if len(parts) > 4 else parts
    return "/".join(trimmed)


def _utc_now() -> str:
    return datetime.now(UTC).isoformat().replace("+00:00", "Z")
