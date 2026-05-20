from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.adapters.c2 import C2ParseError, parse_c2_jsonl_file
from piranesi.cli import app
from piranesi.evidence import load_evidence_index
from piranesi.timeline import load_timeline_events
from piranesi.workspace import AUDIT_LOG_FILE, TIMELINE_FILE, file_sha256

C2_FIXTURE = Path(__file__).parent / "fixtures" / "redteam" / "c2" / "mock-c2-events.jsonl"
runner = CliRunner()


def test_parse_neutral_c2_jsonl_fixture() -> None:
    result = parse_c2_jsonl_file(
        C2_FIXTURE,
        input_sha256=file_sha256(C2_FIXTURE),
        raw_path="raw/c2-log/mock-c2-events.jsonl",
    )

    assert result.metadata["valid_records"] == 2
    assert result.metadata["event_types"] == ["beacon-checkin", "session-created"]
    assert [event.summary for event in result.events] == [
        "C2 session-created for lab-workstation-01",
        "C2 beacon-checkin for lab-workstation-01",
    ]
    assert result.events[0].actor == "lab-operator"
    assert result.events[0].tags == ["c2", "mock-local-lab", "session-created"]


def test_parse_c2_rejects_empty_or_fully_invalid_inputs(tmp_path: Path) -> None:
    empty = tmp_path / "empty.jsonl"
    empty.write_text("\n", encoding="utf-8")
    invalid = tmp_path / "invalid.jsonl"
    invalid.write_text('{"event":"checkin"}\n[]\n', encoding="utf-8")

    for path, expected in [
        (empty, "empty C2 JSONL"),
        (invalid, "no valid event records"),
    ]:
        try:
            parse_c2_jsonl_file(
                path,
                input_sha256=file_sha256(path),
                raw_path=f"raw/c2-log/{path.name}",
            )
        except C2ParseError as exc:
            assert expected in str(exc)
        else:
            raise AssertionError("expected C2ParseError")


def test_ingest_c2_cli_preserves_log_and_appends_timeline(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"

    result = runner.invoke(
        app,
        [
            "ingest",
            "c2",
            "--input",
            str(C2_FIXTURE),
            "--workspace",
            str(workspace),
            "--title",
            "Mock C2 event log",
            "--source",
            "authorized-local-lab",
            "--tag",
            "validation",
            "--json",
        ],
    )

    assert result.exit_code == 0, result.output
    payload = json.loads(result.stdout)
    assert payload["events"] == 2
    assert payload["records"] == 2
    assert payload["warnings"] == []

    evidence = load_evidence_index(workspace).evidence
    events = load_timeline_events(workspace)
    assert len(evidence) == 1
    assert evidence[0].kind == "c2-log"
    assert evidence[0].title == "Mock C2 event log"
    assert evidence[0].tags == ["validation"]
    assert (workspace / evidence[0].raw_path).read_text(encoding="utf-8") == C2_FIXTURE.read_text(
        encoding="utf-8"
    )
    assert [event.summary for event in events] == [
        "C2 session-created for lab-workstation-01",
        "C2 beacon-checkin for lab-workstation-01",
    ]
    assert all(event.evidence_ids == [evidence[0].id] for event in events)
    assert all(event.phase == "c2" for event in events)

    audit_events = [
        json.loads(line)
        for line in (workspace / AUDIT_LOG_FILE).read_text(encoding="utf-8").splitlines()
    ]
    assert audit_events[-1]["command"] == "ingest c2"
    assert audit_events[-1]["input_sha256"] == file_sha256(C2_FIXTURE)
    assert audit_events[-1]["output_path"] == TIMELINE_FILE


def test_ingest_c2_cli_reports_missing_files(tmp_path: Path) -> None:
    result = runner.invoke(
        app,
        [
            "ingest",
            "c2",
            "--input",
            str(tmp_path / "missing.jsonl"),
            "--workspace",
            str(tmp_path / "ws"),
        ],
    )

    assert result.exit_code == 2
    assert "input file does not exist" in result.output
