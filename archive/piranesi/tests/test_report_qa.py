from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.evidence import add_evidence_file
from piranesi.report_qa import validate_delivery
from piranesi.workspace import (
    EvidenceSnippet,
    NormalizedFinding,
    SourceReference,
    create_workspace,
    load_workspace,
    upsert_findings,
    utc_now,
)

runner = CliRunner()


def test_delivery_qa_passes_complete_signed_handoff(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    state = create_workspace(workspace)
    evidence_file = tmp_path / "proof.txt"
    evidence_file.write_text("proof\n", encoding="utf-8")
    _, evidence = add_evidence_file(
        workspace,
        file_path=evidence_file,
        kind="note",
        title="Proof note",
        sensitivity="public",
    )
    state = load_workspace(workspace)
    now = utc_now()
    upsert_findings(
        state,
        [
            NormalizedFinding(
                id="finding:complete",
                title="Critical finding with complete guidance",
                severity="critical",
                confidence="confirmed",
                description="Demonstrates a complete delivery finding.",
                remediation="Patch the exposed service and retest the affected route.",
                evidence=[EvidenceSnippet(kind="note", value="proof")],
                source_references=[
                    SourceReference(
                        tool="operator",
                        input_sha256=evidence.sha256,
                        raw_path=evidence.raw_path,
                    )
                ],
                first_seen=now,
                last_seen=now,
                provenance={"retest_guidance": "Repeat the proof request after patching."},
            )
        ],
    )

    report = runner.invoke(app, ["report", "--workspace", str(workspace), "--format", "json"])
    handoff = runner.invoke(
        app,
        [
            "integrations",
            "email-handoff",
            "--workspace",
            str(workspace),
            "--to",
            "client@example.com",
        ],
    )
    sign = runner.invoke(app, ["sign", "--workspace", str(workspace)])

    assert report.exit_code == 0, report.output
    assert handoff.exit_code == 0, handoff.output
    assert sign.exit_code == 0, sign.output

    result = validate_delivery(workspace)

    assert result.valid is True
    assert result.error_count == 0
    assert result.warning_count == 0
    assert {artifact.kind for artifact in result.artifacts} >= {
        "report",
        "handoff-draft",
        "handoff-manifest",
    }
    assert all(artifact.covered_by for artifact in result.artifacts)


def test_delivery_qa_empty_workspace_returns_warnings(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    create_workspace(workspace)

    result = runner.invoke(
        app,
        ["ci", "validate-delivery", "--workspace", str(workspace), "--json"],
    )

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    assert payload["valid"] is True
    assert payload["error_count"] == 0
    codes = {issue["code"] for issue in payload["issues"]}
    assert {"workspace-empty", "no-report-artifacts", "unsigned-delivery"} <= codes


def test_delivery_qa_fails_missing_evidence_reference_and_retest_guidance(
    tmp_path: Path,
) -> None:
    workspace = tmp_path / "workspace"
    state = create_workspace(workspace)
    now = utc_now()
    upsert_findings(
        state,
        [
            NormalizedFinding(
                id="finding:broken",
                title="High finding without delivery support",
                severity="high",
                confidence="confirmed",
                source_references=[
                    SourceReference(
                        tool="operator",
                        input_sha256="a" * 64,
                        raw_path="raw/operator/missing.txt",
                    )
                ],
                first_seen=now,
                last_seen=now,
            )
        ],
    )

    result = runner.invoke(
        app,
        ["ci", "validate-delivery", "--workspace", str(workspace), "--json"],
    )

    assert result.exit_code == 1, result.output
    payload = json.loads(result.stdout)
    codes = {issue["code"] for issue in payload["issues"]}
    assert "finding-source-reference-missing" in codes
    assert "high-finding-missing-remediation" in codes
    assert "high-finding-missing-retest-guidance" in codes


def test_delivery_qa_fails_stale_email_handoff_manifest(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    init = runner.invoke(app, ["ingest", "init", "--workspace", str(workspace)])
    report = runner.invoke(app, ["report", "--workspace", str(workspace), "--format", "json"])
    handoff = runner.invoke(
        app,
        [
            "integrations",
            "email-handoff",
            "--workspace",
            str(workspace),
            "--to",
            "client@example.com",
            "--json",
        ],
    )
    assert init.exit_code == 0, init.output
    assert report.exit_code == 0, report.output
    assert handoff.exit_code == 0, handoff.output

    manifest_path = Path(json.loads(handoff.stdout)["manifest_path"])
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    manifest["artifacts"][0]["sha256"] = "0" * 64
    manifest_path.write_text(
        json.dumps(manifest, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )

    result = validate_delivery(workspace)

    assert result.valid is False
    assert any(issue.code == "handoff-reference-digest-mismatch" for issue in result.issues)
