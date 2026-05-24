from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path

import pytest

from piranesi.detect.flows import extract_candidate_findings
from piranesi.scan.joern import JoernServer, is_joern_installed
from piranesi.scan.specs import get_sanitizer_specs, get_sink_specs, get_source_specs
from piranesi.scan.transpile import (
    TranspiledProject,
    TypeScriptCompilerNotFoundError,
    transpile_project,
)

PHASE1_CASES_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "typescript" / "phase1_cases"
EXPECT_PATTERN = re.compile(
    r"//\s*@piranesi-expect:\s*(?P<cwe>CWE-\d+),\s*source=(?P<source>[^,]+),\s*sink=(?P<sink>\S+)"
)
EXPECT_CLEAN_PATTERN = re.compile(r"//\s*@piranesi-expect-clean:\s*(?P<reason>.+)")


@dataclass(frozen=True, slots=True)
class ExpectedFinding:
    file: Path
    cwe: str
    source: str
    sink: str


def _fixture_expectations(root: Path) -> tuple[tuple[ExpectedFinding, ...], tuple[Path, ...]]:
    findings: list[ExpectedFinding] = []
    clean_files: set[Path] = set()

    for fixture_path in sorted(root.rglob("*.ts")):
        text = fixture_path.read_text(encoding="utf-8")
        for match in EXPECT_PATTERN.finditer(text):
            findings.append(
                ExpectedFinding(
                    file=fixture_path.resolve(),
                    cwe=match.group("cwe"),
                    source=match.group("source"),
                    sink=match.group("sink"),
                )
            )
        if EXPECT_CLEAN_PATTERN.search(text) is not None:
            clean_files.add(fixture_path.resolve())

    return tuple(findings), tuple(sorted(clean_files))


def _finding_source_expression(source_type: str, parameter_name: str | None) -> str:
    if source_type.startswith("req."):
        return source_type

    prefix = {
        "request_body": "req.body",
        "request_param": "req.query",
        "header": "req.headers",
        "cookie": "req.cookies",
        "env_var": "process.env",
    }.get(source_type, source_type)
    if parameter_name:
        return f"{prefix}.{parameter_name}"
    return prefix


def _transpile_or_skip(project_dir: Path) -> TranspiledProject:
    try:
        return transpile_project(project_dir)
    except TypeScriptCompilerNotFoundError as exc:  # pragma: no cover - environment dependent
        pytest.skip(str(exc))


@pytest.mark.joern
@pytest.mark.integration
def test_phase1_fixture_annotations_match_real_findings() -> None:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")

    expected_findings, clean_files = _fixture_expectations(PHASE1_CASES_DIR)
    body_source = next(spec for spec in get_source_specs() if spec.name == "express_req_body")
    sql_sink = next(spec for spec in get_sink_specs() if spec.name == "raw_sql_query")

    transpiled = _transpile_or_skip(PHASE1_CASES_DIR)
    try:
        with JoernServer(startup_timeout_seconds=30, query_timeout_seconds=30) as server:
            server.import_project(transpiled.out_dir)
            findings = extract_candidate_findings(
                server,
                joern_project_root=transpiled.out_dir,
                source_map=transpiled.source_map,
                source_specs=(body_source,),
                sink_specs=(sql_sink,),
                sanitizer_specs=get_sanitizer_specs(),
            )
    finally:
        transpiled.cleanup()

    actual_keys = {
        (
            Path(finding.source.location.file).resolve(),
            finding.vuln_class,
            _finding_source_expression(
                finding.source.source_type,
                finding.source.parameter_name,
            ),
            finding.sink.api_name,
        )
        for finding in findings
    }
    expected_keys = {
        (expectation.file, expectation.cwe, expectation.source, expectation.sink)
        for expectation in expected_findings
    }

    assert actual_keys == expected_keys
    assert all(
        Path(finding.source.location.file).resolve() not in clean_files for finding in findings
    )
