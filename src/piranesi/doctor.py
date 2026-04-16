from __future__ import annotations

import importlib.util
import os
import platform
import shutil
import subprocess
import sys
from fnmatch import fnmatch
from pathlib import Path

from pydantic import BaseModel, ConfigDict, Field

from piranesi import __version__
from piranesi.config import ConfigError, PiranesiConfig, load_config
from piranesi.scan.framework import resolve_frameworks

LLM_API_ENV_VARS = (
    "OPENAI_API_KEY",
    "ANTHROPIC_API_KEY",
    "OPENROUTER_API_KEY",
    "AZURE_OPENAI_API_KEY",
    "GEMINI_API_KEY",
    "GOOGLE_API_KEY",
    "LITELLM_API_KEY",
)


class DoctorCheck(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    status: str
    summary: str
    detail: str | None = None
    next_step: str | None = None


class DoctorReport(BaseModel):
    model_config = ConfigDict(extra="forbid")

    piranesi_version: str
    target: str
    config_path: str
    ready: bool
    deterministic_ready: bool
    full_pipeline_ready: bool
    frameworks: list[str] = Field(default_factory=list)
    scan_targets: int = 0
    checks: list[DoctorCheck] = Field(default_factory=list)


def build_doctor_report(
    target_dir: str | Path,
    *,
    config_path: str | Path = "piranesi.toml",
) -> DoctorReport:
    target = Path(target_dir).resolve(strict=False)
    config_file = Path(config_path).resolve(strict=False)
    checks: list[DoctorCheck] = []

    checks.append(_python_check())
    checks.append(_path_check("target", target, expected_kind="dir"))

    config = _load_effective_config(config_file, checks)
    if config is None:
        config = PiranesiConfig()

    frameworks: list[str] = []
    scan_target_paths: list[Path] = []
    scan_targets = 0
    if target.is_dir():
        try:
            frameworks = list(resolve_frameworks(target, config.scan.frameworks))
            scan_target_paths = _discover_scan_targets(target, config)
            scan_targets = len(scan_target_paths)
            checks.append(
                DoctorCheck(
                    name="project",
                    status="ok" if scan_targets else "warn",
                    summary=(
                        f"detected {scan_targets} scan target(s); "
                        f"frameworks={', '.join(frameworks) if frameworks else 'none'}"
                    ),
                    next_step=(
                        None
                        if scan_targets
                        else "Review [scan].include_patterns or run `piranesi init`."
                    ),
                )
            )
        except Exception as exc:
            checks.append(
                DoctorCheck(
                    name="project",
                    status="fail",
                    summary="project auto-detection failed",
                    detail=str(exc),
                    next_step="Check scan include/exclude patterns and project permissions.",
                )
            )

    js_tooling_required = _has_javascript_targets(scan_target_paths)
    checks.extend(
        [
            _command_check("uv", ["uv", "--version"], required=False),
            _command_check("git", ["git", "--version"], required=False),
            _command_check("joern", [config.joern.binary_path, "--help"], required=True),
            _command_check("java", ["java", "-version"], required=True),
            _command_check("node", ["node", "--version"], required=js_tooling_required),
            _command_check("npm", ["npm", "--version"], required=False),
            _command_check("tsc", ["tsc", "--version"], required=js_tooling_required),
            _docker_check(),
            _llm_check(),
            _optional_module_check("watchfiles", "watch mode"),
            _optional_module_check("pygls", "LSP server"),
            _optional_module_check("textual", "terminal UI"),
        ]
    )

    deterministic_required = {"python", "target", "config", "joern", "java"}
    if js_tooling_required:
        deterministic_required.update({"node", "tsc"})
    blocking_failures = {
        check.name
        for check in checks
        if check.status == "fail" and check.name in deterministic_required
    }
    deterministic_ready = not blocking_failures and scan_targets > 0
    full_pipeline_ready = deterministic_ready and _check_status(checks, "llm") == "ok"
    ready = deterministic_ready

    return DoctorReport(
        piranesi_version=__version__,
        target=str(target),
        config_path=str(config_file),
        ready=ready,
        deterministic_ready=deterministic_ready,
        full_pipeline_ready=full_pipeline_ready,
        frameworks=frameworks,
        scan_targets=scan_targets,
        checks=checks,
    )


def render_doctor_report(report: DoctorReport) -> str:
    lines = [
        f"Piranesi doctor v{report.piranesi_version}",
        f"Target: {report.target}",
        f"Config: {report.config_path}",
        "",
        f"Deterministic scan ready: {_yes_no(report.deterministic_ready)}",
        f"Full LLM-assisted pipeline ready: {_yes_no(report.full_pipeline_ready)}",
        f"Frameworks: {', '.join(report.frameworks) if report.frameworks else 'none'}",
        f"Scan targets: {report.scan_targets}",
        "",
        "Checks:",
    ]
    for check in report.checks:
        lines.append(f"- [{check.status.upper()}] {check.name}: {check.summary}")
        if check.detail:
            lines.append(f"  detail: {check.detail}")
        if check.next_step:
            lines.append(f"  next: {check.next_step}")
    return "\n".join(lines) + "\n"


def _load_effective_config(config_path: Path, checks: list[DoctorCheck]) -> PiranesiConfig | None:
    if not config_path.exists():
        checks.append(
            DoctorCheck(
                name="config",
                status="warn",
                summary="config file not found; built-in defaults will be used",
                next_step="Run `piranesi init` to generate a tuned piranesi.toml.",
            )
        )
        return PiranesiConfig()
    try:
        config = load_config(config_path)
    except ConfigError as exc:
        checks.append(
            DoctorCheck(
                name="config",
                status="fail",
                summary="config file is invalid",
                detail=str(exc),
                next_step="Fix TOML syntax or regenerate it with `piranesi init`.",
            )
        )
        return None
    checks.append(
        DoctorCheck(
            name="config",
            status="ok",
            summary="config file loaded",
        )
    )
    return config


def _discover_scan_targets(target: Path, config: PiranesiConfig) -> list[Path]:
    files: list[Path] = []
    for path in sorted(target.rglob("*")):
        if not path.is_file():
            continue
        try:
            relative = path.relative_to(target).as_posix()
        except ValueError:
            relative = path.as_posix()
        if any(_matches_glob(relative, pattern) for pattern in config.scan.exclude_patterns):
            continue
        if not any(_matches_glob(relative, pattern) for pattern in config.scan.include_patterns):
            continue
        try:
            if path.stat().st_size > config.scan.max_file_size:
                continue
        except OSError:
            continue
        files.append(path)
    return files


def _matches_glob(relative_path: str, pattern: str) -> bool:
    return fnmatch(relative_path, pattern) or (
        pattern.startswith("**/") and fnmatch(relative_path, pattern[3:])
    )


def _has_javascript_targets(paths: list[Path]) -> bool:
    return any(path.suffix.lower() in {".js", ".jsx", ".ts", ".tsx"} for path in paths)


def _python_check() -> DoctorCheck:
    version = sys.version_info
    version_text = platform.python_version()
    if version < (3, 12):
        return DoctorCheck(
            name="python",
            status="fail",
            summary=f"Python {version_text} is too old",
            next_step="Use Python 3.12 or newer.",
        )
    if version >= (3, 14):
        return DoctorCheck(
            name="python",
            status="warn",
            summary=f"Python {version_text} is newer than the CI-tested range",
            next_step="Use Python 3.12 or 3.13 for release-equivalent behavior.",
        )
    return DoctorCheck(name="python", status="ok", summary=f"Python {version_text}")


def _path_check(name: str, path: Path, *, expected_kind: str) -> DoctorCheck:
    if expected_kind == "dir" and path.is_dir():
        return DoctorCheck(name=name, status="ok", summary="directory exists")
    if expected_kind == "file" and path.is_file():
        return DoctorCheck(name=name, status="ok", summary="file exists")
    return DoctorCheck(
        name=name,
        status="fail",
        summary=f"{expected_kind} not found: {path}",
        next_step=f"Provide an existing {expected_kind} path.",
    )


def _command_check(name: str, command: list[str], *, required: bool) -> DoctorCheck:
    executable = command[0]
    if shutil.which(executable) is None:
        return DoctorCheck(
            name=name,
            status="fail" if required else "warn",
            summary=f"{executable} not found on PATH",
            next_step=_install_hint(name),
        )
    try:
        completed = subprocess.run(
            command,
            check=False,
            capture_output=True,
            text=True,
            timeout=5,
        )
    except Exception as exc:
        return DoctorCheck(
            name=name,
            status="warn" if not required else "fail",
            summary=f"{executable} is installed but could not be executed",
            detail=str(exc),
        )
    output = (completed.stdout or completed.stderr).strip().splitlines()
    summary = output[0] if output else f"{executable} found"
    status = "ok" if completed.returncode == 0 else ("warn" if not required else "fail")
    return DoctorCheck(name=name, status=status, summary=summary)


def _docker_check() -> DoctorCheck:
    if shutil.which("docker") is None:
        return DoctorCheck(
            name="docker",
            status="warn",
            summary="docker not found on PATH",
            next_step="Install Docker to enable exploit verification.",
        )
    try:
        completed = subprocess.run(
            ["docker", "info", "--format", "{{.ServerVersion}}"],
            check=False,
            capture_output=True,
            text=True,
            timeout=5,
        )
    except Exception as exc:
        return DoctorCheck(
            name="docker",
            status="warn",
            summary="docker is installed but daemon status could not be checked",
            detail=str(exc),
            next_step="Start Docker before running verification without --no-execute.",
        )
    if completed.returncode != 0:
        return DoctorCheck(
            name="docker",
            status="warn",
            summary="docker daemon is not ready",
            detail=(completed.stderr or completed.stdout).strip() or None,
            next_step="Start Docker or run with --no-execute.",
        )
    return DoctorCheck(
        name="docker",
        status="ok",
        summary=f"Docker daemon ready ({completed.stdout.strip()})",
    )


def _llm_check() -> DoctorCheck:
    configured = [name for name in LLM_API_ENV_VARS if os.getenv(name)]
    if configured:
        return DoctorCheck(
            name="llm",
            status="ok",
            summary=f"LLM credential configured via {configured[0]}",
        )
    return DoctorCheck(
        name="llm",
        status="warn",
        summary="no LiteLLM-compatible API key configured",
        next_step=(
            "Static scan/detect/report can run in deterministic mode; set one of "
            + ", ".join(LLM_API_ENV_VARS)
            + " to enable LLM triage and patch generation."
        ),
    )


def _optional_module_check(module_name: str, feature: str) -> DoctorCheck:
    if importlib.util.find_spec(module_name) is not None:
        return DoctorCheck(
            name=module_name,
            status="ok",
            summary=f"{feature} optional dependency is installed",
        )
    return DoctorCheck(
        name=module_name,
        status="warn",
        summary=f"{feature} optional dependency is not installed",
        next_step=f"Install the relevant optional extra to use {feature}.",
    )


def _check_status(checks: list[DoctorCheck], name: str) -> str | None:
    for check in checks:
        if check.name == name:
            return check.status
    return None


def _install_hint(name: str) -> str:
    hints = {
        "uv": "Install uv for the documented development workflow.",
        "git": "Install git to enable changed-file, baseline, and repository workflows.",
        "joern": "Install Joern to enable CPG-backed scan and detect stages.",
        "java": "Install a JVM, preferably OpenJDK 17, for Joern.",
        "node": "Install Node.js for JavaScript/TypeScript targets.",
        "npm": "Install npm for JavaScript/TypeScript target setup.",
        "tsc": "Install TypeScript globally with `npm install --global typescript`.",
    }
    return hints.get(name, f"Install {name}.")


def _yes_no(value: bool) -> str:
    return "yes" if value else "no"
