from __future__ import annotations

import json
from pathlib import Path

import pytest
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.workspace import (
    AUDIT_LOG_FILE,
    EVIDENCE_FILE,
    FINDINGS_FILE,
    WORKSPACE_FILE,
    AuditEvent,
    EngagementMetadata,
    EvidenceSnippet,
    NormalizedFinding,
    ServiceContext,
    SourceReference,
    WorkspaceError,
    append_audit_event,
    create_workspace,
    deterministic_finding_id,
    load_workspace,
    upsert_findings,
    utc_now,
    workspace_path,
)

runner = CliRunner()


def test_create_workspace_writes_versioned_layout(tmp_path: Path) -> None:
    state = create_workspace(
        tmp_path / "engagement",
        engagement=EngagementMetadata(
            client="Acme Corp",
            project="External pentest",
            scope=["127.0.0.1"],
        ),
    )

    assert (state.root / WORKSPACE_FILE).is_file()
    assert (state.root / FINDINGS_FILE).is_file()
    assert (state.root / AUDIT_LOG_FILE).is_file()
    assert (state.root / EVIDENCE_FILE).is_file()
    assert (state.root / "raw").is_dir()
    assert (state.root / "evidence").is_dir()
    assert (state.root / "timeline").is_dir()
    assert (state.root / "objectives").is_dir()
    assert (state.root / "procedures").is_dir()
    assert (state.root / "detections").is_dir()
    assert (state.root / "reports").is_dir()
    assert (state.root / "signatures").is_dir()

    reloaded = load_workspace(state.root)
    assert reloaded.workspace.schema_version == "piranesi.workspace.v1"
    assert reloaded.workspace.engagement.client == "Acme Corp"
    assert reloaded.findings.schema_version == "piranesi.findings.v1"


def test_workspace_validation_fails_closed_for_missing_fields(tmp_path: Path) -> None:
    state = create_workspace(tmp_path / "engagement")
    payload = json.loads((state.root / WORKSPACE_FILE).read_text(encoding="utf-8"))
    del payload["created_at"]
    (state.root / WORKSPACE_FILE).write_text(json.dumps(payload), encoding="utf-8")

    with pytest.raises(WorkspaceError, match="invalid workspace schema"):
        load_workspace(state.root)


def test_workspace_validation_rejects_unsupported_versions(tmp_path: Path) -> None:
    state = create_workspace(tmp_path / "engagement")
    payload = json.loads((state.root / WORKSPACE_FILE).read_text(encoding="utf-8"))
    payload["schema_version"] = "piranesi.workspace.v999"
    (state.root / WORKSPACE_FILE).write_text(json.dumps(payload), encoding="utf-8")

    with pytest.raises(WorkspaceError, match="unsupported workspace schema version"):
        load_workspace(state.root)


def test_workspace_path_rejects_traversal_and_absolute_paths(tmp_path: Path) -> None:
    state = create_workspace(tmp_path / "engagement")

    assert workspace_path(state.root, "raw/nmap/scan.xml", allowed_roots=("raw",)).parent.name == (
        "nmap"
    )
    with pytest.raises(WorkspaceError, match="traversal"):
        workspace_path(state.root, "raw/../workspace.json", allowed_roots=("raw",))
    with pytest.raises(WorkspaceError, match="relative"):
        workspace_path(state.root, (tmp_path / "scan.xml").resolve(), allowed_roots=("raw",))
    with pytest.raises(WorkspaceError, match="under one of"):
        workspace_path(state.root, "workspace.json", allowed_roots=("raw", "reports"))


def test_upsert_findings_is_deterministic_and_merges_reingest(tmp_path: Path) -> None:
    state = create_workspace(tmp_path / "engagement")
    first_seen = utc_now()
    finding = NormalizedFinding(
        id=deterministic_finding_id("nmap", "127.0.0.1", "tcp", "22", "ssh"),
        title="Open tcp/22 ssh service",
        severity="info",
        confidence="confirmed",
        asset="127.0.0.1",
        service=ServiceContext(port=22, protocol="tcp", name="ssh"),
        evidence=[EvidenceSnippet(kind="service", value="Open ssh service on tcp/22")],
        source_references=[
            SourceReference(
                tool="nmap",
                input_sha256="a" * 64,
                raw_path="raw/nmap/scan.xml",
                locator="host[127.0.0.1]/port[tcp/22]",
            )
        ],
        first_seen=first_seen,
        last_seen=first_seen,
    )

    state = upsert_findings(state, [finding])
    state = upsert_findings(state, [finding])

    assert len(state.findings.findings) == 1
    assert state.findings.findings[0].id == finding.id


def test_append_audit_event_records_required_chain_fields(tmp_path: Path) -> None:
    state = create_workspace(tmp_path / "engagement")
    append_audit_event(
        state,
        AuditEvent(
            timestamp=utc_now(),
            command="ingest nmap",
            input_path="raw/nmap/scan.xml",
            input_sha256="b" * 64,
            output_path=FINDINGS_FILE,
            output_sha256="c" * 64,
            summary={"created": 1, "updated": 0},
        ),
    )

    events = [
        json.loads(line)
        for line in (state.root / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert events == [
        {
            "command": "ingest nmap",
            "input_path": "raw/nmap/scan.xml",
            "input_sha256": "b" * 64,
            "output_path": FINDINGS_FILE,
            "output_sha256": "c" * 64,
            "schema_version": "piranesi.audit-event.v1",
            "summary": {"created": 1, "updated": 0},
            "timestamp": events[0]["timestamp"],
        }
    ]


def test_cli_ingest_init_initializes_workspace(tmp_path: Path) -> None:
    workspace = tmp_path / "engagement"

    result = runner.invoke(
        app,
        [
            "ingest",
            "init",
            "--workspace",
            str(workspace),
            "--client",
            "Acme Corp",
            "--project",
            "External pentest",
            "--scope",
            "127.0.0.1",
            "--json",
        ],
    )

    assert result.exit_code == 0, result.stdout
    payload = json.loads(result.stdout)
    assert payload["schema_version"] == "piranesi.workspace.v1"
    assert payload["findings"] == 0

    state = load_workspace(workspace)
    assert state.workspace.engagement.client == "Acme Corp"
    assert state.workspace.engagement.scope == ["127.0.0.1"]
