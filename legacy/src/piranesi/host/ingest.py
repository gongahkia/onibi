from __future__ import annotations

import json
import re as _re
import xml.etree.ElementTree as ET
from collections.abc import Callable
from ipaddress import ip_address
from pathlib import Path
from typing import Any, Literal, cast

from pydantic import ValidationError

from piranesi.host.models import (
    AuthEventSummary,
    BaselineCheck,
    EvidenceItem,
    HostIdentity,
    HostPackage,
    HostProcess,
    HostSnapshot,
    ListeningPort,
    LoginSession,
    NetworkInterface,
    OsRelease,
    PackageManager,
    ServiceState,
    Severity,
    UserAccount,
)

_DEFAULT_LISTEN_ADDRESS = "0.0.0.0"  # noqa: S104
BaselineResult = Literal["pass", "fail", "warn", "not_applicable", "unknown"]


class HostInputError(RuntimeError):
    """Raised when a host snapshot or raw tool bundle cannot be loaded."""


def load_host_input(path: str | Path) -> HostSnapshot:
    input_path = Path(path).expanduser().resolve(strict=False)
    if input_path.is_file():
        return _load_snapshot_file(
            input_path,
            manifest_path=input_path.parent / "collection-manifest.json",
        )
    if input_path.is_dir():
        snapshot_file = input_path / "host_snapshot.json"
        if snapshot_file.is_file():
            return _load_snapshot_file(
                snapshot_file,
                manifest_path=input_path / "collection-manifest.json",
            )
        return _load_tool_bundle(input_path)
    raise HostInputError(f"host input does not exist: {input_path}")


def _load_snapshot_file(path: Path, *, manifest_path: Path | None = None) -> HostSnapshot:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
        snapshot = HostSnapshot.model_validate(payload)
    except (OSError, json.JSONDecodeError, ValidationError) as exc:
        raise HostInputError(f"invalid host snapshot {path}: {exc}") from exc
    provenance = dict(snapshot.tool_provenance)
    provenance.setdefault("piranesi_snapshot", str(path))
    snapshot = snapshot.model_copy(update={"tool_provenance": provenance})
    if manifest_path is not None:
        snapshot = _with_collection_manifest(snapshot, manifest_path)
    return snapshot


def _load_tool_bundle(root: Path) -> HostSnapshot:
    osquery_dir = _tool_dir(root, "osquery")
    trivy_dir = _tool_dir(root, "trivy")
    commands_dir = _tool_dir(root, "commands")
    lynis_dir = _tool_dir(root, "lynis")
    openscap_dir = _tool_dir(root, "openscap")
    osquery_payloads = _load_json_files(osquery_dir) if osquery_dir is not None else {}
    trivy_payloads = _load_json_files(trivy_dir) if trivy_dir is not None else {}
    command_payloads = _load_json_files(commands_dir) if commands_dir is not None else {}
    has_baseline = lynis_dir is not None or openscap_dir is not None
    if not osquery_payloads and not trivy_payloads and not command_payloads and not has_baseline:
        raise HostInputError(
            f"raw host bundle at {root} must contain host_snapshot.json, "
            "osquery/*.json, trivy/*.json, commands/*.json, lynis/report.dat, "
            "openscap/results.xml, raw/osquery/*.json, raw/trivy/*.json, "
            "or raw/commands/*.json"
        )

    network_interfaces = _network_interfaces_from_osquery(osquery_payloads)
    identity = _identity_from_osquery(
        osquery_payloads,
        network_interfaces=network_interfaces,
    ) or HostIdentity(hostname=root.name, ip_addresses=_ip_addresses(network_interfaces))
    os_release = _os_from_osquery(osquery_payloads)
    kernel = _kernel_from_osquery(osquery_payloads)
    packages = _packages_from_osquery(osquery_payloads)
    processes = _processes_from_osquery(osquery_payloads)
    listening_ports = _listening_ports_from_osquery(osquery_payloads, processes=processes)
    users = _users_from_osquery(osquery_payloads)
    services = _services_from_osquery(osquery_payloads)
    config = _config_from_evidence(
        osquery_payloads,
        command_payloads,
        os_release=os_release,
        packages=packages,
    )

    baseline_checks: list[BaselineCheck] = []
    if lynis_dir is not None:
        baseline_checks.extend(_parse_lynis_report(lynis_dir))
    if openscap_dir is not None:
        baseline_checks.extend(_parse_openscap_results(openscap_dir))

    login_sessions, auth_events = _auth_evidence_from_commands(command_payloads)

    raw_evidence: dict[str, object] = {}
    if osquery_payloads:
        raw_evidence["osquery"] = osquery_payloads
    if trivy_payloads:
        raw_evidence["trivy"] = trivy_payloads
    if command_payloads:
        raw_evidence["commands"] = command_payloads
    if lynis_dir is not None:
        raw_evidence["lynis"] = {"source_dir": str(lynis_dir)}
    if openscap_dir is not None:
        raw_evidence["openscap"] = {"source_dir": str(openscap_dir)}

    provenance: dict[str, str] = {
        "bundle": str(root),
        "osquery": str(osquery_dir) if osquery_payloads and osquery_dir else "",
        "trivy": str(trivy_dir) if trivy_payloads and trivy_dir else "",
        "commands": str(commands_dir) if command_payloads and commands_dir else "",
    }
    if lynis_dir is not None:
        provenance["lynis"] = str(lynis_dir)
    if openscap_dir is not None:
        provenance["openscap"] = str(openscap_dir)

    snapshot = HostSnapshot(
        identity=identity,
        os=os_release,
        kernel=kernel,
        packages=packages,
        network_interfaces=network_interfaces,
        listening_ports=listening_ports,
        processes=processes,
        services=services,
        users=users,
        baseline_checks=baseline_checks,
        login_sessions=login_sessions,
        auth_event_summaries=auth_events,
        config=config,
        tool_provenance=provenance,
        raw_evidence=raw_evidence,
    )
    return _with_collection_manifest(snapshot, root / "collection-manifest.json")


def _with_collection_manifest(snapshot: HostSnapshot, manifest_path: Path) -> HostSnapshot:
    if not manifest_path.is_file():
        return snapshot
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise HostInputError(f"invalid collection manifest {manifest_path}: {exc}") from exc
    if not isinstance(manifest, dict):
        raise HostInputError(f"invalid collection manifest {manifest_path}: expected JSON object")
    raw_evidence = dict(snapshot.raw_evidence)
    raw_evidence.setdefault("collection_manifest", manifest)
    provenance = dict(snapshot.tool_provenance)
    provenance.setdefault("collection_manifest", str(manifest_path))
    return snapshot.model_copy(
        update={
            "raw_evidence": raw_evidence,
            "tool_provenance": provenance,
        }
    )


def _tool_dir(root: Path, name: str) -> Path | None:
    direct = root / name
    if direct.is_dir():
        return direct
    collected = root / "raw" / name
    if collected.is_dir():
        return collected
    return None


def _load_json_files(path: Path) -> dict[str, object]:
    if not path.is_dir():
        return {}
    payloads: dict[str, object] = {}
    for candidate in sorted(path.glob("*.json")):
        try:
            payloads[candidate.stem] = json.loads(candidate.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            raise HostInputError(f"invalid JSON tool output {candidate}: {exc}") from exc
    return payloads


def _rows(payload: object) -> list[dict[str, Any]]:
    if isinstance(payload, list):
        return [item for item in payload if isinstance(item, dict)]
    if isinstance(payload, dict):
        for key in ("rows", "data", "results"):
            value = payload.get(key)
            if isinstance(value, list):
                return [item for item in value if isinstance(item, dict)]
        if all(
            isinstance(value, (str, int, float, bool, type(None))) for value in payload.values()
        ):
            return [payload]
    return []


def _all_rows(payloads: dict[str, object], *names: str) -> list[dict[str, Any]]:
    rows: list[dict[str, Any]] = []
    for name in names:
        if name in payloads:
            rows.extend(_rows(payloads[name]))
    return rows


def _identity_from_osquery(
    payloads: dict[str, object],
    *,
    network_interfaces: list[NetworkInterface],
) -> HostIdentity | None:
    info = _first(_all_rows(payloads, "system_info", "hostname"))
    if info is None:
        return None
    hostname = _string(info.get("hostname")) or _string(info.get("computer_name"))
    if not hostname:
        return None
    return HostIdentity(
        hostname=hostname,
        host_id=_string(info.get("uuid")) or _string(info.get("hardware_serial")),
        ip_addresses=_ip_addresses(network_interfaces),
    )


def _os_from_osquery(payloads: dict[str, object]) -> OsRelease:
    row = _first(_all_rows(payloads, "os_version", "os_release"))
    if row is None:
        return OsRelease()
    return OsRelease(
        name=_string(row.get("name")) or _string(row.get("id")) or "unknown",
        version=_string(row.get("version")),
        id=_string(row.get("id")),
        version_id=_string(row.get("version_id")),
        pretty_name=_string(row.get("pretty_name")),
    )


def _kernel_from_osquery(payloads: dict[str, object]) -> str | None:
    row = _first(_all_rows(payloads, "kernel_info"))
    if row is None:
        return None
    return _string(row.get("version")) or _string(row.get("release"))


def _rpm_version(row: dict[str, Any]) -> str | None:
    version = _string(row.get("version"))
    if not version:
        return None
    release = _string(row.get("release"))
    epoch = _string(row.get("epoch"))
    rendered = f"{version}-{release}" if release else version
    return f"{epoch}:{rendered}" if epoch and epoch != "0" else rendered


def _package_manager_from_row(row: dict[str, Any]) -> PackageManager:
    raw = (_string(row.get("package_manager")) or _string(row.get("manager")) or "").lower()
    if raw in {"deb", "rpm", "apk", "brew", "winget"}:
        return cast(PackageManager, raw)
    source = (_string(row.get("source")) or "").lower()
    if source in {"deb", "rpm", "apk", "brew", "winget"}:
        return cast(PackageManager, source)
    return "unknown"


def _packages_from_osquery(payloads: dict[str, object]) -> list[HostPackage]:
    packages: list[HostPackage] = []
    for row in _all_rows(payloads, "deb_packages"):
        name = _string(row.get("name"))
        version = _string(row.get("version"))
        if not name or not version:
            continue
        packages.append(
            HostPackage(
                name=name,
                version=version,
                source="osquery",
                architecture=_string(row.get("arch")) or _string(row.get("architecture")),
                package_manager="deb",
            )
        )
    for row in _all_rows(payloads, "rpm_packages"):
        name = _string(row.get("name"))
        version = _rpm_version(row)
        if not name or not version:
            continue
        packages.append(
            HostPackage(
                name=name,
                version=version,
                source="osquery",
                architecture=_string(row.get("arch")) or _string(row.get("architecture")),
                package_manager="rpm",
            )
        )
    for row in _all_rows(payloads, "apk_packages"):
        name = _string(row.get("name"))
        version = _string(row.get("version"))
        if not name or not version:
            continue
        packages.append(
            HostPackage(
                name=name,
                version=version,
                source="osquery",
                architecture=_string(row.get("arch")) or _string(row.get("architecture")),
                package_manager="apk",
            )
        )
    for row in _all_rows(payloads, "packages"):
        name = _string(row.get("name"))
        version = _string(row.get("version"))
        if not name or not version:
            continue
        packages.append(
            HostPackage(
                name=name,
                version=version,
                source="osquery",
                architecture=_string(row.get("arch")) or _string(row.get("architecture")),
                package_manager=_package_manager_from_row(row),
            )
        )
    return _dedupe_models(
        packages,
        key=lambda item: (item.name, item.version, item.package_manager),
    )


def _network_interfaces_from_osquery(payloads: dict[str, object]) -> list[NetworkInterface]:
    interfaces: list[NetworkInterface] = []
    for row in _all_rows(payloads, "interface_addresses", "network_interfaces"):
        address = _string(row.get("address")) or _string(row.get("ip_address"))
        name = _string(row.get("interface")) or _string(row.get("name"))
        if not address or not name or not _is_ip_address(address):
            continue
        interfaces.append(
            NetworkInterface(
                name=name,
                address=address,
                family=_string(row.get("type")) or _ip_family(address),
                mask=_string(row.get("mask")) or _string(row.get("netmask")),
            )
        )
    return _dedupe_models(interfaces, key=lambda item: (item.name, item.address))


def _listening_ports_from_osquery(
    payloads: dict[str, object],
    *,
    processes: list[HostProcess],
) -> list[ListeningPort]:
    ports: list[ListeningPort] = []
    processes_by_pid = {process.pid: process for process in processes}
    for row in _all_rows(payloads, "listening_ports", "process_open_sockets"):
        port = _int(row.get("port") or row.get("local_port"))
        if port is None:
            continue
        pid = _int(row.get("pid"))
        process = processes_by_pid.get(pid) if pid is not None else None
        ports.append(
            ListeningPort(
                protocol=(_string(row.get("protocol")) or "tcp").lower(),
                address=(
                    _string(row.get("address"))
                    or _string(row.get("local_address"))
                    or _DEFAULT_LISTEN_ADDRESS
                ),
                port=port,
                process=(
                    _string(row.get("process_name"))
                    or _string(row.get("name"))
                    or (process.name if process is not None else None)
                ),
                pid=pid,
            )
        )
    return _dedupe_models(
        ports,
        key=lambda item: (item.protocol, item.address, item.port, item.pid),
    )


def _processes_from_osquery(payloads: dict[str, object]) -> list[HostProcess]:
    processes: list[HostProcess] = []
    for row in _all_rows(payloads, "processes"):
        pid = _int(row.get("pid"))
        name = _string(row.get("name"))
        if pid is None or not name:
            continue
        processes.append(
            HostProcess(
                pid=pid,
                name=name,
                path=_string(row.get("path")),
                cmdline=_string(row.get("cmdline")),
                user=_string(row.get("user")) or _string(row.get("username")),
            )
        )
    return _dedupe_models(processes, key=lambda item: item.pid)


def _users_from_osquery(payloads: dict[str, object]) -> list[UserAccount]:
    users: list[UserAccount] = []
    for row in _all_rows(payloads, "users"):
        username = _string(row.get("username")) or _string(row.get("user"))
        if not username:
            continue
        groups = row.get("groups")
        rendered_groups = (
            [str(item) for item in groups if item is not None]
            if isinstance(groups, list)
            else [item.strip() for item in str(groups or "").split(",") if item.strip()]
        )
        users.append(
            UserAccount(
                username=username,
                uid=_int(row.get("uid")),
                gid=_int(row.get("gid")),
                shell=_string(row.get("shell")),
                groups=rendered_groups,
                last_login=_string(row.get("last_login")),
            )
        )
    return _dedupe_models(users, key=lambda item: item.username)


def _services_from_osquery(payloads: dict[str, object]) -> list[ServiceState]:
    services: list[ServiceState] = []
    for row in _all_rows(payloads, "systemd_units", "services"):
        name = _string(row.get("name")) or _string(row.get("unit"))
        if not name:
            continue
        active = (_string(row.get("active_state")) or _string(row.get("status")) or "").lower()
        enabled = (_string(row.get("enabled")) or _string(row.get("unit_file_state")) or "").lower()
        services.append(
            ServiceState(
                name=name,
                running=True if active in {"active", "running"} else False if active else None,
                enabled=True if enabled in {"enabled", "static"} else False if enabled else None,
                source="osquery",
            )
        )
    return _dedupe_models(services, key=lambda item: item.name)


def _config_from_evidence(
    payloads: dict[str, object],
    command_payloads: dict[str, object],
    *,
    os_release: OsRelease,
    packages: list[HostPackage],
) -> dict[str, object]:
    config: dict[str, object] = {}
    platform_config = _platform_config(os_release, packages)
    config["platform"] = platform_config
    ssh_rows = _all_rows(payloads, "ssh_config", "sshd_config")
    ssh_config: dict[str, str] = {}
    for row in ssh_rows:
        key = _string(row.get("key")) or _string(row.get("option"))
        value = _string(row.get("value"))
        if key and value is not None:
            ssh_config[key] = value
    ssh_config.update(_parse_sshd_effective_config(command_payloads))
    if ssh_config:
        config["ssh"] = ssh_config
    firewall = _firewall_config_from_commands(command_payloads)
    if firewall:
        config["firewall"] = firewall
    updates = _updates_from_commands(command_payloads)
    if updates:
        config["updates"] = updates
    selinux = _selinux_from_commands(command_payloads)
    if selinux:
        config["selinux"] = selinux
    sysctl = _sysctl_from_commands(command_payloads)
    if sysctl:
        config["sysctl"] = sysctl
    sudo = _sudo_config_from_evidence(payloads, command_payloads)
    if sudo:
        config["sudo"] = sudo
    return config


def _platform_config(
    os_release: OsRelease,
    packages: list[HostPackage],
) -> dict[str, object]:
    family = _platform_family(os_release)
    package_manager = _platform_package_manager(family, packages)
    supported = ["packages", "listeners", "users", "services"]
    unsupported: list[str] = []
    if family in {"debian"}:
        supported.extend(["apt_updates", "ufw", "sysctl"])
    elif family in {"rhel", "amazon"}:
        supported.extend(["rpm_updates", "firewalld", "selinux", "sysctl"])
        unsupported.extend(["apt_updates", "unattended_upgrades"])
    elif family == "alpine":
        supported.extend(["apk_updates"])
        unsupported.extend(["apt_updates", "unattended_upgrades", "systemd_services"])
    elif family == "macos":
        supported.extend(["listeners", "users"])
        unsupported.extend(["linux_sysctl", "linux_firewall", "linux_package_updates"])
    else:
        unsupported.extend(["platform_specific_updates"])
    return {
        "platform_family": family,
        "package_manager": package_manager,
        "supported_checks": _dedupe_values(supported),
        "unsupported_checks": _dedupe_values(unsupported),
        "confidence": "high" if os_release.id or os_release.name != "unknown" else "low",
    }


def _platform_family(os_release: OsRelease) -> str:
    raw_id = (os_release.id or "").lower()
    name = (os_release.name or "").lower()
    pretty = (os_release.pretty_name or "").lower()
    material = " ".join([raw_id, name, pretty])
    if raw_id in {"debian", "ubuntu"} or "debian" in material or "ubuntu" in material:
        return "debian"
    if raw_id in {"rhel", "centos", "rocky", "almalinux", "fedora"} or any(
        token in material for token in ("red hat", "centos", "rocky", "alma", "fedora")
    ):
        return "rhel"
    if raw_id in {"amzn", "amazon"} or "amazon linux" in material:
        return "amazon"
    if raw_id == "alpine" or "alpine" in material:
        return "alpine"
    if raw_id == "darwin" or "macos" in material or "mac os" in material:
        return "macos"
    return "unknown"


def _platform_package_manager(family: str, packages: list[HostPackage]) -> str:
    managers = {package.package_manager for package in packages if package.package_manager}
    for preferred in ("deb", "rpm", "apk", "brew", "winget"):
        if preferred in managers:
            return preferred
    return {
        "debian": "deb",
        "rhel": "rpm",
        "amazon": "rpm",
        "alpine": "apk",
        "macos": "brew",
    }.get(family, "unknown")


def _parse_sshd_effective_config(command_payloads: dict[str, object]) -> dict[str, str]:
    stdout = _command_stdout(command_payloads.get("sshd_effective_config"))
    config: dict[str, str] = {}
    if not stdout:
        return config
    names = {
        "permitrootlogin": "PermitRootLogin",
        "passwordauthentication": "PasswordAuthentication",
        "permitemptypasswords": "PermitEmptyPasswords",
        "kbdinteractiveauthentication": "KbdInteractiveAuthentication",
        "challengeresponseauthentication": "ChallengeResponseAuthentication",
    }
    for line in stdout.splitlines():
        parts = line.strip().split(None, 1)
        if len(parts) != 2:
            continue
        key = names.get(parts[0].lower())
        if key:
            config.setdefault(key, parts[1].strip())
    return config


def _firewall_config_from_commands(command_payloads: dict[str, object]) -> dict[str, object]:
    ufw_stdout = _command_stdout(command_payloads.get("ufw_status"))
    iptables_stdout = _command_stdout(command_payloads.get("iptables_rules"))
    nft_stdout = _command_stdout(command_payloads.get("nft_ruleset"))
    firewalld_stdout = _command_stdout(command_payloads.get("firewalld_state"))
    config: dict[str, object] = {}
    sources: list[str] = []
    if ufw_stdout is not None:
        sources.append("ufw_status")
        status = _parse_ufw_status(ufw_stdout)
        config["ufw_status"] = status or "unknown"
        if status == "active":
            config["active"] = True
        elif status == "inactive":
            config["active"] = False
    if iptables_stdout is not None:
        sources.append("iptables_rules")
        rules = [line for line in iptables_stdout.splitlines() if line.strip().startswith("-A ")]
        config["iptables_rule_count"] = len(rules)
        if rules and "active" not in config:
            config["active"] = True
    if nft_stdout is not None:
        sources.append("nft_ruleset")
        rules = [line for line in nft_stdout.splitlines() if line.strip()]
        config["nft_rule_count"] = len(rules)
        if rules and "active" not in config:
            config["active"] = True
    if firewalld_stdout is not None:
        sources.append("firewalld_state")
        firewalld_lines = [line.strip() for line in firewalld_stdout.splitlines() if line.strip()]
        state = firewalld_lines[0].lower() if firewalld_lines else "unknown"
        config["firewalld_state"] = state or "unknown"
        if state == "running":
            config["active"] = True
        elif state in {"not running", "stopped", "inactive"}:
            config["active"] = False
    if sources:
        config["sources"] = sources
    return config


def _parse_ufw_status(stdout: str) -> str | None:
    for line in stdout.splitlines():
        normalized = line.strip().lower()
        if normalized.startswith("status:"):
            status = normalized.split(":", 1)[1].strip()
            if status.startswith("active"):
                return "active"
            if status.startswith("inactive"):
                return "inactive"
            return status or None
    return None


def _updates_from_commands(command_payloads: dict[str, object]) -> dict[str, object]:
    stdout = _command_stdout(command_payloads.get("apt_upgradable"))
    if stdout is not None:
        updates = _parse_apt_updates(stdout)
        return {
            "source": "apt_upgradable",
            "package_manager": "deb",
            "upgradable": updates,
            "security_count": sum(1 for update in updates if update["security"]),
        }
    dnf_stdout = _command_stdout(command_payloads.get("dnf_security_updates"))
    if dnf_stdout is not None:
        updates = _parse_rpm_security_updates(dnf_stdout)
        return {
            "source": "dnf_security_updates",
            "package_manager": "rpm",
            "upgradable": updates,
            "security_count": len(updates),
        }
    yum_stdout = _command_stdout(command_payloads.get("yum_security_updates"))
    if yum_stdout is not None:
        updates = _parse_rpm_security_updates(yum_stdout)
        return {
            "source": "yum_security_updates",
            "package_manager": "rpm",
            "upgradable": updates,
            "security_count": len(updates),
        }
    apk_stdout = _command_stdout(command_payloads.get("apk_version_outdated"))
    if apk_stdout is not None:
        updates = _parse_apk_updates(apk_stdout)
        return {
            "source": "apk_version_outdated",
            "package_manager": "apk",
            "upgradable": updates,
            "security_count": 0,
        }
    return {}


def _parse_apt_updates(stdout: str) -> list[dict[str, object]]:
    updates: list[dict[str, object]] = []
    for line in stdout.splitlines():
        if not line or line.startswith("Listing...") or "/" not in line:
            continue
        package, rest = line.split("/", 1)
        fields = rest.split()
        candidate = fields[0] if fields else "unknown"
        installed = _installed_version_from_apt_line(rest)
        is_security = "-security" in rest or "security" in rest.lower()
        updates.append(
            {
                "package": package,
                "candidate": candidate,
                "installed": installed,
                "security": is_security,
            }
        )
    return updates


def _parse_rpm_security_updates(stdout: str) -> list[dict[str, object]]:
    updates: list[dict[str, object]] = []
    for line in stdout.splitlines():
        normalized = line.strip()
        if not normalized or normalized.lower().startswith(("last metadata", "updates info")):
            continue
        fields = normalized.split()
        package_field = next(
            (
                field
                for field in reversed(fields)
                if "." in field and not field.upper().startswith(("RHSA", "ALAS"))
            ),
            fields[-1] if fields else "",
        )
        if not package_field:
            continue
        name = package_field.rsplit(".", 1)[0]
        advisory = next(
            (field for field in fields if field.upper().startswith(("RHSA", "ALAS", "FEDORA"))),
            None,
        )
        severity = next(
            (
                field.rstrip("/Sec.").lower()
                for field in fields
                if field.lower().rstrip("/sec.") in {"critical", "important", "moderate", "low"}
            ),
            None,
        )
        updates.append(
            {
                "package": name,
                "candidate": "unknown",
                "installed": None,
                "security": True,
                "advisory": advisory,
                "severity": severity,
            }
        )
    return updates


def _parse_apk_updates(stdout: str) -> list[dict[str, object]]:
    updates: list[dict[str, object]] = []
    for line in stdout.splitlines():
        normalized = line.strip()
        if not normalized:
            continue
        parts = normalized.split()
        package = parts[0]
        if "-" in package:
            name, installed = package.rsplit("-", 1)
        else:
            name, installed = package, None
        candidate = parts[-1] if parts else "unknown"
        updates.append(
            {
                "package": name,
                "candidate": candidate,
                "installed": installed,
                "security": False,
            }
        )
    return updates


def _selinux_from_commands(command_payloads: dict[str, object]) -> dict[str, object]:
    stdout = _command_stdout(command_payloads.get("selinux_getenforce"))
    if stdout is None:
        return {}
    state = stdout.strip().splitlines()[0].strip() if stdout.strip() else "unknown"
    return {
        "state": state,
        "enabled": state.lower() not in {"disabled", "unknown"},
        "enforcing": state.lower() == "enforcing",
        "source": "getenforce",
    }


def _sysctl_from_commands(command_payloads: dict[str, object]) -> dict[str, object]:
    command_names = {
        "sysctl_net_ipv4_ip_forward": "net.ipv4.ip_forward",
        "sysctl_net_ipv6_conf_all_forwarding": "net.ipv6.conf.all.forwarding",
        "sysctl_kernel_unprivileged_bpf_disabled": "kernel.unprivileged_bpf_disabled",
        "sysctl_kernel_kptr_restrict": "kernel.kptr_restrict",
    }
    values: dict[str, str] = {}
    sources: list[str] = []
    for command_name, setting in command_names.items():
        stdout = _command_stdout(command_payloads.get(command_name))
        if stdout is None:
            continue
        value = stdout.strip().splitlines()[0].strip() if stdout.strip() else ""
        values[setting] = value
        sources.append(command_name)
    if not values:
        return {}
    return {
        "values": values,
        "sources": sources,
    }


def _installed_version_from_apt_line(rest: str) -> str | None:
    marker = "upgradable from: "
    if marker not in rest:
        return None
    return rest.split(marker, 1)[1].strip("] ")


def _sudo_config_from_evidence(
    payloads: dict[str, object],
    command_payloads: dict[str, object],
) -> dict[str, object]:
    admin_groups: list[dict[str, object]] = []
    for group in ("sudo", "admin", "wheel"):
        stdout = _command_stdout(command_payloads.get(f"group_{group}"))
        if not stdout:
            continue
        parts = stdout.strip().split(":")
        members = parts[3].split(",") if len(parts) >= 4 and parts[3] else []
        admin_groups.append({"group": group, "members": [item for item in members if item]})
    sudoers_rows = _all_rows(payloads, "sudoers")
    sudoers = [
        {
            "path": _string(row.get("path")),
            "key": _string(row.get("key")),
            "value": _string(row.get("value")),
        }
        for row in sudoers_rows
        if _string(row.get("value"))
    ]
    config: dict[str, object] = {}
    if admin_groups:
        config["admin_groups"] = admin_groups
    if sudoers:
        config["sudoers_entries"] = sudoers
    return config


def _first(rows: list[dict[str, Any]]) -> dict[str, Any] | None:
    return rows[0] if rows else None


def _string(value: object) -> str | None:
    if value is None:
        return None
    rendered = str(value).strip()
    return rendered or None


def _int(value: object) -> int | None:
    if value is None or value == "":
        return None
    try:
        return int(str(value))
    except ValueError:
        return None


def _command_stdout(payload: object) -> str | None:
    if isinstance(payload, dict):
        stdout = payload.get("stdout")
        return str(stdout) if stdout is not None else None
    if isinstance(payload, str):
        return payload
    return None


def _ip_addresses(interfaces: list[NetworkInterface]) -> list[str]:
    addresses: list[str] = []
    for interface in interfaces:
        if _is_loopback_or_link_local(interface.address):
            continue
        addresses.append(interface.address)
    return _dedupe_values(addresses)


def _is_ip_address(value: str) -> bool:
    try:
        ip_address(value)
    except ValueError:
        return False
    return True


def _ip_family(value: str) -> str | None:
    try:
        parsed = ip_address(value)
    except ValueError:
        return None
    return f"ipv{parsed.version}"


def _is_loopback_or_link_local(value: str) -> bool:
    try:
        parsed = ip_address(value)
    except ValueError:
        return True
    return parsed.is_loopback or parsed.is_link_local


def _dedupe_values(items: list[str]) -> list[str]:
    seen: set[str] = set()
    deduped: list[str] = []
    for item in items:
        if item in seen:
            continue
        seen.add(item)
        deduped.append(item)
    return deduped


# ---------------------------------------------------------------------------
# Lynis report.dat parser
# ---------------------------------------------------------------------------


def _parse_lynis_report(lynis_dir: Path) -> list[BaselineCheck]:
    report_dat = lynis_dir / "report.dat"
    if not report_dat.is_file():
        return []
    try:
        text = report_dat.read_text(encoding="utf-8", errors="replace")
    except OSError:
        return []
    checks: list[BaselineCheck] = []
    hardening_index: str | None = None
    for line in text.splitlines():
        stripped = line.strip()
        if stripped.startswith("hardening_index="):
            hardening_index = stripped.split("=", 1)[1].strip()
            continue
        if stripped.startswith("warning[]="):
            parts = stripped.split("=", 1)[1].split("|")
            check_id = parts[0].strip() if parts else ""
            title = parts[1].strip() if len(parts) > 1 else check_id
            affected = parts[2].strip() if len(parts) > 2 else ""
            if not check_id:
                continue
            checks.append(
                BaselineCheck(
                    source="lynis",
                    check_id=check_id,
                    title=title or check_id,
                    result="warn",
                    severity="medium",
                    evidence=[
                        EvidenceItem(source="lynis", key="warning", value=f"{check_id}: {title}"),
                        *(
                            [EvidenceItem(source="lynis", key="affected", value=affected)]
                            if affected and affected != "-"
                            else []
                        ),
                    ],
                )
            )
        elif stripped.startswith("suggestion[]="):
            parts = stripped.split("=", 1)[1].split("|")
            check_id = parts[0].strip() if parts else ""
            title = parts[1].strip() if len(parts) > 1 else check_id
            detail = parts[2].strip() if len(parts) > 2 else ""
            if not check_id:
                continue
            remediation = detail if detail and detail != "-" else None
            checks.append(
                BaselineCheck(
                    source="lynis",
                    check_id=check_id,
                    title=title or check_id,
                    result="fail",
                    severity="low",
                    evidence=[
                        EvidenceItem(
                            source="lynis", key="suggestion", value=f"{check_id}: {title}"
                        ),
                    ],
                    remediation=remediation,
                )
            )
    if hardening_index is not None:
        hardening_score = _int(hardening_index)
        checks.insert(
            0,
            BaselineCheck(
                source="lynis",
                check_id="LYNIS-HARDENING-INDEX",
                title=f"Lynis hardening index: {hardening_index}",
                result=(
                    "unknown"
                    if hardening_score is None
                    else "pass"
                    if hardening_score >= 80
                    else "warn"
                ),
                severity="informational",
                evidence=[
                    EvidenceItem(source="lynis", key="hardening_index", value=hardening_index),
                ],
            ),
        )
    return checks


# ---------------------------------------------------------------------------
# OpenSCAP XCCDF XML parser
# ---------------------------------------------------------------------------

_XCCDF_NS = "http://checklists.nist.gov/xccdf/1.2"


def _parse_openscap_results(openscap_dir: Path) -> list[BaselineCheck]:
    results_xml = openscap_dir / "results.xml"
    if not results_xml.is_file():
        return []
    try:
        tree = ET.parse(results_xml)  # noqa: S314
    except (OSError, ET.ParseError):
        return []
    root = tree.getroot()
    rules_by_id: dict[str, ET.Element] = {}
    for rule_el in root.iter(f"{{{_XCCDF_NS}}}Rule"):
        rid = rule_el.get("id", "")
        if rid:
            rules_by_id[rid] = rule_el
    checks: list[BaselineCheck] = []
    for rr in root.iter(f"{{{_XCCDF_NS}}}rule-result"):
        idref = rr.get("idref", "")
        result_el = rr.find(f"{{{_XCCDF_NS}}}result")
        raw_result = (result_el.text or "").strip().lower() if result_el is not None else "unknown"
        result = _openscap_result(raw_result)
        rule_for_result = rules_by_id.get(idref)
        severity_raw = (
            rule_for_result.get("severity", "") if rule_for_result is not None else ""
        ).lower()
        severity = _openscap_severity(severity_raw)
        title_el = (
            rule_for_result.find(f"{{{_XCCDF_NS}}}title") if rule_for_result is not None else None
        )
        title = (title_el.text or idref).strip() if title_el is not None else idref
        desc_el = (
            rule_for_result.find(f"{{{_XCCDF_NS}}}description")
            if rule_for_result is not None
            else None
        )
        description = (desc_el.text or "").strip() if desc_el is not None else ""
        fix_el = (
            rule_for_result.find(f"{{{_XCCDF_NS}}}fixtext") if rule_for_result is not None else None
        )
        remediation = (fix_el.text or "").strip() if fix_el is not None else None
        control_refs: list[str] = []
        if rule_for_result is not None:
            for ident_el in rule_for_result.findall(f"{{{_XCCDF_NS}}}ident"):
                ident_text = (ident_el.text or "").strip()
                if ident_text:
                    control_refs.append(ident_text)
        evidence = [
            EvidenceItem(source="openscap", key="rule_result", value=f"{idref}: {raw_result}")
        ]
        if description:
            evidence.append(EvidenceItem(source="openscap", key="description", value=description))
        checks.append(
            BaselineCheck(
                source="openscap",
                check_id=idref,
                title=title,
                result=result,
                severity=severity,
                control_refs=control_refs,
                evidence=evidence,
                remediation=remediation or None,
            )
        )
    return checks


def _openscap_result(raw: str) -> BaselineResult:
    mapping: dict[str, BaselineResult] = {
        "pass": "pass",
        "fail": "fail",
        "error": "fail",
        "unknown": "unknown",
        "notapplicable": "not_applicable",
        "notchecked": "unknown",
        "notselected": "not_applicable",
        "informational": "pass",
        "fixed": "pass",
    }
    return mapping.get(raw, "unknown")


def _openscap_severity(raw: str) -> Severity | None:
    mapping: dict[str, Severity | None] = {
        "high": "high",
        "medium": "medium",
        "low": "low",
        "unknown": None,
        "": None,
    }
    return mapping.get(raw)


def _dedupe_models[T](items: list[T], *, key: Callable[[T], object]) -> list[T]:
    seen: set[object] = set()
    deduped: list[T] = []
    for item in items:
        marker = key(item)
        if marker in seen:
            continue
        seen.add(marker)
        deduped.append(item)
    return deduped


# ---------------------------------------------------------------------------
# Redaction helper
# ---------------------------------------------------------------------------

_SECRET_PATTERNS: list[_re.Pattern[str]] = [
    _re.compile(r"(?i)(password|passwd|secret|token|key|api[_-]?key|bearer)\s*[=:]\s*\S+"),
    _re.compile(r"(?i)(AWS_SECRET|PRIVATE_KEY|ssh-rsa|ssh-ed25519)\s+\S+"),
    _re.compile(r"-----BEGIN [A-Z ]+-----"),
]


def redact_auth_value(text: str) -> str:
    """Redact likely secrets and sensitive arguments from auth evidence text."""
    result = text
    for pattern in _SECRET_PATTERNS:
        result = pattern.sub("[REDACTED]", result)
    return result


# ---------------------------------------------------------------------------
# Auth evidence parsing
# ---------------------------------------------------------------------------


def _auth_evidence_from_commands(
    command_payloads: dict[str, object],
) -> tuple[list[LoginSession], list[AuthEventSummary]]:
    sessions: list[LoginSession] = []
    events: list[AuthEventSummary] = []

    who_data = command_payloads.get("who_sessions")
    if who_data is not None:
        sessions.extend(_parse_who_sessions(who_data))

    last_data = command_payloads.get("last_logins")
    if last_data is not None:
        events.extend(_parse_last_logins(last_data))

    lastb_data = command_payloads.get("lastb_failures")
    if lastb_data is not None:
        events.extend(_parse_lastb_failures(lastb_data))

    journal_data = command_payloads.get("journalctl_sshd_auth_summary")
    if journal_data is not None:
        events.extend(_parse_journalctl_ssh(journal_data))

    return sessions, events


def _parse_who_sessions(payload: object) -> list[LoginSession]:
    text = _command_stdout(payload)
    if not text or not text.strip():
        return []
    sessions: list[LoginSession] = []
    for line in text.strip().splitlines():
        parts = line.split()
        if len(parts) < 2:
            continue
        username = parts[0]
        tty = parts[1] if len(parts) > 1 else None
        # who format: user tty date time (source)
        started_at = None
        if len(parts) >= 4:
            started_at = f"{parts[2]} {parts[3]}"
        source = None
        if len(parts) >= 5 and parts[-1].startswith("(") and parts[-1].endswith(")"):
            source = parts[-1][1:-1]
        sessions.append(
            LoginSession(
                username=username,
                tty=tty,
                started_at=started_at,
                source=source,
            )
        )
    return sessions


def _parse_last_logins(payload: object) -> list[AuthEventSummary]:
    text = _command_stdout(payload)
    if not text or not text.strip():
        return []
    events: list[AuthEventSummary] = []
    for line in text.strip().splitlines():
        parts = line.split()
        if len(parts) < 2 or parts[0] in {"wtmp", "btmp", "reboot", ""}:
            continue
        username = parts[0]
        # last format: user tty source day month time - time (duration)
        source_ip = None
        if len(parts) >= 3 and ("." in parts[2] or ":" in parts[2]):
            source_ip = parts[2]
        events.append(
            AuthEventSummary(
                event_type="login_success",
                username=username,
                source_ip=source_ip,
                evidence_source="last",
            )
        )
    return events


def _parse_lastb_failures(payload: object) -> list[AuthEventSummary]:
    text = _command_stdout(payload)
    if not text or not text.strip():
        return []
    events: list[AuthEventSummary] = []
    for line in text.strip().splitlines():
        parts = line.split()
        if len(parts) < 2 or parts[0] in {"btmp", "wtmp", ""}:
            continue
        username = parts[0]
        source_ip = None
        if len(parts) >= 3 and ("." in parts[2] or ":" in parts[2]):
            source_ip = parts[2]
        events.append(
            AuthEventSummary(
                event_type="login_failure",
                username=username,
                source_ip=source_ip,
                evidence_source="lastb",
            )
        )
    return events


_SSH_FAILED_RE = _re.compile(r"Failed password for (?:invalid user )?(\S+) from (\S+)")
_SSH_INVALID_RE = _re.compile(r"Invalid user (\S+) from (\S+)")
_SSH_ROOT_RE = _re.compile(
    r"(?:Failed password|Accepted password|Accepted publickey) for root from (\S+)"
)
_SUDO_RE = _re.compile(r"(\S+) : .* COMMAND=(.*)")


def _parse_journalctl_ssh(payload: object) -> list[AuthEventSummary]:
    text = _command_stdout(payload)
    if not text or not text.strip():
        return []
    events: list[AuthEventSummary] = []
    for line in text.strip().splitlines():
        line = redact_auth_value(line)
        m = _SSH_ROOT_RE.search(line)
        if m:
            events.append(
                AuthEventSummary(
                    event_type="ssh_root_login",
                    username="root",
                    source_ip=m.group(1),
                    evidence_source="journalctl",
                )
            )
            continue
        m = _SSH_FAILED_RE.search(line)
        if m:
            events.append(
                AuthEventSummary(
                    event_type="ssh_failed_password",
                    username=m.group(1),
                    source_ip=m.group(2),
                    evidence_source="journalctl",
                )
            )
            continue
        m = _SSH_INVALID_RE.search(line)
        if m:
            events.append(
                AuthEventSummary(
                    event_type="ssh_invalid_user",
                    username=m.group(1),
                    source_ip=m.group(2),
                    evidence_source="journalctl",
                )
            )
            continue
        m = _SUDO_RE.search(line)
        if m:
            events.append(
                AuthEventSummary(
                    event_type="sudo_command",
                    username=m.group(1),
                    evidence_source="journalctl",
                )
            )
    return events
