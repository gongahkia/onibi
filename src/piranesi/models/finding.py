from __future__ import annotations

from pydantic import BaseModel, ConfigDict

from piranesi.models.taint import PathCondition, SourceLocation, TaintSink, TaintSource, TaintStep


class EntryPoint(BaseModel):
    model_config = ConfigDict(extra="forbid")

    function_id: str
    location: SourceLocation
    kind: str
    http_method: str | None = None
    route_pattern: str | None = None
    parameters: list[str]


class AttackSurfaceNode(BaseModel):
    model_config = ConfigDict(extra="forbid")

    function_id: str
    location: SourceLocation
    source_type: str
    data_flow_to: list[str]
    sanitizers_on_path: list[str]


class ScanMetadata(BaseModel):
    model_config = ConfigDict(extra="forbid")

    timestamp: str
    duration_ms: int
    tree_sitter_version: str
    piranesi_version: str
    files_parsed: int
    parse_errors: int
    config_hash: str


class ScanResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    project_root: str
    files_scanned: list[str]
    call_graph: dict[str, list[str]]
    entry_points: list[EntryPoint]
    attack_surface: list[AttackSurfaceNode]
    metadata: ScanMetadata


class CandidateFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    vuln_class: str
    source: TaintSource
    sink: TaintSink
    taint_path: list[TaintStep]
    path_conditions: list[PathCondition]
    confidence: float
    severity: str
    affected_individuals_estimate: int | None = None
    cross_border: bool = False
    is_high_risk_ai: bool = False
    third_party_processor: bool = False
    no_encryption_at_rest: bool = False


class TriagedFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding: CandidateFinding
    triage_verdict: str
    skeptic_analysis: str
    ensemble_score: float
    escalated: bool
    triage_override_logged: bool = False


class SandboxResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    container_id: str
    request: dict[str, object]
    response: dict[str, object]
    timing_ms: int
    side_effects: list[str]
    container_diff: list[str]
    stdout: str
    stderr: str
    exit_code: int
    network_isolated: bool
    confirmed: bool = False


class ConfirmedFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding: TriagedFinding
    exploit_payload: str
    exploit_constraints: list[str]
    sandbox_result: SandboxResult
    reproducer_script: str
    related_cves: list[str]


class PatchResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding: ConfirmedFinding
    patch_diff: str
    patch_verified: bool
    patch_explanation: str


class ReportFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    confirmed: ConfirmedFinding
    legal: object | None = None
    patch: PatchResult | None = None


class FinalReport(BaseModel):
    model_config = ConfigDict(extra="forbid")

    scan_metadata: ScanMetadata
    findings: list[ReportFinding]
    summary_stats: dict[str, object]
    pr_body_markdown: str
