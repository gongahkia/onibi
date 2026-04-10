from __future__ import annotations

from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.diff import diff_findings, load_findings, render_diff
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource, TaintStep
from piranesi.pipeline import DetectArtifact
from tests._pipeline_fixtures import fixture_artifacts

runner = CliRunner()


def test_load_findings_supports_verify_artifact(tmp_path: Path) -> None:
    artifacts = fixture_artifacts(tmp_path)
    verify_path = tmp_path / "verify.json"
    verify_path.write_text(artifacts["verify"].model_dump_json(indent=2), encoding="utf-8")

    findings = load_findings(verify_path)

    assert len(findings) == 1
    assert findings[0].id == "finding-001"
    assert findings[0].stable_fingerprint


def test_baseline_save_and_diff_show_expected_counts(tmp_path: Path) -> None:
    baseline_results = tmp_path / "baseline-results"
    current_results = tmp_path / "current-results"
    baseline_path = tmp_path / ".piranesi-baseline.json"

    baseline_candidates = [
        _candidate(
            finding_id="baseline-sqli",
            vuln_class="CWE-89: SQL Injection",
            file_path="src/routes/users.ts",
            parameter="userId",
            sink_api="db.query()",
            source_line=10,
            sink_line=18,
        ),
        _candidate(
            finding_id="baseline-xss",
            vuln_class="CWE-79: Cross-Site Scripting",
            file_path="src/routes/admin.ts",
            parameter="markup",
            sink_api="res.send()",
            source_line=12,
            sink_line=20,
        ),
        _candidate(
            finding_id="baseline-ssrf",
            vuln_class="CWE-918: Server-Side Request Forgery",
            file_path="src/services/fetch.ts",
            parameter="url",
            sink_api="fetch()",
            source_line=14,
            sink_line=24,
        ),
        _candidate(
            finding_id="baseline-path",
            vuln_class="CWE-22: Path Traversal",
            file_path="src/routes/files.ts",
            parameter="file",
            sink_api="fs.readFile()",
            source_line=16,
            sink_line=28,
        ),
        _candidate(
            finding_id="baseline-cmdi",
            vuln_class="CWE-78: Command Injection",
            file_path="src/utils/exec.ts",
            parameter="command",
            sink_api="exec()",
            source_line=18,
            sink_line=32,
        ),
    ]
    current_candidates = [
        _candidate(
            finding_id="current-sqli",
            vuln_class="CWE-89: SQL Injection",
            file_path="src/routes/users.ts",
            parameter="userId",
            sink_api="db.query()",
            source_line=14,
            sink_line=22,
        ),
        _candidate(
            finding_id="current-xss",
            vuln_class="CWE-79: Cross-Site Scripting",
            file_path="src/routes/admin.ts",
            parameter="markup",
            sink_api="res.send()",
            source_line=17,
            sink_line=25,
        ),
        _candidate(
            finding_id="current-ssrf",
            vuln_class="CWE-918: Server-Side Request Forgery",
            file_path="src/services/fetch.ts",
            parameter="url",
            sink_api="fetch()",
            source_line=21,
            sink_line=31,
        ),
        _candidate(
            finding_id="current-path",
            vuln_class="CWE-22: Path Traversal",
            file_path="src/routes/files.ts",
            parameter="file",
            sink_api="fs.readFile()",
            source_line=23,
            sink_line=35,
        ),
        _candidate(
            finding_id="current-new",
            vuln_class="CWE-89: SQL Injection",
            file_path="src/routes/teams.ts",
            parameter="teamId",
            sink_api="db.query()",
            source_line=26,
            sink_line=38,
        ),
    ]

    _write_detect_artifact(baseline_results, baseline_candidates)
    _write_detect_artifact(current_results, current_candidates)

    save_result = runner.invoke(
        app,
        ["baseline", "save", "--from", str(baseline_results), "--to", str(baseline_path)],
    )
    assert save_result.exit_code == 0

    diff_result = diff_findings(load_findings(baseline_path), load_findings(current_results))
    assert len(diff_result.new) == 1
    assert len(diff_result.fixed) == 1
    assert len(diff_result.unchanged) == 4

    rendered = render_diff(diff_result)
    assert "NEW (1):" in rendered
    assert "FIXED (1):" in rendered
    assert "UNCHANGED (4):" in rendered
    assert "Summary: 1 new, 1 fixed, 4 unchanged" in rendered

    cli_result = runner.invoke(app, ["diff", str(baseline_path), str(current_results)])
    assert cli_result.exit_code == 0
    assert "NEW (1):" in cli_result.stdout
    assert "FIXED (1):" in cli_result.stdout
    assert "UNCHANGED (4):" in cli_result.stdout
    assert "Summary: 1 new, 1 fixed, 4 unchanged" in cli_result.stdout

    fail_result = runner.invoke(
        app,
        ["diff", str(baseline_path), str(current_results), "--fail-on-new"],
    )
    assert fail_result.exit_code == 1


def _candidate(
    *,
    finding_id: str,
    vuln_class: str,
    file_path: str,
    parameter: str,
    sink_api: str,
    source_line: int,
    sink_line: int,
) -> CandidateFinding:
    source_location = SourceLocation(
        file=file_path,
        line=source_line,
        column=5,
        snippet=f"const {parameter} = req.query.{parameter};",
    )
    step_location = SourceLocation(
        file=file_path,
        line=source_line + 2,
        column=7,
        snippet=f"const unsafe = {parameter};",
    )
    sink_location = SourceLocation(
        file=file_path,
        line=sink_line,
        column=9,
        snippet=f"return {sink_api.replace('()', '')}(unsafe);",
    )
    return CandidateFinding(
        id=finding_id,
        vuln_class=vuln_class,
        source=TaintSource(
            location=source_location,
            source_type=f"req.query.{parameter}",
            data_categories=["identifier"],
            parameter_name=parameter,
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type=sink_api,
            api_name=sink_api,
        ),
        taint_path=[
            TaintStep(
                location=step_location,
                operation="assignment",
                taint_state="tainted",
            )
        ],
        path_conditions=[],
        confidence=0.95,
        severity="high",
    )


def _write_detect_artifact(output_dir: Path, findings: list[CandidateFinding]) -> None:
    output_dir.mkdir(parents=True, exist_ok=True)
    artifact = DetectArtifact(findings=findings)
    (output_dir / "detect.json").write_text(artifact.model_dump_json(indent=2), encoding="utf-8")
