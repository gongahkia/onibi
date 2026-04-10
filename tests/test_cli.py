from __future__ import annotations

import json
from pathlib import Path

import pytest
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.config import load_config

runner = CliRunner()


def test_help_shows_all_commands() -> None:
    result = runner.invoke(app, ["--help"])

    assert result.exit_code == 0
    commands = [
        "scan",
        "detect",
        "triage",
        "verify",
        "legal",
        "patch",
        "report",
        "suppress",
        "diff",
        "baseline",
        "init",
        "run",
    ]
    for command in commands:
        assert command in result.stdout


def test_scan_requires_authorized_flag(tmp_path: Path) -> None:
    config_path = tmp_path / "piranesi.toml"
    config_path.write_text("", encoding="utf-8")

    result = runner.invoke(app, ["scan", ".", "--config", str(config_path)])

    assert result.exit_code == 2


def test_scan_authorized_yes_runs_stage_and_creates_trace(tmp_path: Path) -> None:
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

    assert result.exit_code in (0, 3)  # may succeed or fail depending on Joern availability
    assert trace_path.exists()


def test_scan_help_lists_incremental_flag() -> None:
    result = runner.invoke(app, ["scan", "--help"])

    assert result.exit_code == 0
    assert "--incremental" in result.stdout
    assert "--sbom" in result.stdout


def test_run_help_lists_incremental_flag() -> None:
    result = runner.invoke(app, ["run", "--help"])

    assert result.exit_code == 0
    assert "--incremental" in result.stdout
    assert "--sbom" in result.stdout


def test_detect_help_lists_include_tests_flag() -> None:
    result = runner.invoke(app, ["detect", "--help"])

    assert result.exit_code == 0
    assert "--include-tests" in result.stdout


def test_run_help_lists_include_tests_flag() -> None:
    result = runner.invoke(app, ["run", "--help"])

    assert result.exit_code == 0
    assert "--include-tests" in result.stdout


def test_run_help_lists_exit_controls_and_exit_codes() -> None:
    result = runner.invoke(app, ["run", "--help"])

    assert result.exit_code == 0
    assert "--fail-severity" in result.stdout
    assert "--no-fail" in result.stdout
    assert "Exit codes:" in result.stdout
    assert "0 = no findings" in result.stdout
    assert "4 = budget exceeded" in result.stdout


def test_init_scaffolds_detected_framework_defaults(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.chdir(tmp_path)
    (tmp_path / "package.json").write_text(
        json.dumps({"dependencies": {"express": "^5.1.0"}}),
        encoding="utf-8",
    )

    result = runner.invoke(app, ["init"])

    assert result.exit_code == 0
    config = load_config(tmp_path / "piranesi.toml")
    assert config.scan.frameworks == ["express"]
    assert config.scan.include_patterns == ["**/*.ts", "**/*.tsx", "**/*.js", "**/*.jsx"]
    assert config.scan.exclude_patterns == ["**/node_modules/**", "**/dist/**", "**/*.d.ts"]
    ignore_payload = (tmp_path / ".piranesi-ignore").read_text(encoding="utf-8")
    assert "suppressions: []" in ignore_payload
    assert 'id: "finding-123"' in ignore_payload
    assert "Detected: Express" in result.stdout
    assert "Run `piranesi run . --authorized --yes` to scan." in result.stdout


def test_init_scaffolds_explicit_framework_defaults(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.chdir(tmp_path)

    result = runner.invoke(app, ["init", "--framework", "fastapi"])

    assert result.exit_code == 0
    config = load_config(tmp_path / "piranesi.toml")
    assert config.scan.frameworks == ["fastapi"]
    assert config.scan.include_patterns == ["**/*.py"]
    assert "**/.venv/**" in config.scan.exclude_patterns
    assert "Using explicit framework: FastAPI" in result.stdout


def test_suppress_command_appends_ignore_rule(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.chdir(tmp_path)

    result = runner.invoke(
        app,
        [
            "suppress",
            "finding-123",
            "--reason",
            "accepted risk",
            "--ticket",
            "SEC-123",
        ],
    )

    assert result.exit_code == 0
    ignore_file = tmp_path / ".piranesi-ignore"
    assert ignore_file.exists()
    payload = ignore_file.read_text(encoding="utf-8")
    assert "id: finding-123" in payload
    assert "reason: accepted risk" in payload
    assert "ticket: SEC-123" in payload
