from __future__ import annotations

import json
from pathlib import Path
from types import SimpleNamespace

import pytest

import piranesi.verify.sandbox as sandbox
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
        proof_mode="safe",
        target_profile_name=None,
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
        proof_mode="safe",
        target_profile_name=None,
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
    assert by_key["proof_mode"].value == "safe:no_execute"


def test_run_verify_stage_raises_for_unknown_target_profile(tmp_path: Path) -> None:
    target_dir = tmp_path / "target"
    target_dir.mkdir()
    prev_result = StageResult(
        stage="triage",
        success=True,
        artifact=TriageArtifact(findings=[]),
        elapsed_s=0.0,
    )
    context = PipelineContext(
        target_dir=target_dir,
        output_dir=tmp_path / "out",
        provider=None,  # type: ignore[arg-type]
        router=None,
        cost_tracker=SimpleNamespace(total_usd=0.0),
        trace_writer=None,  # type: ignore[arg-type]
        no_execute=False,
        use_cache=False,
    )
    config = PiranesiConfig.model_validate(
        {
            "verify": {
                "target_profile": "missing-profile",
                "target_profiles": {},
            }
        }
    )

    with pytest.raises(ValueError, match="missing-profile"):
        _run_verify_stage(context, config, prev_result)


def test_run_verify_stage_applies_target_profile_and_captures_startup_failure(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    target_dir = tmp_path / "target"
    target_dir.mkdir()
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
        no_execute=False,
        use_cache=False,
    )
    config = PiranesiConfig.model_validate(
        {
            "verify": {
                "target_profile": "local-express",
                "target_profiles": {
                    "local-express": {"base_url": "http://127.0.0.1:4010"},
                },
            }
        }
    )

    payload = sandbox.SynthesizedPayload(
        method="GET",
        url="/search",
        body={"q": "<script>alert(1)</script>"},
        payload_values={"q": "<script>alert(1)</script>"},
        encoding="query",
    )
    solve_result = SimpleNamespace(
        status="SAT",
        reason=None,
        solutions=(
            SimpleNamespace(
                payload=payload,
                model_values={"q": "<script>alert(1)</script>"},
            ),
        ),
    )

    monkeypatch.setattr(
        "piranesi.pipeline.solve_exploit_template",
        lambda *_args, **_kwargs: solve_result,
    )

    observed: dict[str, object] = {}
    launch_log_path = str(tmp_path / "verify-launch.log")

    def _fake_run_in_sandbox(
        target_path: str,
        payloads: list[sandbox.SynthesizedPayload],
        *,
        target_profile: object = None,
        logs_base_dir: Path | None = None,
    ) -> list[sandbox.SandboxCapture]:
        _ = (target_path, payloads, logs_base_dir)
        observed["profile"] = None if target_profile is None else target_profile.name
        return [
            sandbox.SandboxCapture.app_not_ready(
                startup_error="TARGET_PROFILE_PROCESS_EXITED(1)",
                launch_profile="local-express",
                launch_log_path=launch_log_path,
            ),
            sandbox.SandboxCapture.app_not_ready(
                startup_error="TARGET_PROFILE_PROCESS_EXITED(1)",
                launch_profile="local-express",
                launch_log_path=launch_log_path,
            ),
        ]

    monkeypatch.setattr("piranesi.pipeline.run_in_sandbox", _fake_run_in_sandbox)

    result = _run_verify_stage(context, config, prev_result)

    assert isinstance(result.artifact, VerifyArtifact)
    assert result.artifact.findings == []
    assert len(result.artifact.attempts) == 1
    attempt = result.artifact.attempts[0]
    assert attempt.status == "inconclusive"
    assert attempt.target_profile == "local-express"
    assert attempt.startup_error == "TARGET_PROFILE_PROCESS_EXITED(1)"
    assert attempt.launch_log_path == launch_log_path
    assert "sandbox capture error" in attempt.reason
    assert f"launch_logs:{launch_log_path}" in attempt.evidence
    assert attempt.rich_evidence is not None
    assert attempt.rich_evidence.template_id == "reflected-xss-probe"
    assert attempt.rich_evidence.attempted_route == "/search"
    assert attempt.rich_evidence.redaction_status.applied is False
    assert attempt.evidence_artifact_path is not None
    assert Path(attempt.evidence_artifact_path).exists()
    assert observed["profile"] == "local-express"


def test_run_verify_stage_uses_metadata_target_url_profile_when_no_config_profile(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    target_dir = tmp_path / "target"
    target_dir.mkdir()
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
            metadata={"verification_target_url": "http://127.0.0.1:9100"},
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
        no_execute=False,
        use_cache=False,
    )

    payload = sandbox.SynthesizedPayload(
        method="GET",
        url="/search",
        body={"q": "<script>alert(1)</script>"},
        payload_values={"q": "<script>alert(1)</script>"},
        encoding="query",
    )
    solve_result = SimpleNamespace(
        status="SAT",
        reason=None,
        solutions=(
            SimpleNamespace(
                payload=payload,
                model_values={"q": "<script>alert(1)</script>"},
            ),
        ),
    )
    monkeypatch.setattr(
        "piranesi.pipeline.solve_exploit_template",
        lambda *_args, **_kwargs: solve_result,
    )

    observed: dict[str, object] = {}

    def _fake_run_in_sandbox(
        target_path: str,
        payloads: list[sandbox.SynthesizedPayload],
        *,
        target_profile: object = None,
        logs_base_dir: Path | None = None,
    ) -> list[sandbox.SandboxCapture]:
        _ = (target_path, payloads, logs_base_dir)
        observed["profile_name"] = None if target_profile is None else target_profile.name
        observed["base_url"] = None if target_profile is None else target_profile.base_url
        return [
            sandbox.SandboxCapture.app_not_ready(
                startup_error="TARGET_PROFILE_READINESS_TIMEOUT",
                launch_profile="metadata_target_url",
            ),
            sandbox.SandboxCapture.app_not_ready(
                startup_error="TARGET_PROFILE_READINESS_TIMEOUT",
                launch_profile="metadata_target_url",
            ),
        ]

    monkeypatch.setattr("piranesi.pipeline.run_in_sandbox", _fake_run_in_sandbox)

    result = _run_verify_stage(context, PiranesiConfig(), prev_result)

    assert isinstance(result.artifact, VerifyArtifact)
    assert len(result.artifact.attempts) == 1
    attempt = result.artifact.attempts[0]
    assert attempt.target_profile == "metadata_target_url"
    assert attempt.startup_error == "TARGET_PROFILE_READINESS_TIMEOUT"
    assert observed["profile_name"] == "metadata_target_url"
    assert observed["base_url"] == "http://127.0.0.1:9100"


def test_run_verify_stage_redacts_sensitive_values_in_rich_evidence(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    target_dir = tmp_path / "target"
    target_dir.mkdir()
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
            metadata={
                "verification_target_url": "http://127.0.0.1:9100",
                "verification_auth_header": "Bearer super-secret-token",
                "verification_cookie": "sid=abc123",
                "api_secret": "super-secret-token",
            },
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
        no_execute=False,
        use_cache=False,
    )

    payload = sandbox.SynthesizedPayload(
        method="GET",
        url="/search?token=super-secret-token",
        headers={
            "Authorization": "Bearer super-secret-token",
            "Cookie": "sid=abc123",
        },
        body={"q": "super-secret-token"},
        payload_values={"q": "super-secret-token"},
        encoding="query",
    )
    solve_result = SimpleNamespace(
        status="SAT",
        reason=None,
        solutions=(
            SimpleNamespace(
                payload=payload,
                model_values={"q": "super-secret-token"},
            ),
        ),
    )
    monkeypatch.setattr(
        "piranesi.pipeline.solve_exploit_template",
        lambda *_args, **_kwargs: solve_result,
    )

    baseline_response = sandbox.ExploitResult(
        status_code=200,
        headers={"content-type": "text/html"},
        body="baseline",
        elapsed_ms=15.0,
        request={"method": "GET", "url": "/search"},
    )
    exploit_response = sandbox.ExploitResult(
        status_code=200,
        headers={
            "content-type": "text/html",
            "set-cookie": "sid=abc123",
        },
        body="Authorization: Bearer super-secret-token; cookie=sid=abc123",
        elapsed_ms=21.0,
        request={"method": "GET", "url": "/search"},
    )
    screenshot_path = str(tmp_path / "verify-screenshot.png")

    def _fake_run_in_sandbox(
        target_path: str,
        payloads: list[sandbox.SynthesizedPayload],
        *,
        target_profile: object = None,
        logs_base_dir: Path | None = None,
    ) -> list[sandbox.SandboxCapture]:
        _ = (target_path, payloads, target_profile, logs_base_dir)
        return [
            sandbox.SandboxCapture(
                http_response=baseline_response,
                container_logs="",
                filesystem_diff=[],
                timing_ms=34.0,
            ),
            sandbox.SandboxCapture(
                http_response=exploit_response,
                container_logs="",
                filesystem_diff=[],
                timing_ms=42.0,
                side_effects=[screenshot_path],
            ),
        ]

    monkeypatch.setattr("piranesi.pipeline.run_in_sandbox", _fake_run_in_sandbox)
    monkeypatch.setattr(
        "piranesi.pipeline.confirm_responses",
        lambda *_args, **_kwargs: SimpleNamespace(
            level="LIKELY",
            evidence="authorization=Bearer super-secret-token cookie=sid=abc123",
        ),
    )

    result = _run_verify_stage(context, PiranesiConfig(), prev_result)

    assert isinstance(result.artifact, VerifyArtifact)
    assert result.artifact.findings == []
    assert len(result.artifact.attempts) == 1
    attempt = result.artifact.attempts[0]
    assert attempt.status == "inconclusive"
    assert "super-secret-token" not in attempt.reason
    assert "sid=abc123" not in attempt.reason
    assert "[REDACTED]" in attempt.reason
    assert attempt.rich_evidence is not None
    assert attempt.rich_evidence.redaction_status.applied is True
    assert attempt.rich_evidence.redaction_status.redacted_value_count > 0
    assert attempt.rich_evidence.body_excerpt.preview is not None
    assert "[REDACTED]" in attempt.rich_evidence.body_excerpt.preview
    assert attempt.rich_evidence.screenshot_paths == [screenshot_path]
    assert attempt.evidence_artifact_path is not None

    artifact_path = Path(attempt.evidence_artifact_path)
    assert artifact_path.exists()
    payload_json = json.loads(artifact_path.read_text(encoding="utf-8"))
    serialized = json.dumps(payload_json)
    assert "super-secret-token" not in serialized
    assert "sid=abc123" not in serialized
    assert "[REDACTED]" in serialized


def _candidate_finding(
    file_path: Path,
    *,
    source_line: int,
    source_snippet: str,
    source_type: str,
    parameter_name: str,
    metadata: dict[str, object] | None = None,
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
        metadata={} if metadata is None else dict(metadata),
    )
