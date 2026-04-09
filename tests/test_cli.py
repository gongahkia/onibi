from __future__ import annotations

from pathlib import Path

from typer.testing import CliRunner

from piranesi.cli import app

runner = CliRunner()


def test_help_shows_all_commands() -> None:
    result = runner.invoke(app, ["--help"])

    assert result.exit_code == 0
    for command in ["scan", "detect", "triage", "verify", "legal", "patch", "report", "run"]:
        assert command in result.stdout


def test_scan_requires_authorized_flag(tmp_path: Path) -> None:
    config_path = tmp_path / "piranesi.toml"
    config_path.write_text("", encoding="utf-8")

    result = runner.invoke(app, ["scan", ".", "--config", str(config_path)])

    assert result.exit_code == 2


def test_scan_authorized_yes_returns_not_implemented_and_creates_trace(tmp_path: Path) -> None:
    config_path = tmp_path / "piranesi.toml"
    trace_path = tmp_path / ".piranesi-trace.jsonl"
    config_path.write_text("", encoding="utf-8")

    result = runner.invoke(
        app,
        [
            "scan",
            ".",
            "--config",
            str(config_path),
            "--trace",
            str(trace_path),
            "--authorized",
            "--yes",
        ],
    )

    assert result.exit_code == 1
    assert "not implemented" in result.stdout
    assert trace_path.exists()
