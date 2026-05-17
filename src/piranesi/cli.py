from __future__ import annotations

import json
import os
import webbrowser
from pathlib import Path
from typing import Annotated, Any, NoReturn

import typer

from piranesi import __version__
from piranesi.adapters import (
    NmapParseError,
    NucleiParseError,
    parse_nmap_xml_file,
    parse_nuclei_jsonl_file,
)
from piranesi.report.pentest import (
    PdfBackend,
    ReportFormat,
    ReportRenderError,
    build_pentest_report,
    render_report_artifact,
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
from piranesi.workspace import (
    FINDINGS_FILE,
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

app = typer.Typer(
    add_completion=False,
    help="Local-first pentest and red-team report engine.",
    no_args_is_help=True,
)
ingest_app = typer.Typer(
    add_completion=False,
    help="Create or update a local pentest workspace from tool exports.",
    no_args_is_help=True,
)
app.add_typer(ingest_app, name="ingest")


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
        ReportFormat,
        typer.Option("--format", "-f", help="Report artifact format."),
    ] = "md",
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
        report_model = build_pentest_report(
            state,
            redact_sensitive_evidence=redact_sensitive_evidence,
        )
        artifact_path = render_report_artifact(
            report_model,
            output_dir=output_dir,
            output_format=output_format,
            pdf_backend=pdf_backend,
        )
    except (WorkspaceError, ReportRenderError) as exc:
        _fail(str(exc), json_errors=json_errors)

    payload = {
        "format": output_format,
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
