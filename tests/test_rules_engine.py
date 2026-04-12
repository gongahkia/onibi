from __future__ import annotations

from collections.abc import Generator
from contextlib import contextmanager
from pathlib import Path
from types import SimpleNamespace

import pytest
from typer.testing import CliRunner

import piranesi.pipeline as pipeline_module
from piranesi.cli import app
from piranesi.config import OutputConfig, PiranesiConfig, ScanConfig
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource
from piranesi.pipeline import DetectArtifact, PipelineContext
from piranesi.rules.engine import PatternKind, compile_rule, load_rules

runner = CliRunner()
FIXTURES_DIR = Path(__file__).resolve().parent / "fixtures" / "custom_rule_authoring"
RULES_DIR = FIXTURES_DIR / "rules"


def test_load_rules_discovers_custom_rule_fixtures() -> None:
    rules = load_rules(RULES_DIR)

    assert [rule.id for rule in rules] == [
        "custom-ldap-001",
        "custom-nosql-001",
        "custom-xml-001",
    ]
    assert all(compile_rule(rule).kind is PatternKind.REGEX for rule in rules)


def test_compile_rule_supports_builtin_inheritance(tmp_path: Path) -> None:
    rule_path = tmp_path / "rules" / "strict-sqli.toml"
    rule_path.parent.mkdir(parents=True, exist_ok=True)
    rule_path.write_text(
        "\n".join(
            [
                "[rule]",
                'id = "custom-sqli-stricter"',
                'extends = "builtin:sqli"',
                'override_severity = "critical"',
                "",
                "[rule.additional_sanitizers]",
                "patterns = ['cpg.call.name(\"companySanitizeSql\")']",
            ]
        ),
        encoding="utf-8",
    )

    compiled = compile_rule(load_rules(rule_path)[0])

    assert compiled.kind is PatternKind.CPGQL
    assert compiled.extends == "builtin:sqli"
    assert compiled.severity == "critical"
    assert compiled.cwe_id == "CWE-89"
    assert 'cpg.call.name("companySanitizeSql")' in compiled.sanitizer_patterns


def test_rules_validate_command_accepts_rule_directory() -> None:
    result = runner.invoke(app, ["rules", "validate", str(RULES_DIR)])

    assert result.exit_code == 0
    assert "validated 3 rule(s)" in result.stdout
    assert "custom-nosql-001" in result.stdout


def test_rules_validate_command_catches_malformed_rule(tmp_path: Path) -> None:
    invalid_rule = tmp_path / "invalid.toml"
    invalid_rule.write_text(
        "\n".join(
            [
                "[rule]",
                'id = "broken-rule"',
                'name = "Broken rule"',
                'cwe_id = "CWE-79"',
                'severity = "high"',
                'description = "Broken CPGQL pattern"',
                "source_pattern = 'cpg.call.name(\"req\"'",
                "sink_pattern = 'cpg.call.name(\"send\")'",
                'message_template = "Broken"',
                'author = "piranesi-tests"',
                'version = "1.0.0"',
            ]
        ),
        encoding="utf-8",
    )

    result = runner.invoke(app, ["rules", "validate", str(invalid_rule)])

    assert result.exit_code == 1
    assert "unbalanced delimiters" in result.stdout


@pytest.mark.parametrize(
    ("rule_file", "fixture_dir", "rule_id"),
    [
        (RULES_DIR / "nosql-injection.toml", FIXTURES_DIR / "nosql", "custom-nosql-001"),
        (RULES_DIR / "ldap-injection.toml", FIXTURES_DIR / "ldap", "custom-ldap-001"),
        (RULES_DIR / "xml-injection.toml", FIXTURES_DIR / "xml", "custom-xml-001"),
    ],
)
def test_rules_test_command_reports_matches(
    rule_file: Path,
    fixture_dir: Path,
    rule_id: str,
) -> None:
    result = runner.invoke(
        app,
        ["rules", "test", str(rule_file), "--fixture", str(fixture_dir)],
    )

    assert result.exit_code == 0
    assert f"{rule_id}: 1 match" in result.stdout
    assert "total matches: 1" in result.stdout
    assert "vulnerable.ts" in result.stdout


def test_detect_stage_includes_custom_rule_findings(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    target_dir = tmp_path / "target"
    rules_dir = target_dir / "rules"
    source_dir = target_dir / "src"
    rules_dir.mkdir(parents=True, exist_ok=True)
    source_dir.mkdir(parents=True, exist_ok=True)
    (rules_dir / "nosql-injection.toml").write_text(
        (RULES_DIR / "nosql-injection.toml").read_text(encoding="utf-8"),
        encoding="utf-8",
    )
    (source_dir / "app.ts").write_text(
        (FIXTURES_DIR / "nosql" / "vulnerable.ts").read_text(encoding="utf-8"),
        encoding="utf-8",
    )

    context = PipelineContext(
        target_dir=target_dir,
        output_dir=tmp_path / "out",
        provider=None,  # type: ignore[arg-type]
        router=None,  # type: ignore[arg-type]
        cost_tracker=SimpleNamespace(total_usd=0.0),  # type: ignore[arg-type]
        trace_writer=None,  # type: ignore[arg-type]
    )
    config = PiranesiConfig(
        output=OutputConfig(output_dir=str(context.output_dir)),
        scan=ScanConfig(include_tests=False),
    )

    monkeypatch.setattr(pipeline_module, "resolve_frameworks", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_source_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sink_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sanitizer_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "extract_secret_findings", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(
        pipeline_module,
        "extract_misconfiguration_findings",
        lambda *_args, **_kwargs: [],
    )
    monkeypatch.setattr(
        pipeline_module,
        "scan_dependency_findings",
        lambda *_args, **_kwargs: SimpleNamespace(findings=[]),
    )

    @contextmanager
    def _fake_scan_session(
        *_args: object,
        **_kwargs: object,
    ) -> Generator[tuple[None, SimpleNamespace], None, None]:
        yield None, SimpleNamespace(joern_project_root=target_dir, source_map=None)

    monkeypatch.setattr(pipeline_module, "_scan_session", _fake_scan_session)
    monkeypatch.setattr(pipeline_module, "_scan_session_for_target", _fake_scan_session)
    monkeypatch.setattr(
        pipeline_module,
        "extract_candidate_findings",
        lambda *_args, **_kwargs: (_builtin_finding(target_dir / "src" / "app.ts"),),
    )

    result = pipeline_module._run_detect_stage(context, config, None)

    assert isinstance(result.artifact, DetectArtifact)
    assert len(result.artifact.findings) == 2
    custom = next(
        finding
        for finding in result.artifact.findings
        if finding.metadata.get("custom_rule_id") == "custom-nosql-001"
    )
    assert custom.vuln_class == "CWE-943"


def _builtin_finding(path: Path) -> CandidateFinding:
    location = SourceLocation(
        file=str(path.resolve(strict=False)),
        line=1,
        column=1,
        snippet="req.body.name",
    )
    return CandidateFinding(
        id="builtin-finding",
        vuln_class="CWE-79",
        source=TaintSource(
            location=location,
            source_type="request_body",
            data_categories=["unknown"],
            parameter_name="name",
        ),
        sink=TaintSink(
            location=location,
            sink_type="html_output",
            api_name="send",
        ),
        taint_path=[],
        path_conditions=[],
        confidence=0.7,
        severity="medium",
    )
