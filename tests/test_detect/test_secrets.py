from __future__ import annotations

import shutil
from collections.abc import Generator
from contextlib import contextmanager
from pathlib import Path
from types import SimpleNamespace

import pytest

import piranesi.pipeline as pipeline_module
from piranesi.config import OutputConfig, PiranesiConfig, ScanConfig
from piranesi.detect.secrets import extract_secret_findings, shannon_entropy
from piranesi.pipeline import DetectArtifact, PipelineContext

FIXTURE_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "secrets"


def test_extract_secret_findings_detects_known_secret_patterns(tmp_path: Path) -> None:
    target_dir = tmp_path / "app"
    shutil.copytree(FIXTURE_DIR, target_dir)

    findings = extract_secret_findings(target_dir)

    assert isinstance(findings, tuple)
    findings_by_kind = {finding.sink.api_name: finding for finding in findings}
    assert set(findings_by_kind) == {
        "aws_access_key",
        "stripe_secret_key",
        "github_token",
        "slack_token",
        "sendgrid_api_key",
        "pem_private_key",
    }
    assert all(finding.vuln_class == "CWE-798" for finding in findings)
    assert findings_by_kind["aws_access_key"].severity == "critical"
    assert findings_by_kind["pem_private_key"].severity == "critical"
    assert findings_by_kind["stripe_secret_key"].severity == "high"
    assert "[REDACTED_SECRET]" in findings_by_kind["aws_access_key"].source.location.snippet
    assert "AWS_ACCESS_KEY_REDACTED" not in findings_by_kind["aws_access_key"].source.location.snippet


def test_extract_secret_findings_flags_high_entropy_strings(tmp_path: Path) -> None:
    source_file = tmp_path / "src" / "config.ts"
    source_file.parent.mkdir(parents=True, exist_ok=True)
    source_file.write_text(
        'const token = "0123456789abcdefghijklmnopqrstuvwxyzABCDEF";\n',
        encoding="utf-8",
    )

    findings = extract_secret_findings(tmp_path)
    entropy_findings = [
        finding for finding in findings if finding.sink.api_name == "high_entropy_string"
    ]

    assert len(entropy_findings) == 1
    assert entropy_findings[0].severity == "high"
    assert shannon_entropy("0123456789abcdefghijklmnopqrstuvwxyzABCDEF") > 4.5
    assert shannon_entropy("aaaaaaaaaaaaaaaaaaaaaaaaaaaa") == 0.0


def test_extract_secret_findings_skips_excluded_paths_and_test_files(tmp_path: Path) -> None:
    excluded_files = {
        tmp_path / "node_modules" / "pkg" / "index.js": 'const key = "AWS_ACCESS_KEY_REDACTED";\n',
        tmp_path / "vendor" / "secrets.txt": 'token="STRIPE_API_KEY_REDACTED"\n',
        tmp_path / ".git" / "config": 'token="GITHUB_TOKEN_REDACTED"\n',
        tmp_path / ".env.example": "SLACK=SLACK_TOKEN_REDACTED\n",
        tmp_path / "src" / "auth.test.ts": (
            "const sendgrid = "
            '"SENDGRID_API_KEY_REDACTED";\n'
        ),
    }
    for path, content in excluded_files.items():
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

    assert extract_secret_findings(tmp_path) == ()


def test_extract_secret_findings_includes_tests_when_requested(tmp_path: Path) -> None:
    source_file = tmp_path / "src" / "auth.spec.ts"
    source_file.parent.mkdir(parents=True, exist_ok=True)
    source_file.write_text(
        'const token = "GITHUB_TOKEN_REDACTED";\n',
        encoding="utf-8",
    )

    findings = extract_secret_findings(tmp_path, include_tests=True)

    assert [finding.sink.api_name for finding in findings] == ["github_token"]


def test_detect_stage_includes_secret_findings(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    target_dir = tmp_path / "target"
    source_file = target_dir / "src" / "secrets.ts"
    source_file.parent.mkdir(parents=True, exist_ok=True)
    source_file.write_text('const awsAccessKey = "AWS_ACCESS_KEY_REDACTED";\n', encoding="utf-8")

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

    @contextmanager
    def _fake_scan_session(
        *_args: object,
        **_kwargs: object,
    ) -> Generator[tuple[None, SimpleNamespace], None, None]:
        yield None, SimpleNamespace(joern_project_root=target_dir, source_map=None)

    monkeypatch.setattr(pipeline_module, "_scan_session", _fake_scan_session)
    monkeypatch.setattr(
        pipeline_module,
        "extract_candidate_findings",
        lambda *_args, **_kwargs: (),
    )

    result = pipeline_module._run_detect_stage(context, config, None)

    assert isinstance(result.artifact, DetectArtifact)
    assert [finding.sink.api_name for finding in result.artifact.findings] == ["aws_access_key"]
