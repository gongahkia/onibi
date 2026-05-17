from __future__ import annotations

import shutil
import tempfile
from collections import defaultdict
from dataclasses import dataclass
from pathlib import Path

import yaml

from piranesi.config import RulesConfig
from piranesi.models import CandidateFinding
from piranesi.rules.engine import compile_rule, load_rules, run_rules_against_fixture
from piranesi.rules.registry import RuleRegistryError, discover_rules, load_rule_document

_CWE_ALIAS_MAP = {
    "CWE-77": "CWE-78",
    "CWE-943": "CWE-89",
}


@dataclass(frozen=True, slots=True)
class RuleMatch:
    rule_id: str
    cwe_id: str
    fixture: Path
    source_line: int | None
    sink_line: int | None


@dataclass(frozen=True, slots=True)
class RuleInlineTestResult:
    rule_id: str
    fixture: Path
    description: str | None
    passed: bool
    message: str
    expected_finding: bool


@dataclass(frozen=True, slots=True)
class RuleTestSummary:
    rule_count: int
    total: int
    passed: int
    failed: int
    results: tuple[RuleInlineTestResult, ...]


@dataclass(frozen=True, slots=True)
class GroundTruthCoverageRow:
    rule_cwe_id: str
    normalized_cwe_id: str
    rule_ids: tuple[str, ...]
    covered_entry_ids: tuple[str, ...]


@dataclass(frozen=True, slots=True)
class RuleCoverageReport:
    rule_count: int
    custom_cwe_ids: tuple[str, ...]
    rows: tuple[GroundTruthCoverageRow, ...]
    ground_truth_total: int
    covered_entry_ids: tuple[str, ...]
    uncovered_entry_ids: tuple[str, ...]


@dataclass(frozen=True, slots=True)
class _GroundTruthEntry:
    entry_id: str
    cwe_id: str


@dataclass(frozen=True, slots=True)
class _RuleTestTarget:
    display_rule_id: str
    file_path: Path


def run_all_rule_tests(
    rules_dir: str | Path | None = None,
    *,
    rules_config: RulesConfig | None = None,
    config_path: Path | None = None,
) -> RuleTestSummary:
    targets = _discover_rule_test_targets(
        rules_dir,
        rules_config=rules_config,
        config_path=config_path,
    )
    results: list[RuleInlineTestResult] = []

    for target in targets:
        results.extend(run_rule_inline_tests(target.file_path, rule_id=target.display_rule_id))

    passed = sum(1 for result in results if result.passed)
    failed = len(results) - passed
    return RuleTestSummary(
        rule_count=len(targets),
        total=len(results),
        passed=passed,
        failed=failed,
        results=tuple(results),
    )


def run_rule_inline_tests(
    rule_path: str | Path,
    *,
    rule_id: str | None = None,
) -> tuple[RuleInlineTestResult, ...]:
    path = Path(rule_path).resolve(strict=False)
    try:
        document = load_rule_document(path)
    except RuleRegistryError as exc:
        raise ValueError(str(exc)) from exc
    display_rule_id = rule_id or document.rule.id
    results: list[RuleInlineTestResult] = []

    for inline_test in document.tests:
        fixture_path = resolve_fixture_path(path, inline_test.fixture)
        findings = _run_rule_against_fixture(path, fixture_path)
        matches = tuple(
            RuleMatch(
                rule_id=display_rule_id,
                cwe_id=finding.vuln_class,
                fixture=fixture_path,
                source_line=finding.source.location.line,
                sink_line=finding.sink.location.line,
            )
            for finding in findings
        )
        results.append(
            _evaluate_inline_test(
                rule_id=display_rule_id,
                fixture_path=fixture_path,
                description=inline_test.description,
                expect_finding=inline_test.expect_finding,
                expect_cwe=inline_test.expect_cwe,
                expect_source_line=inline_test.expect_source_line,
                expect_sink_line=inline_test.expect_sink_line,
                matches=matches,
            )
        )

    return tuple(results)


def build_rule_coverage_report(
    rules_dir: str | Path | None = None,
    *,
    rules_config: RulesConfig | None = None,
    config_path: Path | None = None,
    ground_truth_dir: str | Path = Path("eval/ground_truth"),
) -> RuleCoverageReport:
    targets = _discover_rule_test_targets(
        rules_dir,
        rules_config=rules_config,
        config_path=config_path,
    )
    entries = _load_ground_truth_entries(Path(ground_truth_dir))
    rule_ids_by_cwe: dict[str, list[str]] = defaultdict(list)
    covered_entry_ids: set[str] = set()

    for target in targets:
        compiled = compile_rule(load_rules(target.file_path)[0])
        rule_ids_by_cwe[compiled.cwe_id].append(target.display_rule_id)

    rows: list[GroundTruthCoverageRow] = []
    for cwe_id in sorted(rule_ids_by_cwe, key=_cwe_sort_key):
        normalized_cwe_id = normalize_cwe_id(cwe_id)
        covered_ids = tuple(
            sorted(
                entry.entry_id
                for entry in entries
                if normalize_cwe_id(entry.cwe_id) == normalized_cwe_id
            )
        )
        covered_entry_ids.update(covered_ids)
        rows.append(
            GroundTruthCoverageRow(
                rule_cwe_id=cwe_id,
                normalized_cwe_id=normalized_cwe_id,
                rule_ids=tuple(sorted(rule_ids_by_cwe[cwe_id])),
                covered_entry_ids=covered_ids,
            )
        )

    uncovered_entry_ids = tuple(
        sorted(entry.entry_id for entry in entries if entry.entry_id not in covered_entry_ids)
    )
    return RuleCoverageReport(
        rule_count=len(targets),
        custom_cwe_ids=tuple(sorted(rule_ids_by_cwe, key=_cwe_sort_key)),
        rows=tuple(rows),
        ground_truth_total=len(entries),
        covered_entry_ids=tuple(sorted(covered_entry_ids)),
        uncovered_entry_ids=uncovered_entry_ids,
    )


def render_rule_test_summary(summary: RuleTestSummary) -> str:
    lines: list[str] = []
    for result in summary.results:
        status = "PASS" if result.passed else "FAIL"
        lines.append(f"{status} {result.rule_id} :: {result.fixture} :: {result.message}")
    lines.append(
        "Summary: "
        f"{summary.passed}/{summary.total} passed, "
        f"{summary.failed} failed across {summary.rule_count} rules"
    )
    return "\n".join(lines)


def render_rule_coverage_report(report: RuleCoverageReport) -> str:
    lines = [
        f"Custom rule count: {report.rule_count}",
        "Custom rule CWEs: "
        + (", ".join(report.custom_cwe_ids) if report.custom_cwe_ids else "none"),
        "Ground truth coverage: "
        f"{len(report.covered_entry_ids)}/{report.ground_truth_total} entries",
    ]
    for row in report.rows:
        covered = ", ".join(row.covered_entry_ids) if row.covered_entry_ids else "none"
        rule_ids = ", ".join(row.rule_ids)
        lines.append(
            f"{row.rule_cwe_id} ({rule_ids}) -> normalized {row.normalized_cwe_id} -> {covered}"
        )
    if report.uncovered_entry_ids:
        lines.append("Uncovered ground truth entries: " + ", ".join(report.uncovered_entry_ids))
    return "\n".join(lines)


def resolve_fixture_path(rule_path: Path, fixture: str | Path) -> Path:
    fixture_path = Path(fixture)
    if fixture_path.is_absolute():
        return fixture_path.resolve(strict=False)

    for parent in rule_path.parents:
        candidate = (parent / fixture_path).resolve(strict=False)
        if candidate.exists():
            return candidate
    return (rule_path.parent / fixture_path).resolve(strict=False)


def normalize_cwe_id(value: str | None) -> str:
    if value is None:
        return "UNKNOWN"
    stripped = value.strip().upper()
    if not stripped:
        return "UNKNOWN"
    if stripped.startswith("CWE-") and stripped[4:].isdigit():
        return _CWE_ALIAS_MAP.get(stripped, stripped)
    if stripped.isdigit():
        canonical = f"CWE-{int(stripped)}"
        return _CWE_ALIAS_MAP.get(canonical, canonical)
    return _CWE_ALIAS_MAP.get(stripped, stripped)


def _discover_rule_test_targets(
    rules_dir: str | Path | None,
    *,
    rules_config: RulesConfig | None,
    config_path: Path | None,
) -> list[_RuleTestTarget]:
    if rules_dir is not None:
        return _explicit_rule_test_targets(Path(rules_dir))

    effective_rules_config = rules_config or RulesConfig()
    try:
        discovered = discover_rules(effective_rules_config, config_path=config_path)
    except RuleRegistryError as exc:
        raise ValueError(str(exc)) from exc

    return [
        _RuleTestTarget(
            display_rule_id=rule.rule_id, file_path=rule.file_path.resolve(strict=False)
        )
        for rule in discovered
    ]


def _explicit_rule_test_targets(rules_dir: Path) -> list[_RuleTestTarget]:
    if rules_dir.is_file():
        try:
            document = load_rule_document(rules_dir.resolve(strict=False))
        except RuleRegistryError as exc:
            raise ValueError(str(exc)) from exc
        return [
            _RuleTestTarget(
                display_rule_id=document.rule.id,
                file_path=rules_dir.resolve(strict=False),
            )
        ]

    if not rules_dir.exists():
        raise FileNotFoundError(f"rules path does not exist: {rules_dir}")

    targets: list[_RuleTestTarget] = []
    for path in sorted(candidate for candidate in rules_dir.rglob("*.toml") if candidate.is_file()):
        try:
            document = load_rule_document(path.resolve(strict=False))
        except RuleRegistryError:
            continue
        targets.append(
            _RuleTestTarget(
                display_rule_id=document.rule.id,
                file_path=path.resolve(strict=False),
            )
        )
    return targets


def _run_rule_against_fixture(rule_path: Path, fixture_path: Path) -> tuple[CandidateFinding, ...]:
    if not fixture_path.exists():
        raise FileNotFoundError(f"fixture does not exist: {fixture_path}")

    if fixture_path.is_dir():
        results = run_rules_against_fixture(rule_path, fixture_dir=fixture_path)
        return tuple(results[0].findings) if results else ()

    with tempfile.TemporaryDirectory(prefix="piranesi-inline-rule-") as workspace:
        staged_root = Path(workspace).resolve(strict=False)
        shutil.copy2(fixture_path, staged_root / fixture_path.name)
        results = run_rules_against_fixture(rule_path, fixture_dir=staged_root)
        return tuple(results[0].findings) if results else ()


def _evaluate_inline_test(
    *,
    rule_id: str,
    fixture_path: Path,
    description: str | None,
    expect_finding: bool,
    expect_cwe: str | None,
    expect_source_line: int | None,
    expect_sink_line: int | None,
    matches: tuple[RuleMatch, ...],
) -> RuleInlineTestResult:
    if not expect_finding:
        passed = not matches
        message = (
            "expected no finding and got none"
            if passed
            else ("expected no finding, observed " + _format_matches(matches))
        )
        return RuleInlineTestResult(
            rule_id=rule_id,
            fixture=fixture_path,
            description=description,
            passed=passed,
            message=message,
            expected_finding=False,
        )

    matching = [
        match
        for match in matches
        if _match_satisfies_expectation(
            match,
            expect_cwe=expect_cwe,
            expect_source_line=expect_source_line,
            expect_sink_line=expect_sink_line,
        )
    ]
    if matching:
        message = "expected finding matched " + _format_matches(tuple(matching))
    elif not matches:
        message = "expected finding, observed none"
    else:
        message = "expected finding did not match observed " + _format_matches(matches)

    return RuleInlineTestResult(
        rule_id=rule_id,
        fixture=fixture_path,
        description=description,
        passed=bool(matching),
        message=message,
        expected_finding=True,
    )


def _match_satisfies_expectation(
    match: RuleMatch,
    *,
    expect_cwe: str | None,
    expect_source_line: int | None,
    expect_sink_line: int | None,
) -> bool:
    if expect_cwe is not None and normalize_cwe_id(match.cwe_id) != normalize_cwe_id(expect_cwe):
        return False
    if expect_source_line is not None and match.source_line != expect_source_line:
        return False
    return not (expect_sink_line is not None and match.sink_line != expect_sink_line)


def _load_ground_truth_entries(ground_truth_dir: Path) -> tuple[_GroundTruthEntry, ...]:
    if not ground_truth_dir.exists():
        raise FileNotFoundError(f"ground truth path does not exist: {ground_truth_dir}")

    entries: list[_GroundTruthEntry] = []
    for path in sorted(ground_truth_dir.glob("*.yaml")):
        payload = yaml.safe_load(path.read_text(encoding="utf-8"))
        if not isinstance(payload, dict):
            raise ValueError(f"invalid ground truth document in {path}")
        entry_id = payload.get("id")
        cwe_id = payload.get("cwe_id")
        if not isinstance(entry_id, str) or not entry_id.strip():
            raise ValueError(f"missing id in {path}")
        if not isinstance(cwe_id, str) or not cwe_id.strip():
            raise ValueError(f"missing cwe_id in {path}")
        entries.append(_GroundTruthEntry(entry_id=entry_id.strip(), cwe_id=cwe_id.strip()))
    return tuple(entries)


def _format_matches(matches: tuple[RuleMatch, ...]) -> str:
    return ", ".join(f"{match.cwe_id}@{match.source_line}->{match.sink_line}" for match in matches)


def _cwe_sort_key(cwe_id: str) -> tuple[int, str]:
    normalized = normalize_cwe_id(cwe_id)
    if normalized.startswith("CWE-") and normalized[4:].isdigit():
        return int(normalized[4:]), normalized
    return 2**31 - 1, normalized
