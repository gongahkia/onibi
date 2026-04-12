from __future__ import annotations

import re
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path

from pydantic import BaseModel, ConfigDict, Field

from piranesi.legal.frameworks import resolve_framework_key
from piranesi.legal.rules import RegulatoryRuleSpec, load_all_rule_specs
from piranesi.legal.rules.pci_dss import detect_payment_processing_scope
from piranesi.models import LegalAssessment, ScanResult

_SEVERITY_ORDER = ("critical", "high", "medium", "low", "informational")
_SUPPORTED_FRAMEWORKS = {"SOC2", "PCI_DSS"}
_NON_ALNUM = re.compile(r"[^a-z0-9]+")


class EvidenceFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    cwe: str
    severity: str
    file: str
    line: int
    status: str = "open"
    first_detected: str | None = None


class EvidenceScope(BaseModel):
    model_config = ConfigDict(extra="forbid")

    files_scanned: int
    languages: list[str] = Field(default_factory=list)
    project: str


class EvidenceBundle(BaseModel):
    model_config = ConfigDict(extra="forbid")

    framework: str
    control_ref: str
    control_name: str
    scan_date: str
    scan_tool: str
    scan_version: str
    scope: EvidenceScope
    findings: list[EvidenceFinding] = Field(default_factory=list)
    finding_count: int
    finding_count_by_severity: dict[str, int] = Field(default_factory=dict)
    affected_files: list[str] = Field(default_factory=list)
    remediation_status: dict[str, int] = Field(default_factory=dict)
    control_assessment: str
    evidence_narrative: str
    source_rule_ids: list[str] = Field(default_factory=list)


class _LegalArtifactEnvelope(BaseModel):
    model_config = ConfigDict(extra="forbid")

    assessments: list[LegalAssessment] = Field(default_factory=list)


@dataclass(frozen=True)
class _ControlCatalogEntry:
    framework: str
    control_ref: str
    control_name: str
    rule_ids: tuple[str, ...]
    meta_only: bool


def generate_evidence_bundles(
    *,
    scan: ScanResult,
    assessments: list[LegalAssessment],
    framework: str,
) -> list[EvidenceBundle]:
    framework_keys = _resolve_framework_selection(framework)
    payment_scope = detect_payment_processing_scope(
        Path(scan.project_root),
        files=list(scan.files_scanned),
    )
    findings_by_control = _findings_by_control(assessments, scan_date=scan.metadata.timestamp)

    bundles: list[EvidenceBundle] = []
    for control in _control_catalog(framework_keys):
        evidence_findings = list(
            findings_by_control.get((control.framework, control.control_ref), ())
        )
        in_scope = control.framework != "PCI_DSS" or payment_scope.is_payment_processing
        control_assessment = _control_assessment(
            control=control,
            in_scope=in_scope,
            finding_count=len(evidence_findings),
        )
        bundles.append(
            EvidenceBundle(
                framework=control.framework,
                control_ref=control.control_ref,
                control_name=control.control_name,
                scan_date=scan.metadata.timestamp,
                scan_tool="piranesi",
                scan_version=scan.metadata.piranesi_version,
                scope=EvidenceScope(
                    files_scanned=scan.metadata.files_parsed or len(scan.files_scanned),
                    languages=_scan_languages(scan),
                    project=Path(scan.project_root).name or scan.project_root,
                ),
                findings=evidence_findings if in_scope else [],
                finding_count=len(evidence_findings) if in_scope else 0,
                finding_count_by_severity=(
                    _severity_breakdown(evidence_findings) if in_scope else _severity_breakdown(())
                ),
                affected_files=(
                    sorted({finding.file for finding in evidence_findings}) if in_scope else []
                ),
                remediation_status=(
                    {
                        "open": len(evidence_findings),
                        "in_progress": 0,
                        "resolved": 0,
                        "suppressed": 0,
                    }
                    if in_scope
                    else {"open": 0, "in_progress": 0, "resolved": 0, "suppressed": 0}
                ),
                control_assessment=control_assessment,
                evidence_narrative=_evidence_narrative(
                    control=control,
                    findings=evidence_findings,
                    in_scope=in_scope,
                ),
                source_rule_ids=list(control.rule_ids),
            )
        )
    return bundles


def write_evidence_bundles(
    *,
    scan: ScanResult,
    assessments: list[LegalAssessment],
    framework: str,
    output_dir: Path,
) -> list[Path]:
    output_dir.mkdir(parents=True, exist_ok=True)
    bundles = generate_evidence_bundles(
        scan=scan,
        assessments=assessments,
        framework=framework,
    )
    written: list[Path] = []
    for bundle in bundles:
        path = output_dir / f"{_slug(bundle.framework)}_{_slug(bundle.control_ref)}.json"
        path.write_text(bundle.model_dump_json(indent=2), encoding="utf-8")
        written.append(path)
    return written


def load_evidence_artifacts(
    artifacts_dir: Path,
) -> tuple[ScanResult, list[LegalAssessment]]:
    scan_path = artifacts_dir / "scan.json"
    if not scan_path.exists():
        raise ValueError(f"missing scan artifact: {scan_path}")
    scan = ScanResult.model_validate_json(scan_path.read_text(encoding="utf-8"))

    legal_path = artifacts_dir / "legal.json"
    if legal_path.exists():
        legal = _LegalArtifactEnvelope.model_validate_json(legal_path.read_text(encoding="utf-8"))
        return scan, list(legal.assessments)

    report_path = artifacts_dir / "report.json"
    if report_path.exists():
        from piranesi.report.renderer import PiranesiReport

        PiranesiReport.model_validate_json(report_path.read_text(encoding="utf-8"))
        return scan, []

    raise ValueError(f"missing legal artifact: expected {legal_path} or {report_path}")


def _resolve_framework_selection(value: str) -> tuple[str, ...]:
    normalized = value.strip().lower()
    if normalized == "all":
        return ("SOC2", "PCI_DSS")
    resolved = resolve_framework_key(value)
    if resolved not in _SUPPORTED_FRAMEWORKS:
        raise ValueError(f"unsupported compliance evidence framework: {value}")
    return (resolved,)


def _control_catalog(framework_keys: tuple[str, ...]) -> list[_ControlCatalogEntry]:
    grouped: dict[tuple[str, str], list[RegulatoryRuleSpec]] = defaultdict(list)
    for spec in load_all_rule_specs():
        if spec.framework not in framework_keys:
            continue
        grouped[(spec.framework, spec.section)].append(spec)

    catalog: list[_ControlCatalogEntry] = []
    for framework, control_ref in sorted(grouped):
        specs = grouped[(framework, control_ref)]
        first = specs[0]
        catalog.append(
            _ControlCatalogEntry(
                framework=framework,
                control_ref=control_ref,
                control_name=first.control_name or control_ref,
                rule_ids=tuple(spec.rule_id for spec in specs),
                meta_only=all(spec.meta_only for spec in specs),
            )
        )
    return catalog


def _findings_by_control(
    assessments: list[LegalAssessment],
    *,
    scan_date: str,
) -> dict[tuple[str, str], tuple[EvidenceFinding, ...]]:
    grouped: dict[tuple[str, str], dict[str, EvidenceFinding]] = defaultdict(dict)
    for assessment in assessments:
        candidate = assessment.finding.finding.finding
        for obligation in assessment.obligations:
            key = (obligation.framework, obligation.section)
            grouped[key][candidate.id] = EvidenceFinding(
                id=candidate.id,
                cwe=_extract_cwe(candidate.vuln_class),
                severity=candidate.severity.lower(),
                file=candidate.sink.location.file,
                line=candidate.sink.location.line,
                status="open",
                first_detected=scan_date,
            )
    return {key: tuple(value.values()) for key, value in grouped.items()}


def _extract_cwe(vuln_class: str) -> str:
    match = re.search(r"CWE-\d+", vuln_class, re.IGNORECASE)
    return match.group(0).upper() if match is not None else vuln_class.strip().upper()


def _severity_breakdown(
    findings: tuple[EvidenceFinding, ...] | list[EvidenceFinding],
) -> dict[str, int]:
    counts = dict.fromkeys(_SEVERITY_ORDER, 0)
    for finding in findings:
        counts[finding.severity] = counts.get(finding.severity, 0) + 1
    return counts


def _control_assessment(
    *,
    control: _ControlCatalogEntry,
    in_scope: bool,
    finding_count: int,
) -> str:
    if not in_scope:
        return "not_in_scope"
    if control.meta_only:
        if control.control_ref == "Req 11.3.2":
            return "partial_evidence"
        return "pass"
    if finding_count > 0:
        return "gap_identified"
    return "pass"


def _evidence_narrative(
    *,
    control: _ControlCatalogEntry,
    findings: list[EvidenceFinding],
    in_scope: bool,
) -> str:
    label = f"{control.control_ref} ({control.control_name})"
    if not in_scope:
        return (
            f"{label} marked not in scope because payment-processing indicators were not detected."
        )
    if control.meta_only:
        if control.control_ref == "Req 11.3.2":
            return (
                f"{label} has partial evidence from the current application and dependency scans; "
                "an approved external scan remains separately required."
            )
        return f"{label} satisfied by the presence of the current scan artifact."
    if not findings:
        return f"No findings mapped to {label}. Current evidence supports control effectiveness."
    files = ", ".join(sorted({finding.file for finding in findings}))
    return f"{len(findings)} finding(s) mapped to {label}. Affected files: {files}."


def _scan_languages(scan: ScanResult) -> list[str]:
    labels: list[str] = []
    suffix_map = {
        ".go": "Go",
        ".java": "Java",
        ".js": "JavaScript",
        ".jsx": "JavaScript",
        ".py": "Python",
        ".ts": "TypeScript",
        ".tsx": "TypeScript",
    }
    for path_str in scan.files_scanned:
        label = suffix_map.get(Path(path_str).suffix.lower())
        if label is not None and label not in labels:
            labels.append(label)
    return labels


def _slug(value: str) -> str:
    lowered = value.strip().lower()
    collapsed = _NON_ALNUM.sub("_", lowered)
    return collapsed.strip("_")


__all__ = [
    "EvidenceBundle",
    "EvidenceFinding",
    "EvidenceScope",
    "generate_evidence_bundles",
    "load_evidence_artifacts",
    "write_evidence_bundles",
]
