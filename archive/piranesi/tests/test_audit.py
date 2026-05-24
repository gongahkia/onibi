from __future__ import annotations

import json
from pathlib import Path

from piranesi.audit import append_audit_event


def test_append_audit_event_writes_jsonl_payload(tmp_path: Path) -> None:
    log_path = append_audit_event(
        output_dir=tmp_path / "output",
        event_type="workspace_report_rendered",
        stage="report",
        approved=True,
        details={"artifact": "pentest-report.json"},
    )

    payload = json.loads(log_path.read_text(encoding="utf-8"))
    assert payload["event_type"] == "workspace_report_rendered"
    assert payload["stage"] == "report"
    assert payload["approved"] is True
    assert payload["details"] == {"artifact": "pentest-report.json"}
