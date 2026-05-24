from __future__ import annotations

import json
import shlex
import subprocess
from collections.abc import Sequence
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import UTC, datetime
from pathlib import Path
from typing import Literal, Protocol

from pydantic import BaseModel, ConfigDict, Field

from piranesi.host.collect import (
    OPTIONAL_TEXT_COMMANDS,
    OSQUERY_QUERIES,
    CollectionCommandResult,
    HostCollectionManifest,
)
from piranesi.host.ingest import HostInputError, load_host_input, redact_auth_value

SudoMode = Literal["never", "prompt", "passwordless"]
RemoteCollectionStatus = Literal["ok", "partial", "failed", "dry_run"]
RemoteDoctorStatus = Literal["ok", "warn", "failed"]
REMOTE_SUDO_MODES: set[str] = {"never", "prompt", "passwordless"}
_MAX_STDOUT_CHARS = 2_000_000
_MAX_STDERR_CHARS = 500


class RemoteHostTarget(BaseModel):
    model_config = ConfigDict(extra="forbid")

    host: str
    user: str | None = None
    port: int = 22
    identity_file: str | None = None


class RemoteCommandResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    command: list[str]
    returncode: int
    stdout: str = ""
    stderr: str = ""


class RemoteCollectionResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    target: RemoteHostTarget
    output_dir: str
    status: RemoteCollectionStatus
    manifest_path: str | None = None
    snapshot_path: str | None = None
    error: str | None = None
    planned_commands: list[list[str]] = Field(default_factory=list)


class RemoteCollectionSummary(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = 1
    generated_at: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    output_dir: str
    host_count: int
    success_count: int
    failure_count: int
    dry_run: bool = False
    results: list[RemoteCollectionResult] = Field(default_factory=list)


class RemoteDoctorCheck(BaseModel):
    model_config = ConfigDict(extra="forbid")

    target: RemoteHostTarget
    status: RemoteDoctorStatus
    checks: dict[str, str] = Field(default_factory=dict)
    error: str | None = None


class RemoteCollectionError(RuntimeError):
    """Raised when remote collection configuration is invalid."""


class SSHTransport(Protocol):
    def run(
        self,
        target: RemoteHostTarget,
        command: Sequence[str],
        *,
        timeout: int,
    ) -> RemoteCommandResult:
        """Run a read-only command on a remote target."""


class OpenSSHTransport:
    def run(
        self,
        target: RemoteHostTarget,
        command: Sequence[str],
        *,
        timeout: int,
    ) -> RemoteCommandResult:
        if not command:
            raise RemoteCollectionError("remote command cannot be empty")
        ssh_args = ["ssh", "-p", str(target.port)]
        if target.identity_file:
            ssh_args.extend(["-i", target.identity_file])
        remote_command = shlex.join(list(command))
        ssh_args.extend(["-o", "BatchMode=yes", _ssh_destination(target), remote_command])
        try:
            completed = subprocess.run(
                ssh_args,
                capture_output=True,
                text=True,
                timeout=timeout,
                shell=False,
            )
        except subprocess.TimeoutExpired as exc:
            return RemoteCommandResult(
                command=list(command),
                returncode=124,
                stdout=_bounded_stdout(exc.stdout if isinstance(exc.stdout, str) else ""),
                stderr=f"remote command timed out after {timeout}s",
            )
        return RemoteCommandResult(
            command=list(command),
            returncode=completed.returncode,
            stdout=_bounded_stdout(completed.stdout),
            stderr=_redact_stderr(completed.stderr) or "",
        )


def collect_remote_host(
    target: RemoteHostTarget,
    output_dir: str | Path,
    *,
    transport: SSHTransport | None = None,
    sudo_mode: SudoMode = "never",
    include_trivy: bool = True,
    timeout_seconds: int = 30,
    dry_run: bool = False,
) -> RemoteCollectionResult:
    _validate_sudo_mode(sudo_mode)
    output_path = Path(output_dir).expanduser().resolve(strict=False)
    planned = _planned_collection_commands(
        include_trivy=include_trivy,
        sudo_mode=sudo_mode,
    )
    if dry_run:
        return RemoteCollectionResult(
            target=target,
            output_dir=str(output_path),
            status="dry_run",
            planned_commands=planned,
        )

    runner = transport or OpenSSHTransport()
    raw_dir = output_path / "raw"
    osquery_dir = raw_dir / "osquery"
    commands_dir = raw_dir / "commands"
    trivy_dir = raw_dir / "trivy"
    osquery_dir.mkdir(parents=True, exist_ok=True)
    commands_dir.mkdir(parents=True, exist_ok=True)
    trivy_dir.mkdir(parents=True, exist_ok=True)

    manifest = HostCollectionManifest(
        output_dir=str(output_path),
        raw_dir=str(raw_dir),
    )
    manifest_path = output_path / "collection-manifest.json"

    successful_osquery_outputs = 0
    try:
        version = _run_remote_command(
            runner,
            target,
            ["osqueryi", "--version"],
            tool="osquery",
            name="version",
            timeout_seconds=timeout_seconds,
            output_file=None,
            parse_json=False,
        )
        manifest.commands.append(version)
        if version.status == "ok":
            manifest.tool_versions["osquery"] = "remote osqueryi"

        for name, query in OSQUERY_QUERIES.items():
            result = _run_remote_command(
                runner,
                target,
                ["osqueryi", "--json", query],
                tool="osquery",
                name=name,
                timeout_seconds=timeout_seconds,
                output_file=osquery_dir / f"{name}.json",
                parse_json=True,
            )
            manifest.commands.append(result)
            if result.status == "ok":
                successful_osquery_outputs += 1

        for name, command in OPTIONAL_TEXT_COMMANDS.items():
            manifest.commands.append(
                _run_remote_command(
                    runner,
                    target,
                    command,
                    tool="system",
                    name=name,
                    timeout_seconds=timeout_seconds,
                    output_file=commands_dir / f"{name}.json",
                    parse_json=False,
                    wrap_text_output=True,
                )
            )

        if include_trivy:
            _collect_remote_trivy(
                runner=runner,
                target=target,
                trivy_dir=trivy_dir,
                sudo_mode=sudo_mode,
                timeout_seconds=timeout_seconds,
                manifest=manifest,
            )
        else:
            manifest.commands.append(
                CollectionCommandResult(
                    tool="trivy",
                    name="filesystem_scan",
                    status="skipped",
                    stderr="Trivy collection disabled by --no-trivy",
                )
            )

        if successful_osquery_outputs == 0:
            raise RemoteCollectionError("remote osqueryi did not produce usable JSON evidence")
        try:
            snapshot = load_host_input(output_path)
        except HostInputError as exc:
            raise RemoteCollectionError(str(exc)) from exc
        snapshot_path = output_path / "host_snapshot.json"
        snapshot_path.write_text(snapshot.model_dump_json(indent=2), encoding="utf-8")
        manifest.snapshot_file = str(snapshot_path)
        _write_manifest(manifest_path, manifest)
        return RemoteCollectionResult(
            target=target,
            output_dir=str(output_path),
            status="ok",
            manifest_path=str(manifest_path),
            snapshot_path=str(snapshot_path),
        )
    except Exception as exc:
        _write_manifest(manifest_path, manifest)
        return RemoteCollectionResult(
            target=target,
            output_dir=str(output_path),
            status="failed",
            manifest_path=str(manifest_path),
            error=str(exc),
        )


def collect_remote_hosts(
    targets: Sequence[RemoteHostTarget],
    output_dir: str | Path,
    *,
    transport: SSHTransport | None = None,
    sudo_mode: SudoMode = "never",
    include_trivy: bool = True,
    jobs: int = 1,
    timeout_seconds: int = 30,
    dry_run: bool = False,
) -> RemoteCollectionSummary:
    _validate_sudo_mode(sudo_mode)
    output_path = Path(output_dir).expanduser().resolve(strict=False)
    if not dry_run:
        output_path.mkdir(parents=True, exist_ok=True)
    results: list[RemoteCollectionResult] = []
    if jobs <= 1 or len(targets) <= 1:
        for target in targets:
            results.append(
                collect_remote_host(
                    target,
                    output_path / _target_output_name(target),
                    transport=transport,
                    sudo_mode=sudo_mode,
                    include_trivy=include_trivy,
                    timeout_seconds=timeout_seconds,
                    dry_run=dry_run,
                )
            )
    else:
        with ThreadPoolExecutor(max_workers=jobs) as executor:
            futures = {
                executor.submit(
                    collect_remote_host,
                    target,
                    output_path / _target_output_name(target),
                    transport=transport,
                    sudo_mode=sudo_mode,
                    include_trivy=include_trivy,
                    timeout_seconds=timeout_seconds,
                    dry_run=dry_run,
                ): target
                for target in targets
            }
            for future in as_completed(futures):
                results.append(future.result())
        results.sort(key=lambda item: item.target.host)
    summary = RemoteCollectionSummary(
        output_dir=str(output_path),
        host_count=len(results),
        success_count=sum(1 for result in results if result.status in {"ok", "dry_run"}),
        failure_count=sum(1 for result in results if result.status == "failed"),
        dry_run=dry_run,
        results=results,
    )
    if not dry_run:
        write_remote_collection_summary(summary, output_path)
    return summary


def doctor_remote_hosts(
    targets: Sequence[RemoteHostTarget],
    *,
    transport: SSHTransport | None = None,
    include_trivy: bool = True,
    timeout_seconds: int = 30,
) -> list[RemoteDoctorCheck]:
    runner = transport or OpenSSHTransport()
    checks: list[RemoteDoctorCheck] = []
    for target in targets:
        try:
            osquery = runner.run(target, ["osqueryi", "--version"], timeout=timeout_seconds)
            uname = runner.run(target, ["uname", "-s"], timeout=timeout_seconds)
            check_map = {
                "ssh": "ok",
                "osquery": "ok" if osquery.returncode == 0 else "missing",
                "kernel": _first_line(uname.stdout) if uname.returncode == 0 else "unknown",
            }
            if include_trivy:
                trivy = runner.run(target, ["trivy", "--version"], timeout=timeout_seconds)
                check_map["trivy"] = "ok" if trivy.returncode == 0 else "missing"
            status: RemoteDoctorStatus = "ok" if check_map["osquery"] == "ok" else "failed"
            checks.append(RemoteDoctorCheck(target=target, status=status, checks=check_map))
        except Exception as exc:
            checks.append(RemoteDoctorCheck(target=target, status="failed", error=str(exc)))
    return checks


def parse_remote_hosts_file(
    path: str | Path,
    *,
    user: str | None = None,
    port: int = 22,
    identity_file: str | None = None,
) -> list[RemoteHostTarget]:
    targets: list[RemoteHostTarget] = []
    for raw_line in Path(path).read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        targets.append(
            RemoteHostTarget(
                host=line,
                user=user,
                port=port,
                identity_file=str(identity_file) if identity_file else None,
            )
        )
    return targets


def write_remote_collection_summary(
    summary: RemoteCollectionSummary,
    output_dir: str | Path,
) -> tuple[Path, Path]:
    path = Path(output_dir)
    json_path = path / "remote-collection-summary.json"
    md_path = path / "remote-collection-summary.md"
    json_path.write_text(summary.model_dump_json(indent=2), encoding="utf-8")
    md_path.write_text(render_remote_collection_summary(summary), encoding="utf-8")
    return json_path, md_path


def render_remote_collection_summary(summary: RemoteCollectionSummary) -> str:
    lines = [
        "# Piranesi Remote Collection Summary",
        "",
        f"- Hosts: {summary.host_count}",
        f"- Successful: {summary.success_count}",
        f"- Failed: {summary.failure_count}",
        f"- Dry run: {'yes' if summary.dry_run else 'no'}",
        "",
        "| Host | Status | Output | Error |",
        "| --- | --- | --- | --- |",
    ]
    for result in summary.results:
        lines.append(
            "| "
            f"{result.target.host} | {result.status} | {result.output_dir} | "
            f"{result.error or ''} |"
        )
    lines.append("")
    return "\n".join(lines)


def render_remote_doctor(checks: Sequence[RemoteDoctorCheck]) -> str:
    lines = ["Piranesi remote doctor", ""]
    for check in checks:
        lines.append(f"[{check.status.upper()}] {check.target.host}")
        for name, status in check.checks.items():
            lines.append(f"  - {name}: {status}")
        if check.error:
            lines.append(f"  - error: {check.error}")
    lines.append("")
    return "\n".join(lines)


def _collect_remote_trivy(
    *,
    runner: SSHTransport,
    target: RemoteHostTarget,
    trivy_dir: Path,
    sudo_mode: SudoMode,
    timeout_seconds: int,
    manifest: HostCollectionManifest,
) -> None:
    base_command = ["trivy", "fs", "--format", "json", "--quiet", "--scanners", "vuln", "/"]
    command = _sudo_command(base_command, sudo_mode=sudo_mode)
    if command is None:
        manifest.commands.append(
            CollectionCommandResult(
                tool="trivy",
                name="filesystem_scan",
                command=base_command,
                status="skipped",
                stderr="sudo-dependent Trivy filesystem collection requires --sudo-mode opt-in",
            )
        )
        return
    manifest.commands.append(
        _run_remote_command(
            runner,
            target,
            command,
            tool="trivy",
            name="filesystem_scan",
            timeout_seconds=timeout_seconds,
            output_file=trivy_dir / "results.json",
            parse_json=True,
        )
    )


def _run_remote_command(
    runner: SSHTransport,
    target: RemoteHostTarget,
    command: Sequence[str],
    *,
    tool: str,
    name: str,
    timeout_seconds: int,
    output_file: Path | None,
    parse_json: bool,
    wrap_text_output: bool = False,
) -> CollectionCommandResult:
    try:
        completed = runner.run(target, command, timeout=timeout_seconds)
    except Exception as exc:
        return CollectionCommandResult(
            tool=tool,
            name=name,
            command=list(command),
            status="failed",
            stderr=_redact_stderr(str(exc)),
        )
    if completed.returncode == 124:
        return CollectionCommandResult(
            tool=tool,
            name=name,
            command=list(command),
            status="timeout",
            exit_code=124,
            stderr=_redact_stderr(completed.stderr),
        )
    if completed.returncode != 0:
        return CollectionCommandResult(
            tool=tool,
            name=name,
            command=list(command),
            status="failed",
            exit_code=completed.returncode,
            stderr=_redact_stderr(completed.stderr),
        )
    stdout = _bounded_stdout(completed.stdout)
    if parse_json:
        try:
            payload = json.loads(stdout or "[]")
        except json.JSONDecodeError as exc:
            return CollectionCommandResult(
                tool=tool,
                name=name,
                command=list(command),
                status="failed",
                exit_code=completed.returncode,
                stderr=f"invalid JSON output: {exc}",
            )
    elif wrap_text_output:
        payload = {
            "command": list(command),
            "stdout": stdout,
            "stderr": _redact_stderr(completed.stderr),
        }
    else:
        payload = None
    if output_file is not None and payload is not None:
        output_file.parent.mkdir(parents=True, exist_ok=True)
        output_file.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    return CollectionCommandResult(
        tool=tool,
        name=name,
        command=list(command),
        status="ok",
        exit_code=0,
        output_file=str(output_file) if output_file is not None and payload is not None else None,
    )


def _planned_collection_commands(*, include_trivy: bool, sudo_mode: SudoMode) -> list[list[str]]:
    commands = [["osqueryi", "--version"]]
    commands.extend(["osqueryi", "--json", query] for query in OSQUERY_QUERIES.values())
    commands.extend(list(command) for command in OPTIONAL_TEXT_COMMANDS.values())
    if include_trivy:
        trivy = ["trivy", "fs", "--format", "json", "--quiet", "--scanners", "vuln", "/"]
        commands.append(_sudo_command(trivy, sudo_mode=sudo_mode) or trivy)
    return commands


def _sudo_command(command: list[str], *, sudo_mode: SudoMode) -> list[str] | None:
    if sudo_mode == "never":
        return None
    if sudo_mode == "passwordless":
        return ["sudo", "-n", *command]
    return ["sudo", *command]


def _validate_sudo_mode(value: str) -> None:
    if value not in REMOTE_SUDO_MODES:
        raise RemoteCollectionError(
            f"invalid sudo mode '{value}'. Supported values: never, prompt, passwordless"
        )


def _ssh_destination(target: RemoteHostTarget) -> str:
    return f"{target.user}@{target.host}" if target.user else target.host


def _target_output_name(target: RemoteHostTarget) -> str:
    return target.host.replace("/", "_").replace(":", "_")


def _write_manifest(path: Path, manifest: HostCollectionManifest) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(manifest.model_dump_json(indent=2), encoding="utf-8")


def _first_line(value: str) -> str:
    return next((line.strip() for line in value.splitlines() if line.strip()), "")


def _bounded_stdout(value: str | None) -> str:
    rendered = value or ""
    if len(rendered) > _MAX_STDOUT_CHARS:
        rendered = rendered[:_MAX_STDOUT_CHARS] + (
            f"\n[TRUNCATED after {_MAX_STDOUT_CHARS} characters]"
        )
    return rendered


def _redact_stderr(value: str | None) -> str | None:
    if value is None:
        return None
    rendered = redact_auth_value(" ".join(value.split()))
    return rendered[:_MAX_STDERR_CHARS] if rendered else None


__all__ = [
    "REMOTE_SUDO_MODES",
    "OpenSSHTransport",
    "RemoteCollectionError",
    "RemoteCollectionResult",
    "RemoteCollectionSummary",
    "RemoteCommandResult",
    "RemoteDoctorCheck",
    "RemoteHostTarget",
    "SSHTransport",
    "SudoMode",
    "collect_remote_host",
    "collect_remote_hosts",
    "doctor_remote_hosts",
    "parse_remote_hosts_file",
    "render_remote_collection_summary",
    "render_remote_doctor",
    "write_remote_collection_summary",
]
