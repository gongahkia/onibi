from __future__ import annotations

import json
from collections import OrderedDict
from pathlib import Path
from typing import Any

import jsonschema
import pytest
from tests._pipeline_fixtures import fixture_artifacts
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.models import ScanResult, SourceLocation, TaintStep
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
from piranesi.report import generate_sarif
from piranesi.report.renderer import PiranesiReport, build_report, write_report_outputs

runner = CliRunner()
_SCHEMA_PATH = (
    Path(__file__).resolve().parents[1] / "fixtures" / "report" / "sarif-schema-2.1.0.json"
)


def test_generate_sarif_validates_against_schema(tmp_path: Path) -> None:
    report = _build_report(tmp_path)

    sarif = generate_sarif(report)

    _validate_sarif(sarif)
    run = sarif["runs"][0]  # type: ignore[index]
    result = run["results"][0]
    rule = run["tool"]["driver"]["rules"][0]

    assert run["tool"]["driver"]["version"] == report.appendix.piranesi_version
    assert rule["id"] == "CWE-89"
    assert rule["helpUri"] == "https://cwe.mitre.org/data/definitions/89.html"
    assert "owasp-a03" in rule["properties"]["tags"]
    assert result["message"]["text"] == "' OR 1=1--"
    assert (
        result["fixes"][0]["artifactChanges"][0]["artifactLocation"]["uri"] == "src/routes/login.ts"
    )


def test_generate_sarif_preserves_taint_path_order(tmp_path: Path) -> None:
    report = _build_report(tmp_path)
    finding = report.findings[0].model_copy(
        update={
            "taint_path": [
                TaintStep(
                    location=_location(tmp_path, line=11, snippet="const body = req.body;"),
                    operation="read",
                    taint_state="tainted",
                ),
                TaintStep(
                    location=_location(
                        tmp_path, line=13, snippet="const username = body.username;"
                    ),
                    operation="assignment",
                    taint_state="tainted",
                    through_function="loginHandler",
                ),
                TaintStep(
                    location=_location(
                        tmp_path,
                        line=15,
                        snippet="return db.query(sql, [username]);",
                    ),
                    operation="call",
                    taint_state="tainted",
                ),
            ]
        }
    )
    report = report.model_copy(update={"findings": [finding]})

    sarif = generate_sarif(report)

    thread_flow_locations = sarif["runs"][0]["results"][0]["codeFlows"][0]["threadFlows"][0][  # type: ignore[index]
        "locations"
    ]
    assert [
        location["location"]["physicalLocation"]["region"]["startLine"]
        for location in thread_flow_locations
    ] == [11, 13, 15]


def test_generate_sarif_maps_all_severity_levels(tmp_path: Path) -> None:
    report = _build_report(tmp_path)
    base_finding = report.findings[0]
    report = report.model_copy(
        update={
            "findings": [
                base_finding.model_copy(
                    update={"finding_id": "finding-critical", "severity": "critical"}
                ),
                base_finding.model_copy(update={"finding_id": "finding-high", "severity": "high"}),
                base_finding.model_copy(
                    update={"finding_id": "finding-medium", "severity": "medium"}
                ),
                base_finding.model_copy(update={"finding_id": "finding-low", "severity": "low"}),
            ]
        }
    )

    sarif = generate_sarif(report)

    assert [result["level"] for result in sarif["runs"][0]["results"]] == [  # type: ignore[index]
        "error",
        "error",
        "warning",
        "note",
    ]


def test_generate_sarif_includes_regulatory_property_bags(tmp_path: Path) -> None:
    report = _build_report(tmp_path)

    sarif = generate_sarif(report)

    regulatory = sarif["runs"][0]["results"][0]["properties"]["regulatory"]  # type: ignore[index]
    assert regulatory["riskTier"] == "high"
    assert regulatory["memoMarkdown"] == "## Legal memo"
    assert regulatory["obligations"][0]["framework"] == "PDPA"
    assert regulatory["obligations"][0]["section"] == "Section 24"


def test_cli_run_writes_valid_sarif_report(
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
            "sarif",
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 1
    sarif_path = output_dir / "report.sarif.json"
    assert sarif_path.exists()
    _validate_sarif(json.loads(sarif_path.read_text(encoding="utf-8")))


def _build_report(tmp_path: Path) -> PiranesiReport:
    artifacts = fixture_artifacts(tmp_path)
    return build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=artifacts["detect"].findings,  # type: ignore[attr-defined]
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


def _location(target_dir: Path, *, line: int, snippet: str) -> SourceLocation:
    return SourceLocation(
        file=str(target_dir / "src" / "routes" / "login.ts"),
        line=line,
        column=1,
        snippet=snippet,
    )


def _validate_sarif(payload: dict[str, object]) -> None:
    schema = json.loads(_SCHEMA_PATH.read_text(encoding="utf-8"))
    validator_cls = jsonschema.validators.validator_for(schema)
    validator_cls.check_schema(schema)
    validator_cls(schema).validate(payload)
