from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.host.api import assess_host_bundle, load_host_report
from piranesi.host.models import HostPostureReport
from piranesi.schema import build_schema, write_schema

FIXTURES = Path(__file__).parent / "fixtures" / "host"


def test_public_api_assesses_fixture_bundle_as_model_and_dict() -> None:
    report = assess_host_bundle(FIXTURES / "debian-vulnerable")

    assert isinstance(report, HostPostureReport)
    assert report.target == "debian-vm-01"
    assert report.schema_version == 1
    assert report.summary["findings_total"] >= 5

    payload = assess_host_bundle(str(FIXTURES / "debian-vulnerable"), format="dict")
    assert payload["target"] == "debian-vm-01"
    assert payload["schema_version"] == 1
    assert payload["summary"]["findings_total"] >= 5


def test_public_schema_export_for_host_models(tmp_path: Path) -> None:
    host_report_schema = build_schema("host-report")
    host_snapshot_schema = build_schema("host-snapshot")
    output = write_schema("fleet-report", tmp_path / "fleet-report.schema.json")

    assert host_report_schema["$schema"] == "https://json-schema.org/draft/2020-12/schema"
    assert host_report_schema["x-piranesi-schema-name"] == "host-report"
    assert "snapshot" in host_report_schema["properties"]
    assert host_snapshot_schema["x-piranesi-schema-name"] == "host-snapshot"
    assert output.is_file()
    assert json.loads(output.read_text(encoding="utf-8"))["x-piranesi-schema-name"] == (
        "fleet-report"
    )


def test_schema_cli_writes_host_report_schema(tmp_path: Path) -> None:
    output = tmp_path / "host-report.schema.json"

    result = CliRunner().invoke(app, ["schema", "host-report", "--output", str(output)])

    assert result.exit_code == 0, result.stdout
    payload = json.loads(output.read_text(encoding="utf-8"))
    assert payload["x-piranesi-schema-name"] == "host-report"
    assert "HostPostureReport" in payload["title"]


def test_load_host_report_accepts_older_minimal_payload() -> None:
    report = load_host_report(FIXTURES / "api" / "host-report-v0.json")

    assert report.schema_version == 1
    assert report.target == "legacy-vm"
    assert report.snapshot.identity.hostname == "legacy-vm"
    assert report.findings == []


def test_public_host_api_imports_without_typer_dependency() -> None:
    script = """
import builtins
real_import = builtins.__import__
def blocked_import(name, globals=None, locals=None, fromlist=(), level=0):
    if name == "typer" or name.startswith("typer."):
        raise AssertionError("public host API imported typer")
    return real_import(name, globals, locals, fromlist, level)
builtins.__import__ = blocked_import
from piranesi.host.api import assess_host_bundle, load_host_report
print(assess_host_bundle.__name__)
print(load_host_report.__name__)
"""
    env = os.environ.copy()
    env["PYTHONPATH"] = str(Path(__file__).resolve().parents[1] / "src")

    result = subprocess.run(
        [sys.executable, "-c", script],
        check=False,
        capture_output=True,
        text=True,
        env=env,
        timeout=10,
    )

    assert result.returncode == 0, result.stderr
    assert "assess_host_bundle" in result.stdout
    assert "load_host_report" in result.stdout
