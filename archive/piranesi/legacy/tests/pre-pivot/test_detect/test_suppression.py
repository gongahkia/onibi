from __future__ import annotations

from collections.abc import Generator
from contextlib import contextmanager
from datetime import date
from pathlib import Path
from types import SimpleNamespace

import pytest

import piranesi.pipeline as pipeline_module
from piranesi.config import OutputConfig, PiranesiConfig
from piranesi.detect.suppression import (
    apply_suppressions,
    apply_suppressions_with_lifecycle,
    load_ignore_file,
    load_ignore_file_with_diagnostics,
    parse_inline_suppressions,
)
from piranesi.models import CandidateFinding, SourceLocation, TaintSink, TaintSource
from piranesi.pipeline import DetectArtifact, PipelineContext


def test_file_based_suppression_marks_matching_finding(tmp_path: Path) -> None:
    source_file = tmp_path / "src" / "admin" / "panel.ts"
    source_file.parent.mkdir(parents=True, exist_ok=True)
    source_file.write_text("res.send(input);\n", encoding="utf-8")
    (tmp_path / ".piranesi-ignore").write_text(
        (
            "suppressions:\n"
            "  - cwe: CWE-79\n"
            "    path: src/admin/**\n"
            '    reason: "accepted risk"\n'
            "    ticket: SEC-123\n"
        ),
        encoding="utf-8",
    )

    findings = apply_suppressions(
        [_candidate(source_file, cwe="CWE-79")],
        load_ignore_file(tmp_path),
        [],
    )

    assert findings[0].suppressed is True
    assert findings[0].suppression_reason == "accepted risk (ticket: SEC-123)"


def test_inline_suppression_matches_nearby_finding(tmp_path: Path) -> None:
    source_file = tmp_path / "src" / "routes" / "admin.ts"
    source_file.parent.mkdir(parents=True, exist_ok=True)
    source_file.write_text(
        (
            "const userInput = req.body.userInput;\n"
            '// piranesi:suppress CWE-79 reason:"admin-only endpoint"\n'
            "res.send(userInput);\n"
        ),
        encoding="utf-8",
    )

    findings = apply_suppressions(
        [_candidate(source_file, cwe="CWE-79", sink_line=3)],
        [],
        parse_inline_suppressions(source_file),
    )

    assert findings[0].suppressed is True
    assert findings[0].suppression_reason == "admin-only endpoint"


def test_detect_stage_applies_suppressions_after_extraction(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    target_dir = tmp_path / "target"
    source_file = target_dir / "src" / "routes" / "admin.ts"
    source_file.parent.mkdir(parents=True, exist_ok=True)
    source_file.write_text("res.send(userInput);\n", encoding="utf-8")
    (target_dir / ".piranesi-ignore").write_text(
        ('suppressions:\n  - id: finding-001\n    reason: "known false positive"\n'),
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
    config = PiranesiConfig(output=OutputConfig(output_dir=str(context.output_dir)))
    finding = _candidate(source_file, cwe="CWE-79")

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
        lambda *_args, **_kwargs: (finding,),
    )

    result = pipeline_module._run_detect_stage(context, config, None)

    assert isinstance(result.artifact, DetectArtifact)
    assert result.artifact.findings[0].suppressed is True
    assert result.artifact.findings[0].suppression_reason == "known false positive"
    assert result.artifact.suppression_lifecycle is not None
    assert result.artifact.suppression_lifecycle.active_rules == 1


def test_load_ignore_file_with_diagnostics_captures_invalid_entries(tmp_path: Path) -> None:
    (tmp_path / ".piranesi-ignore").write_text(
        (
            "suppressions:\n"
            '  - reason: "missing selector"\n'
            "  - id: finding-1\n"
            "    expires: not-a-date\n"
        ),
        encoding="utf-8",
    )

    validation = load_ignore_file_with_diagnostics(tmp_path)

    assert validation.rules == []
    assert len(validation.invalid_entries) == 2


def test_apply_suppressions_with_lifecycle_reports_expired_and_stale(tmp_path: Path) -> None:
    source_file = tmp_path / "src" / "app.ts"
    source_file.parent.mkdir(parents=True, exist_ok=True)
    source_file.write_text("res.send(input);\n", encoding="utf-8")
    (tmp_path / ".piranesi-ignore").write_text(
        (
            "suppressions:\n"
            "  - id: finding-001\n"
            '    reason: "accepted risk"\n'
            "    owner: appsec\n"
            "    created: 2026-04-01\n"
            "    expires: 2026-08-01\n"
            "  - cwe: CWE-79\n"
            '    path: "src/admin/**"\n'
            '    reason: "stale path"\n'
            "    expires: 2026-08-01\n"
            "  - cwe: CWE-79\n"
            '    path: "src/**"\n'
            '    reason: "expired rule"\n'
            "    expires: 2026-01-01\n"
        ),
        encoding="utf-8",
    )
    findings = [_candidate(source_file, cwe="CWE-79")]
    rules = load_ignore_file(tmp_path)

    outcome = apply_suppressions_with_lifecycle(findings, rules, [], today=date(2026, 4, 16))

    assert outcome.findings[0].suppressed is True
    assert outcome.lifecycle.total_rules == 3
    assert outcome.lifecycle.active_rules == 1
    assert outcome.lifecycle.expired_rules == 1
    assert outcome.lifecycle.stale_rules == 1


def test_detect_stage_fails_on_expired_suppression_when_configured(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    target_dir = tmp_path / "target"
    source_file = target_dir / "src" / "routes" / "admin.ts"
    source_file.parent.mkdir(parents=True, exist_ok=True)
    source_file.write_text("res.send(userInput);\n", encoding="utf-8")
    (target_dir / ".piranesi-ignore").write_text(
        (
            "suppressions:\n"
            "  - id: finding-001\n"
            '    reason: "old suppression"\n'
            "    expires: 2026-01-01\n"
        ),
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
    config = PiranesiConfig.model_validate(
        {
            "output": {"output_dir": str(context.output_dir)},
            "suppression": {
                "fail_on_invalid": True,
                "fail_on_expired": True,
                "fail_on_stale": False,
            },
        }
    )
    finding = _candidate(source_file, cwe="CWE-79")

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
        lambda *_args, **_kwargs: (finding,),
    )

    with pytest.raises(ValueError, match="expired suppression"):
        pipeline_module._run_detect_stage(context, config, None)


def _candidate(
    source_file: Path,
    *,
    cwe: str,
    sink_line: int = 1,
) -> CandidateFinding:
    location = SourceLocation(
        file=str(source_file),
        line=1,
        column=1,
        snippet="const userInput = req.body.userInput;",
    )
    sink_location = SourceLocation(
        file=str(source_file),
        line=sink_line,
        column=1,
        snippet="res.send(userInput);",
    )
    return CandidateFinding(
        id="finding-001",
        vuln_class=cwe,
        source=TaintSource(
            location=location,
            source_type="req.body.userInput",
            data_categories=["unknown"],
            parameter_name="userInput",
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type="response_write",
            api_name="res.send",
        ),
        taint_path=[],
        path_conditions=[],
        confidence=0.9,
        severity="medium",
    )
