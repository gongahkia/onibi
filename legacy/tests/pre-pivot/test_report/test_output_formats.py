from __future__ import annotations

import csv
import io
import xml.etree.ElementTree as ET
from collections import OrderedDict
from pathlib import Path
from typing import Any

import pytest
import xmlschema
from tests._pipeline_fixtures import fixture_artifacts
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.config import OwnershipConfig
from piranesi.models import ScanResult
from piranesi.pipeline import (
    DetectArtifact,
    LegalArtifact,
    PatchArtifact,
    PipelineContext,
    PipelineStage,
    StageResult,
    TriageArtifact,
    VerifyArtifact,
)
from piranesi.report import generate_csv, generate_junit_xml
from piranesi.report.renderer import (
    PiranesiReport,
    build_report,
    render_markdown,
    write_report_outputs,
)

runner = CliRunner()
_JUNIT_SCHEMA_PATH = Path(__file__).resolve().parents[1] / "fixtures" / "report" / "junit.xsd"
_CSV_COLUMNS = [
    "id",
    "cwe_id",
    "cwe_name",
    "severity",
    "source_file",
    "source_line",
    "sink_file",
    "sink_line",
    "taint_source",
    "taint_sink",
    "exploit_payload",
    "regulatory_frameworks",
    "suppressed",
    "suppression_reason",
]


def test_generate_junit_xml_validates_against_schema(tmp_path: Path) -> None:
    report = _build_report(tmp_path, include_suppressed=True)

    payload = generate_junit_xml(report)

    _validate_junit(payload)
    root = ET.fromstring(payload)  # noqa: S314 - payload is generated locally in the test
    assert root.attrib["tests"] == "2"
    assert root.attrib["failures"] == "1"
    assert root.attrib["skipped"] == "1"

    failure = root.find("./testcase/failure")
    assert failure is not None
    assert "Taint path:" in (failure.text or "")
    assert "Severity: HIGH" in (failure.text or "")
    assert "Exploit: ' OR 1=1--" in (failure.text or "")

    skipped = root.find("./testcase/skipped")
    assert skipped is not None
    assert skipped.attrib["message"] == "Suppressed: accepted risk"


def test_generate_csv_is_importable(tmp_path: Path) -> None:
    report = _build_report(tmp_path, include_suppressed=True)

    payload = generate_csv(report)

    reader = csv.DictReader(io.StringIO(payload))
    assert reader.fieldnames == _CSV_COLUMNS
    rows = list(reader)
    assert len(rows) == 2

    confirmed = rows[0]
    assert confirmed["id"] == "finding-001"
    assert confirmed["cwe_id"] == "CWE-89"
    assert confirmed["cwe_name"] == "SQL Injection"
    assert confirmed["severity"] == "HIGH"
    assert confirmed["source_file"] == "src/routes/login.ts"
    assert confirmed["source_line"] == "10"
    assert confirmed["sink_file"] == "src/routes/login.ts"
    assert confirmed["sink_line"] == "15"
    assert confirmed["taint_source"] == "req.body.username"
    assert confirmed["taint_sink"] == "db.query"
    assert confirmed["exploit_payload"] == "' OR 1=1--"
    assert confirmed["regulatory_frameworks"] == "PDPA"
    assert confirmed["suppressed"] == "false"
    assert confirmed["suppression_reason"] == ""

    suppressed = rows[1]
    assert suppressed["id"] == "finding-suppressed"
    assert suppressed["suppressed"] == "true"
    assert suppressed["suppression_reason"] == "accepted risk"
    assert suppressed["exploit_payload"] == ""


@pytest.mark.parametrize(
    ("report_format", "output_name"),
    [("junit", "report.junit.xml"), ("csv", "findings.csv")],
)
def test_cli_run_writes_requested_report_format(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
    report_format: str,
    output_name: str,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    config_path.write_text("", encoding="utf-8")

    def _registry(context: PipelineContext) -> OrderedDict[str, PipelineStage]:
        artifacts = fixture_artifacts(context.target_dir)
        return _build_fake_registry(context, artifacts=artifacts)

    monkeypatch.setattr("piranesi.cli.build_default_stage_registry", _registry)

    result = runner.invoke(
        app,
        [
            "run",
            str(tmp_path),
            "--config",
            str(config_path),
            "--output",
            str(output_dir),
            "--format",
            report_format,
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 1
    report_path = output_dir / output_name
    assert report_path.exists()

    if report_format == "junit":
        _validate_junit(report_path.read_text(encoding="utf-8"))
    else:
        reader = csv.DictReader(io.StringIO(report_path.read_text(encoding="utf-8")))
        assert reader.fieldnames == _CSV_COLUMNS
        assert next(reader)["id"] == "finding-001"


def test_cli_run_emits_compliance_report_to_stdout(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    output_dir = tmp_path / "out"
    config_path.write_text("", encoding="utf-8")

    def _registry(context: PipelineContext) -> OrderedDict[str, PipelineStage]:
        artifacts = fixture_artifacts(context.target_dir)
        return _build_fake_registry(context, artifacts=artifacts)

    monkeypatch.setattr("piranesi.cli.build_default_stage_registry", _registry)

    result = runner.invoke(
        app,
        [
            "run",
            str(tmp_path),
            "--config",
            str(config_path),
            "--output",
            str(output_dir),
            "--format",
            "compliance",
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 1
    assert "Regulatory Coverage Matrix" in result.stdout
    assert "OWASP Top 10 2021 Coverage" in result.stdout


def test_markdown_renders_ownership_context_and_finding_metadata(tmp_path: Path) -> None:
    artifacts = fixture_artifacts(tmp_path)
    report = build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=artifacts["detect"].findings,  # type: ignore[attr-defined]
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.2,
        duration_s=2.0,
        stage_timings_s={"scan": 0.5},
        ownership_config=OwnershipConfig(
            service="auth-api",
            system="identity-platform",
            team="identity-eng",
            owner="identity-oncall",
            repository="acme/identity",
            environment="staging",
            control_owner="grc-identity",
            control_mappings=[{"framework": "SOC2", "control": "CC6.6", "owner": "grc-controls"}],
        ),
    )

    markdown = render_markdown(report)

    assert "## Ownership Context" in markdown
    assert "acme/identity" in markdown
    assert "SOC2 CC6.6" in markdown
    assert "control_owner=`grc-identity`" in markdown


def _build_report(tmp_path: Path, *, include_suppressed: bool) -> PiranesiReport:
    artifacts = fixture_artifacts(tmp_path)
    detected_findings = list(artifacts["detect"].findings)  # type: ignore[attr-defined]
    if include_suppressed:
        detected_findings.append(
            detected_findings[0].model_copy(
                update={
                    "id": "finding-suppressed",
                    "suppressed": True,
                    "suppression_reason": "accepted risk",
                }
            )
        )
    return build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=detected_findings,
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.73,
        duration_s=8.5,
        stage_timings_s={
            "scan": 1.0,
            "detect": 1.0,
            "triage": 2.0,
            "verify": 2.0,
            "legal": 1.0,
            "patch": 1.0,
            "report": 0.5,
        },
    )


def _build_fake_registry(
    context: PipelineContext,
    *,
    artifacts: dict[str, Any],
) -> OrderedDict[str, PipelineStage]:
    def _runner(stage_name: str, artifact: Any) -> Any:
        def _run(config: Any, prev: Any) -> StageResult:
            _ = prev
            if stage_name == "report":
                report = build_report(
                    scan_result=artifacts["scan"],
                    detected_findings=artifacts["detect"].findings,
                    confirmed_findings=artifacts["verify"].findings,
                    legal_assessments=artifacts["legal"].assessments,
                    patch_results=artifacts["patch"].patches,
                    target_dir=context.target_dir,
                    total_llm_cost_usd=0.42,
                    duration_s=1.25,
                    stage_timings_s={"scan": 0.1, "detect": 0.1, "triage": 0.1},
                )
                write_report_outputs(
                    report,
                    context.output_dir,
                    report_format=config.output.format,
                )
                return StageResult(stage=stage_name, success=True, artifact=report, elapsed_s=0.05)
            return StageResult(stage=stage_name, success=True, artifact=artifact, elapsed_s=0.05)

        return _run

    return OrderedDict(
        (
            ("scan", PipelineStage("scan", ScanResult, _runner("scan", artifacts["scan"]))),
            (
                "detect",
                PipelineStage("detect", DetectArtifact, _runner("detect", artifacts["detect"])),
            ),
            (
                "triage",
                PipelineStage("triage", TriageArtifact, _runner("triage", artifacts["triage"])),
            ),
            (
                "verify",
                PipelineStage("verify", VerifyArtifact, _runner("verify", artifacts["verify"])),
            ),
            ("legal", PipelineStage("legal", LegalArtifact, _runner("legal", artifacts["legal"]))),
            ("patch", PipelineStage("patch", PatchArtifact, _runner("patch", artifacts["patch"]))),
            ("report", PipelineStage("report", PiranesiReport, _runner("report", None))),
        )
    )


def _validate_junit(payload: str) -> None:
    schema = xmlschema.XMLSchema(_JUNIT_SCHEMA_PATH)
    schema.validate(ET.fromstring(payload))  # noqa: S314 - payload is generated locally
