from __future__ import annotations

import os
import platform
import shutil
import subprocess
import sys
from collections.abc import Callable, Sequence
from pathlib import Path
from typing import Protocol

from pydantic import BaseModel, ConfigDict, Field

from piranesi import __version__

LLM_API_ENV_VARS = (
    "OPENAI_API_KEY",
    "ANTHROPIC_API_KEY",
    "OPENROUTER_API_KEY",
    "AZURE_OPENAI_API_KEY",
    "GEMINI_API_KEY",
    "GOOGLE_API_KEY",
    "LITELLM_API_KEY",
)
HOST_OPTIONAL_HELPERS: dict[str, list[str]] = {
    "ufw": ["ufw", "--version"],
    "iptables": ["iptables", "--version"],
    "nft": ["nft", "--version"],
    "apt": ["apt", "--version"],
    "dnf": ["dnf", "--version"],
    "yum": ["yum", "--version"],
    "apk": ["apk", "--version"],
    "firewall-cmd": ["firewall-cmd", "--version"],
    "getenforce": ["getenforce"],
    "sshd": ["sshd", "-T"],
    "getent": ["getent", "--version"],
    "sysctl": ["sysctl", "--version"],
}


class CommandRunner(Protocol):
    def __call__(
        self,
        args: Sequence[str],
        *,
        check: bool,
        capture_output: bool,
        text: bool,
        timeout: int,
    ) -> subprocess.CompletedProcess[str]:
        """Run a command and return a completed process."""


ExecutableLookup = Callable[[str], str | None]


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
    deterministic_ready: bool = False
    full_pipeline_ready: bool = False
    collect_ready: bool = False
    assess_ready: bool = False
    required_tools: dict[str, str] = Field(default_factory=dict)
    optional_tools: dict[str, str] = Field(default_factory=dict)
    warnings: list[str] = Field(default_factory=list)
    next_steps: list[str] = Field(default_factory=list)
    frameworks: list[str] = Field(default_factory=list)
    scan_targets: int = 0
    checks: list[DoctorCheck] = Field(default_factory=list)


def build_doctor_report(
    target_dir: str | Path,
    *,
    config_path: str | Path = "piranesi.toml",
    host_only: bool = False,
    executable_lookup: ExecutableLookup = shutil.which,
    command_runner: CommandRunner = subprocess.run,
) -> DoctorReport:
    target = Path(target_dir).resolve(strict=False)
    config_file = Path(config_path).resolve(strict=False)
    checks = [
        _python_check(),
        _path_check("target", target),
        _platform_check(),
        _command_check(
            "osquery",
            ["osqueryi", "--version"],
            required=True,
            executable_lookup=executable_lookup,
            command_runner=command_runner,
        ),
        _command_check(
            "trivy",
            ["trivy", "--version"],
            required=False,
            executable_lookup=executable_lookup,
            command_runner=command_runner,
        ),
        *[
            _command_check(
                name,
                command,
                required=False,
                executable_lookup=executable_lookup,
                command_runner=command_runner,
            )
            for name, command in HOST_OPTIONAL_HELPERS.items()
        ],
    ]
    if not host_only:
        checks.append(_llm_check())

    required_tools = {
        check.name: check.status
        for check in checks
        if check.name in {"python", "target", "platform", "osquery"}
    }
    optional_names = {"trivy", *HOST_OPTIONAL_HELPERS}
    if not host_only:
        optional_names.add("llm")
    optional_tools = {check.name: check.status for check in checks if check.name in optional_names}
    warnings = [check.summary for check in checks if check.status == "warn"]
    next_steps = [
        check.next_step
        for check in checks
        if check.next_step is not None and check.status in {"warn", "fail"}
    ]

    assess_ready = _all_ok(checks, {"python", "target"})
    collect_ready = _all_ok(checks, {"python", "target", "osquery"})

    return DoctorReport(
        piranesi_version=__version__,
        target=str(target),
        config_path=str(config_file),
        ready=assess_ready,
        deterministic_ready=assess_ready,
        full_pipeline_ready=(
            collect_ready if host_only else collect_ready and _check_status(checks, "llm") == "ok"
        ),
        collect_ready=collect_ready,
        assess_ready=assess_ready,
        required_tools=required_tools,
        optional_tools=optional_tools,
        warnings=warnings,
        next_steps=next_steps,
        checks=checks,
    )


def render_doctor_report(report: DoctorReport) -> str:
    lines = [
        f"Piranesi doctor v{report.piranesi_version}",
        f"Target: {report.target}",
        "",
        f"Host collection ready: {_yes_no(report.collect_ready)}",
        f"Host assessment ready: {_yes_no(report.assess_ready)}",
        "",
        "Checks:",
    ]
    for check in report.checks:
        lines.append(f"- [{check.status.upper()}] {check.name}: {check.summary}")
        if check.detail:
            lines.append(f"  detail: {check.detail}")
        if check.next_step:
            lines.append(f"  next: {check.next_step}")
    if report.next_steps:
        lines.extend(["", "Next steps:"])
        for step in report.next_steps:
            lines.append(f"- {step}")
    return "\n".join(lines) + "\n"


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


def _path_check(name: str, path: Path) -> DoctorCheck:
    if path.exists():
        kind = "directory" if path.is_dir() else "file"
        return DoctorCheck(name=name, status="ok", summary=f"{kind} exists")
    return DoctorCheck(
        name=name,
        status="fail",
        summary=f"path not found: {path}",
        next_step="Provide an existing host snapshot, evidence bundle, or working directory.",
    )


def _platform_check() -> DoctorCheck:
    system = platform.system()
    if system == "Linux":
        distro = _linux_distribution()
        if distro and distro not in {"debian", "ubuntu"}:
            return DoctorCheck(
                name="platform",
                status="warn",
                summary=f"Linux distribution `{distro}` is not in the Phase 1 support target",
                next_step="Use Debian/Ubuntu evidence for the most complete host assessment.",
            )
        return DoctorCheck(
            name="platform",
            status="ok",
            summary=f"Linux host supported{f' ({distro})' if distro else ''}",
        )
    return DoctorCheck(
        name="platform",
        status="warn",
        summary=f"{system or 'unknown'} is a development host; collection targets Linux VMs",
        next_step="Run `piranesi collect` inside a Debian/Ubuntu VM or assess an existing bundle.",
    )


def _command_check(
    name: str,
    command: list[str],
    *,
    required: bool,
    executable_lookup: ExecutableLookup,
    command_runner: CommandRunner,
) -> DoctorCheck:
    executable = command[0]
    if executable_lookup(executable) is None:
        return DoctorCheck(
            name=name,
            status="fail" if required else "warn",
            summary=f"{executable} not found on PATH",
            next_step=_install_hint(name),
        )
    try:
        completed = command_runner(
            command,
            check=False,
            capture_output=True,
            text=True,
            timeout=5,
        )
    except Exception as exc:
        return DoctorCheck(
            name=name,
            status="fail" if required else "warn",
            summary=f"{executable} is installed but could not be executed",
            detail=str(exc),
        )
    output = (completed.stdout or completed.stderr).strip().splitlines()
    summary = output[0] if output else f"{executable} found"
    status = "ok" if completed.returncode == 0 else ("fail" if required else "warn")
    return DoctorCheck(
        name=name,
        status=status,
        summary=summary,
        detail=None if completed.returncode == 0 else (completed.stderr or completed.stdout),
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
            "`piranesi assess --analysis deterministic` works without an LLM; set one of "
            + ", ".join(LLM_API_ENV_VARS)
            + " to enable `--analysis llm` or `--analysis both`."
        ),
    )


def _linux_distribution() -> str | None:
    os_release = Path("/etc/os-release")
    if not os_release.is_file():
        return None
    try:
        lines = os_release.read_text(encoding="utf-8").splitlines()
    except OSError:
        return None
    for line in lines:
        if line.startswith("ID="):
            return line.split("=", 1)[1].strip().strip('"').lower()
    return None


def _all_ok(checks: list[DoctorCheck], names: set[str]) -> bool:
    return all(_check_status(checks, name) in {"ok", "warn"} for name in names)


def _check_status(checks: list[DoctorCheck], name: str) -> str | None:
    for check in checks:
        if check.name == name:
            return check.status
    return None


def _install_hint(name: str) -> str:
    hints = {
        "osquery": "Install osquery to enable `piranesi collect`.",
        "trivy": "Install Trivy to add package CVE evidence to host assessment.",
        "ufw": "Install ufw or ensure another firewall helper is available.",
        "iptables": "Install iptables or ensure another firewall helper is available.",
        "nft": "Install nftables or ensure another firewall helper is available.",
        "apt": "Install apt to add Debian/Ubuntu package update evidence.",
        "sshd": "Install OpenSSH server utilities to collect effective SSH configuration.",
        "getent": "Install libc-bin or equivalent getent support for admin group evidence.",
        "sysctl": "Install procps to collect kernel hardening settings.",
    }
    return hints.get(name, f"Install {name}.")


def _yes_no(value: bool) -> str:
    return "yes" if value else "no"
