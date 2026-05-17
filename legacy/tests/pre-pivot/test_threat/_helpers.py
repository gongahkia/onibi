from __future__ import annotations

from pathlib import Path

from piranesi.models import (
    AttackSurfaceNode,
    CandidateFinding,
    EntryPoint,
    ScanMetadata,
    ScanResult,
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
)
from piranesi.pipeline import DetectArtifact


def make_finding(
    *,
    finding_id: str = "finding-001",
    vuln_class: str = "CWE-89",
    severity: str = "high",
    confidence: float = 0.9,
    source_type: str = "request_body",
    source_file: str = "/workspace/app.ts",
    source_line: int = 10,
    sink_type: str = "sql_query",
    sink_api_name: str = "db.query",
    sink_file: str | None = None,
    sink_line: int = 20,
    taint_path_length: int = 1,
    data_categories: list[str] | None = None,
    metadata: dict[str, object] | None = None,
) -> CandidateFinding:
    source_location = SourceLocation(
        file=source_file,
        line=source_line,
        column=1,
        snippet="const input = req.body.input;",
    )
    sink_location = SourceLocation(
        file=sink_file or source_file,
        line=sink_line,
        column=1,
        snippet=f"{sink_api_name}(input)",
    )
    taint_path = [
        TaintStep(
            location=SourceLocation(
                file=source_file,
                line=source_line + index + 1,
                column=1,
                snippet=f"step_{index}",
            ),
            operation="propagate",
            taint_state="tainted",
            through_function=None if index == 0 else f"app.ts::step_{index}",
        )
        for index in range(taint_path_length)
    ]
    return CandidateFinding(
        id=finding_id,
        vuln_class=vuln_class,
        source=TaintSource(
            location=source_location,
            source_type=source_type,
            data_categories=list(data_categories or ["user_input"]),
            parameter_name="input",
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type=sink_type,
            api_name=sink_api_name,
        ),
        taint_path=taint_path,
        path_conditions=[],
        confidence=confidence,
        severity=severity,
        metadata=metadata or {},
    )


def make_entry_point(
    *,
    function_id: str = "app.ts::handler",
    source_file: str = "/workspace/app.ts",
    line: int = 10,
    kind: str = "route_handler",
    http_method: str | None = "POST",
    route_pattern: str | None = "/api/users",
    middleware: list[str] | None = None,
) -> EntryPoint:
    return EntryPoint(
        function_id=function_id,
        location=SourceLocation(
            file=source_file,
            line=line,
            column=1,
            snippet="app.post('/api/users', handler)",
        ),
        kind=kind,
        http_method=http_method,
        route_pattern=route_pattern,
        parameters=["req", "res"],
        middleware=list(middleware or []),
    )


def make_attack_surface(
    *,
    function_id: str = "app.ts::handler",
    source_file: str = "/workspace/app.ts",
    line: int = 10,
    source_type: str = "request_body",
    sanitizers_on_path: list[str] | None = None,
) -> AttackSurfaceNode:
    return AttackSurfaceNode(
        function_id=function_id,
        location=SourceLocation(
            file=source_file,
            line=line,
            column=1,
            snippet="req.body.input",
        ),
        source_type=source_type,
        data_flow_to=["db.query"],
        sanitizers_on_path=list(sanitizers_on_path or []),
    )


def write_threat_artifacts(tmp_path: Path) -> Path:
    output_dir = tmp_path / "piranesi-output"
    output_dir.mkdir()
    finding = make_finding()
    entry_point = make_entry_point()
    attack_surface = make_attack_surface()
    scan = ScanResult(
        project_root=str(tmp_path),
        files_scanned=[finding.source.location.file],
        call_graph={entry_point.function_id: []},
        functions=[],
        entry_points=[entry_point],
        attack_surface=[attack_surface],
        metadata=ScanMetadata(
            timestamp="2026-04-11T00:00:00Z",
            duration_ms=100,
            tree_sitter_version="test",
            piranesi_version="test",
            files_parsed=1,
            parse_errors=0,
            config_hash="abc123",
        ),
    )
    (output_dir / "scan.json").write_text(scan.model_dump_json(indent=2), encoding="utf-8")
    (output_dir / "detect.json").write_text(
        DetectArtifact(findings=[finding]).model_dump_json(indent=2),
        encoding="utf-8",
    )
    return output_dir
