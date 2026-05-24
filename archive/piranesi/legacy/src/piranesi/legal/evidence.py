from __future__ import annotations

import hashlib
import json
import re
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.legal.frameworks import FRAMEWORK_BY_KEY, resolve_framework_key
from piranesi.legal.rules import RegulatoryRuleSpec, discover_rule_files, load_all_rule_specs
from piranesi.legal.rules.pci_dss import detect_payment_processing_scope
from piranesi.models import ComplianceMappingMetadata, LegalAssessment, ScanResult

_SEVERITY_ORDER = ("critical", "high", "medium", "low", "informational")
_SUPPORTED_FRAMEWORKS = {"SOC2", "PCI_DSS"}
_NON_ALNUM = re.compile(r"[^a-z0-9]+")
_SENSITIVE_KEY_PATTERN = re.compile(
    r"(api[_-]?key|authorization|cookie|credential|password|secret|session|token)",
    re.IGNORECASE,
)
_SENSITIVE_ASSIGNMENT_PATTERN = re.compile(
    r"""(?ix)
    (?P<prefix>
        \b[\w.-]*
        (?:api[_-]?key|access[_-]?token|authorization|cookie|credential|password|passwd|secret|session(?:id)?|token)
        [\w.-]*\b
        \s*[:=]\s*
        ['"]?
    )
    (?P<value>[^'"\s,;]+)
    (?P<suffix>['"]?)
    """
)
_SENSITIVE_VALUE_PATTERN = re.compile(
    r"(?i)(bearer\s+[a-z0-9._\-]+|sk-[a-z0-9_-]{6,}|gh[pousr]_[A-Za-z0-9]{20,}|AKIA[0-9A-Z]{16})",
    re.IGNORECASE,
)


class EvidenceFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    cwe: str
    severity: str
    file: str
    line: int
    status: str = "open"
    first_detected: str | None = None
    ownership: dict[str, str | None] | None = None


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
    control_owner: str | None = None
    mapping_metadata: ComplianceMappingMetadata | None = None
    compliance_support_scope: str = (
        "supporting_evidence_only_not_a_certification_or_formal_audit_opinion"
    )


class BundleManifestEntry(BaseModel):
    model_config = ConfigDict(extra="forbid")

    path: str
    sha256: str
    size_bytes: int


class ComplianceEvidenceBundleManifest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = 1
    framework_selection: list[str] = Field(default_factory=list)
    scan_timestamp: str
    generated_at: str
    redacted: bool = True
    project: str
    files: list[BundleManifestEntry] = Field(default_factory=list)
    metadata_path: str = "metadata.json"
    checksum_manifest_path: str = "manifest.json"


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
    mapping_metadata: ComplianceMappingMetadata


def generate_evidence_bundles(
    *,
    scan: ScanResult,
    assessments: list[LegalAssessment],
    framework: str,
    control_owner_lookup: dict[tuple[str, str], str] | None = None,
    finding_ownership_lookup: dict[str, dict[str, str | None]] | None = None,
) -> list[EvidenceBundle]:
    framework_keys = _resolve_framework_selection(framework)
    payment_scope = detect_payment_processing_scope(
        Path(scan.project_root),
        files=list(scan.files_scanned),
    )
    findings_by_control = _findings_by_control(
        assessments,
        scan_date=scan.metadata.timestamp,
        finding_ownership_lookup=finding_ownership_lookup or {},
    )

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
                control_owner=(control_owner_lookup or {}).get(
                    (control.framework, control.control_ref)
                ),
                mapping_metadata=control.mapping_metadata,
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


def build_compliance_evidence_bundle(
    *,
    artifacts_dir: Path,
    framework: str,
    output_dir: Path,
    redact: bool = True,
    config_path: Path | None = None,
) -> ComplianceEvidenceBundleManifest:
    scan, assessments = load_evidence_artifacts(artifacts_dir)
    finding_ownership_lookup, control_owner_lookup, ownership_context = _ownership_metadata(
        artifacts_dir=artifacts_dir
    )
    bundles = generate_evidence_bundles(
        scan=scan,
        assessments=assessments,
        framework=framework,
        control_owner_lookup=control_owner_lookup,
        finding_ownership_lookup=finding_ownership_lookup,
    )
    framework_keys = _resolve_framework_selection(framework)
    output_dir.mkdir(parents=True, exist_ok=True)
    written_paths: list[Path] = []

    artifacts_output = output_dir / "artifacts"
    controls_output = output_dir / "controls"
    artifacts_output.mkdir(parents=True, exist_ok=True)
    controls_output.mkdir(parents=True, exist_ok=True)

    source_files = _bundle_source_files(artifacts_dir=artifacts_dir, config_path=config_path)
    for source_path in source_files:
        destination = artifacts_output / source_path.name
        _write_bundle_file(source_path, destination, redact=redact)
        written_paths.append(destination)

    for bundle in bundles:
        control_path = (
            controls_output / f"{_slug(bundle.framework)}_{_slug(bundle.control_ref)}.json"
        )
        payload = bundle.model_dump(mode="json")
        if redact:
            payload = _redact_payload(payload)
        _write_json(control_path, payload)
        written_paths.append(control_path)

    metadata_path = output_dir / "metadata.json"
    metadata_payload = _bundle_metadata_payload(
        scan=scan,
        framework_keys=framework_keys,
        bundles=bundles,
        included_sources=[path.name for path in source_files],
        redacted=redact,
        ownership_context=ownership_context,
    )
    _write_json(metadata_path, metadata_payload)
    written_paths.append(metadata_path)

    manifest_entries = [
        BundleManifestEntry(
            path=path.relative_to(output_dir).as_posix(),
            sha256=_sha256_file(path),
            size_bytes=path.stat().st_size,
        )
        for path in sorted(written_paths)
    ]
    manifest = ComplianceEvidenceBundleManifest(
        framework_selection=list(framework_keys),
        scan_timestamp=scan.metadata.timestamp,
        generated_at=scan.metadata.timestamp,
        redacted=redact,
        project=Path(scan.project_root).name or scan.project_root,
        files=manifest_entries,
    )
    (output_dir / manifest.checksum_manifest_path).write_text(
        manifest.model_dump_json(indent=2),
        encoding="utf-8",
    )
    return manifest


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


def _bundle_source_files(*, artifacts_dir: Path, config_path: Path | None) -> list[Path]:
    candidates = [
        artifacts_dir / "scan.json",
        artifacts_dir / "detect.json",
        artifacts_dir / "verify.json",
        artifacts_dir / "legal.json",
        artifacts_dir / "report.json",
        artifacts_dir / "report.md",
    ]
    discovered = [path for path in candidates if path.exists()]
    explicit_config = config_path
    if explicit_config is None:
        local_config = artifacts_dir.parent / "piranesi.toml"
        explicit_config = local_config if local_config.exists() else None
    if explicit_config is not None and explicit_config.exists():
        discovered.append(explicit_config.resolve(strict=False))
    return discovered


def _write_bundle_file(source_path: Path, destination: Path, *, redact: bool) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    if source_path.suffix.lower() == ".json":
        payload = json.loads(source_path.read_text(encoding="utf-8"))
        if redact:
            payload = _redact_payload(payload)
        _write_json(destination, payload)
        return
    text = source_path.read_text(encoding="utf-8")
    destination.write_text(_redact_text(text) if redact else text, encoding="utf-8")


def _write_json(path: Path, payload: object) -> None:
    path.write_text(
        json.dumps(payload, indent=2, sort_keys=True),
        encoding="utf-8",
    )


def _ownership_metadata(
    *,
    artifacts_dir: Path,
) -> tuple[
    dict[str, dict[str, str | None]],
    dict[tuple[str, str], str],
    dict[str, Any] | None,
]:
    report_path = artifacts_dir / "report.json"
    if not report_path.exists():
        return {}, {}, None
    try:
        from piranesi.report.renderer import PiranesiReport

        report = PiranesiReport.model_validate_json(report_path.read_text(encoding="utf-8"))
    except (OSError, ValidationError, json.JSONDecodeError, ValueError):
        return {}, {}, None
    finding_ownership_lookup = {
        finding.finding_id: {
            "service": finding.ownership.service,
            "system": finding.ownership.system,
            "team": finding.ownership.team,
            "owner": finding.ownership.owner,
            "repository": finding.ownership.repository,
            "environment": finding.ownership.environment,
            "control_owner": finding.ownership.control_owner,
            "package": finding.ownership.package,
            "source_path": finding.ownership.source_path,
            "sink_path": finding.ownership.sink_path,
        }
        for finding in report.findings
    }
    control_owner_lookup = {
        (mapping.framework, mapping.control): mapping.owner
        for mapping in report.ownership_context.control_mappings
    }
    ownership_context = report.ownership_context.model_dump(mode="json")
    return finding_ownership_lookup, control_owner_lookup, ownership_context


def _bundle_metadata_payload(
    *,
    scan: ScanResult,
    framework_keys: tuple[str, ...],
    bundles: list[EvidenceBundle],
    included_sources: list[str],
    redacted: bool,
    ownership_context: dict[str, Any] | None,
) -> dict[str, Any]:
    return {
        "bundle_kind": "piranesi-compliance-evidence",
        "bundle_schema_version": 1,
        "project": Path(scan.project_root).name or scan.project_root,
        "scan": {
            "timestamp": scan.metadata.timestamp,
            "tool": "piranesi",
            "version": scan.metadata.piranesi_version,
            "files_scanned": scan.metadata.files_parsed or len(scan.files_scanned),
        },
        "framework_selection": list(framework_keys),
        "framework_metadata": [
            {
                "key": framework.key,
                "name": framework.long_label,
                "version": framework.version,
                "mapping_last_reviewed": framework.mapping_last_reviewed,
                "mapping_reviewer": framework.mapping_reviewer,
                "mapping_source": framework.mapping_source,
                "mapping_confidence": framework.mapping_confidence,
            }
            for framework in FRAMEWORK_BY_KEY.values()
            if framework.key in framework_keys
        ],
        "control_bundle_count": len(bundles),
        "source_artifacts": sorted(included_sources),
        "rule_catalog": _rule_catalog_metadata(),
        "query_spec_summary": _query_spec_summary(scan),
        "redaction": {
            "enabled": redacted,
            "sensitive_key_pattern": _SENSITIVE_KEY_PATTERN.pattern,
            "sensitive_value_pattern": _SENSITIVE_VALUE_PATTERN.pattern,
        },
        "ownership": ownership_context,
        "compliance_claim_boundary": (
            "Bundle content is supporting technical evidence only. "
            "It does not certify compliance or replace legal/audit review."
        ),
    }


def _rule_catalog_metadata() -> dict[str, object]:
    entries: list[dict[str, object]] = []
    for path in discover_rule_files():
        if not path.exists():
            continue
        entries.append(
            {
                "file": path.name,
                "sha256": _sha256_file(path),
                "size_bytes": path.stat().st_size,
            }
        )
    return {
        "rule_file_count": len(entries),
        "rule_files": sorted(entries, key=lambda item: str(item["file"])),
    }


def _query_spec_summary(scan: ScanResult) -> dict[str, object] | None:
    quality = scan.query_quality
    if quality is None:
        return None
    return {
        "loaded_source_specs": quality.loaded_source_specs,
        "loaded_sink_specs": quality.loaded_sink_specs,
        "matched_source_specs": quality.matched_source_specs,
        "matched_sink_specs": quality.matched_sink_specs,
    }


def _redact_payload(value: Any) -> Any:
    if isinstance(value, dict):
        redacted: dict[str, Any] = {}
        for key, nested in value.items():
            key_str = str(key)
            if _SENSITIVE_KEY_PATTERN.search(key_str):
                redacted[key_str] = "[REDACTED]"
            else:
                redacted[key_str] = _redact_payload(nested)
        return redacted
    if isinstance(value, list):
        return [_redact_payload(item) for item in value]
    if isinstance(value, str):
        return _redact_text(value)
    return value


def _redact_text(value: str) -> str:
    assigned_redacted = _SENSITIVE_ASSIGNMENT_PATTERN.sub(
        r"\g<prefix>[REDACTED]\g<suffix>",
        value,
    )
    return _SENSITIVE_VALUE_PATTERN.sub("[REDACTED]", assigned_redacted)


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


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
                mapping_metadata=_control_mapping_metadata(
                    framework_key=framework,
                    control_ref=control_ref,
                    specs=specs,
                ),
            )
        )
    return catalog


def _control_mapping_metadata(
    *,
    framework_key: str,
    control_ref: str,
    specs: list[RegulatoryRuleSpec],
) -> ComplianceMappingMetadata:
    framework = FRAMEWORK_BY_KEY.get(framework_key)
    primary = specs[0]
    rule_ids = ", ".join(spec.rule_id for spec in specs)
    rationale_bits: list[str] = []
    if any(spec.vuln_classes for spec in specs):
        rationale_bits.append("vulnerability-class matching")
    if any(spec.data_categories for spec in specs):
        rationale_bits.append("data-category matching")
    if any(spec.requires_boolean_facts or spec.requires_any_boolean_facts for spec in specs):
        rationale_bits.append("context guards")
    if all(spec.meta_only for spec in specs):
        rationale_bits.append("meta/control evidence rules")
    rationale = ", ".join(rationale_bits) if rationale_bits else "deterministic control mapping"
    confidence = _mapping_confidence(specs=specs, framework_key=framework_key)
    return ComplianceMappingMetadata(
        framework_name=(
            framework.long_label if framework is not None else primary.framework.replace("_", " ")
        ),
        framework_version=None if framework is None else framework.version,
        control_id=control_ref,
        mapping_rationale=(
            f"Control {control_ref} mapped via {len(specs)} rule(s) ({rule_ids}) using {rationale}."
        ),
        last_reviewed=None if framework is None else framework.mapping_last_reviewed,
        reviewer=None if framework is None else framework.mapping_reviewer,
        source=(
            f"{framework.mapping_source}#{rule_ids.replace(', ', ',')}"
            if framework is not None
            else f"rules:{rule_ids.replace(', ', ',')}"
        ),
        confidence=confidence,
    )


def _mapping_confidence(*, specs: list[RegulatoryRuleSpec], framework_key: str) -> float:
    framework = FRAMEWORK_BY_KEY.get(framework_key)
    score = 0.7 if framework is None else framework.mapping_confidence
    if all(spec.meta_only for spec in specs):
        score -= 0.2
    if any(spec.vuln_classes for spec in specs):
        score += 0.08
    if any(spec.data_categories for spec in specs):
        score += 0.05
    if any(spec.requires_boolean_facts or spec.requires_any_boolean_facts for spec in specs):
        score += 0.04
    return round(max(0.0, min(score, 1.0)), 2)


def _findings_by_control(
    assessments: list[LegalAssessment],
    *,
    scan_date: str,
    finding_ownership_lookup: dict[str, dict[str, str | None]],
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
                ownership=finding_ownership_lookup.get(candidate.id),
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
            f"{label} marked not in scope because payment-processing indicators were not detected. "
            "This artifact is compliance support evidence only."
        )
    if control.meta_only:
        if control.control_ref == "Req 11.3.2":
            return (
                f"{label} has partial evidence from the current application and dependency scans; "
                "an approved external scan remains separately required. "
                "This does not certify PCI-DSS control effectiveness."
            )
        return (
            f"{label} supported by the presence of the current scan artifact. "
            "This is not a certification statement."
        )
    if not findings:
        return (
            f"No findings mapped to {label}. Current scan output provides supporting evidence only "
            "and does not certify compliance."
        )
    files = ", ".join(sorted({finding.file for finding in findings}))
    return (
        f"{len(findings)} finding(s) mapped to {label}. Affected files: {files}. "
        "Treat this as audit support evidence, not a formal compliance determination."
    )


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
    "BundleManifestEntry",
    "ComplianceEvidenceBundleManifest",
    "EvidenceBundle",
    "EvidenceFinding",
    "EvidenceScope",
    "build_compliance_evidence_bundle",
    "generate_evidence_bundles",
    "load_evidence_artifacts",
    "write_evidence_bundles",
]
