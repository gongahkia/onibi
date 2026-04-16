from __future__ import annotations

import json
import re
from pathlib import Path

import pytest
from typer.testing import CliRunner

from piranesi.cli import app
from piranesi.config import load_config
from piranesi.doctor import DoctorCheck, DoctorReport
from piranesi.watch import WatchModeSummary

runner = CliRunner()
_ANSI_RE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")


def _plain_output(output: str) -> str:
    return _ANSI_RE.sub("", output)


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
        "trends",
        "suppress",
        "diff",
        "rules",
        "baseline",
        "hook",
        "doctor",
        "init",
        "run",
        "watch",
    ]
    for command in commands:
        assert command in result.stdout


def test_scan_requires_authorized_flag(tmp_path: Path) -> None:
    config_path = tmp_path / "piranesi.toml"
    config_path.write_text("", encoding="utf-8")

    result = runner.invoke(app, ["scan", ".", "--config", str(config_path)])

    assert result.exit_code == 2


def test_doctor_command_renders_readiness(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
    report = DoctorReport(
        piranesi_version="0.2.0",
        target=str(tmp_path),
        config_path=str(tmp_path / "piranesi.toml"),
        ready=True,
        deterministic_ready=True,
        full_pipeline_ready=False,
        frameworks=["express"],
        scan_targets=2,
        checks=[
            DoctorCheck(name="python", status="ok", summary="Python 3.12"),
            DoctorCheck(name="llm", status="warn", summary="no API key configured"),
        ],
    )

    monkeypatch.setattr("piranesi.cli.build_doctor_report", lambda *_args, **_kwargs: report)

    result = runner.invoke(app, ["doctor", str(tmp_path)])

    assert result.exit_code == 0
    assert "Deterministic scan ready: yes" in result.stdout
    assert "Full LLM-assisted pipeline ready: no" in result.stdout
    assert "[WARN] llm" in result.stdout


def test_doctor_json_output(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
    report = DoctorReport(
        piranesi_version="0.2.0",
        target=str(tmp_path),
        config_path=str(tmp_path / "piranesi.toml"),
        ready=True,
        deterministic_ready=True,
        full_pipeline_ready=True,
    )
    monkeypatch.setattr("piranesi.cli.build_doctor_report", lambda *_args, **_kwargs: report)

    result = runner.invoke(app, ["doctor", str(tmp_path), "--json"])

    assert result.exit_code == 0
    payload = json.loads(result.stdout)
    assert payload["deterministic_ready"] is True
    assert payload["full_pipeline_ready"] is True


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
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "--incremental" in output
    assert "--package" in output
    assert "--changed-packag" in output
    assert "--max-parallel" in output
    assert "--sbom" in output


def test_run_help_lists_incremental_flag() -> None:
    result = runner.invoke(app, ["run", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "--incremental" in output
    assert "--staged-only" in output
    assert "--hook-timeout" in output
    assert "--package" in output
    assert "--changed-packag" in output
    assert "--max-parallel" in output
    assert "--sbom" in output


def test_watch_help_lists_watch_mode_flags() -> None:
    result = runner.invoke(app, ["watch", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "--filter" in output
    assert "--debounce" in output
    assert "--on-finding" in output
    assert "--max-scans" in output


def test_lsp_help_lists_lsp_flags() -> None:
    result = runner.invoke(app, ["lsp", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "--tcp" in output
    assert "--port" in output
    assert "--log" in output


def test_lsp_command_invokes_server(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    from piranesi.lsp import server as lsp_server

    config_path = tmp_path / "piranesi.toml"
    config_path.write_text("", encoding="utf-8")
    recorded: dict[str, object] = {}

    def _fake_serve(*, config_path: Path, tcp: bool, host: str, port: int) -> None:
        recorded["config_path"] = config_path
        recorded["tcp"] = tcp
        recorded["host"] = host
        recorded["port"] = port

    monkeypatch.setattr(lsp_server, "serve", _fake_serve)

    result = runner.invoke(
        app,
        [
            "lsp",
            "--config",
            str(config_path),
            "--tcp",
            "--host",
            "127.0.0.1",
            "--port",
            "9258",
        ],
    )

    assert result.exit_code == 0
    assert recorded == {
        "config_path": config_path.resolve(strict=False),
        "tcp": True,
        "host": "127.0.0.1",
        "port": 9258,
    }


def test_watch_command_invokes_watch_mode(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    config_path = tmp_path / "piranesi.toml"
    target_dir = tmp_path / "project"
    target_dir.mkdir()
    config_path.write_text("", encoding="utf-8")
    captured: dict[str, object] = {}

    def _fake_watch_mode(
        target_dir_arg: Path,
        *,
        config: object,
        output_dir: Path,
        debounce_ms: int,
        filter_glob: str | None,
        on_finding: str | None,
        fail_severity: str,
        max_scans: int | None,
        use_cache: bool,
        max_parallel: int | None,
        render_ui: bool,
    ) -> WatchModeSummary:
        _ = (config, render_ui)
        captured["target_dir"] = target_dir_arg
        captured["output_dir"] = output_dir
        captured["debounce_ms"] = debounce_ms
        captured["filter_glob"] = filter_glob
        captured["on_finding"] = on_finding
        captured["fail_severity"] = fail_severity
        captured["max_scans"] = max_scans
        captured["use_cache"] = use_cache
        captured["max_parallel"] = max_parallel
        return WatchModeSummary(scans=1, findings_remaining=0, fixed_total=0, exit_code=0)

    monkeypatch.setattr("piranesi.cli.run_watch_mode", _fake_watch_mode)

    result = runner.invoke(
        app,
        [
            "watch",
            str(target_dir),
            "--config",
            str(config_path),
            "--authorized",
            "--yes",
            "--filter",
            "**/*.ts",
            "--debounce",
            "250",
            "--on-finding",
            "echo hook",
            "--max-scans",
            "2",
        ],
    )

    assert result.exit_code == 0
    assert captured == {
        "target_dir": target_dir,
        "output_dir": Path("./piranesi-output"),
        "debounce_ms": 250,
        "filter_glob": "**/*.ts",
        "on_finding": "echo hook",
        "fail_severity": "low",
        "max_scans": 2,
        "use_cache": True,
        "max_parallel": None,
    }


def test_detect_help_lists_include_tests_flag() -> None:
    result = runner.invoke(app, ["detect", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "--include-tests" in output


def test_run_help_lists_include_tests_flag() -> None:
    result = runner.invoke(app, ["run", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "--include-tests" in output


def test_run_help_lists_reachability_flags() -> None:
    result = runner.invoke(app, ["run", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "--include-unreac" in output
    assert "--dead-code-repo" in output


def test_report_help_lists_reachability_flags() -> None:
    result = runner.invoke(app, ["report", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "--include-unreachable" in output
    assert "--dead-code-report" in output


def test_run_help_lists_exit_controls_and_exit_codes() -> None:
    result = runner.invoke(app, ["run", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "--fail-severity" in output
    assert "--no-fail" in output
    assert "Exit codes:" in output
    assert "0 = no findings" in output
    assert "4 = budget exceeded" in output


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
    assert config.scan.exclude_patterns == [
        "**/node_modules/**",
        "**/dist/**",
        "**/*.d.ts",
        "**/piranesi-output/**",
        "**/.piranesi-cache/**",
        "**/.piranesi-out/**",
        "**/.piranesi-trace*",
    ]
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
