from __future__ import annotations

import json
import os
import re
from importlib.metadata import entry_points
from pathlib import Path

import pytest
from typer.testing import CliRunner

from piranesi.cli import _load_local_llm_env, app
from piranesi.config import OwnershipConfig, load_config
from piranesi.doctor import DoctorCheck, DoctorReport
from piranesi.host import analyze_snapshot, load_host_input
from piranesi.launcher_tui import LauncherAction, LauncherSelection
from piranesi.models.finding import (
    VerificationAttempt,
    VerificationBodyExcerpt,
    VerificationEvidence,
    VerificationPrecondition,
    VerificationRedactionStatus,
    VerificationResponseDiffSummary,
    VerificationTimingSummary,
)
from piranesi.pipeline import DetectArtifact
from piranesi.report.renderer import build_report
from piranesi.watch import WatchModeSummary
from tests._pipeline_fixtures import fixture_artifacts

runner = CliRunner()
_ANSI_RE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")
HOST_FIXTURES = Path(__file__).parent / "fixtures" / "host"


def _plain_output(output: str) -> str:
    return _ANSI_RE.sub("", output)


def test_cli_loads_local_openai_env_alias(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.chdir(tmp_path)
    monkeypatch.delenv("OPENAI_API_KEY", raising=False)
    (tmp_path / ".env").write_text("OPENAI-API-KEY=sk-local-test\n", encoding="utf-8")

    _load_local_llm_env()

    assert os.environ["OPENAI_API_KEY"] == "sk-local-test"


def test_cli_local_env_does_not_override_existing_key(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.chdir(tmp_path)
    monkeypatch.setenv("OPENAI_API_KEY", "sk-existing")
    (tmp_path / ".env").write_text("OPENAI_API_KEY=sk-local-test\n", encoding="utf-8")

    _load_local_llm_env()

    assert os.environ["OPENAI_API_KEY"] == "sk-existing"


def test_help_shows_all_commands() -> None:
    result = runner.invoke(app, ["--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    commands = [
        "version",
        "quickstart",
        "demo",
        "collect",
        "doctor",
        "assess",
        "fleet",
        "container",
        "k8s",
        "schema",
        "policy",
        "export",
        "remote",
        "validate-evidence",
        "init",
        "explain",
        "trends",
        "ui",
    ]
    for command in commands:
        assert command in output

    hidden_legacy_commands = ["run", "pipeline", "rules", "plugins", "dev"]
    for command in hidden_legacy_commands:
        assert f"│ {command}" not in output


def test_quickstart_exits_zero_and_prints_next_steps() -> None:
    result = runner.invoke(app, ["quickstart"])

    assert result.exit_code == 0
    assert "piranesi demo --output" in result.stdout
    assert "piranesi doctor --host" in result.stdout
    assert "piranesi assess piranesi-evidence" in result.stdout
    assert "LLM" not in result.stdout


def test_demo_writes_json_and_markdown_from_bundled_fixture(tmp_path: Path) -> None:
    output_dir = tmp_path / "demo"

    result = runner.invoke(app, ["demo", "--output", str(output_dir)])

    assert result.exit_code == 0, result.stdout
    payload = json.loads((output_dir / "host-report.json").read_text(encoding="utf-8"))
    markdown = (output_dir / "host-report.md").read_text(encoding="utf-8")
    assert payload["target"] == "debian-vm-01"
    assert payload["summary"]["findings_total"] >= 5
    assert "Piranesi Host Posture Report" in markdown
    assert "LLM" not in result.stdout


def test_package_console_script_resolves() -> None:
    scripts = {entry.name: entry.value for entry in entry_points(group="console_scripts")}

    assert scripts["piranesi"] == "piranesi.cli:app"


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
        collect_ready=True,
        assess_ready=True,
        checks=[
            DoctorCheck(name="python", status="ok", summary="Python 3.12"),
            DoctorCheck(name="llm", status="warn", summary="no API key configured"),
            DoctorCheck(name="sysctl", status="ok", summary="sysctl available"),
        ],
    )

    monkeypatch.setattr("piranesi.cli.build_doctor_report", lambda *_args, **_kwargs: report)

    result = runner.invoke(app, ["doctor", str(tmp_path)])

    assert result.exit_code == 0
    assert "Host collection ready: yes" in result.stdout
    assert "Host assessment ready: yes" in result.stdout
    assert "[WARN] llm" in result.stdout
    assert "[OK] sysctl" in result.stdout


def test_doctor_json_output(monkeypatch: pytest.MonkeyPatch, tmp_path: Path) -> None:
    report = DoctorReport(
        piranesi_version="0.2.0",
        target=str(tmp_path),
        config_path=str(tmp_path / "piranesi.toml"),
        ready=True,
        collect_ready=True,
        assess_ready=True,
    )
    monkeypatch.setattr("piranesi.cli.build_doctor_report", lambda *_args, **_kwargs: report)

    result = runner.invoke(app, ["doctor", str(tmp_path), "--json"])

    assert result.exit_code == 0
    payload = json.loads(result.stdout)
    assert payload["collect_ready"] is True
    assert payload["assess_ready"] is True


def test_doctor_host_focus_omits_llm_dependency_noise(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    captured: dict[str, object] = {}
    report = DoctorReport(
        piranesi_version="0.2.0",
        target=str(tmp_path),
        config_path=str(tmp_path / "piranesi.toml"),
        ready=True,
        collect_ready=True,
        assess_ready=True,
        checks=[
            DoctorCheck(name="python", status="ok", summary="Python 3.12"),
            DoctorCheck(name="osquery", status="ok", summary="osqueryi version 5.12.0"),
            DoctorCheck(name="trivy", status="warn", summary="trivy not found on PATH"),
            DoctorCheck(name="sysctl", status="ok", summary="sysctl available"),
        ],
    )

    def _fake_build_doctor_report(*_args: object, **kwargs: object) -> DoctorReport:
        captured.update(kwargs)
        return report

    monkeypatch.setattr("piranesi.cli.build_doctor_report", _fake_build_doctor_report)

    result = runner.invoke(app, ["doctor", "--host", str(tmp_path)])

    assert result.exit_code == 0
    assert captured["host_only"] is True
    assert "[OK] osquery" in result.stdout
    assert "[WARN] trivy" in result.stdout
    assert "llm" not in result.stdout.lower()


def test_assess_cli_writes_collection_health_with_manifest(tmp_path: Path) -> None:
    input_dir = tmp_path / "evidence"
    output_dir = tmp_path / "out"
    raw_osquery = input_dir / "raw" / "osquery"
    raw_osquery.mkdir(parents=True)
    (raw_osquery / "system_info.json").write_text(
        json.dumps([{"hostname": "manifest-vm"}]),
        encoding="utf-8",
    )
    (raw_osquery / "deb_packages.json").write_text(
        json.dumps([{"name": "openssl", "version": "3.0.2"}]),
        encoding="utf-8",
    )
    (input_dir / "collection-manifest.json").write_text(
        json.dumps(
            {
                "schema_version": 1,
                "output_dir": str(input_dir),
                "raw_dir": str(input_dir / "raw"),
                "commands": [
                    {"tool": "osquery", "name": "system_info", "status": "ok"},
                    {"tool": "osquery", "name": "deb_packages", "status": "ok"},
                    {"tool": "trivy", "name": "filesystem_scan", "status": "missing"},
                ],
            }
        ),
        encoding="utf-8",
    )

    result = runner.invoke(
        app,
        ["assess", str(input_dir), "--output", str(output_dir), "--format", "both"],
    )

    assert result.exit_code == 0, result.stdout
    payload = json.loads((output_dir / "host-report.json").read_text(encoding="utf-8"))
    markdown = (output_dir / "host-report.md").read_text(encoding="utf-8")
    assert payload["collection_health"]["status_counts"]["missing"] == 1
    assert payload["collection_health"]["optional"]["trivy"]["status"] == "warn"
    assert "## Collection Health" in markdown


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
    assert "--proof-mode" in output
    assert "--target-profile" in output
    assert "--debug-bundle" in output
    assert "--fail-on-new" in output
    assert "--fail-on-new-se" in output


def test_diff_help_lists_pr_friendly_flags() -> None:
    result = runner.invoke(app, ["diff", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "--format" in output
    assert "--fail-on-new" in output
    assert "--fail-on-new-se" in output


def test_pipeline_help_lists_stage_commands() -> None:
    result = runner.invoke(app, ["pipeline", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    for name in ("run", "scan", "detect", "triage", "verify", "legal", "patch", "report"):
        assert name in output


def test_dev_help_lists_launch_plan() -> None:
    result = runner.invoke(app, ["dev", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "launch-plan" in output


def test_suppressions_help_lists_add_alias() -> None:
    result = runner.invoke(app, ["suppressions", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "add" in output


def test_baseline_help_lists_diff_alias() -> None:
    result = runner.invoke(app, ["baseline", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "diff" in output


def test_verify_help_lists_proof_mode_flag() -> None:
    result = runner.invoke(app, ["verify", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "--proof-mode" in output
    assert "--target-profile" in output


def test_watch_help_lists_watch_mode_flags() -> None:
    result = runner.invoke(app, ["watch", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "--filter" in output
    assert "--debounce" in output
    assert "--on-finding" in output
    assert "--max-scans" in output


def test_ui_help_lists_dashboard_flags() -> None:
    result = runner.invoke(app, ["ui", "--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    assert "--target" in output
    assert "--output" in output
    assert "--config" in output
    assert "--trace" in output
    assert "--host" in output
    assert "--port" in output
    assert "--watch" in output
    assert "--workbench" in output
    assert "--jobs-dir" in output
    assert "--max-upload-mb" in output
    assert "--scan-timeout" in output
    assert "--open" in output


def test_ui_workbench_noninteractive_reports_bind_url() -> None:
    result = runner.invoke(app, ["ui", "--workbench", "--port", "0"])

    assert result.exit_code == 0
    assert "local workbench would bind" in result.stdout


def test_ui_requires_interactive_tty() -> None:
    result = runner.invoke(app, ["ui"])

    assert result.exit_code == 2
    assert "requires an interactive TTY" in result.stdout


def test_no_args_launches_launcher_ui_when_interactive(monkeypatch: pytest.MonkeyPatch) -> None:
    captured: dict[str, Path] = {}

    monkeypatch.setattr("piranesi.cli._interactive_tty_available", lambda: True)

    def _fake_launch(
        *,
        target_dir: Path,
        output_dir: Path,
        config_path: Path,
        trace_path: Path,
    ) -> LauncherSelection | None:
        captured["target_dir"] = target_dir
        captured["output_dir"] = output_dir
        captured["config_path"] = config_path
        captured["trace_path"] = trace_path
        return LauncherSelection(
            action=LauncherAction.QUIT,
            target_dir=target_dir,
            output_dir=output_dir,
            config_path=config_path,
            trace_path=trace_path,
        )

    monkeypatch.setattr("piranesi.cli.launch_cli_tui", _fake_launch)

    result = runner.invoke(app, [])

    assert result.exit_code == 0
    assert captured["target_dir"] == Path(".").resolve(strict=False)
    assert captured["output_dir"] == Path("./piranesi-output").resolve(strict=False)
    assert captured["config_path"] == Path("./piranesi.toml").resolve(strict=False)
    assert captured["trace_path"] == Path(".piranesi-trace.jsonl").resolve(strict=False)


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
    pytest.importorskip("lsprotocol")
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
    assert "Run `piranesi doctor .`" in result.stdout
    assert "Run `piranesi collect --output piranesi-evidence`" in result.stdout
    assert "Run `piranesi assess piranesi-evidence --output piranesi-output`" in result.stdout


def test_init_host_workflow_scaffolds_host_first_defaults(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.chdir(tmp_path)

    result = runner.invoke(app, ["init", "--workflow", "host"])

    assert result.exit_code == 0
    assert "Host workflow selected" in result.stdout
    assert "piranesi collect --output piranesi-evidence" in result.stdout
    assert "host-finding-id" in (tmp_path / ".piranesi-ignore").read_text(encoding="utf-8")


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


def test_init_scaffolds_php_framework_defaults(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.chdir(tmp_path)
    (tmp_path / "composer.json").write_text(
        json.dumps({"require": {"laravel/framework": "^11.0"}}),
        encoding="utf-8",
    )
    (tmp_path / "app.php").write_text("<?php echo $_GET['q'];\n", encoding="utf-8")

    result = runner.invoke(app, ["init"])

    assert result.exit_code == 0
    config = load_config(tmp_path / "piranesi.toml")
    assert config.scan.frameworks == ["laravel"]
    assert config.scan.include_patterns == ["**/*.php", "**/*.blade.php"]
    assert "**/vendor/**" in config.scan.exclude_patterns
    assert "Detected: Laravel" in result.stdout
    assert "Composer dependencies" in result.stdout


def test_init_scaffolds_ruby_framework_defaults(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.chdir(tmp_path)
    (tmp_path / "Gemfile").write_text("gem 'rails'\n", encoding="utf-8")
    (tmp_path / "app.rb").write_text("puts params[:q]\n", encoding="utf-8")

    result = runner.invoke(app, ["init"])

    assert result.exit_code == 0
    config = load_config(tmp_path / "piranesi.toml")
    assert config.scan.frameworks == ["rails"]
    assert config.scan.include_patterns == ["**/*.rb"]
    assert "**/vendor/bundle/**" in config.scan.exclude_patterns
    assert "Detected: Rails" in result.stdout
    assert "Bundler dependencies" in result.stdout


def test_explain_command_renders_confirmed_finding(tmp_path: Path) -> None:
    output_dir = tmp_path / "out"
    artifacts = fixture_artifacts(tmp_path)
    report = build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=artifacts["detect"].findings,  # type: ignore[attr-defined]
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.0,
        duration_s=1.0,
        stage_timings_s={},
    )
    output_dir.mkdir()
    (output_dir / "report.json").write_text(report.model_dump_json(indent=2), encoding="utf-8")

    result = runner.invoke(app, ["explain", "finding-001", "--output", str(output_dir)])

    assert result.exit_code == 0
    assert "Piranesi Finding Explanation" in result.stdout
    assert "Status: confirmed" in result.stdout
    assert "Evidence: Dynamically verified issue" in result.stdout
    assert "CWE-89" in result.stdout
    assert "What matched:" in result.stdout
    assert "Confidence contributors:" in result.stdout
    assert "Verification state:" in result.stdout
    assert "Verified: yes" in result.stdout
    assert "Composite risk:" in result.stdout
    assert "Composite risk contributors:" in result.stdout
    assert "Patch: generated, not verified" in result.stdout
    assert "db.query" in result.stdout


def test_explain_command_renders_ownership_metadata(tmp_path: Path) -> None:
    output_dir = tmp_path / "out"
    artifacts = fixture_artifacts(tmp_path)
    report = build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=artifacts["detect"].findings,  # type: ignore[attr-defined]
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.0,
        duration_s=1.0,
        stage_timings_s={},
        ownership_config=OwnershipConfig(
            service="checkout-api",
            system="payments-platform",
            team="payments-eng",
            owner="payments-oncall",
            repository="acme/checkout",
            environment="prod",
            control_owner="grc-team",
            path_mappings=[
                {
                    "path": "src/routes/**",
                    "team": "route-security",
                    "owner": "route-owner",
                }
            ],
        ),
    )
    output_dir.mkdir()
    (output_dir / "report.json").write_text(report.model_dump_json(indent=2), encoding="utf-8")

    result = runner.invoke(app, ["explain", "finding-001", "--output", str(output_dir)])

    assert result.exit_code == 0
    assert "Ownership:" in result.stdout
    assert "Service: checkout-api" in result.stdout
    assert "System: payments-platform" in result.stdout
    assert "Team: route-security" in result.stdout
    assert "Owner: route-owner" in result.stdout
    assert "Repository: acme/checkout" in result.stdout
    assert "Environment: prod" in result.stdout
    assert "Control owner: grc-team" in result.stdout
    assert "Path mapping: src/routes/**" in result.stdout


def test_explain_command_renders_candidate_statuses(tmp_path: Path) -> None:
    output_dir = tmp_path / "out"
    artifacts = fixture_artifacts(tmp_path)
    base = artifacts["detect"].findings[0]  # type: ignore[attr-defined]
    active = base.model_copy(
        update={
            "id": "finding-active",
            "metadata": {
                "source_spec_name": "express_req_body",
                "source_spec_category": "request_body",
                "source_spec_custom": False,
                "sink_spec_name": "raw_sql_query",
                "sink_spec_category": "sql_query",
                "sink_spec_cwe": "CWE-89",
                "sink_spec_custom": False,
                "sanitizer_effectiveness": {"escapeHtml": "partial"},
                "partial_sanitizers": ["escapeHtml"],
            },
            "taint_path": [
                base.taint_path[0].model_copy(update={"sanitizer_applied": "escapeHtml"})
            ],
        }
    )
    unreachable = base.model_copy(
        update={
            "id": "finding-unreachable",
            "reachability": "unreachable",
            "severity": "informational",
        }
    )
    suppressed = base.model_copy(
        update={
            "id": "finding-suppressed",
            "suppressed": True,
            "suppression_reason": "accepted risk",
        }
    )
    triaged_active = artifacts["triage"].findings[0].model_copy(  # type: ignore[attr-defined]
        update={"finding": active, "triage_verdict": "true_positive", "triage_mode": "llm"}
    )
    verification_attempt = VerificationAttempt(
        finding_id="finding-active",
        status="skipped",
        reason="verification skipped: missing required preconditions (route_mapping)",
        template_id="generic-probe",
        template_reason="fallback",
        rich_evidence=VerificationEvidence(
            attempted_url="http://127.0.0.1:3000/search",
            attempted_route="/search",
            method="GET",
            payload_class="CWE-89: SQL Injection",
            template_id="generic-probe",
            status_code=403,
            response_diff_summary=VerificationResponseDiffSummary(
                summary="status:200->403; body_changed:yes; header_changes:1",
            ),
            timing_summary=VerificationTimingSummary(
                baseline_elapsed_ms=11.0,
                exploit_elapsed_ms=17.0,
                delta_elapsed_ms=6.0,
            ),
            error_signature="TARGET_PROFILE_NOT_READY",
            headers_subset={"set-cookie": "[REDACTED]"},
            body_excerpt=VerificationBodyExcerpt(
                sha256="abc123",
                preview="blocked",
                truncated=False,
                length=7,
            ),
            redaction_status=VerificationRedactionStatus(
                applied=True,
                redacted_value_count=1,
                redacted_fields=["response_headers"],
            ),
        ),
        evidence_artifact_path=str(
            tmp_path / "out" / "verification-evidence" / "finding-active.json"
        ),
        preconditions=[
            VerificationPrecondition(
                key="route_mapping",
                description="HTTP route for exercising the vulnerable code path",
                status="missing",
                required=True,
                next_step="Add finding.metadata['verification_route'] with a concrete endpoint.",
            )
        ],
    )
    report = build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=[active, unreachable, suppressed],
        triaged_findings=[triaged_active],
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        verification_attempts=[verification_attempt],
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.0,
        duration_s=1.0,
        stage_timings_s={},
    )
    output_dir.mkdir()
    (output_dir / "report.json").write_text(report.model_dump_json(indent=2), encoding="utf-8")

    active_result = runner.invoke(app, ["explain", "finding-active", "--output", str(output_dir)])
    assert active_result.exit_code == 0
    assert "Status: triaged active candidate" in active_result.stdout
    assert "Evidence: LLM-triaged active candidate" in active_result.stdout
    assert "Source spec: source:express_req_body" in active_result.stdout
    assert "Sink spec: sink:raw_sql_query" in active_result.stdout
    assert "Sanitizers observed: escapeHtml" in active_result.stdout
    assert "Outcome: skipped" in active_result.stdout
    assert "Verification reason: verification skipped: missing required preconditions" in (
        active_result.stdout
    )
    assert "Missing preconditions: route_mapping" in active_result.stdout
    assert "Verification request: GET /search" in active_result.stdout
    assert "Response diff summary: status:200->403; body_changed:yes; header_changes:1" in (
        active_result.stdout
    )
    assert "Evidence artifact: " in active_result.stdout
    assert "Confidence contributors:" in active_result.stdout

    unreachable_result = runner.invoke(
        app,
        ["explain", "finding-unreachable", "--output", str(output_dir)],
    )
    assert unreachable_result.exit_code == 0
    assert "Status: unreachable candidate" in unreachable_result.stdout
    assert "Evidence: Unreachable candidate" in unreachable_result.stdout

    suppressed_result = runner.invoke(
        app,
        ["explain", "finding-suppressed", "--output", str(output_dir)],
    )
    assert suppressed_result.exit_code == 0
    assert "Status: suppressed" in suppressed_result.stdout
    assert "Evidence: Suppressed finding" in suppressed_result.stdout


def test_explain_command_can_emit_json(tmp_path: Path) -> None:
    output_dir = tmp_path / "out"
    artifacts = fixture_artifacts(tmp_path)
    report = build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=artifacts["detect"].findings,  # type: ignore[attr-defined]
        confirmed_findings=artifacts["verify"].findings,  # type: ignore[attr-defined]
        legal_assessments=artifacts["legal"].assessments,  # type: ignore[attr-defined]
        patch_results=artifacts["patch"].patches,  # type: ignore[attr-defined]
        target_dir=tmp_path,
        total_llm_cost_usd=0.0,
        duration_s=1.0,
        stage_timings_s={},
    )
    output_dir.mkdir()
    (output_dir / "report.json").write_text(report.model_dump_json(indent=2), encoding="utf-8")

    result = runner.invoke(
        app,
        ["explain", "finding-001", "--output", str(output_dir), "--json"],
    )

    assert result.exit_code == 0
    payload = json.loads(result.stdout)
    assert payload["status"] == "confirmed"
    assert payload["evidence"] == "Dynamically verified issue"
    assert payload["explanation"]["verification_state"]["state"] == "verified_confirmed"
    assert payload["explanation"]["confidence"]["model_version"] == "v1"
    assert (
        payload["explanation"]["confidence"]["final_confidence"]
        == payload["finding"]["confidence"]
    )
    assert payload["finding"]["finding_id"] == "finding-001"
    assert "ownership" in payload["finding"]


def test_explain_command_json_includes_active_candidate_confidence_breakdown(
    tmp_path: Path,
) -> None:
    output_dir = tmp_path / "out"
    artifacts = fixture_artifacts(tmp_path)
    base = artifacts["detect"].findings[0]  # type: ignore[attr-defined]
    active = base.model_copy(
        update={
            "id": "finding-active",
            "metadata": {
                "source_spec_name": "express_req_body",
                "source_spec_category": "request_body",
                "source_spec_custom": False,
                "sink_spec_name": "raw_sql_query",
                "sink_spec_category": "sql_query",
                "sink_spec_cwe": "CWE-89",
                "sink_spec_custom": False,
                "sanitizer_effectiveness": {"escapeHtml": "partial"},
                "partial_sanitizers": ["escapeHtml"],
            },
            "taint_path": [
                base.taint_path[0].model_copy(update={"sanitizer_applied": "escapeHtml"})
            ],
        }
    )
    triaged_active = artifacts["triage"].findings[0].model_copy(  # type: ignore[attr-defined]
        update={"finding": active, "triage_verdict": "true_positive", "triage_mode": "llm"}
    )
    report = build_report(
        scan_result=artifacts["scan"],  # type: ignore[arg-type]
        detected_findings=[active],
        triaged_findings=[triaged_active],
        confirmed_findings=[],
        legal_assessments=[],
        patch_results=[],
        target_dir=tmp_path,
        total_llm_cost_usd=0.0,
        duration_s=1.0,
        stage_timings_s={},
    )
    output_dir.mkdir()
    (output_dir / "report.json").write_text(report.model_dump_json(indent=2), encoding="utf-8")

    result = runner.invoke(
        app,
        ["explain", "finding-active", "--output", str(output_dir), "--json"],
    )

    assert result.exit_code == 0
    payload = json.loads(result.stdout)
    assert payload["status"] == "triaged_active_candidate"
    assert payload["explanation"]["matched_source_spec"]["spec_id"] == "source:express_req_body"
    assert payload["explanation"]["matched_sink_spec"]["spec_id"] == "sink:raw_sql_query"
    assert "escapeHtml" in payload["explanation"]["sanitizers_observed"]
    assert payload["explanation"]["confidence"]["triage_signal"]["score"] > 0.9


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


def test_suppress_command_supports_extended_metadata(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.chdir(tmp_path)

    result = runner.invoke(
        app,
        [
            "suppress",
            "finding-456",
            "--reason",
            "accepted risk",
            "--reason-code",
            "risk_accepted",
            "--owner",
            "appsec",
            "--ticket",
            "SEC-456",
            "--reference",
            "jira://SEC-456",
            "--created",
            "2026-04-16",
            "--expires",
            "2026-06-16",
            "--scope",
            "id",
        ],
    )

    assert result.exit_code == 0
    payload = (tmp_path / ".piranesi-ignore").read_text(encoding="utf-8")
    assert "reason_code: risk_accepted" in payload
    assert "owner: appsec" in payload
    assert "reference: jira://SEC-456" in payload
    assert "created: '2026-04-16'" in payload
    assert "expires: '2026-06-16'" in payload
    assert "scope: id" in payload


def test_suppressions_list_and_validate_cli(tmp_path: Path) -> None:
    artifacts = fixture_artifacts(tmp_path)
    detect_path = tmp_path / "detect.json"
    detect_path.write_text(
        DetectArtifact(findings=artifacts["detect"].findings).model_dump_json(indent=2),  # type: ignore[attr-defined]
        encoding="utf-8",
    )
    (tmp_path / ".piranesi-ignore").write_text(
        (
            "suppressions:\n"
            "  - id: finding-001\n"
            '    reason: "accepted risk"\n'
            "    owner: appsec\n"
            "    created: 2026-04-16\n"
            "    expires: 2026-06-16\n"
        ),
        encoding="utf-8",
    )
    (tmp_path / "piranesi.toml").write_text(
        "\n".join(
            [
                "[suppression]",
                "fail_on_invalid = true",
                "fail_on_expired = true",
                "fail_on_stale = true",
            ]
        ),
        encoding="utf-8",
    )

    list_result = runner.invoke(
        app,
        ["suppressions", "list", "--project-root", str(tmp_path)],
    )
    assert list_result.exit_code == 0
    assert "Rules: 1" in list_result.stdout
    assert "owner=appsec" in list_result.stdout

    validate_result = runner.invoke(
        app,
        [
            "suppressions",
            "validate",
            "--project-root",
            str(tmp_path),
            "--findings",
            str(detect_path),
            "--config",
            str(tmp_path / "piranesi.toml"),
            "--json",
        ],
    )
    assert validate_result.exit_code == 0
    payload = json.loads(validate_result.stdout)
    assert payload["summary"]["total_rules"] == 1
    assert payload["summary"]["stale_rules"] == 0


def test_suppressions_validate_fails_for_expired_or_stale_when_policy_requires(
    tmp_path: Path,
) -> None:
    artifacts = fixture_artifacts(tmp_path)
    detect_path = tmp_path / "detect.json"
    detect_path.write_text(
        DetectArtifact(findings=artifacts["detect"].findings).model_dump_json(indent=2),  # type: ignore[attr-defined]
        encoding="utf-8",
    )
    (tmp_path / ".piranesi-ignore").write_text(
        (
            "suppressions:\n"
            "  - cwe: CWE-79\n"
            '    path: "src/admin/**"\n'
            '    reason: "stale"\n'
            "    expires: 2026-08-16\n"
            "  - id: finding-001\n"
            '    reason: "expired"\n'
            "    expires: 2026-01-01\n"
        ),
        encoding="utf-8",
    )
    (tmp_path / "piranesi.toml").write_text(
        "\n".join(
            [
                "[suppression]",
                "fail_on_invalid = true",
                "fail_on_expired = true",
                "fail_on_stale = true",
            ]
        ),
        encoding="utf-8",
    )

    result = runner.invoke(
        app,
        [
            "suppressions",
            "validate",
            "--project-root",
            str(tmp_path),
            "--findings",
            str(detect_path),
            "--config",
            str(tmp_path / "piranesi.toml"),
        ],
    )

    assert result.exit_code == 1
    assert "expired" in result.stdout


def test_suppressions_validate_auto_detects_host_report_directory(tmp_path: Path) -> None:
    report = analyze_snapshot(load_host_input(HOST_FIXTURES / "debian-vulnerable"))
    output_dir = tmp_path / "out"
    output_dir.mkdir()
    (output_dir / "host-report.json").write_text(report.model_dump_json(indent=2), encoding="utf-8")
    active_id = report.findings[0].id
    expired_id = report.findings[1].id
    (tmp_path / ".piranesi-ignore").write_text(
        (
            "suppressions:\n"
            f"  - id: {active_id}\n"
            '    reason: "accepted risk"\n'
            "    expires: 2026-06-16\n"
            "  - id: host-not-present\n"
            '    reason: "stale host suppression"\n'
            "    expires: 2026-06-16\n"
            f"  - id: {expired_id}\n"
            '    reason: "expired host suppression"\n'
            "    expires: 2026-01-01\n"
            "  - cwe: CWE-79\n"
            '    path: "src/**"\n'
            '    reason: "ignored for host reports"\n'
        ),
        encoding="utf-8",
    )
    (tmp_path / "piranesi.toml").write_text(
        "\n".join(
            [
                "[suppression]",
                "fail_on_invalid = true",
                "fail_on_expired = true",
                "fail_on_stale = true",
            ]
        ),
        encoding="utf-8",
    )

    result = runner.invoke(
        app,
        [
            "suppressions",
            "validate",
            "--project-root",
            str(tmp_path),
            "--findings",
            str(output_dir),
            "--config",
            str(tmp_path / "piranesi.toml"),
            "--json",
        ],
    )

    assert result.exit_code == 1
    payload = json.loads(result.stdout)
    assert payload["summary"]["active_rules"] == 1
    assert payload["summary"]["stale_rules"] == 1
    assert payload["summary"]["expired_rules"] == 1
    assert payload["summary"]["inline_suppressions"] == 0
    assert payload["summary"]["stale_selectors"] == ["id=host-not-present"]


def test_suppressions_validate_host_report_invalid_yaml_and_artifact_exit_codes(
    tmp_path: Path,
) -> None:
    report = analyze_snapshot(load_host_input(HOST_FIXTURES / "debian-vulnerable"))
    host_report_path = tmp_path / "host-report.json"
    host_report_path.write_text(report.model_dump_json(indent=2), encoding="utf-8")
    (tmp_path / "piranesi.toml").write_text(
        "[suppression]\nfail_on_invalid = true\nfail_on_expired = false\nfail_on_stale = false\n",
        encoding="utf-8",
    )
    (tmp_path / ".piranesi-ignore").write_text("suppressions: [", encoding="utf-8")

    invalid_yaml = runner.invoke(
        app,
        [
            "suppressions",
            "validate",
            "--project-root",
            str(tmp_path),
            "--findings",
            str(host_report_path),
            "--config",
            str(tmp_path / "piranesi.toml"),
            "--json",
        ],
    )
    assert invalid_yaml.exit_code == 1
    assert json.loads(invalid_yaml.stdout)["summary"]["invalid_rules"] == 1

    host_report_path.write_text('{"findings": []}', encoding="utf-8")
    (tmp_path / ".piranesi-ignore").write_text("suppressions: []", encoding="utf-8")
    invalid_artifact = runner.invoke(
        app,
        [
            "suppressions",
            "validate",
            "--project-root",
            str(tmp_path),
            "--findings",
            str(host_report_path),
            "--config",
            str(tmp_path / "piranesi.toml"),
        ],
    )
    assert invalid_artifact.exit_code == 2
    assert "invalid host report artifact" in invalid_artifact.stdout


def test_eval_help_lists_subcommands() -> None:
    result = runner.invoke(app, ["eval", "--help"])

    assert result.exit_code == 0
    assert "audit" in result.stdout
    assert "enrich-ground-truth" in result.stdout
    assert "coverage-gaps" in result.stdout
    assert "validate-all" in result.stdout
    assert "compare-reports" in result.stdout


def test_eval_enrich_ground_truth_cli_outputs_json(tmp_path: Path) -> None:
    gt_dir = tmp_path / "ground_truth"
    gt_dir.mkdir(parents=True, exist_ok=True)
    (gt_dir / "gt-001.yaml").write_text(
        json.dumps(
            {
                "id": "gt-001",
                "source_project": "synthetic",
                "commit_hash": "deadbeef",
                "cwe_id": "CWE-89",
                "cwe_name": "SQL Injection",
                "label": "true_positive",
                "affected_files": ["eval/synthetic/sqli-pg-raw.ts"],
                "line_numbers": [5],
                "taint_source": "req.query.id",
                "taint_sink": "db.query()",
                "taint_path": ["req.query.id", "db.query(sql)"],
                "complexity": "simple",
                "exploitable": True,
                "reference_exploit": None,
                "reference_fix_commit": None,
                "notes": "fixture",
                "discovery_method": "synthetic",
            }
        ),
        encoding="utf-8",
    )

    result = runner.invoke(
        app,
        [
            "eval",
            "enrich-ground-truth",
            "--gt-dir",
            str(gt_dir),
            "--json",
        ],
    )

    assert result.exit_code == 0
    payload = json.loads(result.stdout)
    assert payload["updated_entries"] == 1
    assert payload["updated_fields"] == 5
    assert payload["unresolved"] == {}


def test_eval_coverage_gaps_cli_outputs_json(tmp_path: Path) -> None:
    gt_dir = tmp_path / "ground_truth"
    gt_dir.mkdir(parents=True, exist_ok=True)
    (gt_dir / "gt-001.yaml").write_text(
        json.dumps(
            {
                "id": "gt-001",
                "source_project": "synthetic",
                "commit_hash": "deadbeef",
                "cwe_id": "CWE-89",
                "cwe_name": "SQL Injection",
                "label": "true_positive",
                "affected_files": ["eval/synthetic/sqli-pg-raw.ts"],
                "line_numbers": [5],
                "taint_source": "req.query.id",
                "taint_sink": "db.query()",
                "taint_path": ["req.query.id", "db.query(sql)"],
                "complexity": "simple",
                "exploitable": True,
                "reference_exploit": None,
                "reference_fix_commit": None,
                "notes": "fixture",
                "discovery_method": "synthetic",
                "language": "typescript",
                "framework": "express",
                "taint_step_count": 2,
                "taint_field_path": "query.id",
            }
        ),
        encoding="utf-8",
    )

    result = runner.invoke(
        app,
        [
            "eval",
            "coverage-gaps",
            "--gt-dir",
            str(gt_dir),
            "--dimension",
            "cwe+language",
            "--min-count",
            "2",
            "--json",
        ],
    )

    assert result.exit_code == 0
    payload = json.loads(result.stdout)
    assert payload["considered_entries"] == 1
    assert payload["gap_count"] == 1
    assert payload["gaps"][0]["dimension"] == "cwe+language"
    assert payload["gaps"][0]["needed_for_min_count"] == 1


def test_eval_compare_reports_cli_outputs_json(tmp_path: Path) -> None:
    baseline = tmp_path / "baseline.json"
    current = tmp_path / "current.json"
    baseline.write_text(
        json.dumps(
            {
                "results": {
                    "overall": {
                        "detection_rate": 0.8,
                        "fp_suppression_rate": 0.7,
                    }
                }
            }
        ),
        encoding="utf-8",
    )
    current.write_text(
        json.dumps(
            {
                "results": {
                    "overall": {
                        "detection_rate": 0.82,
                        "fp_suppression_rate": 0.69,
                    }
                }
            }
        ),
        encoding="utf-8",
    )

    result = runner.invoke(
        app,
        [
            "eval",
            "compare-reports",
            "--baseline-report",
            str(baseline),
            "--current-report",
            str(current),
            "--json",
        ],
    )

    assert result.exit_code == 0
    payload = json.loads(result.stdout)
    assert payload["comparison"]["overall"]["detection_rate"]["delta"] == pytest.approx(0.02)


def test_eval_compare_reports_cli_supports_history_dir(tmp_path: Path) -> None:
    history_dir = tmp_path / "history"
    history_dir.mkdir(parents=True, exist_ok=True)
    older = history_dir / "older.json"
    newer = history_dir / "newer.json"
    older.write_text(
        json.dumps(
            {
                "results": {
                    "overall": {
                        "detection_rate": 0.8,
                        "fp_suppression_rate": 0.7,
                    }
                }
            }
        ),
        encoding="utf-8",
    )
    newer.write_text(
        json.dumps(
            {
                "results": {
                    "overall": {
                        "detection_rate": 0.82,
                        "fp_suppression_rate": 0.69,
                    }
                }
            }
        ),
        encoding="utf-8",
    )
    (history_dir / "index.json").write_text(
        json.dumps(
            {
                "entries": [
                    {"timestamp": "2026-04-18T12:00:00Z", "snapshot_path": str(older)},
                    {"timestamp": "2026-04-18T12:05:00Z", "snapshot_path": str(newer)},
                ]
            }
        ),
        encoding="utf-8",
    )

    result = runner.invoke(
        app,
        [
            "eval",
            "compare-reports",
            "--history-dir",
            str(history_dir),
            "--json",
        ],
    )

    assert result.exit_code == 0
    payload = json.loads(result.stdout)
    assert payload["baseline_report"] == str(older)
    assert payload["current_report"] == str(newer)
