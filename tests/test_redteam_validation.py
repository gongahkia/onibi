from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app

FIXTURE_ROOT = Path(__file__).parent / "fixtures"
REDTEAM_FIXTURE_ROOT = FIXTURE_ROOT / "redteam"
NMAP_FIXTURE = FIXTURE_ROOT / "pentest" / "nmap" / "localhost-http.xml"
NUCLEI_FIXTURE = FIXTURE_ROOT / "pentest" / "nuclei" / "localhost-http.jsonl"

runner = CliRunner()


def test_authorized_lab_red_team_workspace_validation_flow(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    _invoke(
        [
            "ingest",
            "init",
            "--workspace",
            str(workspace),
            "--client",
            "Authorized Local Lab",
            "--project",
            "Red-team workspace validation",
            "--scope",
            "127.0.0.1",
            "--scope",
            "lab.local",
        ]
    )

    note_id = _add_evidence(
        workspace,
        "notes/operator-note.md",
        "note",
        "Operator validation note",
    )
    transcript_id = _add_evidence(
        workspace,
        "transcripts/operator-terminal.txt",
        "transcript",
        "Terminal transcript",
    )
    c2_log_id = _add_evidence(
        workspace,
        "c2/mock-c2-events.jsonl",
        "c2-log",
        "Mock C2 event log",
    )
    screenshot_id = _add_evidence(
        workspace,
        "screenshots/portal-login.svg",
        "screenshot",
        "Synthetic portal screenshot",
    )

    _invoke(["ingest", "nmap", "--workspace", str(workspace), "--input", str(NMAP_FIXTURE)])
    _invoke(["ingest", "nuclei", "--workspace", str(workspace), "--input", str(NUCLEI_FIXTURE)])
    finding_id = _first_finding_id(workspace)

    timeline = _invoke_json(
        [
            "timeline",
            "add",
            "--workspace",
            str(workspace),
            "--timestamp",
            "2026-05-20T00:05:00+00:00",
            "--phase",
            "initial-access",
            "--actor",
            "lab-operator",
            "--summary",
            "Captured local-lab portal behavior and mock session activity",
            "--evidence-id",
            note_id,
            "--evidence-id",
            transcript_id,
            "--evidence-id",
            c2_log_id,
            "--finding-id",
            finding_id,
            "--confidence",
            "high",
            "--json",
        ]
    )
    objective = _invoke_json(
        [
            "objectives",
            "add",
            "--workspace",
            str(workspace),
            "--title",
            "Validate local-lab evidence handoff",
            "--status",
            "achieved",
            "--owner",
            "lab-operator",
            "--target-asset",
            "127.0.0.1",
            "--success-criterion",
            "Evidence, findings, timeline, detections, report, and manifest exist",
            "--evidence-id",
            note_id,
            "--event-id",
            timeline["id"],
            "--json",
        ]
    )
    procedure = _invoke_json(
        [
            "procedures",
            "add",
            "--workspace",
            str(workspace),
            "--summary",
            "Reviewed synthetic portal and preserved operator activity",
            "--tactic",
            "Discovery",
            "--technique-id",
            "T1083",
            "--technique-name",
            "File and Directory Discovery",
            "--command",
            "curl -I http://127.0.0.1:8080/",
            "--evidence-id",
            transcript_id,
            "--event-id",
            timeline["id"],
            "--finding-id",
            finding_id,
            "--objective-id",
            objective["id"],
            "--json",
        ]
    )
    _invoke(
        [
            "detections",
            "add-ioc",
            "--workspace",
            str(workspace),
            "--type",
            "domain",
            "--value",
            "c2.lab.local",
            "--first-observed",
            "2026-05-20T00:03:00+00:00",
            "--evidence-id",
            c2_log_id,
            "--event-id",
            timeline["id"],
            "--procedure-id",
            procedure["id"],
            "--confidence",
            "high",
        ]
    )
    _invoke(
        [
            "detections",
            "add-note",
            "--workspace",
            str(workspace),
            "--title",
            "Local-lab detection handoff",
            "--body",
            "Correlate mock check-in events with portal access during the validation window.",
            "--evidence-id",
            screenshot_id,
            "--event-id",
            timeline["id"],
            "--procedure-id",
            procedure["id"],
            "--finding-id",
            finding_id,
        ]
    )

    redteam_json = _invoke_json(
        [
            "report",
            "--workspace",
            str(workspace),
            "--type",
            "red-team",
            "--format",
            "json",
            "--include-sensitive-evidence",
            "--json",
        ]
    )
    redteam_md = _invoke_json(
        [
            "report",
            "--workspace",
            str(workspace),
            "--type",
            "red-team",
            "--format",
            "md",
            "--include-sensitive-evidence",
            "--json",
        ]
    )
    manifest = _invoke_json(["sign", "--workspace", str(workspace), "--json"])
    verified = _invoke_json(["sign", "--workspace", str(workspace), "--verify", "--json"])

    report_payload = json.loads(Path(redteam_json["path"]).read_text(encoding="utf-8"))
    markdown = Path(redteam_md["path"]).read_text(encoding="utf-8")
    manifest_payload = json.loads(Path(manifest["path"]).read_text(encoding="utf-8"))

    assert (workspace / "workspace.json").is_file()
    assert (workspace / "evidence" / "index.json").is_file()
    assert (workspace / "timeline" / "events.jsonl").is_file()
    assert Path(redteam_json["path"]).is_file()
    assert Path(redteam_md["path"]).is_file()
    assert verified["ok"] is True
    assert report_payload["executive_summary"]["evidence_count"] == 4
    assert report_payload["executive_summary"]["timeline_event_count"] == 1
    assert report_payload["executive_summary"]["finding_count"] > 0
    assert report_payload["timeline"][0]["summary"].startswith("Captured local-lab")
    assert report_payload["findings"]
    assert "## Timeline" in markdown
    assert "Captured local-lab portal behavior" in markdown
    assert "## Findings" in markdown
    assert any(
        artifact["path"] == "reports/red-team-report.json"
        for artifact in manifest_payload["artifacts"]
    )
    assert any(
        artifact["path"] == "reports/red-team-report.md"
        for artifact in manifest_payload["artifacts"]
    )


def _add_evidence(workspace: Path, relative_path: str, kind: str, title: str) -> str:
    payload = _invoke_json(
        [
            "evidence",
            "add",
            "--workspace",
            str(workspace),
            "--file",
            str(REDTEAM_FIXTURE_ROOT / relative_path),
            "--kind",
            kind,
            "--title",
            title,
            "--sensitivity",
            "internal",
            "--source",
            "authorized-local-lab",
            "--tag",
            "validation",
            "--json",
        ]
    )
    return str(payload["id"])


def _first_finding_id(workspace: Path) -> str:
    payload = json.loads((workspace / "normalized" / "findings.json").read_text(encoding="utf-8"))
    findings = payload["findings"]
    assert findings
    return str(findings[0]["id"])


def _invoke_json(args: list[str]) -> dict[str, object]:
    result = _invoke(args)
    return json.loads(result.stdout)


def _invoke(args: list[str]):
    result = runner.invoke(app, args)
    assert result.exit_code == 0, result.output
    return result
