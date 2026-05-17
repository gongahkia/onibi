from __future__ import annotations

from pathlib import Path

import pytest

types = pytest.importorskip("lsprotocol.types")
uris = pytest.importorskip("pygls.uris")
from_fs_path = uris.from_fs_path

from piranesi.lsp.server import (  # noqa: E402
    IncrementalPipelineScanner,
    create_server,
    finding_to_diagnostic,
)
from piranesi.models import SourceLocation, TaintSink, TaintSource, TaintStep  # noqa: E402
from piranesi.models.finding import CandidateFinding  # noqa: E402


class _FakeScanner:
    def __init__(self, findings: list[CandidateFinding]) -> None:
        self.findings = findings
        self.calls: list[str] = []

    def scan_uri(self, uri: str) -> list[CandidateFinding]:
        self.calls.append(uri)
        return list(self.findings)


def test_did_save_publishes_diagnostics(tmp_path: Path) -> None:
    config_path = tmp_path / "piranesi.toml"
    config_path.write_text("[lsp]\ndebounce_ms = 0\n", encoding="utf-8")

    source_file = tmp_path / "src" / "app.ts"
    source_file.parent.mkdir(parents=True, exist_ok=True)
    source_file.write_text("res.send(req.body.name)\n", encoding="utf-8")
    uri = from_fs_path(str(source_file.resolve(strict=False)))

    scanner = _FakeScanner([_finding_for_path(source_file)])
    server = create_server(config_path=config_path, scanner=scanner)
    notifications: list[tuple[str, object]] = []
    server.protocol.notify = lambda method, params=None: notifications.append((method, params))

    server.handle_did_open(
        types.DidOpenTextDocumentParams(
            text_document=types.TextDocumentItem(
                uri=uri,
                language_id="typescript",
                version=1,
                text=source_file.read_text(encoding="utf-8"),
            )
        )
    )
    notifications.clear()

    server.handle_did_save(
        types.DidSaveTextDocumentParams(
            text_document=types.TextDocumentIdentifier(uri=uri),
        )
    )

    assert scanner.calls == [uri]
    assert len(notifications) == 1
    method, params = notifications[0]
    assert method == types.TEXT_DOCUMENT_PUBLISH_DIAGNOSTICS
    assert isinstance(params, types.PublishDiagnosticsParams)
    assert params.uri == uri
    assert len(params.diagnostics) == 1
    diagnostic = params.diagnostics[0]
    assert diagnostic.code == "CWE-79"
    assert diagnostic.severity == types.DiagnosticSeverity.Warning
    assert diagnostic.source == "piranesi"
    assert isinstance(diagnostic.data, dict)
    assert diagnostic.data["finding_id"] == "finding-1"
    assert isinstance(diagnostic.data["stable_id"], str)
    assert diagnostic.data["severity"] == "medium"
    assert diagnostic.data["evidence_level"] == "high_confidence_static_candidate"
    assert isinstance(diagnostic.data["action"], str)
    assert "Evidence: high_confidence_static_candidate." in diagnostic.message
    assert "Action:" in diagnostic.message
    assert diagnostic.related_information is not None
    assert [item.message for item in diagnostic.related_information] == [
        "Sink: res.send",
        "Taint step 1: html-encode",
    ]


def test_repeated_save_with_same_findings_skips_duplicate_publish(tmp_path: Path) -> None:
    config_path = tmp_path / "piranesi.toml"
    config_path.write_text("[lsp]\ndebounce_ms = 0\n", encoding="utf-8")

    source_file = tmp_path / "src" / "app.ts"
    source_file.parent.mkdir(parents=True, exist_ok=True)
    source_file.write_text("res.send(req.body.name)\n", encoding="utf-8")
    uri = from_fs_path(str(source_file.resolve(strict=False)))

    scanner = _FakeScanner([_finding_for_path(source_file)])
    server = create_server(config_path=config_path, scanner=scanner)
    notifications: list[tuple[str, object]] = []
    server.protocol.notify = lambda method, params=None: notifications.append((method, params))

    server.handle_did_open(
        types.DidOpenTextDocumentParams(
            text_document=types.TextDocumentItem(
                uri=uri,
                language_id="typescript",
                version=1,
                text=source_file.read_text(encoding="utf-8"),
            )
        )
    )
    notifications.clear()

    server.handle_did_save(
        types.DidSaveTextDocumentParams(
            text_document=types.TextDocumentIdentifier(uri=uri),
        )
    )
    assert len(notifications) == 1
    notifications.clear()

    server.handle_did_save(
        types.DidSaveTextDocumentParams(
            text_document=types.TextDocumentIdentifier(uri=uri),
        )
    )
    assert notifications == []


def test_incremental_scanner_falls_back_to_full_scan(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    config_path.write_text("", encoding="utf-8")
    source_file = tmp_path / "app.ts"
    source_file.write_text("export const value = 1;\n", encoding="utf-8")
    uri = from_fs_path(str(source_file.resolve(strict=False)))
    finding = _finding_for_path(source_file)

    scanner = IncrementalPipelineScanner(config_path)

    def _missing_incremental(**_kwargs: object) -> object:
        raise FileNotFoundError("no incremental manifest")

    captured_incremental: list[object] = []

    def _fake_run_incremental_scan(
        *,
        target_dir: Path,
        output_dir: Path,
        config: object,
        incremental: object,
    ) -> tuple[CandidateFinding, ...]:
        _ = (target_dir, output_dir, config)
        captured_incremental.append(incremental)
        return (finding,)

    monkeypatch.setattr("piranesi.lsp.server._saved_file_incremental_state", _missing_incremental)
    monkeypatch.setattr("piranesi.lsp.server._run_incremental_scan", _fake_run_incremental_scan)

    findings = scanner.scan_uri(uri)

    assert len(findings) == 1
    assert findings[0].id == finding.id
    assert captured_incremental == [None]


def test_finding_to_diagnostic_maps_high_severity_to_error(tmp_path: Path) -> None:
    source_file = tmp_path / "src" / "app.ts"
    source_file.parent.mkdir(parents=True, exist_ok=True)
    source_file.write_text("db.query(req.body.id)\n", encoding="utf-8")
    uri = from_fs_path(str(source_file.resolve(strict=False)))

    finding = _finding_for_path(source_file, severity="high", vuln_class="CWE-89: SQL Injection")
    diagnostic = finding_to_diagnostic(
        finding,
        uri=uri,
        project_root=tmp_path.resolve(strict=False),
    )

    assert diagnostic.code == "CWE-89"
    assert diagnostic.severity == types.DiagnosticSeverity.Error


def _finding_for_path(
    path: Path,
    *,
    severity: str = "medium",
    vuln_class: str = "CWE-79: Cross-Site Scripting",
) -> CandidateFinding:
    resolved = path.resolve(strict=False)
    source_location = SourceLocation(
        file=str(resolved),
        line=1,
        column=1,
        end_line=1,
        end_column=4,
        snippet="req.body.name",
    )
    sink_location = SourceLocation(
        file=str(resolved),
        line=1,
        column=5,
        end_line=1,
        end_column=13,
        snippet="res.send",
    )
    step_location = SourceLocation(
        file=str(resolved),
        line=1,
        column=14,
        end_line=1,
        end_column=22,
        snippet="sanitize",
    )
    return CandidateFinding(
        id="finding-1",
        vuln_class=vuln_class,
        source=TaintSource(
            location=source_location,
            source_type="req.body",
            data_categories=["identifier"],
            parameter_name="name",
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type="http_response",
            api_name="res.send",
        ),
        taint_path=[
            TaintStep(
                location=step_location,
                operation="html-encode",
                taint_state="tainted",
            )
        ],
        path_conditions=[],
        confidence=0.92,
        severity=severity,
    )
