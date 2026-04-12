from __future__ import annotations

import re
from dataclasses import dataclass
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


class VerifyArtifact(BaseModel):
    model_config = ConfigDict(extra="forbid")

    findings: list[ConfirmedFinding] = Field(default_factory=list)


@dataclass(frozen=True, slots=True)
class DiffResult:
    new: list[Finding]
    fixed: list[Finding]
    unchanged: list[Finding]


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

    unchanged_keys = baseline_by_fingerprint.keys() & current_by_fingerprint.keys()
    new_keys = current_by_fingerprint.keys() - baseline_by_fingerprint.keys()
    fixed_keys = baseline_by_fingerprint.keys() - current_by_fingerprint.keys()

    return DiffResult(
        new=sorted((current_by_fingerprint[key] for key in new_keys), key=_finding_sort_key),
        fixed=sorted((baseline_by_fingerprint[key] for key in fixed_keys), key=_finding_sort_key),
        unchanged=sorted(
            (current_by_fingerprint[key] for key in unchanged_keys),
            key=_finding_sort_key,
        ),
    )


def render_diff(diff_result: DiffResult) -> str:
    sections = [
        _render_section("NEW", "+", diff_result.new),
        _render_section("FIXED", "-", diff_result.fixed),
        _render_section("UNCHANGED", "=", diff_result.unchanged),
        (
            "Summary: "
            f"{len(diff_result.new)} new, "
            f"{len(diff_result.fixed)} fixed, "
            f"{len(diff_result.unchanged)} unchanged"
        ),
    ]
    return "\n\n".join(sections)


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


def _findings_by_fingerprint(findings: list[Finding]) -> dict[str, Finding]:
    indexed: dict[str, Finding] = {}
    for finding in findings:
        indexed.setdefault(finding.stable_fingerprint, finding)
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


def _finding_summary(finding: Finding) -> str:
    cwe = extract_cwe_id(finding.vuln_class)
    title = cwe_title(cwe, fallback=finding.vuln_class)
    sink = finding.sink_api or finding.sink_type
    location = f"{_display_path(finding.sink_location.file)}:{finding.sink_location.line}"
    return f"{cwe} {title} in {location} -> {sink}"


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
