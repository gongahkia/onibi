from __future__ import annotations

import json
import os
import webbrowser
from pathlib import Path
from typing import Annotated, Any, Literal, NoReturn, cast

import typer

from piranesi import __version__
from piranesi.adapters import (
    NmapParseError,
    NucleiParseError,
    parse_nmap_xml_file,
    parse_nuclei_jsonl_file,
)
from piranesi.detections import (
    DetectionConfidence,
    DetectionError,
    DetectionSensitivity,
    IOCType,
    add_detection_note,
    add_ioc,
    load_detections,
)
from piranesi.evidence import (
    EvidenceError,
    EvidenceKind,
    EvidenceSensitivity,
    add_evidence_file,
    load_evidence_index,
)
from piranesi.objectives import (
    ObjectiveError,
    ObjectiveStatus,
    add_objective,
    add_procedure,
    load_objectives,
    load_procedures,
)
from piranesi.report.pentest import (
    PdfBackend,
    ReportFormat,
    ReportRenderError,
    build_pentest_report,
    render_report_artifact,
)
from piranesi.report.redteam import (
    RedTeamReportError,
    build_red_team_report,
    render_red_team_report_artifact,
)
from piranesi.retest import (
    RetestError,
    append_retest_audit,
    compare_workspaces,
    write_retest_output,
)
from piranesi.signing import (
    SigningError,
    sign_workspace,
    verification_result_payload,
    verify_workspace,
)
from piranesi.timeline import (
    TimelineConfidence,
    TimelineError,
    append_timeline_event,
    load_timeline_events,
)
from piranesi.workspace import (
    DETECTIONS_FILE,
    EVIDENCE_FILE,
    FINDINGS_FILE,
    OBJECTIVES_FILE,
    PROCEDURES_FILE,
    TIMELINE_FILE,
    AuditEvent,
    EngagementMetadata,
    WorkspaceError,
    copy_tool_input,
    create_workspace,
    file_sha256,
    load_workspace,
    upsert_findings,
    utc_now,
    workspace_path,
)
from piranesi.workspace import (
    append_audit_event as append_workspace_audit_event,
)
from piranesi.workspace_server import (
    WorkspaceServerError,
    create_workspace_server,
    is_loopback_host,
)

EXIT_OK = 0
EXIT_OPERATION_FAILED = 1
EXIT_USAGE_ERROR = 2
EXIT_NOT_IMPLEMENTED = 64

DEFAULT_WORKSPACE = Path("piranesi-workspace")
ReportType = Literal["pentest", "red-team"]
ReportOutputFormat = Literal["json", "md", "pdf", "archive"]

app = typer.Typer(
    add_completion=False,
    help="Local-first red-team engagement workspace.",
    no_args_is_help=True,
)
ingest_app = typer.Typer(
    add_completion=False,
    help="Create or update local findings from scanner exports.",
    no_args_is_help=True,
)
evidence_app = typer.Typer(
    add_completion=False,
    help="Add and inspect red-team operator evidence.",
    no_args_is_help=True,
)
timeline_app = typer.Typer(
    add_completion=False,
    help="Record and inspect red-team engagement timeline events.",
    no_args_is_help=True,
)
objectives_app = typer.Typer(
    add_completion=False,
    help="Track red-team objectives.",
    no_args_is_help=True,
)
procedures_app = typer.Typer(
    add_completion=False,
    help="Track red-team procedures and ATT&CK mapping.",
    no_args_is_help=True,
)
detections_app = typer.Typer(
    add_completion=False,
    help="Track IOCs and blue-team detection handoff notes.",
    no_args_is_help=True,
)
app.add_typer(ingest_app, name="ingest")
app.add_typer(evidence_app, name="evidence")
app.add_typer(timeline_app, name="timeline")
app.add_typer(objectives_app, name="objectives")
app.add_typer(procedures_app, name="procedures")
app.add_typer(detections_app, name="detections")


def _load_local_llm_env(env_path: Path | None = None) -> None:
    """Load local OpenAI key aliases for callers that still use report helpers."""
    path = env_path or Path(".env")
    if "OPENAI_API_KEY" in os.environ or not path.is_file():
        return
    for line in path.read_text(encoding="utf-8").splitlines():
        key, separator, value = line.partition("=")
        if separator and key.strip() in {"OPENAI_API_KEY", "OPENAI-API-KEY"}:
            os.environ["OPENAI_API_KEY"] = value.strip().strip("\"'")
            return


def _emit(payload: dict[str, Any], *, json_output: bool, text: str) -> None:
    if json_output:
        typer.echo(json.dumps(payload, indent=2, sort_keys=True))
    else:
        typer.echo(text)


def _fail(message: str, *, code: int = EXIT_USAGE_ERROR, json_errors: bool = False) -> NoReturn:
    if json_errors:
        typer.echo(json.dumps({"error": message, "exit_code": code}, sort_keys=True), err=True)
    else:
        typer.echo(f"error: {message}", err=True)
    raise typer.Exit(code=code)


@app.callback()
def main(
    version: Annotated[
        bool,
        typer.Option("--version", help="Print the installed Piranesi version and exit."),
    ] = False,
) -> None:
    if version:
        typer.echo(f"piranesi {__version__}")
        raise typer.Exit(code=EXIT_OK)


@ingest_app.command("init", help="Initialize or update a pentest engagement workspace.")
def ingest_init_command(
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to create or update.",
        ),
    ] = DEFAULT_WORKSPACE,
    client: Annotated[
        str | None,
        typer.Option("--client", help="Client name to store in workspace metadata."),
    ] = None,
    project: Annotated[
        str | None,
        typer.Option("--project", help="Project or engagement name to store in metadata."),
    ] = None,
    scope: Annotated[
        list[str] | None,
        typer.Option("--scope", help="In-scope target or asset; repeatable."),
    ] = None,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print the initialized workspace metadata as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    engagement = EngagementMetadata(client=client, project=project, scope=scope or [])
    try:
        state = create_workspace(workspace, engagement=engagement)
    except WorkspaceError as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = {
        "workspace": str(state.root),
        "schema_version": state.workspace.schema_version,
        "findings": len(state.findings.findings),
        "tool_inputs": len(state.workspace.tool_inputs),
    }
    _emit(payload, json_output=json_output, text=f"Initialized workspace: {state.root}")


@ingest_app.command("nmap", help="Ingest real nmap XML into a pentest workspace.")
def ingest_nmap_command(
    input_path: Annotated[
        Path,
        typer.Option(
            "--input",
            "-i",
            exists=False,
            dir_okay=False,
            file_okay=True,
            help="Real nmap XML export to ingest.",
        ),
    ],
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to create or update.",
        ),
    ] = DEFAULT_WORKSPACE,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print ingest summary as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    if not input_path.is_file():
        _fail(f"input file does not exist: {input_path}", json_errors=json_errors)

    try:
        state = create_workspace(workspace)
        state, record = copy_tool_input(state, tool="nmap", input_path=input_path)
        raw_input_path = workspace_path(state.root, record.raw_path, allowed_roots=("raw",))
        parse_result = parse_nmap_xml_file(
            raw_input_path,
            input_sha256=record.sha256,
            raw_path=record.raw_path,
        )
        state, record = copy_tool_input(
            state,
            tool="nmap",
            input_path=input_path,
            metadata=parse_result.metadata,
        )
        before_ids = {finding.id for finding in state.findings.findings}
        incoming_ids = {finding.id for finding in parse_result.findings}
        state = upsert_findings(state, parse_result.findings)
        output_digest = file_sha256(state.root / FINDINGS_FILE)
        summary = {
            "tool": "nmap",
            "created": len(incoming_ids - before_ids),
            "updated": len(incoming_ids & before_ids),
            "findings": len(parse_result.findings),
            "warnings": parse_result.warnings,
            "input_record": record.id,
        }
        append_workspace_audit_event(
            state,
            AuditEvent(
                timestamp=utc_now(),
                command="ingest nmap",
                input_path=record.raw_path,
                input_sha256=record.sha256,
                output_path=FINDINGS_FILE,
                output_sha256=output_digest,
                summary=summary,
            ),
        )
    except (WorkspaceError, NmapParseError) as exc:
        _fail(str(exc), json_errors=json_errors)

    warning_count = len(parse_result.warnings)
    _emit(
        summary,
        json_output=json_output,
        text=(
            "Ingested nmap XML: "
            f"{summary['findings']} findings "
            f"({summary['created']} created, {summary['updated']} updated, "
            f"{warning_count} warnings)"
        ),
    )


@ingest_app.command("nuclei", help="Ingest real nuclei JSONL into a pentest workspace.")
def ingest_nuclei_command(
    input_path: Annotated[
        Path,
        typer.Option(
            "--input",
            "-i",
            exists=False,
            dir_okay=False,
            file_okay=True,
            help="Real nuclei JSONL export to ingest.",
        ),
    ],
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to create or update.",
        ),
    ] = DEFAULT_WORKSPACE,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print ingest summary as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    if not input_path.is_file():
        _fail(f"input file does not exist: {input_path}", json_errors=json_errors)

    try:
        state = create_workspace(workspace)
        state, record = copy_tool_input(state, tool="nuclei", input_path=input_path)
        raw_input_path = workspace_path(state.root, record.raw_path, allowed_roots=("raw",))
        parse_result = parse_nuclei_jsonl_file(
            raw_input_path,
            input_sha256=record.sha256,
            raw_path=record.raw_path,
        )
        state, record = copy_tool_input(
            state,
            tool="nuclei",
            input_path=input_path,
            metadata=parse_result.metadata,
        )
        before_ids = {finding.id for finding in state.findings.findings}
        incoming_ids = {finding.id for finding in parse_result.findings}
        state = upsert_findings(state, parse_result.findings)
        output_digest = file_sha256(state.root / FINDINGS_FILE)
        summary = {
            "tool": "nuclei",
            "created": len(incoming_ids - before_ids),
            "updated": len(incoming_ids & before_ids),
            "records": parse_result.metadata["valid_records"],
            "findings": len(incoming_ids),
            "warnings": parse_result.warnings,
            "input_record": record.id,
        }
        append_workspace_audit_event(
            state,
            AuditEvent(
                timestamp=utc_now(),
                command="ingest nuclei",
                input_path=record.raw_path,
                input_sha256=record.sha256,
                output_path=FINDINGS_FILE,
                output_sha256=output_digest,
                summary=summary,
            ),
        )
    except (WorkspaceError, NucleiParseError) as exc:
        _fail(str(exc), json_errors=json_errors)

    warning_count = len(parse_result.warnings)
    _emit(
        summary,
        json_output=json_output,
        text=(
            "Ingested nuclei JSONL: "
            f"{summary['findings']} findings "
            f"({summary['created']} created, {summary['updated']} updated, "
            f"{warning_count} warnings)"
        ),
    )


@ingest_app.command("burp", help="Reserved for Burp Suite Pro Issues XML ingestion.")
def ingest_burp_command(
    input_path: Annotated[
        Path,
        typer.Option(
            "--input",
            "-i",
            exists=False,
            dir_okay=False,
            file_okay=True,
            help="Burp Suite Pro Issues XML export to ingest.",
        ),
    ],
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to create or update.",
        ),
    ] = DEFAULT_WORKSPACE,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    _ = (input_path, workspace)
    _fail(
        "Burp Suite Pro Issues XML ingestion is tracked in issue #32 and needs a real fixture.",
        code=EXIT_NOT_IMPLEMENTED,
        json_errors=json_errors,
    )


@evidence_app.command("add", help="Add an operator artifact to the local evidence vault.")
def evidence_add_command(
    file_path: Annotated[
        Path,
        typer.Option(
            "--file",
            "-f",
            exists=False,
            dir_okay=False,
            file_okay=True,
            help="Evidence file to preserve under raw/<kind>/.",
        ),
    ],
    kind: Annotated[
        EvidenceKind,
        typer.Option("--kind", help="Evidence kind."),
    ] = "other",
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to create or update.",
        ),
    ] = DEFAULT_WORKSPACE,
    title: Annotated[
        str | None,
        typer.Option("--title", help="Human-readable evidence title."),
    ] = None,
    observed_at: Annotated[
        str | None,
        typer.Option("--observed-at", help="When the operator observed this evidence."),
    ] = None,
    source: Annotated[
        str | None,
        typer.Option("--source", help="Operator, host, tool, or system that produced it."),
    ] = None,
    sensitivity: Annotated[
        EvidenceSensitivity,
        typer.Option("--sensitivity", help="Evidence sensitivity marker."),
    ] = "sensitive",
    tags: Annotated[
        list[str] | None,
        typer.Option("--tag", help="Evidence tag; repeatable."),
    ] = None,
    notes: Annotated[
        str | None,
        typer.Option("--notes", help="Short operator note for this evidence."),
    ] = None,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print the evidence record as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    if not file_path.is_file():
        _fail(f"evidence file does not exist: {file_path}", json_errors=json_errors)

    try:
        state = create_workspace(workspace)
        _index, record = add_evidence_file(
            state.root,
            file_path=file_path,
            kind=kind,
            title=title,
            observed_at=observed_at,
            source=source,
            sensitivity=sensitivity,
            tags=tags,
            notes=notes,
        )
        output_digest = file_sha256(state.root / EVIDENCE_FILE)
        summary = {
            "evidence_id": record.id,
            "kind": record.kind,
            "raw_path": record.raw_path,
            "title": record.title,
        }
        append_workspace_audit_event(
            state,
            AuditEvent(
                timestamp=utc_now(),
                command="evidence add",
                input_path=record.raw_path,
                input_sha256=record.sha256,
                output_path=EVIDENCE_FILE,
                output_sha256=output_digest,
                summary=summary,
            ),
        )
    except (WorkspaceError, EvidenceError, OSError) as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = record.model_dump(mode="json")
    _emit(
        payload,
        json_output=json_output,
        text=f"evidence: {record.id} ({record.kind}) -> {record.raw_path}",
    )


@evidence_app.command("list", help="List evidence records in a workspace.")
def evidence_list_command(
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to inspect.",
        ),
    ] = DEFAULT_WORKSPACE,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print evidence records as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    try:
        state = load_workspace(workspace)
        index = load_evidence_index(state.root)
    except (WorkspaceError, EvidenceError) as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = {
        "workspace": str(state.root),
        "count": len(index.evidence),
        "evidence": [record.model_dump(mode="json") for record in index.evidence],
    }
    if json_output:
        _emit(payload, json_output=True, text="")
        return
    if not index.evidence:
        typer.echo("No evidence records.")
        return
    for record in index.evidence:
        typer.echo(f"{record.id}\t{record.kind}\t{record.title}\t{record.raw_path}")


@timeline_app.command("add", help="Append an operator event to the engagement timeline.")
def timeline_add_command(
    summary: Annotated[
        str,
        typer.Option("--summary", "-s", help="Short timeline event summary."),
    ],
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to create or update.",
        ),
    ] = DEFAULT_WORKSPACE,
    timestamp: Annotated[
        str | None,
        typer.Option("--timestamp", help="Event timestamp. Defaults to now."),
    ] = None,
    phase: Annotated[
        str | None,
        typer.Option("--phase", help="Engagement phase, such as initial-access."),
    ] = None,
    actor: Annotated[
        str | None,
        typer.Option("--actor", help="Operator, system, or source actor."),
    ] = None,
    details: Annotated[
        str | None,
        typer.Option("--details", help="Longer operator event detail."),
    ] = None,
    evidence_ids: Annotated[
        list[str] | None,
        typer.Option("--evidence-id", help="Evidence record ID to link; repeatable."),
    ] = None,
    finding_ids: Annotated[
        list[str] | None,
        typer.Option("--finding-id", help="Finding ID to link; repeatable."),
    ] = None,
    objective_ids: Annotated[
        list[str] | None,
        typer.Option("--objective-id", help="Objective ID to link; repeatable."),
    ] = None,
    tags: Annotated[
        list[str] | None,
        typer.Option("--tag", help="Timeline tag; repeatable."),
    ] = None,
    confidence: Annotated[
        TimelineConfidence,
        typer.Option("--confidence", help="Confidence in the event details."),
    ] = "medium",
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print the timeline event as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    try:
        state = create_workspace(workspace)
        event = append_timeline_event(
            state,
            summary=summary,
            timestamp=timestamp,
            phase=phase,
            actor=actor,
            details=details,
            evidence_ids=evidence_ids,
            finding_ids=finding_ids,
            objective_ids=objective_ids,
            tags=tags,
            confidence=confidence,
        )
        output_digest = file_sha256(state.root / TIMELINE_FILE)
        append_workspace_audit_event(
            state,
            AuditEvent(
                timestamp=utc_now(),
                command="timeline add",
                output_path=TIMELINE_FILE,
                output_sha256=output_digest,
                summary={
                    "event_id": event.id,
                    "summary": event.summary,
                    "evidence_ids": event.evidence_ids,
                    "finding_ids": event.finding_ids,
                    "objective_ids": event.objective_ids,
                },
            ),
        )
    except (WorkspaceError, TimelineError) as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = event.model_dump(mode="json")
    _emit(
        payload,
        json_output=json_output,
        text=f"timeline: {event.id} {event.timestamp} {event.summary}",
    )


@timeline_app.command("list", help="List timeline events in a workspace.")
def timeline_list_command(
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to inspect.",
        ),
    ] = DEFAULT_WORKSPACE,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print timeline events as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    try:
        state = load_workspace(workspace)
        events = load_timeline_events(state.root)
    except (WorkspaceError, TimelineError) as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = {
        "workspace": str(state.root),
        "count": len(events),
        "events": [event.model_dump(mode="json") for event in events],
    }
    if json_output:
        _emit(payload, json_output=True, text="")
        return
    if not events:
        typer.echo("No timeline events.")
        return
    for event in events:
        phase = event.phase or "-"
        typer.echo(f"{event.timestamp}\t{phase}\t{event.id}\t{event.summary}")


@objectives_app.command("add", help="Add or update a red-team objective.")
def objective_add_command(
    title: Annotated[
        str,
        typer.Option("--title", "-t", help="Objective title."),
    ],
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to create or update.",
        ),
    ] = DEFAULT_WORKSPACE,
    status: Annotated[
        ObjectiveStatus,
        typer.Option("--status", help="Objective status."),
    ] = "planned",
    owner: Annotated[
        str | None,
        typer.Option("--owner", help="Objective owner."),
    ] = None,
    target_assets: Annotated[
        list[str] | None,
        typer.Option("--target-asset", help="Target asset; repeatable."),
    ] = None,
    success_criteria: Annotated[
        list[str] | None,
        typer.Option("--success-criterion", help="Success criterion; repeatable."),
    ] = None,
    evidence_ids: Annotated[
        list[str] | None,
        typer.Option("--evidence-id", help="Evidence record ID to link; repeatable."),
    ] = None,
    timeline_event_ids: Annotated[
        list[str] | None,
        typer.Option("--event-id", help="Timeline event ID to link; repeatable."),
    ] = None,
    tags: Annotated[
        list[str] | None,
        typer.Option("--tag", help="Objective tag; repeatable."),
    ] = None,
    notes: Annotated[
        str | None,
        typer.Option("--notes", help="Objective note."),
    ] = None,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print the objective as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    try:
        state = create_workspace(workspace)
        _document, objective = add_objective(
            state,
            title=title,
            status=status,
            owner=owner,
            target_assets=target_assets,
            success_criteria=success_criteria,
            evidence_ids=evidence_ids,
            timeline_event_ids=timeline_event_ids,
            tags=tags,
            notes=notes,
        )
        output_digest = file_sha256(state.root / OBJECTIVES_FILE)
        append_workspace_audit_event(
            state,
            AuditEvent(
                timestamp=utc_now(),
                command="objectives add",
                output_path=OBJECTIVES_FILE,
                output_sha256=output_digest,
                summary={"objective_id": objective.id, "title": objective.title},
            ),
        )
    except (WorkspaceError, ObjectiveError) as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = objective.model_dump(mode="json")
    _emit(
        payload,
        json_output=json_output,
        text=f"objective: {objective.id} {objective.status} {objective.title}",
    )


@objectives_app.command("list", help="List red-team objectives.")
def objective_list_command(
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to inspect.",
        ),
    ] = DEFAULT_WORKSPACE,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print objectives as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    try:
        state = load_workspace(workspace)
        document = load_objectives(state.root)
    except (WorkspaceError, ObjectiveError) as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = {
        "workspace": str(state.root),
        "count": len(document.objectives),
        "objectives": [objective.model_dump(mode="json") for objective in document.objectives],
    }
    if json_output:
        _emit(payload, json_output=True, text="")
        return
    if not document.objectives:
        typer.echo("No objectives.")
        return
    for objective in document.objectives:
        typer.echo(f"{objective.id}\t{objective.status}\t{objective.title}")


@procedures_app.command("add", help="Add or update a red-team procedure.")
def procedure_add_command(
    summary: Annotated[
        str,
        typer.Option("--summary", "-s", help="Procedure summary."),
    ],
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to create or update.",
        ),
    ] = DEFAULT_WORKSPACE,
    tactic: Annotated[
        str | None,
        typer.Option("--tactic", help="ATT&CK tactic or procedure category."),
    ] = None,
    technique_id: Annotated[
        str | None,
        typer.Option("--technique-id", help="ATT&CK technique ID."),
    ] = None,
    technique_name: Annotated[
        str | None,
        typer.Option("--technique-name", help="ATT&CK technique name."),
    ] = None,
    command: Annotated[
        str | None,
        typer.Option("--command", help="Command or action summary."),
    ] = None,
    evidence_ids: Annotated[
        list[str] | None,
        typer.Option("--evidence-id", help="Evidence record ID to link; repeatable."),
    ] = None,
    timeline_event_ids: Annotated[
        list[str] | None,
        typer.Option("--event-id", help="Timeline event ID to link; repeatable."),
    ] = None,
    finding_ids: Annotated[
        list[str] | None,
        typer.Option("--finding-id", help="Finding ID to link; repeatable."),
    ] = None,
    objective_ids: Annotated[
        list[str] | None,
        typer.Option("--objective-id", help="Objective ID to link; repeatable."),
    ] = None,
    tags: Annotated[
        list[str] | None,
        typer.Option("--tag", help="Procedure tag; repeatable."),
    ] = None,
    notes: Annotated[
        str | None,
        typer.Option("--notes", help="Procedure note."),
    ] = None,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print the procedure as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    try:
        state = create_workspace(workspace)
        _document, procedure = add_procedure(
            state,
            summary=summary,
            tactic=tactic,
            technique_id=technique_id,
            technique_name=technique_name,
            command=command,
            evidence_ids=evidence_ids,
            timeline_event_ids=timeline_event_ids,
            finding_ids=finding_ids,
            objective_ids=objective_ids,
            tags=tags,
            notes=notes,
        )
        output_digest = file_sha256(state.root / PROCEDURES_FILE)
        append_workspace_audit_event(
            state,
            AuditEvent(
                timestamp=utc_now(),
                command="procedures add",
                output_path=PROCEDURES_FILE,
                output_sha256=output_digest,
                summary={"procedure_id": procedure.id, "summary": procedure.summary},
            ),
        )
    except (WorkspaceError, ObjectiveError) as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = procedure.model_dump(mode="json")
    _emit(
        payload,
        json_output=json_output,
        text=f"procedure: {procedure.id} {procedure.summary}",
    )


@procedures_app.command("list", help="List red-team procedures.")
def procedure_list_command(
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to inspect.",
        ),
    ] = DEFAULT_WORKSPACE,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print procedures as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    try:
        state = load_workspace(workspace)
        document = load_procedures(state.root)
    except (WorkspaceError, ObjectiveError) as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = {
        "workspace": str(state.root),
        "count": len(document.procedures),
        "procedures": [procedure.model_dump(mode="json") for procedure in document.procedures],
    }
    if json_output:
        _emit(payload, json_output=True, text="")
        return
    if not document.procedures:
        typer.echo("No procedures.")
        return
    for procedure in document.procedures:
        technique = procedure.technique_id or "-"
        typer.echo(f"{procedure.id}\t{technique}\t{procedure.summary}")


@detections_app.command("add-ioc", help="Add or update an IOC for blue-team handoff.")
def detection_ioc_add_command(
    ioc_type: Annotated[
        IOCType,
        typer.Option("--type", help="IOC type."),
    ],
    value: Annotated[
        str,
        typer.Option("--value", help="IOC value."),
    ],
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to create or update.",
        ),
    ] = DEFAULT_WORKSPACE,
    first_observed: Annotated[
        str | None,
        typer.Option("--first-observed", help="First observed timestamp."),
    ] = None,
    last_observed: Annotated[
        str | None,
        typer.Option("--last-observed", help="Last observed timestamp."),
    ] = None,
    evidence_ids: Annotated[
        list[str] | None,
        typer.Option("--evidence-id", help="Evidence record ID to link; repeatable."),
    ] = None,
    timeline_event_ids: Annotated[
        list[str] | None,
        typer.Option("--event-id", help="Timeline event ID to link; repeatable."),
    ] = None,
    procedure_ids: Annotated[
        list[str] | None,
        typer.Option("--procedure-id", help="Procedure ID to link; repeatable."),
    ] = None,
    sensitivity: Annotated[
        DetectionSensitivity,
        typer.Option("--sensitivity", help="IOC sensitivity marker."),
    ] = "sensitive",
    confidence: Annotated[
        DetectionConfidence,
        typer.Option("--confidence", help="IOC confidence."),
    ] = "medium",
    tags: Annotated[
        list[str] | None,
        typer.Option("--tag", help="IOC tag; repeatable."),
    ] = None,
    notes: Annotated[
        str | None,
        typer.Option("--notes", help="IOC note."),
    ] = None,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print the IOC as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    try:
        state = create_workspace(workspace)
        _document, ioc = add_ioc(
            state,
            ioc_type=ioc_type,
            value=value,
            first_observed=first_observed,
            last_observed=last_observed,
            evidence_ids=evidence_ids,
            timeline_event_ids=timeline_event_ids,
            procedure_ids=procedure_ids,
            sensitivity=sensitivity,
            confidence=confidence,
            tags=tags,
            notes=notes,
        )
        output_digest = file_sha256(state.root / DETECTIONS_FILE)
        append_workspace_audit_event(
            state,
            AuditEvent(
                timestamp=utc_now(),
                command="detections add-ioc",
                output_path=DETECTIONS_FILE,
                output_sha256=output_digest,
                summary={"ioc_id": ioc.id, "type": ioc.type, "value": ioc.value},
            ),
        )
    except (WorkspaceError, DetectionError) as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = ioc.model_dump(mode="json")
    _emit(payload, json_output=json_output, text=f"ioc: {ioc.id} {ioc.type} {ioc.value}")


@detections_app.command("add-note", help="Add a blue-team detection handoff note.")
def detection_note_add_command(
    title: Annotated[
        str,
        typer.Option("--title", "-t", help="Detection note title."),
    ],
    body: Annotated[
        str,
        typer.Option("--body", "-b", help="Detection note body."),
    ],
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to create or update.",
        ),
    ] = DEFAULT_WORKSPACE,
    evidence_ids: Annotated[
        list[str] | None,
        typer.Option("--evidence-id", help="Evidence record ID to link; repeatable."),
    ] = None,
    timeline_event_ids: Annotated[
        list[str] | None,
        typer.Option("--event-id", help="Timeline event ID to link; repeatable."),
    ] = None,
    procedure_ids: Annotated[
        list[str] | None,
        typer.Option("--procedure-id", help="Procedure ID to link; repeatable."),
    ] = None,
    finding_ids: Annotated[
        list[str] | None,
        typer.Option("--finding-id", help="Finding ID to link; repeatable."),
    ] = None,
    sensitivity: Annotated[
        DetectionSensitivity,
        typer.Option("--sensitivity", help="Detection note sensitivity marker."),
    ] = "sensitive",
    tags: Annotated[
        list[str] | None,
        typer.Option("--tag", help="Detection note tag; repeatable."),
    ] = None,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print the detection note as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    try:
        state = create_workspace(workspace)
        _document, note = add_detection_note(
            state,
            title=title,
            body=body,
            evidence_ids=evidence_ids,
            timeline_event_ids=timeline_event_ids,
            procedure_ids=procedure_ids,
            finding_ids=finding_ids,
            sensitivity=sensitivity,
            tags=tags,
        )
        output_digest = file_sha256(state.root / DETECTIONS_FILE)
        append_workspace_audit_event(
            state,
            AuditEvent(
                timestamp=utc_now(),
                command="detections add-note",
                output_path=DETECTIONS_FILE,
                output_sha256=output_digest,
                summary={"note_id": note.id, "title": note.title},
            ),
        )
    except (WorkspaceError, DetectionError) as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = note.model_dump(mode="json")
    _emit(payload, json_output=json_output, text=f"detection-note: {note.id} {note.title}")


@detections_app.command("list", help="List IOCs and detection notes.")
def detection_list_command(
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to inspect.",
        ),
    ] = DEFAULT_WORKSPACE,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print detection data as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    try:
        state = load_workspace(workspace)
        document = load_detections(state.root)
    except (WorkspaceError, DetectionError) as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = {
        "workspace": str(state.root),
        "ioc_count": len(document.iocs),
        "note_count": len(document.notes),
        "iocs": [ioc.model_dump(mode="json") for ioc in document.iocs],
        "notes": [note.model_dump(mode="json") for note in document.notes],
    }
    if json_output:
        _emit(payload, json_output=True, text="")
        return
    if not document.iocs and not document.notes:
        typer.echo("No detection handoff records.")
        return
    for ioc in document.iocs:
        typer.echo(f"ioc\t{ioc.id}\t{ioc.type}\t{ioc.value}")
    for note in document.notes:
        typer.echo(f"note\t{note.id}\t{note.title}")


@app.command("report", help="Generate pentest report artifacts from a workspace.")
def report_command(
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to render.",
        ),
    ] = DEFAULT_WORKSPACE,
    output_format: Annotated[
        ReportOutputFormat,
        typer.Option("--format", "-f", help="Report artifact format."),
    ] = "md",
    report_type: Annotated[
        ReportType,
        typer.Option("--type", help="Report type."),
    ] = "pentest",
    output: Annotated[
        Path | None,
        typer.Option(
            "--output",
            "-o",
            dir_okay=True,
            file_okay=False,
            help="Directory for report artifacts. Defaults to workspace/reports.",
        ),
    ] = None,
    pdf_backend: Annotated[
        PdfBackend,
        typer.Option("--pdf-backend", help="PDF backend used when --format pdf."),
    ] = "weasyprint",
    redact_sensitive_evidence: Annotated[
        bool,
        typer.Option(
            "--redact-sensitive-evidence/--include-sensitive-evidence",
            help="Redact evidence snippets marked sensitive in the workspace.",
        ),
    ] = True,
    include_raw_evidence: Annotated[
        bool,
        typer.Option(
            "--include-raw-evidence",
            help="Include raw evidence files in red-team archive exports.",
        ),
    ] = False,
    include_secret_raw_evidence: Annotated[
        bool,
        typer.Option(
            "--include-secret-raw-evidence",
            help="Include raw evidence marked secret in red-team archive exports.",
        ),
    ] = False,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print artifact metadata as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    try:
        state = load_workspace(workspace)
        output_dir = output or workspace_path(state.root, "reports", allowed_roots=("reports",))
        if report_type == "red-team":
            red_team_report = build_red_team_report(
                state,
                redact_sensitive_evidence=redact_sensitive_evidence,
            )
            artifact_path = render_red_team_report_artifact(
                red_team_report,
                output_dir=output_dir,
                output_format=output_format,
                pdf_backend=pdf_backend,
                workspace_root=state.root,
                include_raw_evidence=include_raw_evidence,
                include_secret_raw_evidence=include_secret_raw_evidence,
            )
        else:
            if output_format == "archive":
                raise ReportRenderError("archive format is only supported for red-team reports")
            report_model = build_pentest_report(
                state,
                redact_sensitive_evidence=redact_sensitive_evidence,
            )
            artifact_path = render_report_artifact(
                report_model,
                output_dir=output_dir,
                output_format=cast(ReportFormat, output_format),
                pdf_backend=pdf_backend,
            )
    except (WorkspaceError, ReportRenderError, RedTeamReportError) as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = {
        "format": output_format,
        "type": report_type,
        "pdf_backend": pdf_backend if output_format == "pdf" else None,
        "path": str(artifact_path),
        "sha256": file_sha256(artifact_path),
    }
    _emit(payload, json_output=json_output, text=f"report: {artifact_path}")


@app.command("retest", help="Compare two workspaces and classify finding lifecycle status.")
def retest_command(
    baseline: Annotated[
        Path,
        typer.Option(
            "--baseline",
            dir_okay=True,
            file_okay=False,
            help="Baseline workspace directory.",
        ),
    ],
    current: Annotated[
        Path,
        typer.Option(
            "--current",
            dir_okay=True,
            file_okay=False,
            help="Current workspace directory.",
        ),
    ],
    output: Annotated[
        Path,
        typer.Option(
            "--output",
            "-o",
            dir_okay=False,
            file_okay=True,
            help="Retest output path. Use .json or .md.",
        ),
    ] = Path("retest.json"),
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print retest summary as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    try:
        result = compare_workspaces(baseline, current)
        output_path = write_retest_output(result, output)
        current_state = load_workspace(current)
        append_retest_audit(current_state, result, output_path)
    except (WorkspaceError, RetestError) as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = {
        "path": str(output_path),
        "sha256": file_sha256(output_path),
        "summary": result.summary,
        "ambiguous": len(result.ambiguous_matches),
    }
    _emit(payload, json_output=json_output, text=f"retest: {output_path}")


@app.command("sign", help="Create or verify a local chain-of-custody manifest.")
def sign_command(
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to sign or verify.",
        ),
    ] = DEFAULT_WORKSPACE,
    verify: Annotated[
        bool,
        typer.Option("--verify", help="Verify the latest manifest instead of creating one."),
    ] = False,
    manifest: Annotated[
        Path | None,
        typer.Option(
            "--manifest",
            dir_okay=False,
            file_okay=True,
            help="Specific manifest to verify. Defaults to latest signatures/manifest-*.json.",
        ),
    ] = None,
    json_output: Annotated[
        bool,
        typer.Option("--json", help="Print manifest or verification summary as JSON."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    try:
        if verify:
            result = verify_workspace(workspace, manifest_path=manifest)
            payload = verification_result_payload(result)
            if json_output:
                typer.echo(json.dumps(payload, indent=2, sort_keys=True))
            elif result.ok:
                typer.echo(f"manifest verified: {result.manifest_path}")
            else:
                typer.echo(f"manifest verification failed: {result.manifest_path}")
                for failure in result.failures:
                    typer.echo(
                        f"- {failure.path}: {failure.message} "
                        f"(expected={failure.expected_sha256}, "
                        f"actual={failure.actual_sha256})"
                    )
            if not result.ok:
                raise typer.Exit(code=EXIT_OPERATION_FAILED)
            return

        manifest_model, manifest_path = sign_workspace(workspace)
    except SigningError as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = {
        "manifest_id": manifest_model.manifest_id,
        "path": str(manifest_path),
        "artifacts": len(manifest_model.artifacts),
        "audit_events": len(manifest_model.audit_chain),
        "sha256": file_sha256(manifest_path),
    }
    _emit(payload, json_output=json_output, text=f"manifest: {manifest_path}")


@app.command("serve", help="Preview a pentest workspace report on a local-only web UI.")
def serve_command(
    workspace: Annotated[
        Path,
        typer.Option(
            "--workspace",
            "-w",
            dir_okay=True,
            file_okay=False,
            help="Workspace directory to preview.",
        ),
    ] = DEFAULT_WORKSPACE,
    host: Annotated[
        str,
        typer.Option("--host", help="Bind host. Defaults to loopback for local-only preview."),
    ] = "127.0.0.1",
    port: Annotated[
        int,
        typer.Option("--port", "-p", min=0, max=65535, help="Bind port."),
    ] = 8765,
    unsafe_bind: Annotated[
        bool,
        typer.Option(
            "--unsafe-bind",
            help="Allow binding to a non-loopback host after printing a security warning.",
        ),
    ] = False,
    open_browser: Annotated[
        bool,
        typer.Option("--open/--no-open", help="Open the preview URL in the default browser."),
    ] = False,
    json_errors: Annotated[
        bool,
        typer.Option("--json-errors", help="Print command errors as JSON."),
    ] = False,
) -> None:
    if not is_loopback_host(host):
        warning = (
            f"WARNING: {host} is not a loopback bind address. The workspace preview "
            "may expose pentest evidence to the local network."
        )
        if not unsafe_bind:
            _fail(f"{warning} Rerun with --unsafe-bind to acknowledge.", json_errors=json_errors)
        typer.echo(warning, err=True)
    try:
        server = create_workspace_server(workspace, host=host, port=port)
    except (WorkspaceError, WorkspaceServerError, OSError) as exc:
        _fail(str(exc), json_errors=json_errors)
    address = server.server_address[0]
    host_name = address.decode() if isinstance(address, bytes) else str(address)
    url = f"http://{host_name}:{server.server_address[1]}"
    typer.echo(f"serve: {url}")
    if open_browser:
        webbrowser.open(url)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
