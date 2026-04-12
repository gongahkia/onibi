from __future__ import annotations

import logging
import re
import threading
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Protocol, cast

from lsprotocol import types
from pygls.uris import from_fs_path, to_fs_path

try:
    from pygls.lsp.server import LanguageServer
except ImportError:
    from pygls.server import LanguageServer

from piranesi import __version__
from piranesi.config import LspConfig, PiranesiConfig, load_config
from piranesi.llm.cost import CostTracker
from piranesi.models import CandidateFinding, SourceLocation
from piranesi.pipeline import (
    IncrementalState,
    PipelineContext,
    StageResult,
    _run_detect_stage,
    _run_scan_stage,
    _write_artifact,
)
from piranesi.report.cwe import cwe_title, extract_cwe_id
from piranesi.report.renderer import PiranesiReport
from piranesi.scan.incremental import (
    FileManifest,
    IncrementalResult,
    build_manifest,
    diff_manifests,
    load_manifest,
)

_HUNK_HEADER_RE = re.compile(
    r"^@@ -(?P<old_start>\d+)(?:,(?P<old_len>\d+))? \+(?P<new_start>\d+)(?:,(?P<new_len>\d+))? @@"
)
_SEVERITY_TO_DIAGNOSTIC = {
    "critical": types.DiagnosticSeverity.Error,
    "high": types.DiagnosticSeverity.Error,
    "medium": types.DiagnosticSeverity.Warning,
    "low": types.DiagnosticSeverity.Information,
    "informational": types.DiagnosticSeverity.Hint,
}
_SEVERITY_RANK = {
    "informational": 0,
    "low": 1,
    "medium": 2,
    "high": 3,
    "critical": 4,
}


class FindingScanner(Protocol):
    def scan_uri(self, uri: str) -> Sequence[CandidateFinding]: ...


@dataclass(frozen=True, slots=True)
class _RuntimePaths:
    config_path: Path
    project_root: Path
    output_dir: Path


class _NoopRouter:
    def resolve(self, _stage: str) -> str | None:
        return None


class _NoopProvider:
    pass


class _NoopTraceWriter:
    pass


class IncrementalPipelineScanner:
    def __init__(self, config_path: Path, logger: logging.Logger | None = None) -> None:
        self._paths = _resolve_runtime_paths(config_path)
        self._logger = logger or logging.getLogger("piranesi.lsp.scanner")

    @property
    def project_root(self) -> Path:
        return self._paths.project_root

    @property
    def output_dir(self) -> Path:
        return self._paths.output_dir

    def scan_uri(self, uri: str) -> Sequence[CandidateFinding]:
        config = load_config(self._paths.config_path)
        saved_path = _path_from_uri(uri)
        if saved_path is None:
            raise ValueError(f"unsupported URI for Piranesi LSP: {uri}")

        resolved_path = saved_path.resolve(strict=False)
        try:
            resolved_path.relative_to(self.project_root)
        except ValueError as exc:
            raise ValueError(
                f"saved file is outside the configured project root: {resolved_path}"
            ) from exc

        self.output_dir.mkdir(parents=True, exist_ok=True)
        incremental = _saved_file_incremental_state(
            target_dir=self.project_root,
            output_dir=self.output_dir,
            saved_path=resolved_path,
        )
        context = PipelineContext(
            target_dir=self.project_root,
            output_dir=self.output_dir,
            provider=cast(Any, _NoopProvider()),
            router=cast(Any, _NoopRouter()),
            cost_tracker=CostTracker(),
            trace_writer=cast(Any, _NoopTraceWriter()),
            use_cache=True,
            incremental=incremental,
        )

        scan_result = _run_scan_stage(context, config, None)
        context.stage_outputs["scan"] = scan_result.artifact
        detect_result = _run_detect_stage(context, config, None)
        context.stage_outputs["detect"] = detect_result.artifact

        _write_stage_artifact(self.output_dir / "scan.json", scan_result)
        _write_stage_artifact(self.output_dir / "detect.json", detect_result)

        artifact = detect_result.artifact
        if not hasattr(artifact, "findings"):
            raise TypeError("detect stage did not return findings")
        return tuple(cast(Sequence[CandidateFinding], artifact.findings))


class PiranesiLanguageServer(LanguageServer):
    def __init__(
        self,
        *,
        config_path: Path,
        output_dir: Path,
        scanner: FindingScanner,
        settings: LspConfig,
        max_workers: int | None = None,
    ) -> None:
        super().__init__(
            name="piranesi",
            version=__version__,
            max_workers=max_workers,
            text_document_sync_kind=types.TextDocumentSyncKind.Incremental,
        )
        self.config_path = config_path.resolve(strict=False)
        self.project_root = self.config_path.parent.resolve(strict=False)
        self.output_dir = output_dir.resolve(strict=False)
        self.scanner = scanner
        self.settings = settings
        self._logger = logging.getLogger("piranesi.lsp")
        self._tracked_uris: set[str] = set()
        self._findings_by_uri: dict[str, list[CandidateFinding]] = {}
        self._findings_by_id: dict[str, CandidateFinding] = {}
        self._diagnostics_by_uri: dict[str, list[types.Diagnostic]] = {}
        self._patches_by_finding_id: dict[str, str] = {}
        self._debounce_timer: threading.Timer | None = None
        self._scan_lock = threading.Lock()
        self._scan_running = False
        self._queued_uri: str | None = None

    def handle_did_open(self, params: types.DidOpenTextDocumentParams) -> None:
        uri = params.text_document.uri
        self._tracked_uris.add(uri)
        self._publish_uri(uri)

    def handle_did_close(self, params: types.DidCloseTextDocumentParams) -> None:
        uri = params.text_document.uri
        self._tracked_uris.discard(uri)
        self._publish(uri, [])

    def handle_did_save(self, params: types.DidSaveTextDocumentParams) -> None:
        uri = params.text_document.uri
        self._tracked_uris.add(uri)
        if not self.settings.scan_on_save:
            self._publish_uri(uri)
            return
        self._queue_scan(uri)

    def handle_code_action(self, params: types.CodeActionParams) -> list[types.CodeAction]:
        uri = params.text_document.uri
        matching_ids = {
            diagnostic.data.get("finding_id")
            for diagnostic in (params.context.diagnostics or [])
            if (
                isinstance(diagnostic.data, dict)
                and isinstance(diagnostic.data.get("finding_id"), str)
            )
        }
        findings = (
            [
                self._findings_by_id[finding_id]
                for finding_id in matching_ids
                if finding_id in self._findings_by_id
            ]
            if matching_ids
            else [
                finding
                for finding in self._findings_by_uri.get(uri, [])
                if _ranges_overlap(self._primary_range_for_uri(finding, uri), params.range)
            ]
        )

        diagnostics_for_uri = {
            diagnostic.data.get("finding_id"): diagnostic
            for diagnostic in self._diagnostics_by_uri.get(uri, [])
            if (
                isinstance(diagnostic.data, dict)
                and isinstance(diagnostic.data.get("finding_id"), str)
            )
        }
        actions: list[types.CodeAction] = []
        for finding in findings:
            patch_diff = self._patch_diff_for_finding(finding.id)
            if patch_diff is None:
                continue
            workspace_edit = self._workspace_edit_for_patch(patch_diff)
            if workspace_edit is None:
                continue
            cwe = extract_cwe_id(finding.vuln_class)
            diagnostic = diagnostics_for_uri.get(finding.id)
            actions.append(
                types.CodeAction(
                    title=f"Apply Piranesi fix for {cwe}",
                    kind=types.CodeActionKind.QuickFix,
                    diagnostics=[diagnostic] if diagnostic is not None else None,
                    edit=workspace_edit,
                    is_preferred=True,
                )
            )
        return actions

    def handle_hover(self, params: types.HoverParams) -> types.Hover | None:
        uri = params.text_document.uri
        for finding in self._findings_by_uri.get(uri, []):
            location = _hover_location_for_uri(
                finding,
                uri=uri,
                position=params.position,
                project_root=self.project_root,
            )
            if location is None:
                continue
            title = cwe_title(extract_cwe_id(finding.vuln_class), fallback=finding.vuln_class)
            contents = types.MarkupContent(
                kind=types.MarkupKind.Markdown,
                value=(
                    f"**{title}**\n\n"
                    f"- Severity: `{finding.severity.upper()}`\n"
                    f"- Source: `{finding.source.source_type}`\n"
                    f"- Sink: `{finding.sink.api_name}`\n"
                    f"- Confidence: `{finding.confidence:.2f}`"
                ),
            )
            return types.Hover(contents=contents, range=_range_from_location(location))
        return None

    def handle_document_diagnostic(
        self,
        params: types.DocumentDiagnosticParams,
    ) -> types.RelatedFullDocumentDiagnosticReport:
        diagnostics = list(self._diagnostics_by_uri.get(params.text_document.uri, ()))
        return types.RelatedFullDocumentDiagnosticReport(items=diagnostics)

    def handle_shutdown(self) -> None:
        with self._scan_lock:
            if self._debounce_timer is not None:
                self._debounce_timer.cancel()
                self._debounce_timer = None

    def _queue_scan(self, uri: str) -> None:
        run_now = False
        with self._scan_lock:
            self._queued_uri = uri
            if self._debounce_timer is not None:
                self._debounce_timer.cancel()
                self._debounce_timer = None
            if self._scan_running:
                return
            if self.settings.debounce_ms <= 0:
                run_now = True
            else:
                self._debounce_timer = threading.Timer(
                    self.settings.debounce_ms / 1000.0,
                    self._drain_pending_scan,
                )
                self._debounce_timer.daemon = True
                self._debounce_timer.start()
        if run_now:
            self._drain_pending_scan()

    def _drain_pending_scan(self) -> None:
        uri: str | None = None
        with self._scan_lock:
            if self._scan_running or self._queued_uri is None:
                self._debounce_timer = None
                return
            self._debounce_timer = None
            self._scan_running = True
            uri = self._queued_uri
            self._queued_uri = None

        assert uri is not None
        try:
            findings = list(self.scanner.scan_uri(uri))
            self._refresh_patch_index()
            self._rebuild_state(findings)
            self._publish_tracked_uris()
        except Exception as exc:
            self._logger.exception("LSP scan failed for %s", uri)
            self.window_show_message(
                types.ShowMessageParams(
                    type=types.MessageType.Error,
                    message=f"Piranesi scan failed: {exc}",
                )
            )
        finally:
            rerun = False
            with self._scan_lock:
                self._scan_running = False
                rerun = self._queued_uri is not None
            if rerun:
                if self.settings.debounce_ms <= 0:
                    self._drain_pending_scan()
                else:
                    queued_uri = self._queued_uri or uri
                    self._queue_scan(queued_uri)

    def _rebuild_state(self, findings: Sequence[CandidateFinding]) -> None:
        findings_by_uri: dict[str, list[CandidateFinding]] = {}
        findings_by_id: dict[str, CandidateFinding] = {}
        diagnostics_by_uri: dict[str, list[types.Diagnostic]] = {}
        severity_threshold = _SEVERITY_RANK.get(self.settings.severity_filter.lower(), 0)

        for finding in findings:
            findings_by_id[finding.id] = finding
            for uri in _finding_uris(finding, self.project_root):
                findings_by_uri.setdefault(uri, []).append(finding)

        for uri, uri_findings in findings_by_uri.items():
            diagnostics = [
                finding_to_diagnostic(finding, uri=uri, project_root=self.project_root)
                for finding in uri_findings
                if _SEVERITY_RANK.get(finding.severity.lower(), 0) >= severity_threshold
            ]
            diagnostics.sort(
                key=lambda diagnostic: (
                    diagnostic.range.start.line,
                    diagnostic.range.start.character,
                    diagnostic.message,
                )
            )
            diagnostics_by_uri[uri] = diagnostics[: self.settings.max_findings_per_file]

        self._findings_by_uri = findings_by_uri
        self._findings_by_id = findings_by_id
        self._diagnostics_by_uri = diagnostics_by_uri

    def _refresh_patch_index(self) -> None:
        report_path = self.output_dir / "report.json"
        if not report_path.exists():
            self._patches_by_finding_id = {}
            return
        try:
            report = PiranesiReport.model_validate_json(report_path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            self._patches_by_finding_id = {}
            return
        self._patches_by_finding_id = {
            finding.finding_id: finding.patch_diff
            for finding in report.findings
            if finding.patch_diff is not None
        }

    def _patch_diff_for_finding(self, finding_id: str) -> str | None:
        return self._patches_by_finding_id.get(finding_id)

    def _workspace_edit_for_patch(self, patch_diff: str) -> types.WorkspaceEdit | None:
        target_uri = _patch_target_uri(patch_diff, project_root=self.project_root)
        if target_uri is None:
            return None
        target_path = _path_from_uri(target_uri)
        if target_path is None or not target_path.exists():
            return None
        original_text = target_path.read_text(encoding="utf-8")
        patched_text = _apply_unified_patch(original_text, patch_diff)
        if patched_text is None:
            return None
        return types.WorkspaceEdit(
            changes={
                target_uri: [
                    types.TextEdit(
                        range=_full_text_range(original_text),
                        new_text=patched_text,
                    )
                ]
            }
        )

    def _publish_tracked_uris(self) -> None:
        for uri in sorted(self._tracked_uris):
            self._publish_uri(uri)

    def _publish_uri(self, uri: str) -> None:
        self._publish(uri, self._diagnostics_by_uri.get(uri, []))

    def _publish(self, uri: str, diagnostics: Sequence[types.Diagnostic]) -> None:
        self.protocol.notify(
            types.TEXT_DOCUMENT_PUBLISH_DIAGNOSTICS,
            types.PublishDiagnosticsParams(uri=uri, diagnostics=list(diagnostics)),
        )

    def _primary_range_for_uri(self, finding: CandidateFinding, uri: str) -> types.Range:
        location = _primary_location_for_uri(finding, uri=uri, project_root=self.project_root)
        return _range_from_location(location)


def create_server(
    *,
    config_path: Path,
    scanner: FindingScanner | None = None,
) -> PiranesiLanguageServer:
    config = load_config(config_path)
    resolved_config = config_path.resolve(strict=False)
    output_dir = _resolve_output_dir(resolved_config.parent.resolve(strict=False), config)
    server = PiranesiLanguageServer(
        config_path=resolved_config,
        output_dir=output_dir,
        scanner=scanner or IncrementalPipelineScanner(resolved_config),
        settings=config.lsp,
    )

    @server.feature(types.TEXT_DOCUMENT_DID_OPEN)
    def _did_open(
        ls: PiranesiLanguageServer,
        params: types.DidOpenTextDocumentParams,
    ) -> None:
        ls.handle_did_open(params)

    @server.feature(types.TEXT_DOCUMENT_DID_SAVE)
    def _did_save(
        ls: PiranesiLanguageServer,
        params: types.DidSaveTextDocumentParams,
    ) -> None:
        ls.handle_did_save(params)

    @server.feature(types.TEXT_DOCUMENT_DID_CLOSE)
    def _did_close(
        ls: PiranesiLanguageServer,
        params: types.DidCloseTextDocumentParams,
    ) -> None:
        ls.handle_did_close(params)

    @server.feature(
        types.TEXT_DOCUMENT_CODE_ACTION,
        types.CodeActionOptions(code_action_kinds=[types.CodeActionKind.QuickFix]),
    )
    def _code_action(
        ls: PiranesiLanguageServer,
        params: types.CodeActionParams,
    ) -> list[types.CodeAction]:
        return ls.handle_code_action(params)

    @server.feature(types.TEXT_DOCUMENT_HOVER)
    def _hover(
        ls: PiranesiLanguageServer,
        params: types.HoverParams,
    ) -> types.Hover | None:
        return ls.handle_hover(params)

    @server.feature(
        types.TEXT_DOCUMENT_DIAGNOSTIC,
        types.DiagnosticOptions(inter_file_dependencies=True, workspace_diagnostics=False),
    )
    def _document_diagnostic(
        ls: PiranesiLanguageServer,
        params: types.DocumentDiagnosticParams,
    ) -> types.RelatedFullDocumentDiagnosticReport:
        return ls.handle_document_diagnostic(params)

    @server.feature(types.SHUTDOWN)
    def _shutdown(ls: PiranesiLanguageServer, *_args: object) -> None:
        ls.handle_shutdown()

    return server


def serve(
    *,
    config_path: Path,
    tcp: bool = False,
    host: str = "127.0.0.1",
    port: int = 9257,
) -> None:
    server = create_server(config_path=config_path)
    if not server.settings.enabled:
        raise RuntimeError(f"LSP support is disabled in {config_path}")
    if tcp:
        server.start_tcp(host, port)
        return
    server.start_io()


def finding_to_diagnostic(
    finding: CandidateFinding,
    *,
    uri: str,
    project_root: Path,
) -> types.Diagnostic:
    primary_location = _primary_location_for_uri(finding, uri=uri, project_root=project_root)
    cwe = extract_cwe_id(finding.vuln_class)
    source_label = finding.source.parameter_name or finding.source.source_type
    related_information = [
        types.DiagnosticRelatedInformation(
            location=types.Location(
                uri=_uri_for_location(location, project_root),
                range=_range_from_location(location),
            ),
            message=message,
        )
        for location, message in _related_locations(finding, primary_location, project_root)
    ]
    return types.Diagnostic(
        range=_range_from_location(primary_location),
        severity=_SEVERITY_TO_DIAGNOSTIC.get(
            finding.severity.lower(),
            types.DiagnosticSeverity.Warning,
        ),
        code=cwe,
        code_description=types.CodeDescription(href=_cwe_href(cwe)),
        source="piranesi",
        message=f"{cwe}: {source_label} -> {finding.sink.api_name}",
        related_information=related_information or None,
        data={"finding_id": finding.id},
    )


def _resolve_runtime_paths(config_path: Path) -> _RuntimePaths:
    resolved_config = config_path.resolve(strict=False)
    config = load_config(resolved_config)
    project_root = resolved_config.parent.resolve(strict=False)
    output_dir = _resolve_output_dir(project_root, config)
    return _RuntimePaths(
        config_path=resolved_config,
        project_root=project_root,
        output_dir=output_dir,
    )


def _resolve_output_dir(project_root: Path, config: PiranesiConfig) -> Path:
    configured = Path(config.output.output_dir)
    if configured.is_absolute():
        return configured.resolve(strict=False)
    return (project_root / configured).resolve(strict=False)


def _saved_file_incremental_state(
    *,
    target_dir: Path,
    output_dir: Path,
    saved_path: Path,
) -> IncrementalState:
    current_manifest = build_manifest(target_dir)
    relative_str = saved_path.resolve(strict=False).relative_to(target_dir).as_posix()
    if relative_str not in current_manifest.files:
        raise FileNotFoundError(f"saved file is not part of the scan set: {saved_path}")

    relative_path = Path(relative_str)
    previous_manifest = load_manifest(output_dir, expected_target_dir=target_dir)
    if previous_manifest is None:
        previous_files = {
            path: entry for path, entry in current_manifest.files.items() if path != relative_str
        }
        synthetic_previous = FileManifest(target_dir=target_dir, files=previous_files)
        return IncrementalState(
            previous_manifest=synthetic_previous,
            current_manifest=current_manifest,
            diff=IncrementalResult(
                added={relative_path},
                modified=set(),
                deleted=set(),
                unchanged={Path(path) for path in previous_files},
            ),
            manifest_write_stage="detect",
        )

    full_diff = diff_manifests(previous_manifest, current_manifest)
    unchanged = set(full_diff.unchanged)
    unchanged.update(path for path in full_diff.modified if path != relative_path)
    added = {relative_path} if relative_str not in previous_manifest.files else set()
    modified = set() if added else {relative_path}
    return IncrementalState(
        previous_manifest=previous_manifest,
        current_manifest=current_manifest,
        diff=IncrementalResult(
            added=added,
            modified=modified,
            deleted=set(full_diff.deleted),
            unchanged=unchanged,
        ),
        manifest_write_stage="detect",
    )


def _write_stage_artifact(path: Path, result: StageResult) -> None:
    artifact = result.artifact
    if hasattr(artifact, "model_dump_json"):
        _write_artifact(path, artifact)


def _finding_uris(finding: CandidateFinding, project_root: Path) -> set[str]:
    uris = {
        _uri_for_location(finding.source.location, project_root),
        _uri_for_location(finding.sink.location, project_root),
    }
    uris.update(_uri_for_location(step.location, project_root) for step in finding.taint_path)
    uris.update(
        _uri_for_location(condition.location, project_root) for condition in finding.path_conditions
    )
    return uris


def _primary_location_for_uri(
    finding: CandidateFinding,
    *,
    uri: str,
    project_root: Path,
) -> SourceLocation:
    ordered_locations = [
        finding.source.location,
        finding.sink.location,
        *(step.location for step in finding.taint_path),
        *(condition.location for condition in finding.path_conditions),
    ]
    for location in ordered_locations:
        if _uri_for_location(location, project_root) == uri:
            return location
    return finding.source.location


def _hover_location_for_uri(
    finding: CandidateFinding,
    *,
    uri: str,
    position: types.Position,
    project_root: Path,
) -> SourceLocation | None:
    for location in (finding.source.location, finding.sink.location):
        if _uri_for_location(location, project_root) != uri:
            continue
        if _position_in_range(position, _range_from_location(location)):
            return location
    return None


def _related_locations(
    finding: CandidateFinding,
    primary_location: SourceLocation,
    project_root: Path,
) -> list[tuple[SourceLocation, str]]:
    related: list[tuple[SourceLocation, str]] = []
    if finding.source.location != primary_location:
        related.append((finding.source.location, f"Source: {finding.source.source_type}"))
    if finding.sink.location != primary_location:
        related.append((finding.sink.location, f"Sink: {finding.sink.api_name}"))
    for index, step in enumerate(finding.taint_path, start=1):
        related.append((step.location, f"Taint step {index}: {step.operation}"))
    return related


def _range_from_location(location: SourceLocation) -> types.Range:
    start_line = max(location.line - 1, 0)
    start_char = max(location.column - 1, 0)
    if location.end_line is not None:
        end_line = max(location.end_line - 1, start_line)
    else:
        end_line = start_line
    if location.end_column is not None:
        end_char = max(location.end_column - 1, start_char)
    else:
        snippet = location.snippet.splitlines()[0] if location.snippet else ""
        end_char = start_char + max(len(snippet), 1)
    return types.Range(
        start=types.Position(line=start_line, character=start_char),
        end=types.Position(line=end_line, character=end_char),
    )


def _uri_for_location(location: SourceLocation, project_root: Path) -> str:
    path = Path(location.file)
    if not path.is_absolute():
        path = project_root / path
    return from_fs_path(str(path.resolve(strict=False)))


def _path_from_uri(uri: str) -> Path | None:
    path = to_fs_path(uri)
    return None if path is None else Path(path)


def _position_in_range(position: types.Position, range_: types.Range) -> bool:
    if position.line < range_.start.line or position.line > range_.end.line:
        return False
    if position.line == range_.start.line and position.character < range_.start.character:
        return False
    return not (position.line == range_.end.line and position.character > range_.end.character)


def _ranges_overlap(left: types.Range, right: types.Range) -> bool:
    left_start = (left.start.line, left.start.character)
    left_end = (left.end.line, left.end.character)
    right_start = (right.start.line, right.start.character)
    right_end = (right.end.line, right.end.character)
    return left_start <= right_end and right_start <= left_end


def _cwe_href(cwe: str) -> str:
    digits = "".join(char for char in cwe if char.isdigit())
    if not digits:
        return "https://cwe.mitre.org/"
    return f"https://cwe.mitre.org/data/definitions/{digits}.html"


def _patch_target_uri(patch_diff: str, *, project_root: Path) -> str | None:
    for line in patch_diff.splitlines():
        if not line.startswith("+++ "):
            continue
        rendered = line[4:].strip()
        if rendered.startswith("b/"):
            rendered = rendered[2:]
        if not rendered:
            return None
        return from_fs_path(str((project_root / rendered).resolve(strict=False)))
    return None


def _apply_unified_patch(original_text: str, patch_diff: str) -> str | None:
    original_lines = original_text.splitlines(keepends=True)
    result: list[str] = []
    current_index = 0
    lines = patch_diff.splitlines(keepends=True)
    index = 0

    while index < len(lines):
        line = lines[index]
        if line.startswith(("--- ", "+++ ")):
            index += 1
            continue
        match = _HUNK_HEADER_RE.match(line.rstrip("\n"))
        if match is None:
            index += 1
            continue

        old_start = int(match.group("old_start"))
        old_index = max(old_start - 1, 0)
        if old_index < current_index:
            return None
        result.extend(original_lines[current_index:old_index])
        current_index = old_index
        index += 1

        while index < len(lines):
            hunk_line = lines[index]
            if _HUNK_HEADER_RE.match(hunk_line.rstrip("\n")):
                break
            if hunk_line.startswith("\\"):
                index += 1
                continue
            marker = hunk_line[:1]
            payload = hunk_line[1:]
            if marker == " ":
                if current_index >= len(original_lines) or original_lines[current_index] != payload:
                    return None
                result.append(original_lines[current_index])
                current_index += 1
            elif marker == "-":
                if current_index >= len(original_lines) or original_lines[current_index] != payload:
                    return None
                current_index += 1
            elif marker == "+":
                result.append(payload)
            else:
                return None
            index += 1

    result.extend(original_lines[current_index:])
    return "".join(result)


def _full_text_range(text: str) -> types.Range:
    lines = text.splitlines()
    if not lines:
        end_line = 0
        end_char = 0
    elif text.endswith(("\n", "\r")):
        end_line = len(lines)
        end_char = 0
    else:
        end_line = len(lines) - 1
        end_char = len(lines[-1])
    return types.Range(
        start=types.Position(line=0, character=0),
        end=types.Position(line=end_line, character=end_char),
    )
