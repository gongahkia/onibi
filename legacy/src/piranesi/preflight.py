from __future__ import annotations

import platform
import shutil
import subprocess
import sys
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from piranesi import __version__

PreflightMode = Literal["workbench", "source", "host", "container", "kubernetes", "all"]
PreflightStatus = Literal["ok", "missing", "error"]
ExecutableLookup = Callable[[str], str | None]
CommandRunner = Callable[..., subprocess.CompletedProcess[str]]


class PreflightTool(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    label: str
    command: list[str]
    required: bool
    available: bool
    status: PreflightStatus
    version: str | None = None
    path: str | None = None
    summary: str
    install_hint: str
    docs_url: str | None = None


class PreflightReport(BaseModel):
    model_config = ConfigDict(extra="forbid")

    generated_at: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    mode: PreflightMode
    piranesi_version: str
    python: str
    platform: str
    ready: bool
    checks: list[PreflightTool]
    summary: dict[str, int]


@dataclass(frozen=True, slots=True)
class _ToolSpec:
    name: str
    label: str
    executable: str
    version_command: tuple[str, ...]
    install_hint: str
    docs_url: str
    required_modes: frozenset[PreflightMode]


_DOCS_BASE = "https://github.com/gongahkia/piranesi/tree/main/docs"
_TOOL_SPECS: tuple[_ToolSpec, ...] = (
    _ToolSpec(
        name="joern",
        label="Joern",
        executable="joern",
        version_command=("joern", "--version"),
        install_hint=(
            "Install Joern for CPG-backed source analysis, or use deterministic workflows "
            "that do not require it."
        ),
        docs_url=f"{_DOCS_BASE}/getting-started.md",
        required_modes=frozenset(),
    ),
    _ToolSpec(
        name="java",
        label="Java",
        executable="java",
        version_command=("java", "-version"),
        install_hint="Install a recent OpenJDK build before using Joern-backed source analysis.",
        docs_url=f"{_DOCS_BASE}/getting-started.md",
        required_modes=frozenset(),
    ),
    _ToolSpec(
        name="node",
        label="Node.js",
        executable="node",
        version_command=("node", "--version"),
        install_hint=(
            "Install Node.js when scanning TypeScript/JavaScript projects that need local "
            "package tooling."
        ),
        docs_url=f"{_DOCS_BASE}/getting-started.md",
        required_modes=frozenset(),
    ),
    _ToolSpec(
        name="npm",
        label="npm",
        executable="npm",
        version_command=("npm", "--version"),
        install_hint=(
            "Install npm with Node.js when project scripts or TypeScript tooling are needed."
        ),
        docs_url=f"{_DOCS_BASE}/getting-started.md",
        required_modes=frozenset(),
    ),
    _ToolSpec(
        name="docker",
        label="Docker",
        executable="docker",
        version_command=("docker", "--version"),
        install_hint="Install Docker before running dynamic verification or container workflows.",
        docs_url=f"{_DOCS_BASE}/docker.md",
        required_modes=frozenset(),
    ),
    _ToolSpec(
        name="osquery",
        label="osquery",
        executable="osqueryi",
        version_command=("osqueryi", "--version"),
        install_hint="Install osquery to collect live Linux host evidence.",
        docs_url=f"{_DOCS_BASE}/host-posture.md",
        required_modes=frozenset({"host", "all"}),
    ),
    _ToolSpec(
        name="trivy",
        label="Trivy",
        executable="trivy",
        version_command=("trivy", "--version"),
        install_hint="Install Trivy for CVE/package evidence in host and container workflows.",
        docs_url=f"{_DOCS_BASE}/container-kubernetes.md",
        required_modes=frozenset({"container"}),
    ),
    _ToolSpec(
        name="lynis",
        label="Lynis",
        executable="lynis",
        version_command=("lynis", "--version"),
        install_hint="Install Lynis to include hardening baseline evidence in host assessments.",
        docs_url=f"{_DOCS_BASE}/host-posture.md",
        required_modes=frozenset(),
    ),
    _ToolSpec(
        name="openscap",
        label="OpenSCAP",
        executable="oscap",
        version_command=("oscap", "--version"),
        install_hint=(
            "Install OpenSCAP to include compliance baseline evidence in host assessments."
        ),
        docs_url=f"{_DOCS_BASE}/host-posture.md",
        required_modes=frozenset(),
    ),
    _ToolSpec(
        name="kubectl",
        label="kubectl",
        executable="kubectl",
        version_command=("kubectl", "version", "--client=true"),
        install_hint="Install kubectl when collecting read-only Kubernetes API snapshots.",
        docs_url=f"{_DOCS_BASE}/container-kubernetes.md",
        required_modes=frozenset({"kubernetes"}),
    ),
)


def build_preflight_report(
    *,
    mode: PreflightMode = "workbench",
    executable_lookup: ExecutableLookup = shutil.which,
    command_runner: CommandRunner = subprocess.run,
) -> PreflightReport:
    checks = [
        _python_tool(),
        *[
            _tool_readiness(
                spec,
                mode=mode,
                executable_lookup=executable_lookup,
                command_runner=command_runner,
            )
            for spec in _TOOL_SPECS
        ],
    ]
    required = [check for check in checks if check.required]
    missing_required = [check for check in required if check.status in {"missing", "error"}]
    summary = {
        "total": len(checks),
        "required": len(required),
        "available": sum(1 for check in checks if check.available),
        "missing_required": len(missing_required),
        "missing_optional": sum(
            1 for check in checks if not check.required and check.status == "missing"
        ),
        "errors": sum(1 for check in checks if check.status == "error"),
    }
    return PreflightReport(
        mode=mode,
        piranesi_version=__version__,
        python=sys.version.split()[0],
        platform=f"{platform.system()} {platform.release()}".strip(),
        ready=not missing_required,
        checks=checks,
        summary=summary,
    )


def _python_tool() -> PreflightTool:
    return PreflightTool(
        name="python",
        label="Python",
        command=[sys.executable, "--version"],
        required=True,
        available=True,
        status="ok",
        version=sys.version.split()[0],
        path=sys.executable,
        summary=f"Python {sys.version.split()[0]} is running Piranesi.",
        install_hint="Use Python 3.12 or newer.",
        docs_url=f"{_DOCS_BASE}/getting-started.md",
    )


def _tool_readiness(
    spec: _ToolSpec,
    *,
    mode: PreflightMode,
    executable_lookup: ExecutableLookup,
    command_runner: CommandRunner,
) -> PreflightTool:
    required = mode in spec.required_modes
    path = executable_lookup(spec.executable)
    if path is None:
        return PreflightTool(
            name=spec.name,
            label=spec.label,
            command=list(spec.version_command),
            required=required,
            available=False,
            status="missing",
            summary=f"{spec.label} was not found on PATH.",
            install_hint=spec.install_hint,
            docs_url=spec.docs_url,
        )
    version, status = _version_output(spec.version_command, command_runner)
    return PreflightTool(
        name=spec.name,
        label=spec.label,
        command=list(spec.version_command),
        required=required,
        available=status == "ok",
        status=status,
        version=version,
        path=path,
        summary=(
            f"{spec.label} is available."
            if status == "ok"
            else f"{spec.label} exists but version probing failed."
        ),
        install_hint=spec.install_hint,
        docs_url=spec.docs_url,
    )


def _version_output(
    command: tuple[str, ...],
    command_runner: CommandRunner,
) -> tuple[str | None, PreflightStatus]:
    try:
        completed = command_runner(
            list(command),
            text=True,
            capture_output=True,
            timeout=1,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return None, "error"
    output = "\n".join(part for part in (completed.stdout, completed.stderr) if part)
    first_line = next((line.strip() for line in output.splitlines() if line.strip()), None)
    return first_line, "ok" if completed.returncode == 0 else "error"


__all__ = [
    "PreflightMode",
    "PreflightReport",
    "PreflightTool",
    "build_preflight_report",
]
