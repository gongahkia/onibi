from __future__ import annotations

from collections.abc import Generator
from contextlib import contextmanager
from pathlib import Path
from types import SimpleNamespace

import pytest

import piranesi.pipeline as pipeline_module
from piranesi.config import OutputConfig, PiranesiConfig
from piranesi.detect.sanitizer_discovery import (
    DEFAULT_DISCOVERED_CONFIDENCE,
    discover_custom_sanitizers,
)
from piranesi.pipeline import PipelineContext
from piranesi.scan.specs import SanitizerKind, SanitizerSpec


def _write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content)


def test_discovers_js_sanitizers(tmp_path: Path) -> None:
    _write(
        tmp_path / "a.js",
        "function sanitizeHtml(x) { return x.replace(/</g, '&lt;'); }\n"
        "const escapeShell = (s) => s.replace(/'/g, \"\\\\'\");\n"
        "function unrelated() { return 1; }\n",
    )
    specs = {s.name: s for s in discover_custom_sanitizers(tmp_path)}
    assert "discovered_sanitizeHtml" in specs
    assert specs["discovered_sanitizeHtml"].kind == SanitizerKind.SANITIZE
    assert specs["discovered_sanitizeHtml"].mitigates == ("CWE-79",)
    assert "discovered_escapeShell" in specs
    assert specs["discovered_escapeShell"].mitigates == ("CWE-78",)
    assert "discovered_unrelated" not in specs


def test_discovers_python_sanitizers(tmp_path: Path) -> None:
    _write(
        tmp_path / "b.py",
        'def sanitize_sql(x): return x.replace("\'", "\'\'")\n'
        "def validate_url(u): return u.startswith('http')\n"
        "def unrelated(): pass\n",
    )
    specs = {s.name: s for s in discover_custom_sanitizers(tmp_path)}
    assert specs["discovered_sanitize_sql"].mitigates == ("CWE-89",)
    assert "CWE-601" in specs["discovered_validate_url"].mitigates
    assert "discovered_unrelated" not in specs


def test_confidence_and_blocks_flow(tmp_path: Path) -> None:
    _write(tmp_path / "c.js", "function sanitizeInput(x){ return x; }\n")
    specs = discover_custom_sanitizers(tmp_path)
    assert len(specs) == 1
    spec = specs[0]
    assert spec.confidence == DEFAULT_DISCOVERED_CONFIDENCE
    assert spec.blocks_flow is False


def test_skips_excluded_dirs(tmp_path: Path) -> None:
    _write(tmp_path / "node_modules" / "x.js", "function sanitizeHtml(x){return x;}\n")
    _write(tmp_path / "src" / "y.js", "function escapeHtml(x){return x;}\n")
    names = {s.name for s in discover_custom_sanitizers(tmp_path)}
    assert "discovered_escapeHtml" in names
    assert "discovered_sanitizeHtml" not in names


def test_detect_stage_merges_discovered_sanitizers(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    target_dir = tmp_path / "project"
    _write(
        target_dir / "src" / "app.js",
        "function escapeHtml(x) { return String(x).replace(/</g, '&lt;'); }\n"
        "app.get('/profile', (req, res) => res.send(escapeHtml(req.query.name)));\n",
    )
    builtin = SanitizerSpec(
        name="builtin_html_escape",
        pattern=r"\bhtmlEscape\s*\(",
        kind=SanitizerKind.ESCAPE,
        mitigates=("CWE-79",),
    )
    captured: dict[str, tuple[SanitizerSpec, ...]] = {}

    context = PipelineContext(
        target_dir=target_dir,
        output_dir=tmp_path / "out",
        provider=None,  # type: ignore[arg-type]
        router=None,  # type: ignore[arg-type]
        cost_tracker=SimpleNamespace(total_usd=0.0),  # type: ignore[arg-type]
        trace_writer=None,  # type: ignore[arg-type]
    )
    config = PiranesiConfig(output=OutputConfig(output_dir=str(context.output_dir)))

    monkeypatch.setattr(pipeline_module, "resolve_frameworks", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "discover_rule_plugins", lambda **_kwargs: ())
    monkeypatch.setattr(pipeline_module, "get_source_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sink_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(
        pipeline_module,
        "get_sanitizer_specs",
        lambda *_args, **_kwargs: (builtin,),
    )

    @contextmanager
    def _fake_scan_session(
        *_args: object,
        **_kwargs: object,
    ) -> Generator[tuple[None, SimpleNamespace], None, None]:
        yield None, SimpleNamespace(joern_project_root=target_dir, source_map=None)

    def _capture_sanitizer_specs(
        *_args: object,
        sanitizer_specs: tuple[SanitizerSpec, ...],
        **_kwargs: object,
    ) -> tuple[object, ...]:
        captured["sanitizer_specs"] = tuple(sanitizer_specs)
        return ()

    monkeypatch.setattr(pipeline_module, "_scan_session", _fake_scan_session)
    monkeypatch.setattr(pipeline_module, "extract_candidate_findings", _capture_sanitizer_specs)
    monkeypatch.setattr(pipeline_module, "execute_custom_rules", lambda *_args, **_kwargs: ())
    monkeypatch.setattr(
        pipeline_module,
        "extract_crypto_transport_findings",
        lambda *_args, **_kwargs: (),
    )
    monkeypatch.setattr(pipeline_module, "extract_secret_findings", lambda *_args, **_kwargs: ())
    monkeypatch.setattr(
        pipeline_module,
        "extract_misconfiguration_findings",
        lambda *_args, **_kwargs: (),
    )
    monkeypatch.setattr(pipeline_module, "extract_redos_findings", lambda *_args, **_kwargs: ())
    monkeypatch.setattr(
        pipeline_module,
        "extract_auth_access_findings",
        lambda *_args, **_kwargs: (),
    )
    monkeypatch.setattr(
        pipeline_module,
        "scan_dependency_findings",
        lambda *_args, **_kwargs: SimpleNamespace(findings=(), sbom_artifacts={}),
    )

    pipeline_module._detect_findings_for_target(context, config, target_dir)

    sanitizer_names = [spec.name for spec in captured["sanitizer_specs"]]
    assert sanitizer_names[0] == "builtin_html_escape"
    assert sanitizer_names[-1] == "discovered_escapeHtml"


def test_detect_stage_disables_category_llm_without_api_key(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    target_dir = tmp_path / "project"
    _write(
        target_dir / "src" / "app.js",
        "app.get('/profile', (req, res) => res.send(req.query.name));\n",
    )

    captured: dict[str, tuple[object | None, object | None]] = {}

    context = PipelineContext(
        target_dir=target_dir,
        output_dir=tmp_path / "out",
        provider=SimpleNamespace(),  # type: ignore[arg-type]
        router=SimpleNamespace(resolve=lambda _stage: "gpt-4o-mini"),  # type: ignore[arg-type]
        cost_tracker=SimpleNamespace(total_usd=0.0),  # type: ignore[arg-type]
        trace_writer=None,  # type: ignore[arg-type]
    )
    config = PiranesiConfig(output=OutputConfig(output_dir=str(context.output_dir)))

    monkeypatch.setattr(pipeline_module, "_llm_is_configured", lambda: False)
    monkeypatch.setattr(pipeline_module, "resolve_frameworks", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "discover_rule_plugins", lambda **_kwargs: ())
    monkeypatch.setattr(pipeline_module, "get_source_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sink_specs", lambda *_args, **_kwargs: [])
    monkeypatch.setattr(pipeline_module, "get_sanitizer_specs", lambda *_args, **_kwargs: [])

    @contextmanager
    def _fake_scan_session(
        *_args: object,
        **_kwargs: object,
    ) -> Generator[tuple[None, SimpleNamespace], None, None]:
        yield None, SimpleNamespace(joern_project_root=target_dir, source_map=None)

    def _capture_extract(
        *_args: object,
        category_provider: object | None = None,
        category_model: object | None = None,
        **_kwargs: object,
    ) -> tuple[()]:
        captured["extract"] = (category_provider, category_model)
        return ()

    def _capture_custom(
        *_args: object,
        category_provider: object | None = None,
        category_model: object | None = None,
        **_kwargs: object,
    ) -> tuple[()]:
        captured["custom"] = (category_provider, category_model)
        return ()

    monkeypatch.setattr(pipeline_module, "_scan_session", _fake_scan_session)
    monkeypatch.setattr(pipeline_module, "extract_candidate_findings", _capture_extract)
    monkeypatch.setattr(pipeline_module, "execute_custom_rules", _capture_custom)
    monkeypatch.setattr(
        pipeline_module,
        "extract_crypto_transport_findings",
        lambda *_args, **_kwargs: (),
    )
    monkeypatch.setattr(pipeline_module, "extract_secret_findings", lambda *_args, **_kwargs: ())
    monkeypatch.setattr(
        pipeline_module,
        "extract_misconfiguration_findings",
        lambda *_args, **_kwargs: (),
    )
    monkeypatch.setattr(pipeline_module, "extract_redos_findings", lambda *_args, **_kwargs: ())
    monkeypatch.setattr(
        pipeline_module,
        "extract_auth_access_findings",
        lambda *_args, **_kwargs: (),
    )
    monkeypatch.setattr(
        pipeline_module,
        "scan_dependency_findings",
        lambda *_args, **_kwargs: SimpleNamespace(findings=(), sbom_artifacts={}),
    )

    pipeline_module._detect_findings_for_target(context, config, target_dir)

    assert captured["extract"] == (None, None)
    assert captured["custom"] == (None, None)
