from __future__ import annotations

from pathlib import Path
from types import SimpleNamespace

from piranesi.config import PiranesiConfig
from piranesi.models import (
    CandidateFinding,
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
    TriagedFinding,
)
from piranesi.pipeline import (
    PipelineContext,
    StageResult,
    TriageArtifact,
    VerifyArtifact,
    _run_verify_stage,
)
from piranesi.verify.constraints import extract_exploit_template
from piranesi.verify.preconditions import evaluate_verification_preconditions


def test_evaluate_preconditions_marks_target_url_missing_without_runtime_or_metadata(
    tmp_path: Path,
) -> None:
    app_file = tmp_path / "app.ts"
    app_file.write_text(
        "\n".join(
            [
                'app.get("/search", (req, res) => {',
                "  const q = req.query.q;",
                "  return res.send(q);",
                "});",
            ]
        ),
        encoding="utf-8",
    )
    finding = _candidate_finding(
        app_file,
        source_line=2,
        source_snippet="const q = req.query.q;",
        source_type="req.query.q",
        parameter_name="q",
    )
    template = extract_exploit_template(finding)

    runtime_dir = tmp_path / "runtime-no-package"
    runtime_dir.mkdir()
    evaluation = evaluate_verification_preconditions(
        finding=finding,
        template=template,
        target_dir=runtime_dir,
        no_execute=False,
    )

    by_key = {precondition.key: precondition for precondition in evaluation.preconditions}
    assert by_key["target_url"].status == "missing"
    assert by_key["runtime_service"].status == "missing"
    assert evaluation.skip_reason is not None
    assert "target_url" in evaluation.skip_reason


def test_evaluate_preconditions_marks_route_mapping_missing_when_endpoint_unknown(
    tmp_path: Path,
) -> None:
    app_file = tmp_path / "handler.ts"
    app_file.write_text(
        "\n".join(
            [
                "export const handler = (req, res) => {",
                "  const q = req.query.q;",
                "  return res.send(q);",
                "};",
            ]
        ),
        encoding="utf-8",
    )
    finding = _candidate_finding(
        app_file,
        source_line=2,
        source_snippet="const q = req.query.q;",
        source_type="req.query.q",
        parameter_name="q",
    )
    template = extract_exploit_template(finding)

    runtime_dir = tmp_path / "runtime"
    runtime_dir.mkdir()
    (runtime_dir / "package.json").write_text(
        '{"name":"app","scripts":{"start":"node index.js"}}',
        encoding="utf-8",
    )
    evaluation = evaluate_verification_preconditions(
        finding=finding,
        template=template,
        target_dir=runtime_dir,
        no_execute=False,
    )

    by_key = {precondition.key: precondition for precondition in evaluation.preconditions}
    assert by_key["route_mapping"].status == "missing"
    assert evaluation.skip_reason is not None
    assert "route_mapping" in evaluation.skip_reason


def test_run_verify_stage_records_no_execute_skip_reason(tmp_path: Path) -> None:
    target_dir = tmp_path / "target"
    target_dir.mkdir()
    (target_dir / "package.json").write_text(
        '{"name":"verify-target","scripts":{"start":"node index.js"}}',
        encoding="utf-8",
    )
    app_file = target_dir / "app.ts"
    app_file.write_text(
        "\n".join(
            [
                'app.get("/search", (req, res) => {',
                "  const q = req.query.q;",
                "  return res.send(q);",
                "});",
            ]
        ),
        encoding="utf-8",
    )

    triaged = TriagedFinding(
        finding=_candidate_finding(
            app_file,
            source_line=2,
            source_snippet="const q = req.query.q;",
            source_type="req.query.q",
            parameter_name="q",
        ),
        triage_verdict="true_positive",
        triage_mode="deterministic",
        skeptic_analysis="deterministic",
        ensemble_score=0.9,
        escalated=False,
    )
    prev_result = StageResult(
        stage="triage",
        success=True,
        artifact=TriageArtifact(findings=[triaged]),
        elapsed_s=0.0,
    )
    context = PipelineContext(
        target_dir=target_dir,
        output_dir=tmp_path / "out",
        provider=None,  # type: ignore[arg-type]
        router=None,
        cost_tracker=SimpleNamespace(total_usd=0.0),
        trace_writer=None,  # type: ignore[arg-type]
        no_execute=True,
        use_cache=False,
    )

    result = _run_verify_stage(context, PiranesiConfig(), prev_result)

    assert isinstance(result.artifact, VerifyArtifact)
    assert result.artifact.findings == []
    assert len(result.artifact.attempts) == 1
    attempt = result.artifact.attempts[0]
    assert attempt.status == "skipped"
    assert "--no-execute" in attempt.reason
    by_key = {precondition.key: precondition for precondition in attempt.preconditions}
    assert by_key["proof_mode"].status == "user_provided"
    assert by_key["proof_mode"].value == "no_execute"


def _candidate_finding(
    file_path: Path,
    *,
    source_line: int,
    source_snippet: str,
    source_type: str,
    parameter_name: str,
) -> CandidateFinding:
    source_location = SourceLocation(
        file=str(file_path),
        line=source_line,
        column=11,
        snippet=source_snippet,
    )
    sink_location = SourceLocation(
        file=str(file_path),
        line=source_line + 1,
        column=9,
        snippet="res.send(q);",
    )
    step_location = SourceLocation(
        file=str(file_path),
        line=source_line + 1,
        column=9,
        snippet="return res.send(q);",
    )
    return CandidateFinding(
        id=f"finding-{file_path.stem}-{source_line}",
        vuln_class="CWE-79",
        source=TaintSource(
            location=source_location,
            source_type=source_type,
            data_categories=["identifier"],
            parameter_name=parameter_name,
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type="html_output",
            api_name="res.send",
        ),
        taint_path=[
            TaintStep(
                location=step_location,
                operation="call_arg",
                taint_state="tainted",
            )
        ],
        path_conditions=[],
        confidence=0.9,
        severity="medium",
    )
