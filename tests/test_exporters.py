from __future__ import annotations

import csv
import json
from io import StringIO
from pathlib import Path
from typing import Any

import pytest

from piranesi.exporters import (
    build_webhook_payload,
    export_csv,
    export_github_issues,
    export_jira_issues,
    export_sarif,
    export_webhook,
    generate_csv,
    generate_sarif,
    ticket_dedupe_key,
)
from piranesi.host import analyze_snapshot, assess_fleet_evidence, load_host_input
from piranesi.host.report import write_host_report_outputs

HOST_FIXTURES = Path(__file__).parent / "fixtures" / "host"
FLEET_FIXTURES = Path(__file__).parent / "fixtures" / "fleet"


def _host_report_path(tmp_path: Path) -> Path:
    report = analyze_snapshot(load_host_input(HOST_FIXTURES / "debian-vulnerable"))
    output_dir = tmp_path / "host"
    write_host_report_outputs(report, output_dir, report_format="json")
    return output_dir / "host-report.json"


def test_sarif_export_from_host_findings(tmp_path: Path) -> None:
    report_path = _host_report_path(tmp_path)
    output = tmp_path / "host-report.sarif.json"

    result = export_sarif(report_path, output=output)
    payload = json.loads(output.read_text(encoding="utf-8"))

    assert result.item_count > 0
    assert payload["version"] == "2.1.0"
    assert payload["runs"][0]["results"]
    first_result = payload["runs"][0]["results"][0]
    assert first_result["properties"]["findingId"].startswith("host-")
    assert first_result["properties"]["dedupeKey"].startswith("piranesi-")
    assert (tmp_path / "audit-log.jsonl").is_file()


def test_generate_sarif_skips_suppressed_findings(tmp_path: Path) -> None:
    report = analyze_snapshot(load_host_input(HOST_FIXTURES / "debian-vulnerable"))
    report.findings[0].suppressed = True
    report.findings[0].suppression_reason = "accepted lab risk"

    sarif = generate_sarif(report)
    exported_ids = {result["properties"]["findingId"] for result in sarif["runs"][0]["results"]}

    assert report.findings[0].id not in exported_ids


def test_csv_export_from_host_report(tmp_path: Path) -> None:
    report_path = _host_report_path(tmp_path)
    output = tmp_path / "findings.csv"

    result = export_csv(report_path, output=output)
    rows = list(csv.DictReader(StringIO(output.read_text(encoding="utf-8"))))

    assert result.item_count == len(rows)
    assert rows
    assert rows[0]["target"] == "debian-vm-01"
    assert rows[0]["dedupe_key"].startswith("piranesi-")


def test_csv_export_from_fleet_report(tmp_path: Path) -> None:
    fleet_dir = tmp_path / "fleet"
    assess_fleet_evidence(FLEET_FIXTURES, fleet_dir, report_format="json")

    result = export_csv(fleet_dir / "fleet-report.json", output=tmp_path / "fleet.csv")
    rows = list(csv.DictReader(StringIO((tmp_path / "fleet.csv").read_text(encoding="utf-8"))))

    assert result.item_count == len(rows)
    assert rows
    assert {row["target"] for row in rows} >= {"fleet-vulnerable-01"}
    assert all(row["report_path"] for row in rows)


def test_generate_csv_accepts_host_model(tmp_path: Path) -> None:
    report = analyze_snapshot(load_host_input(HOST_FIXTURES / "debian-vulnerable"))

    body = generate_csv(report, report_path=tmp_path / "host-report.json")

    rows = list(csv.DictReader(StringIO(body)))
    assert rows
    assert rows[0]["finding_id"].startswith("host-")


def test_webhook_payload_shape_redacts_metadata_and_omits_snapshot() -> None:
    report = analyze_snapshot(load_host_input(HOST_FIXTURES / "debian-vulnerable"))
    report.host_metadata = {
        "hostname": "debian-vm-01",
        "ip_addresses": ["10.0.0.5"],
        "asset_tag": "lab",
    }

    payload = build_webhook_payload(report, report_path="host-report.json")
    rendered = json.dumps(payload)

    assert payload["target"] == "[redacted-host]"
    assert "snapshot" not in payload
    assert payload["host_metadata"]["hostname"] == "[redacted]"
    assert payload["host_metadata"]["ip_addresses"] == ["[redacted]"]
    assert "10.0.0.5" not in rendered
    assert "debian-vm-01" not in rendered
    assert payload["findings"]
    assert payload["findings"][0]["target"] == "[redacted-host]"


def test_ticket_deduplication_key_generation() -> None:
    one = ticket_dedupe_key("debian-vm-01", "host-abc")
    two = ticket_dedupe_key("DEBIAN-VM-01", "HOST-ABC")
    other = ticket_dedupe_key("other-host", "host-abc")

    assert one == two
    assert one != other
    assert one.startswith("piranesi-")


def test_dry_run_creates_no_external_side_effects(tmp_path: Path) -> None:
    report_path = _host_report_path(tmp_path)

    def fail_if_called(*_args: Any, **_kwargs: Any) -> Any:
        raise AssertionError("external requester should not be called during dry-run")

    webhook_result = export_webhook(
        report_path,
        url="https://example.invalid/hook",
        requester=fail_if_called,
    )
    github_result = export_github_issues(
        report_path,
        repo="example/repo",
        requester=fail_if_called,
    )

    assert webhook_result.dry_run is True
    assert webhook_result.sent is False
    assert github_result.dry_run is True
    assert github_result.created is False
    assert (report_path.parent / "audit-log.jsonl").is_file()


def test_mocked_external_exports_cover_success_payloads(tmp_path: Path) -> None:
    report_path = _host_report_path(tmp_path)
    calls: list[dict[str, Any]] = []

    class Response:
        def raise_for_status(self) -> None:
            return None

    def recorder(*args: Any, **kwargs: Any) -> Response:
        calls.append({"args": args, "kwargs": kwargs})
        return Response()

    webhook_result = export_webhook(
        report_path,
        url="https://hooks.slack.invalid/services/T000/B000/fixture",
        dry_run=False,
        yes=True,
        requester=recorder,
    )
    github_result = export_github_issues(
        report_path,
        repo="example/repo",
        dry_run=False,
        yes=True,
        token="ghp_fixture",  # noqa: S106 - fixture token is never sent to a real service.
        requester=recorder,
    )
    jira_result = export_jira_issues(
        report_path,
        project="SEC",
        url="https://example.atlassian.net",
        dry_run=False,
        yes=True,
        email="security@example.com",
        token="jira_fixture",  # noqa: S106 - fixture token is never sent to a real service.
        requester=recorder,
    )

    assert webhook_result.sent is True
    assert github_result.created is True
    assert jira_result.created is True
    assert any("hooks.slack.invalid" in str(call["args"][0]) for call in calls)
    assert any("api.github.com/repos/example/repo/issues" in str(call["args"][0]) for call in calls)
    assert any("rest/api/3/issue" in str(call["args"][0]) for call in calls)
    rendered_calls = json.dumps(calls, default=str)
    assert "snapshot" not in rendered_calls
    assert "debian-vm-01" not in rendered_calls


def test_payload_contracts_are_redacted_and_deduplicated(tmp_path: Path) -> None:
    report_path = _host_report_path(tmp_path)
    report = load_host_input(HOST_FIXTURES / "debian-vulnerable")
    analyzed = analyze_snapshot(report)
    analyzed.host_metadata = {
        "hostname": "debian-vm-01",
        "ip_addresses": ["10.0.0.5"],
        "owner": "fixture-owner",
    }

    webhook_payload = build_webhook_payload(analyzed, report_path=report_path)
    sarif_payload = generate_sarif(analyzed, report_path=report_path)
    csv_payload = generate_csv(analyzed, report_path=report_path)

    webhook_rendered = json.dumps(webhook_payload, sort_keys=True)
    file_export_contract = json.dumps(
        {
            "sarif_result_properties": sarif_payload["runs"][0]["results"][0]["properties"],
            "csv_header": csv_payload.splitlines()[0],
        },
        sort_keys=True,
    )

    assert "snapshot" not in webhook_rendered
    assert "debian-vm-01" not in webhook_rendered
    assert "10.0.0.5" not in webhook_rendered
    assert "dedupeKey" in file_export_contract
    assert "finding_id" in file_export_contract


def test_real_external_actions_require_yes(tmp_path: Path) -> None:
    report_path = _host_report_path(tmp_path)

    with pytest.raises(Exception, match="requires --yes"):
        export_webhook(
            report_path,
            url="https://example.invalid/hook",
            dry_run=False,
        )
