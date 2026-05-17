from __future__ import annotations

import subprocess

from piranesi.preflight import build_preflight_report


def test_preflight_marks_missing_required_host_tool() -> None:
    report = build_preflight_report(
        mode="host",
        executable_lookup=lambda _name: None,
    )

    osquery = next(check for check in report.checks if check.name == "osquery")

    assert report.ready is False
    assert osquery.required is True
    assert osquery.status == "missing"
    assert report.summary["missing_required"] == 1


def test_preflight_reports_available_optional_tool_version() -> None:
    def lookup(name: str) -> str | None:
        return f"/usr/bin/{name}" if name == "trivy" else None

    def runner(command: list[str], **_kwargs: object) -> subprocess.CompletedProcess[str]:
        return subprocess.CompletedProcess(command, 0, stdout="Version: 1.2.3\n", stderr="")

    report = build_preflight_report(
        mode="workbench",
        executable_lookup=lookup,
        command_runner=runner,
    )
    trivy = next(check for check in report.checks if check.name == "trivy")

    assert report.ready is True
    assert trivy.required is False
    assert trivy.available is True
    assert trivy.version == "Version: 1.2.3"
