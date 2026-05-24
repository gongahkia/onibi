from __future__ import annotations

import json
import shutil
import subprocess
from collections.abc import Callable, Sequence
from datetime import UTC, datetime
from pathlib import Path
from typing import Literal, Protocol

from pydantic import BaseModel, ConfigDict, Field

from piranesi.host.ingest import HostInputError, load_host_input, redact_auth_value
from piranesi.host.models import HostSnapshot

CollectionStatus = Literal["ok", "missing", "failed", "timeout", "skipped"]

OSQUERY_QUERIES: dict[str, str] = {
    "system_info": "select hostname, uuid, hardware_serial, computer_name from system_info;",
    "os_version": (
        "select name, version, platform as id, version as version_id, name || ' ' || version "
        "as pretty_name from os_version;"
    ),
    "kernel_info": "select version from kernel_info;",
    "interface_addresses": ("select interface, address, mask, type from interface_addresses;"),
    "deb_packages": "select name, version, arch from deb_packages;",
    "rpm_packages": "select name, version, release, arch from rpm_packages;",
    "apk_packages": "select name, version, arch from apk_packages;",
    "listening_ports": (
        "select lp.protocol, lp.address, lp.port, lp.pid, p.name as process_name, "
        "p.path as process_path, u.username as user "
        "from listening_ports lp "
        "left join processes p on lp.pid = p.pid "
        "left join users u on p.uid = u.uid;"
    ),
    "processes": (
        "select p.pid, p.name, p.path, p.cmdline, u.username as user "
        "from processes p left join users u on p.uid = u.uid;"
    ),
    "users": (
        "select u.username, u.uid, u.gid, u.shell, group_concat(g.groupname) as groups "
        "from users u "
        "left join user_groups ug on u.uid = ug.uid "
        "left join groups g on ug.gid = g.gid "
        "group by u.username, u.uid, u.gid, u.shell;"
    ),
    "systemd_units": "select name, active_state, unit_file_state from systemd_units;",
    "sshd_config": (
        "select label as key, value from augeas "
        "where path = '/etc/ssh/sshd_config' "
        "and label in ("
        "'PermitRootLogin', 'PasswordAuthentication', 'PermitEmptyPasswords', "
        "'KbdInteractiveAuthentication', 'ChallengeResponseAuthentication'"
        ");"
    ),
    "sudoers": (
        "select path, label as key, value from augeas "
        "where path like '/etc/sudoers%' and value != '';"
    ),
}

OPTIONAL_TEXT_COMMANDS: dict[str, list[str]] = {
    "ufw_status": ["ufw", "status", "verbose"],
    "iptables_rules": ["iptables", "-S"],
    "nft_ruleset": ["nft", "list", "ruleset"],
    "apt_upgradable": ["apt", "list", "--upgradable"],
    "dnf_security_updates": ["dnf", "updateinfo", "list", "security"],
    "yum_security_updates": ["yum", "updateinfo", "list", "security"],
    "apk_version_outdated": ["apk", "version", "-l", "<"],
    "firewalld_state": ["firewall-cmd", "--state"],
    "selinux_getenforce": ["getenforce"],
    "sshd_effective_config": ["sshd", "-T"],
    "group_sudo": ["getent", "group", "sudo"],
    "group_admin": ["getent", "group", "admin"],
    "group_wheel": ["getent", "group", "wheel"],
    "sysctl_net_ipv4_ip_forward": ["sysctl", "-n", "net.ipv4.ip_forward"],
    "sysctl_net_ipv6_conf_all_forwarding": [
        "sysctl",
        "-n",
        "net.ipv6.conf.all.forwarding",
    ],
    "sysctl_kernel_unprivileged_bpf_disabled": [
        "sysctl",
        "-n",
        "kernel.unprivileged_bpf_disabled",
    ],
    "sysctl_kernel_kptr_restrict": ["sysctl", "-n", "kernel.kptr_restrict"],
}

AUTH_TEXT_COMMANDS: dict[str, list[str]] = {
    "who_sessions": ["who"],
    "last_logins": ["last", "-n", "50"],
    "lastb_failures": ["lastb", "-n", "50"],
    "journalctl_sshd_auth_summary": [
        "journalctl",
        "-u",
        "ssh",
        "--since",
        "-7d",
        "--no-pager",
        "-n",
        "100",
    ],
}


class CommandRunner(Protocol):
    def __call__(
        self,
        args: Sequence[str],
        *,
        capture_output: bool,
        text: bool,
        timeout: int,
    ) -> subprocess.CompletedProcess[str]:
        """Run a command and return a completed process."""


ExecutableLookup = Callable[[str], str | None]


class CollectionCommandResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    tool: str
    name: str
    command: list[str] = Field(default_factory=list)
    status: CollectionStatus
    exit_code: int | None = None
    output_file: str | None = None
    stderr: str | None = None


class HostCollectionManifest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = 1
    collected_at: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    output_dir: str
    raw_dir: str
    snapshot_file: str | None = None
    tool_versions: dict[str, str] = Field(default_factory=dict)
    commands: list[CollectionCommandResult] = Field(default_factory=list)


class HostCollectionResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    output_dir: str
    manifest_path: str
    snapshot_path: str
    raw_dir: str
    snapshot: HostSnapshot
    manifest: HostCollectionManifest


class HostCollectionError(RuntimeError):
    """Raised when local host collection cannot produce usable evidence."""


def collect_host_evidence(
    output_dir: str | Path,
    *,
    include_trivy: bool = True,
    include_lynis: bool = False,
    include_openscap: bool = False,
    include_auth_evidence: bool = False,
    trivy_target: str | Path = Path("/"),
    timeout_seconds: int = 30,
    trivy_timeout_seconds: int = 300,
    executable_lookup: ExecutableLookup = shutil.which,
    command_runner: CommandRunner = subprocess.run,
) -> HostCollectionResult:
    output_path = Path(output_dir).expanduser().resolve(strict=False)
    raw_dir = output_path / "raw"
    osquery_dir = raw_dir / "osquery"
    trivy_dir = raw_dir / "trivy"
    commands_dir = raw_dir / "commands"
    lynis_dir = raw_dir / "lynis"
    openscap_dir = raw_dir / "openscap"
    output_path.mkdir(parents=True, exist_ok=True)
    osquery_dir.mkdir(parents=True, exist_ok=True)
    trivy_dir.mkdir(parents=True, exist_ok=True)
    commands_dir.mkdir(parents=True, exist_ok=True)

    manifest = HostCollectionManifest(
        output_dir=str(output_path),
        raw_dir=str(raw_dir),
    )
    manifest_path = output_path / "collection-manifest.json"
    osquery_path = executable_lookup("osqueryi")
    if osquery_path is None:
        manifest.commands.append(
            CollectionCommandResult(
                tool="osquery",
                name="discovery",
                status="missing",
                stderr="osqueryi was not found on PATH",
            )
        )
        _write_manifest(manifest_path, manifest)
        raise HostCollectionError("osqueryi was not found on PATH")

    osquery_version = _tool_version(
        tool="osquery",
        executable=osquery_path,
        command_runner=command_runner,
        timeout_seconds=timeout_seconds,
        commands=manifest.commands,
    )
    if osquery_version:
        manifest.tool_versions["osquery"] = osquery_version

    successful_osquery_outputs = 0
    for name, query in OSQUERY_QUERIES.items():
        status = _run_json_command(
            tool="osquery",
            name=name,
            command=[osquery_path, "--json", query],
            output_file=osquery_dir / f"{name}.json",
            timeout_seconds=timeout_seconds,
            command_runner=command_runner,
        )
        manifest.commands.append(status)
        if status.status == "ok":
            successful_osquery_outputs += 1

    for name, command in OPTIONAL_TEXT_COMMANDS.items():
        manifest.commands.append(
            _run_optional_text_command(
                name=name,
                command=command,
                output_file=commands_dir / f"{name}.json",
                timeout_seconds=timeout_seconds,
                executable_lookup=executable_lookup,
                command_runner=command_runner,
            )
        )

    if include_auth_evidence:
        for name, command in AUTH_TEXT_COMMANDS.items():
            manifest.commands.append(
                _run_optional_text_command(
                    name=name,
                    command=command,
                    output_file=commands_dir / f"{name}.json",
                    timeout_seconds=timeout_seconds,
                    executable_lookup=executable_lookup,
                    command_runner=command_runner,
                    redact=True,
                    max_stdout_lines=120,
                    max_stdout_chars=16_000,
                )
            )

    if include_trivy:
        _collect_trivy(
            trivy_dir=trivy_dir,
            target=trivy_target,
            timeout_seconds=trivy_timeout_seconds,
            executable_lookup=executable_lookup,
            command_runner=command_runner,
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

    if include_lynis:
        _collect_lynis(
            lynis_dir=lynis_dir,
            timeout_seconds=timeout_seconds,
            executable_lookup=executable_lookup,
            command_runner=command_runner,
            manifest=manifest,
        )

    if include_openscap:
        _collect_openscap(
            openscap_dir=openscap_dir,
            executable_lookup=executable_lookup,
            manifest=manifest,
        )

    if successful_osquery_outputs == 0:
        _write_manifest(manifest_path, manifest)
        raise HostCollectionError("osqueryi did not produce usable JSON evidence")

    try:
        snapshot = load_host_input(output_path)
    except HostInputError as exc:
        _write_manifest(manifest_path, manifest)
        raise HostCollectionError(str(exc)) from exc

    snapshot_path = output_path / "host_snapshot.json"
    snapshot_path.write_text(snapshot.model_dump_json(indent=2), encoding="utf-8")
    manifest.snapshot_file = str(snapshot_path)
    _write_manifest(manifest_path, manifest)
    return HostCollectionResult(
        output_dir=str(output_path),
        manifest_path=str(manifest_path),
        snapshot_path=str(snapshot_path),
        raw_dir=str(raw_dir),
        snapshot=snapshot,
        manifest=manifest,
    )


def _collect_trivy(
    *,
    trivy_dir: Path,
    target: str | Path,
    timeout_seconds: int,
    executable_lookup: ExecutableLookup,
    command_runner: CommandRunner,
    manifest: HostCollectionManifest,
) -> None:
    trivy_path = executable_lookup("trivy")
    if trivy_path is None:
        manifest.commands.append(
            CollectionCommandResult(
                tool="trivy",
                name="filesystem_scan",
                status="missing",
                stderr="trivy was not found on PATH",
            )
        )
        return
    trivy_version = _tool_version(
        tool="trivy",
        executable=trivy_path,
        command_runner=command_runner,
        timeout_seconds=30,
        commands=manifest.commands,
    )
    if trivy_version:
        manifest.tool_versions["trivy"] = trivy_version
    manifest.commands.append(
        _run_json_command(
            tool="trivy",
            name="filesystem_scan",
            command=[
                trivy_path,
                "fs",
                "--format",
                "json",
                "--quiet",
                "--scanners",
                "vuln",
                str(target),
            ],
            output_file=trivy_dir / "results.json",
            timeout_seconds=timeout_seconds,
            command_runner=command_runner,
        )
    )


def _tool_version(
    *,
    tool: str,
    executable: str,
    command_runner: CommandRunner,
    timeout_seconds: int,
    commands: list[CollectionCommandResult],
) -> str | None:
    command = [executable, "--version"]
    try:
        completed = command_runner(
            command,
            capture_output=True,
            text=True,
            timeout=timeout_seconds,
        )
    except subprocess.TimeoutExpired:
        commands.append(
            CollectionCommandResult(
                tool=tool,
                name="version",
                command=command,
                status="timeout",
                stderr=f"{tool} version command timed out",
            )
        )
        return None
    if completed.returncode != 0:
        commands.append(
            CollectionCommandResult(
                tool=tool,
                name="version",
                command=command,
                status="failed",
                exit_code=completed.returncode,
                stderr=_compact_stderr(completed.stderr),
            )
        )
        return None
    commands.append(
        CollectionCommandResult(
            tool=tool,
            name="version",
            command=command,
            status="ok",
            exit_code=0,
        )
    )
    return _first_line(completed.stdout)


def _run_json_command(
    *,
    tool: str,
    name: str,
    command: list[str],
    output_file: Path,
    timeout_seconds: int,
    command_runner: CommandRunner,
) -> CollectionCommandResult:
    try:
        completed = command_runner(
            command,
            capture_output=True,
            text=True,
            timeout=timeout_seconds,
        )
    except subprocess.TimeoutExpired:
        return CollectionCommandResult(
            tool=tool,
            name=name,
            command=command,
            status="timeout",
            stderr=f"{tool} command timed out after {timeout_seconds}s",
        )
    if completed.returncode != 0:
        return CollectionCommandResult(
            tool=tool,
            name=name,
            command=command,
            status="failed",
            exit_code=completed.returncode,
            stderr=_compact_stderr(completed.stderr),
        )
    try:
        payload = json.loads(completed.stdout or "[]")
    except json.JSONDecodeError as exc:
        return CollectionCommandResult(
            tool=tool,
            name=name,
            command=command,
            status="failed",
            exit_code=completed.returncode,
            stderr=f"invalid JSON output: {exc}",
        )
    output_file.parent.mkdir(parents=True, exist_ok=True)
    output_file.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    return CollectionCommandResult(
        tool=tool,
        name=name,
        command=command,
        status="ok",
        exit_code=0,
        output_file=str(output_file),
    )


def _run_optional_text_command(
    *,
    name: str,
    command: list[str],
    output_file: Path,
    timeout_seconds: int,
    executable_lookup: ExecutableLookup,
    command_runner: CommandRunner,
    redact: bool = False,
    max_stdout_lines: int | None = None,
    max_stdout_chars: int | None = None,
) -> CollectionCommandResult:
    executable = command[0]
    resolved = executable_lookup(executable)
    if resolved is None:
        return CollectionCommandResult(
            tool="system",
            name=name,
            command=command,
            status="missing",
            stderr=f"{executable} was not found on PATH",
        )
    resolved_command = [resolved, *command[1:]]
    try:
        completed = command_runner(
            resolved_command,
            capture_output=True,
            text=True,
            timeout=timeout_seconds,
        )
    except subprocess.TimeoutExpired:
        return CollectionCommandResult(
            tool="system",
            name=name,
            command=resolved_command,
            status="timeout",
            stderr=f"system command timed out after {timeout_seconds}s",
        )
    if completed.returncode != 0:
        return CollectionCommandResult(
            tool="system",
            name=name,
            command=resolved_command,
            status="failed",
            exit_code=completed.returncode,
            stderr=_compact_stderr(completed.stderr),
        )
    output_file.parent.mkdir(parents=True, exist_ok=True)
    stdout = _bounded_stdout(
        completed.stdout,
        redact=redact,
        max_lines=max_stdout_lines,
        max_chars=max_stdout_chars,
    )
    output_file.write_text(
        json.dumps(
            {
                "command": resolved_command,
                "stdout": stdout,
                "stderr": _compact_stderr(completed.stderr),
            },
            indent=2,
        ),
        encoding="utf-8",
    )
    return CollectionCommandResult(
        tool="system",
        name=name,
        command=resolved_command,
        status="ok",
        exit_code=0,
        output_file=str(output_file),
    )


def _collect_lynis(
    *,
    lynis_dir: Path,
    timeout_seconds: int,
    executable_lookup: ExecutableLookup,
    command_runner: CommandRunner,
    manifest: HostCollectionManifest,
) -> None:
    lynis_path = executable_lookup("lynis")
    if lynis_path is None:
        manifest.commands.append(
            CollectionCommandResult(
                tool="lynis",
                name="audit_system",
                status="missing",
                stderr="lynis was not found on PATH",
            )
        )
        return
    lynis_dir.mkdir(parents=True, exist_ok=True)
    command = [lynis_path, "audit", "system", "--no-colors", "--quiet"]
    try:
        completed = command_runner(
            command,
            capture_output=True,
            text=True,
            timeout=timeout_seconds,
        )
    except subprocess.TimeoutExpired:
        manifest.commands.append(
            CollectionCommandResult(
                tool="lynis",
                name="audit_system",
                command=command,
                status="timeout",
                stderr=f"lynis audit timed out after {timeout_seconds}s",
            )
        )
        return
    # lynis exits 0 on success; non-zero may still produce a report
    report_sources = [
        Path("/var/log/lynis-report.dat"),
        Path("/tmp/lynis-report.dat"),  # noqa: S108
    ]
    report_dat = None
    for candidate in report_sources:
        if candidate.is_file():
            report_dat = candidate
            break
    if report_dat is not None:
        import shutil as _shutil

        _shutil.copy2(report_dat, lynis_dir / "report.dat")
    manifest.commands.append(
        CollectionCommandResult(
            tool="lynis",
            name="audit_system",
            command=command,
            status="ok" if report_dat is not None or completed.returncode == 0 else "failed",
            exit_code=completed.returncode,
            output_file=str(lynis_dir / "report.dat") if report_dat else None,
            stderr=_compact_stderr(completed.stderr),
        )
    )


def _collect_openscap(
    *,
    openscap_dir: Path,
    executable_lookup: ExecutableLookup,
    manifest: HostCollectionManifest,
) -> None:
    oscap_path = executable_lookup("oscap")
    if oscap_path is None:
        manifest.commands.append(
            CollectionCommandResult(
                tool="openscap",
                name="xccdf_eval",
                status="missing",
                stderr=(
                    "oscap was not found on PATH. Live OpenSCAP profile execution "
                    "requires distribution-specific SCAP content packages. "
                    "Place pre-existing results.xml in raw/openscap/ for ingestion."
                ),
            )
        )
        return
    # Discovery only — live profile execution requires SCAP content
    openscap_dir.mkdir(parents=True, exist_ok=True)
    manifest.commands.append(
        CollectionCommandResult(
            tool="openscap",
            name="xccdf_eval",
            status="skipped",
            stderr=(
                "oscap found but live profile execution requires SCAP content "
                "packages. Place pre-existing results.xml in raw/openscap/."
            ),
        )
    )


def _write_manifest(path: Path, manifest: HostCollectionManifest) -> None:
    path.write_text(manifest.model_dump_json(indent=2), encoding="utf-8")


def _first_line(value: str) -> str:
    return next((line.strip() for line in value.splitlines() if line.strip()), "")


def _compact_stderr(value: str | None) -> str | None:
    if value is None:
        return None
    rendered = " ".join(value.split())
    return rendered[:500] if rendered else None


def _bounded_stdout(
    value: str | None,
    *,
    redact: bool,
    max_lines: int | None,
    max_chars: int | None,
) -> str:
    rendered = value or ""
    if redact:
        rendered = redact_auth_value(rendered)
    if max_lines is not None:
        lines = rendered.splitlines()
        if len(lines) > max_lines:
            rendered = "\n".join(lines[:max_lines])
            rendered += f"\n[TRUNCATED after {max_lines} lines]"
    if max_chars is not None and len(rendered) > max_chars:
        rendered = rendered[:max_chars] + f"\n[TRUNCATED after {max_chars} characters]"
    return rendered
