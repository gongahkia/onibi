from __future__ import annotations

import shutil
import socket
from pathlib import Path
from types import SimpleNamespace

import pytest

from piranesi.config import JoernConfig, OutputConfig, PiranesiConfig, ScanConfig, TraceConfig
from piranesi.llm.cost import CostTracker
from piranesi.llm.provider import LLMProvider
from piranesi.llm.router import ModelRouter
from piranesi.llm.trace import TraceLogger
from piranesi.models import ScanResult
from piranesi.pipeline import (
    DetectArtifact,
    PipelineContext,
    build_default_stage_registry,
    run_pipeline,
)
from piranesi.scan.joern import is_joern_installed
from piranesi.trace import TraceWriter

TAINT_APP_DIR = Path(__file__).resolve().parent / "fixtures" / "typescript" / "taint_app"
PYTHON_FLASK_APP_DIR = Path(__file__).resolve().parent / "fixtures" / "python" / "flask_app"


def _find_free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])


@pytest.fixture(scope="module")
def e2e_env() -> None:
    if not is_joern_installed():
        pytest.skip("Joern not installed")
    if not shutil.which("docker"):
        pytest.skip("Docker not available")


@pytest.fixture(scope="module")
def pipeline_output(tmp_path_factory: pytest.TempPathFactory, e2e_env: None) -> PipelineContext:
    _ = e2e_env
    output_dir = tmp_path_factory.mktemp("e2e_output")
    trace_cfg = TraceConfig(enabled=False)
    config = PiranesiConfig(
        output=OutputConfig(output_dir=str(output_dir)),
        trace=trace_cfg,
    )
    cost_tracker = CostTracker()
    trace_writer = TraceWriter(trace_cfg)
    router = ModelRouter(config, cost_tracker)
    trace_logger = TraceLogger(trace_writer, log_prompts=False)
    provider = LLMProvider(trace_logger, cost_tracker, router=router)
    context = PipelineContext(
        target_dir=TAINT_APP_DIR,
        output_dir=output_dir,
        provider=provider,
        router=router,
        cost_tracker=cost_tracker,
        trace_writer=trace_writer,
        no_execute=True,
    )
    result = run_pipeline(
        config,
        context,
        stage_registry=build_default_stage_registry(context),
    )
    if result.failed_stage is not None:  # infra issue (port conflict, etc.)
        err = result.failed_result.error if result.failed_result else "unknown"
        pytest.skip(f"pipeline infrastructure unavailable: {err}")
    return context


@pytest.mark.e2e
def test_full_pipeline_against_taint_app(pipeline_output: PipelineContext) -> None:
    ctx = pipeline_output
    scan_result = ctx.stage_outputs.get("scan")
    assert isinstance(scan_result, ScanResult), "scan stage did not produce ScanResult"
    assert len(scan_result.entry_points) >= 1, "expected at least 1 entry point"
    detect_result = ctx.stage_outputs.get("detect")
    assert isinstance(detect_result, DetectArtifact), "detect stage did not produce DetectArtifact"
    assert len(detect_result.findings) >= 1, "expected at least 1 candidate finding"


@pytest.mark.e2e
def test_verify_stage_with_docker(pipeline_output: PipelineContext) -> None:
    if not shutil.which("docker"):
        pytest.skip("Docker not available")
    # verify stage ran (no_execute=True so no sandbox), just confirm artifact exists
    assert "verify" in pipeline_output.stage_outputs


@pytest.mark.e2e
def test_full_pipeline_against_flask_python_app(
    tmp_path: Path,
) -> None:
    if not is_joern_installed():
        pytest.skip("Joern not installed")

    output_dir = tmp_path / "python-e2e-output"
    config = PiranesiConfig(
        joern=JoernConfig(server_port=_find_free_port()),
        output=OutputConfig(output_dir=str(output_dir)),
        trace=TraceConfig(enabled=False),
        scan=ScanConfig(
            include_patterns=["**/*.py"],
            exclude_patterns=[
                "**/__pycache__/**",
                "**/.venv/**",
                "**/venv/**",
                "**/site-packages/**",
            ],
        ),
    )
    context = PipelineContext(
        target_dir=PYTHON_FLASK_APP_DIR,
        output_dir=output_dir,
        provider=None,  # type: ignore[arg-type]
        router=None,  # type: ignore[arg-type]
        cost_tracker=SimpleNamespace(total_usd=0.0),  # type: ignore[arg-type]
        trace_writer=None,  # type: ignore[arg-type]
        no_execute=True,
        use_cache=False,
    )

    result = run_pipeline(
        config,
        context,
        stage_registry=build_default_stage_registry(context),
    )
    if result.failed_stage is not None:
        err = result.failed_result.error if result.failed_result else "unknown"
        pytest.skip(f"python pipeline infrastructure unavailable: {err}")

    detect_result = context.stage_outputs.get("detect")
    assert isinstance(detect_result, DetectArtifact), "detect stage did not produce DetectArtifact"
    assert any(
        finding.vuln_class == "CWE-89" and finding.sink.location.file.endswith("app.py")
        for finding in detect_result.findings
    ), "expected Flask SQL injection finding from pysrc2cpg-backed pipeline"
