from __future__ import annotations

import json
import zipfile
from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.support import SupportBundleOptions, create_support_bundle

runner = CliRunner()


def test_support_bundle_redacts_config_logs_and_report_artifacts(tmp_path: Path) -> None:
    config = tmp_path / "piranesi.toml"
    config.write_text('api_key = "sk-live-secret"\n', encoding="utf-8")
    report_dir = tmp_path / "out"
    report_dir.mkdir()
    (report_dir / "report.json").write_text(
        json.dumps(
            {
                "schema_version": 1,
                "target": "fixture-host",
                "summary": {"findings_total": 1},
                "findings": [{"id": "finding-1", "token": "secret-token"}],
            }
        ),
        encoding="utf-8",
    )
    log = tmp_path / "scan.log"
    log.write_text("Authorization: Bearer secret-token\nhost 10.0.0.5\n", encoding="utf-8")
    output = tmp_path / "support.zip"

    manifest = create_support_bundle(
        SupportBundleOptions(
            output=output,
            project_root=tmp_path,
            config_path=config,
            report_path=report_dir,
            include_report_artifacts=True,
            log_paths=[log],
        )
    )

    assert output.is_file()
    assert any(entry.path == "report/artifacts/report.json" for entry in manifest.entries)
    with zipfile.ZipFile(output) as archive:
        names = set(archive.namelist())
        config_text = archive.read("config/piranesi.toml.redacted").decode()
        log_text = archive.read("logs/scan.log.redacted").decode()
        report_text = archive.read("report/artifacts/report.json").decode()

    assert "manifest.json" in names
    assert "sk-live-secret" not in config_text
    assert "secret-token" not in log_text
    assert "10.0.0.5" not in log_text
    assert "secret-token" not in report_text


def test_support_bundle_cli_writes_archive(tmp_path: Path) -> None:
    output = tmp_path / "support.zip"

    result = runner.invoke(
        app,
        [
            "support-bundle",
            "--project-root",
            str(tmp_path),
            "--output",
            str(output),
            "--json",
        ],
    )

    assert result.exit_code == 0
    assert output.is_file()
    payload = json.loads(result.stdout)
    assert payload["include_report_artifacts"] is False
    assert payload["preflight_mode"] == "workbench"
