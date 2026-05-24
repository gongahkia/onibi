from __future__ import annotations

import json
import re
import tomllib
from datetime import UTC, date, datetime
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, HttpUrl, ValidationError, model_validator

from piranesi.host.analyze import analyze_snapshot
from piranesi.host.eval import HostGroundTruth, load_host_ground_truth
from piranesi.host.ingest import load_host_input
from piranesi.host.models import (
    EvidenceItem,
    HostFinding,
    HostRiskScore,
    HostSnapshot,
    Severity,
    host_finding_id,
)

RuleOperator = Literal["equals", "not_equals", "contains", "exists", "in"]
CommunityValidationStatus = Literal["ok", "error"]

_RULE_ID_RE = re.compile(r"^community\.[a-z0-9][a-z0-9_.-]{2,120}$")
_EVIDENCE_PATH_RE = re.compile(r"^[A-Za-z0-9_.-]+$")
_UNSAFE_KEYS = {
    "command",
    "commands",
    "exec",
    "eval",
    "import",
    "module",
    "python",
    "script",
    "shell",
    "subprocess",
}


class HostCommunityError(RuntimeError):
    """Raised when a community host contribution is invalid or unsafe."""


class HostRuleDefinition(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    title: str = Field(min_length=6, max_length=180)
    category: str = Field(min_length=2, max_length=64)
    severity: Severity
    confidence: float = Field(default=0.85, ge=0.1, le=1.0)
    platform_support: list[str] = Field(default_factory=list)
    control_refs: list[str] = Field(default_factory=list)
    documentation_url: HttpUrl | None = None

    @model_validator(mode="after")
    def _valid_id(self) -> HostRuleDefinition:
        if not _RULE_ID_RE.match(self.id):
            raise ValueError("rule id must start with community. and use lowercase safe tokens")
        return self


class HostRuleMatch(BaseModel):
    model_config = ConfigDict(extra="forbid")

    evidence: str
    equals: str | int | float | bool | None = None
    not_equals: str | int | float | bool | None = None
    contains: str | int | float | bool | None = None
    exists: bool | None = None
    in_values: list[str | int | float | bool] = Field(default_factory=list, alias="in")
    case_sensitive: bool = False

    @model_validator(mode="after")
    def _valid_match(self) -> HostRuleMatch:
        if not _EVIDENCE_PATH_RE.match(self.evidence):
            raise ValueError(
                "evidence path may only contain letters, numbers, dots, dashes, and underscores"
            )
        operators = [
            self.equals is not None,
            self.not_equals is not None,
            self.contains is not None,
            self.exists is not None,
            bool(self.in_values),
        ]
        if sum(1 for item in operators if item) != 1:
            raise ValueError("match must specify exactly one operator")
        return self


class HostRuleRemediation(BaseModel):
    model_config = ConfigDict(extra="forbid")

    text: str = Field(min_length=12, max_length=800)
    verification: str | None = Field(default=None, max_length=400)


class HostRuleMetadata(BaseModel):
    model_config = ConfigDict(extra="forbid")

    maintainer: str = Field(default="community", min_length=2, max_length=120)
    fixture: str | None = None
    expected_finding_ids: list[str] = Field(default_factory=list)
    false_positive_notes: str | None = Field(default=None, max_length=800)
    mapping_confidence: float | None = Field(default=None, ge=0.0, le=1.0)
    last_validation_date: date | None = None
    tags: list[str] = Field(default_factory=list)


class HostRulePack(BaseModel):
    model_config = ConfigDict(extra="forbid", populate_by_name=True)

    rule: HostRuleDefinition
    match: list[HostRuleMatch] = Field(min_length=1)
    remediation: HostRuleRemediation
    metadata: HostRuleMetadata = Field(default_factory=HostRuleMetadata)


class HostRuleTestResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    rule_path: str
    fixture_path: str
    target: str
    finding_count: int
    findings: list[HostFinding] = Field(default_factory=list)
    expected_finding_ids: list[str] = Field(default_factory=list)
    missing_expected_finding_ids: list[str] = Field(default_factory=list)
    passed: bool


class HostRuleTestAllResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    rules_root: str
    tested: int = 0
    passed: int = 0
    failed: int = 0
    skipped: int = 0
    results: list[HostRuleTestResult] = Field(default_factory=list)
    errors: list[str] = Field(default_factory=list)


class HostFixtureValidationResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    fixture_path: str
    status: CommunityValidationStatus
    target: str | None = None
    has_ground_truth: bool = False
    expected_findings: int = 0
    expected_absent: int = 0
    evidence_inventory: dict[str, int] = Field(default_factory=dict)
    finding_count: int = 0
    errors: list[str] = Field(default_factory=list)


class HostBenchmarkSubmission(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: Literal[1] = 1
    fixture: str
    generated_at: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    target: str
    platform_family: str = "unknown"
    evidence_inventory: dict[str, int] = Field(default_factory=dict)
    expected_findings: int = 0
    expected_absent: int = 0
    maintainer: str = "community"
    notes: list[str] = Field(default_factory=list)


def load_host_rule_pack(path: str | Path) -> HostRulePack:
    rule_path = Path(path).expanduser().resolve(strict=False)
    try:
        payload = tomllib.loads(rule_path.read_text(encoding="utf-8"))
    except (OSError, tomllib.TOMLDecodeError) as exc:
        raise HostCommunityError(f"invalid host rule pack {rule_path}: {exc}") from exc
    if not isinstance(payload, dict):
        raise HostCommunityError(f"invalid host rule pack {rule_path}: expected TOML table")
    unsafe = _unsafe_key_path(payload)
    if unsafe is not None:
        raise HostCommunityError(f"unsafe host rule pack key `{unsafe}` is not allowed")
    try:
        return HostRulePack.model_validate(payload)
    except ValidationError as exc:
        raise HostCommunityError(f"invalid host rule pack {rule_path}: {exc}") from exc


def evaluate_host_rule_pack(rule_pack: HostRulePack, snapshot: HostSnapshot) -> list[HostFinding]:
    matched_evidence: list[EvidenceItem] = []
    for match in rule_pack.match:
        values = _resolve_evidence_values(snapshot, match.evidence)
        selected = [value for value in values if _match_value(value, match)]
        if not selected:
            return []
        matched_evidence.append(
            EvidenceItem(
                source="community_rule",
                key=match.evidence,
                value=", ".join(_stringify(value) for value in selected[:5]),
            )
        )
    finding_id = host_finding_id(
        "community_rule",
        rule_pack.rule.id,
        snapshot.identity.hostname,
    )
    return [
        HostFinding(
            id=finding_id,
            rule_id=rule_pack.rule.id,
            instance_key=f"community:{rule_pack.rule.id}",
            title=rule_pack.rule.title,
            category=rule_pack.rule.category,
            severity=rule_pack.rule.severity,
            confidence=rule_pack.rule.confidence,
            affected_component=_affected_component(rule_pack.match),
            control_refs=list(rule_pack.rule.control_refs),
            evidence=matched_evidence,
            remediation=rule_pack.remediation.text,
            source_tool="community-rule",
            rationale="Community rule matched normalized host evidence.",
            risk=_community_risk(rule_pack.rule.severity, rule_pack.rule.confidence),
        )
    ]


def test_host_rule_pack(rule_path: str | Path, fixture_path: str | Path) -> HostRuleTestResult:
    rule = load_host_rule_pack(rule_path)
    snapshot = load_host_input(fixture_path)
    findings = evaluate_host_rule_pack(rule, snapshot)
    observed_ids = {finding.id for finding in findings}
    expected_ids = list(rule.metadata.expected_finding_ids)
    missing = [finding_id for finding_id in expected_ids if finding_id not in observed_ids]
    passed = bool(findings) if not expected_ids else not missing
    return HostRuleTestResult(
        rule_path=str(Path(rule_path).resolve(strict=False)),
        fixture_path=str(Path(fixture_path).resolve(strict=False)),
        target=snapshot.identity.hostname,
        finding_count=len(findings),
        findings=findings,
        expected_finding_ids=expected_ids,
        missing_expected_finding_ids=missing,
        passed=passed,
    )


def test_all_host_rule_packs(rules_root: str | Path) -> HostRuleTestAllResult:
    root = Path(rules_root).expanduser().resolve(strict=False)
    if not root.is_dir():
        raise HostCommunityError(f"host rule directory does not exist: {root}")
    result = HostRuleTestAllResult(rules_root=str(root))
    for rule_path in sorted(root.glob("*.toml")):
        if rule_path.name.startswith("_"):
            continue
        try:
            rule = load_host_rule_pack(rule_path)
            if rule.metadata.fixture is None:
                result.skipped += 1
                result.errors.append(f"{rule_path}: metadata.fixture is required for test-all")
                continue
            test_result = test_host_rule_pack(rule_path, rule.metadata.fixture)
            result.results.append(test_result)
            if test_result.passed:
                result.passed += 1
            else:
                result.failed += 1
        except Exception as exc:
            result.failed += 1
            result.errors.append(f"{rule_path}: {exc}")
        finally:
            result.tested += 1
    return result


def scaffold_host_rule(title: str, output_dir: str | Path = "rules/community/host") -> Path:
    slug = _slugify(title)
    if not slug:
        raise HostCommunityError("rule title must contain at least one letter or number")
    path = Path(output_dir) / f"{slug}.toml"
    if path.exists():
        raise HostCommunityError(f"host rule already exists: {path}")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(_rule_template(title, slug), encoding="utf-8")
    return path


def validate_host_fixture(fixture_path: str | Path) -> HostFixtureValidationResult:
    path = Path(fixture_path).expanduser().resolve(strict=False)
    errors: list[str] = []
    try:
        snapshot = load_host_input(path)
        report = analyze_snapshot(snapshot)
    except Exception as exc:
        return HostFixtureValidationResult(
            fixture_path=str(path),
            status="error",
            errors=[str(exc)],
        )
    ground_truth: HostGroundTruth | None = None
    if (path / "ground_truth.json").is_file():
        try:
            ground_truth = load_host_ground_truth(path / "ground_truth.json")
        except Exception as exc:
            errors.append(f"invalid ground_truth.json: {exc}")
    return HostFixtureValidationResult(
        fixture_path=str(path),
        status="ok" if not errors else "error",
        target=snapshot.identity.hostname,
        has_ground_truth=ground_truth is not None,
        expected_findings=len(ground_truth.expected_findings) if ground_truth else 0,
        expected_absent=len(ground_truth.expected_absent) if ground_truth else 0,
        evidence_inventory=report.evidence_inventory,
        finding_count=len(report.findings),
        errors=errors,
    )


def validate_host_benchmark_submission(fixture_path: str | Path) -> HostBenchmarkSubmission:
    validation = validate_host_fixture(fixture_path)
    if validation.status != "ok":
        raise HostCommunityError("; ".join(validation.errors) or "fixture validation failed")
    path = Path(fixture_path).expanduser().resolve(strict=False)
    snapshot = load_host_input(path)
    if not validation.has_ground_truth:
        raise HostCommunityError("benchmark submissions must include ground_truth.json")
    platform = snapshot.config.get("platform")
    platform_family = "unknown"
    if isinstance(platform, dict):
        platform_family = str(platform.get("platform_family") or "unknown")
    return HostBenchmarkSubmission(
        fixture=str(path),
        target=snapshot.identity.hostname,
        platform_family=platform_family,
        evidence_inventory=validation.evidence_inventory,
        expected_findings=validation.expected_findings,
        expected_absent=validation.expected_absent,
        notes=["Validated locally; open a PR with this fixture and ground_truth.json."],
    )


def render_rule_test_result(result: HostRuleTestResult) -> str:
    lines = [
        f"rule: {result.rule_path}",
        f"fixture: {result.fixture_path}",
        f"target: {result.target}",
        f"findings: {result.finding_count}",
        f"passed: {'yes' if result.passed else 'no'}",
    ]
    if result.missing_expected_finding_ids:
        lines.append("missing expected IDs:")
        lines.extend(f"- {finding_id}" for finding_id in result.missing_expected_finding_ids)
    for finding in result.findings:
        lines.append(f"- {finding.id} {finding.severity} {finding.title}")
    return "\n".join(lines) + "\n"


def render_rule_test_all_result(result: HostRuleTestAllResult) -> str:
    lines = [
        f"rules: {result.rules_root}",
        f"tested: {result.tested}",
        f"passed: {result.passed}",
        f"failed: {result.failed}",
        f"skipped: {result.skipped}",
    ]
    if result.errors:
        lines.append("errors:")
        lines.extend(f"- {error}" for error in result.errors)
    return "\n".join(lines) + "\n"


def render_fixture_validation(result: HostFixtureValidationResult) -> str:
    lines = [
        f"fixture: {result.fixture_path}",
        f"status: {result.status}",
    ]
    if result.target:
        lines.append(f"target: {result.target}")
    lines.extend(
        [
            f"ground_truth: {'yes' if result.has_ground_truth else 'no'}",
            f"expected_findings: {result.expected_findings}",
            f"expected_absent: {result.expected_absent}",
            f"findings: {result.finding_count}",
        ]
    )
    if result.evidence_inventory:
        lines.append("evidence:")
        lines.extend(
            f"- {key}: {value}" for key, value in sorted(result.evidence_inventory.items())
        )
    if result.errors:
        lines.append("errors:")
        lines.extend(f"- {error}" for error in result.errors)
    return "\n".join(lines) + "\n"


def _resolve_evidence_values(snapshot: HostSnapshot, path: str) -> list[Any]:
    current: list[Any] = [snapshot.model_dump(mode="json")]
    for part in path.split("."):
        next_values: list[Any] = []
        for value in current:
            next_values.extend(_descend(value, part))
        current = next_values
        if not current:
            return []
    return current


def _descend(value: Any, key: str) -> list[Any]:
    if isinstance(value, dict):
        if key in value:
            return [value[key]]
        lowered = key.casefold()
        return [item for candidate, item in value.items() if str(candidate).casefold() == lowered]
    if isinstance(value, list):
        if key.isdigit():
            index = int(key)
            return [value[index]] if index < len(value) else []
        results: list[Any] = []
        for item in value:
            results.extend(_descend(item, key))
        return results
    return []


def _match_value(value: Any, match: HostRuleMatch) -> bool:
    if match.exists is not None:
        present = value is not None and value != "" and value != []
        return present is match.exists
    if match.equals is not None:
        return _compare(value, match.equals, case_sensitive=match.case_sensitive)
    if match.not_equals is not None:
        return not _compare(value, match.not_equals, case_sensitive=match.case_sensitive)
    if match.contains is not None:
        haystack = _normalize(value, case_sensitive=match.case_sensitive)
        needle = _normalize(match.contains, case_sensitive=match.case_sensitive)
        return needle in haystack
    if match.in_values:
        return any(
            _compare(value, expected, case_sensitive=match.case_sensitive)
            for expected in match.in_values
        )
    return False


def _compare(value: Any, expected: Any, *, case_sensitive: bool) -> bool:
    return _normalize(value, case_sensitive=case_sensitive) == _normalize(
        expected,
        case_sensitive=case_sensitive,
    )


def _normalize(value: Any, *, case_sensitive: bool) -> str:
    rendered = _stringify(value)
    return rendered if case_sensitive else rendered.casefold()


def _stringify(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, (str, int, float)):
        return str(value)
    return json.dumps(value, sort_keys=True)


def _affected_component(matches: list[HostRuleMatch]) -> str | None:
    if not matches:
        return None
    parts = matches[0].evidence.split(".")
    return parts[1] if len(parts) > 1 else parts[0]


def _community_risk(severity: Severity, confidence: float) -> HostRiskScore:
    severity_scores = {
        "informational": 0.05,
        "low": 0.25,
        "medium": 0.55,
        "high": 0.8,
        "critical": 1.0,
    }
    sev = severity_scores[severity]
    total = round(min(100.0, max(1.0, (sev * 70.0) + (confidence * 20.0))), 1)
    return HostRiskScore(
        total=total,
        severity=sev,
        confidence=confidence,
        exploitability=max(0.1, sev * 0.75),
        blast_radius=max(0.1, sev * 0.7),
        remediation_urgency=max(0.1, sev * 0.8),
        evidence_quality=confidence,
        rationale=["Community rule matched explicit normalized evidence."],
    )


def _unsafe_key_path(value: Any, prefix: str = "") -> str | None:
    if isinstance(value, dict):
        for key, item in value.items():
            key_text = str(key)
            full = f"{prefix}.{key_text}" if prefix else key_text
            if key_text.casefold() in _UNSAFE_KEYS:
                return full
            found = _unsafe_key_path(item, full)
            if found is not None:
                return found
    elif isinstance(value, list):
        for index, item in enumerate(value):
            found = _unsafe_key_path(item, f"{prefix}[{index}]")
            if found is not None:
                return found
    return None


def _slugify(value: str) -> str:
    slug = re.sub(r"[^a-z0-9]+", "-", value.casefold()).strip("-")
    return re.sub(r"-+", "-", slug)[:80]


def _rule_template(title: str, slug: str) -> str:
    rule_id = f"community.{slug}"
    return f"""[rule]
id = "{rule_id}"
title = "{title}"
category = "configuration"
severity = "medium"
confidence = 0.85
platform_support = ["linux"]

[[match]]
evidence = "config.example.setting"
equals = "risky"

[remediation]
text = "Replace this placeholder with the concrete remediation and verification guidance."
verification = "Collect evidence again and rerun this rule test."

[metadata]
maintainer = "community"
fixture = "tests/fixtures/host/my-fixture"
expected_finding_ids = []
false_positive_notes = "Describe known safe exceptions or compensating controls."
last_validation_date = "{date.today().isoformat()}"
tags = ["community"]
"""


__all__ = [
    "HostBenchmarkSubmission",
    "HostCommunityError",
    "HostFixtureValidationResult",
    "HostRulePack",
    "HostRuleTestAllResult",
    "HostRuleTestResult",
    "evaluate_host_rule_pack",
    "load_host_rule_pack",
    "render_fixture_validation",
    "render_rule_test_all_result",
    "render_rule_test_result",
    "scaffold_host_rule",
    "test_all_host_rule_packs",
    "test_host_rule_pack",
    "validate_host_benchmark_submission",
    "validate_host_fixture",
]
