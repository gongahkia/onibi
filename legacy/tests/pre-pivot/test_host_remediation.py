from __future__ import annotations

import json
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.detect.suppression import SuppressionRule
from piranesi.host import analyze_snapshot, apply_host_suppressions, load_host_input
from piranesi.host.remediation import (
    build_remediation_plan,
    diff_host_reports,
    render_remediation_checklist,
)

FIXTURES = Path(__file__).parent / "fixtures" / "host"
runner = CliRunner()


def test_remediation_plan_generation_writes_json_and_markdown(tmp_path: Path) -> None:
    report = analyze_snapshot(load_host_input(FIXTURES / "debian-vulnerable"))
    report_path = tmp_path / "host-report.json"
    output_path = tmp_path / "remediation-plan.md"
    report_path.write_text(report.model_dump_json(indent=2), encoding="utf-8")

    result = runner.invoke(
        app,
        ["remediate", "plan", str(report_path), "--output", str(output_path)],
    )

    assert result.exit_code == 0, result.stdout
    assert output_path.is_file()
    json_path = output_path.with_suffix(".json")
    assert json_path.is_file()
    payload = json.loads(json_path.read_text(encoding="utf-8"))
    markdown = output_path.read_text(encoding="utf-8")
    assert payload["target"] == "debian-vm-01"
    assert payload["actions"]
    first_action = payload["actions"][0]
    assert first_action["owner"] == "TODO: assign owner"
    assert first_action["related_finding_ids"]
    assert first_action["verification_command"].startswith("piranesi ")
    assert "Rollback" in markdown
    assert "Dependencies" in markdown


def test_host_diff_detects_fixed_and_unchanged_findings() -> None:
    before = analyze_snapshot(load_host_input(FIXTURES / "debian-vulnerable"))
    fixed_finding = before.findings[0]
    after = before.model_copy(
        update={
            "findings": [finding for finding in before.findings if finding.id != fixed_finding.id]
        }
    )

    diff = diff_host_reports(before, after)

    assert diff.summary["fixed"] == 1
    assert diff.fixed[0].id == fixed_finding.id
    assert diff.summary["unchanged"] == len(before.findings) - 1
    assert diff.summary["new"] == 0


def test_host_diff_fallback_matching_across_title_and_id_changes() -> None:
    before = analyze_snapshot(load_host_input(FIXTURES / "debian-vulnerable"))
    original = before.findings[0]
    changed = original.model_copy(
        update={
            "id": "host-renamed-title-id",
            "title": f"{original.title} (renamed)",
        }
    )
    after = before.model_copy(update={"findings": [changed, *before.findings[1:]]})

    diff = diff_host_reports(before, after)

    assert diff.summary["changed"] == 1
    assert diff.changed[0].before.id == original.id
    assert diff.changed[0].after.id == "host-renamed-title-id"
    assert "title" in diff.changed[0].changed_fields
    assert diff.summary["new"] == 0
    assert diff.summary["fixed"] == 0


def test_markdown_checklist_renders_stable_action_groups() -> None:
    report = analyze_snapshot(load_host_input(FIXTURES / "debian-vulnerable"))

    checklist = render_remediation_checklist(report, output_format="markdown")

    assert "# Piranesi Remediation Checklist" in checklist
    assert "- [ ]" in checklist
    assert "owner: TODO: assign owner" in checklist
    assert "## Exposure" in checklist


def test_suppressed_findings_are_tracked_separately() -> None:
    before = analyze_snapshot(load_host_input(FIXTURES / "debian-vulnerable"))
    suppressed_id = before.findings[0].id
    after = apply_host_suppressions(
        before,
        [SuppressionRule(id=suppressed_id, reason="accepted exception")],
    )

    diff = diff_host_reports(before, after)
    plan = build_remediation_plan(after)

    assert diff.summary["suppressed"] == 1
    assert diff.suppressed[0].id == suppressed_id
    assert all(suppressed_id not in action.related_finding_ids for action in plan.actions)


def test_host_diff_cli_json_output(tmp_path: Path) -> None:
    before = analyze_snapshot(load_host_input(FIXTURES / "debian-vulnerable"))
    after = before.model_copy(update={"findings": before.findings[1:]})
    before_path = tmp_path / "before.json"
    after_path = tmp_path / "after.json"
    before_path.write_text(before.model_dump_json(indent=2), encoding="utf-8")
    after_path.write_text(after.model_dump_json(indent=2), encoding="utf-8")

    result = runner.invoke(
        app,
        ["host", "diff", str(before_path), str(after_path), "--format", "json"],
    )

    assert result.exit_code == 0, result.stdout
    payload = json.loads(result.stdout)
    assert payload["summary"]["fixed"] == 1
