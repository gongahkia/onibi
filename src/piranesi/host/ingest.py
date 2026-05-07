from __future__ import annotations

import json
from collections.abc import Callable
from ipaddress import ip_address
from pathlib import Path
from typing import Any

from pydantic import ValidationError

from piranesi.host.models import (
    HostIdentity,
    HostPackage,
    HostProcess,
    HostSnapshot,
    ListeningPort,
    NetworkInterface,
    OsRelease,
    ServiceState,
    UserAccount,
)

_DEFAULT_LISTEN_ADDRESS = "0.0.0.0"  # noqa: S104


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
    osquery_payloads = _load_json_files(osquery_dir) if osquery_dir is not None else {}
    trivy_payloads = _load_json_files(trivy_dir) if trivy_dir is not None else {}
    command_payloads = _load_json_files(commands_dir) if commands_dir is not None else {}
    if not osquery_payloads and not trivy_payloads and not command_payloads:
        raise HostInputError(
            f"raw host bundle at {root} must contain host_snapshot.json, "
            "osquery/*.json, trivy/*.json, commands/*.json, raw/osquery/*.json, "
            "raw/trivy/*.json, or raw/commands/*.json"
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
    config = _config_from_evidence(osquery_payloads, command_payloads)

    raw_evidence: dict[str, object] = {}
    if osquery_payloads:
        raw_evidence["osquery"] = osquery_payloads
    if trivy_payloads:
        raw_evidence["trivy"] = trivy_payloads
    if command_payloads:
        raw_evidence["commands"] = command_payloads

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
        config=config,
        tool_provenance={
            "bundle": str(root),
            "osquery": str(osquery_dir) if osquery_payloads and osquery_dir else "",
            "trivy": str(trivy_dir) if trivy_payloads and trivy_dir else "",
            "commands": str(commands_dir) if command_payloads and commands_dir else "",
        },
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
            isinstance(value, (str, int, float, bool, type(None)))
            for value in payload.values()
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


def _packages_from_osquery(payloads: dict[str, object]) -> list[HostPackage]:
    packages: list[HostPackage] = []
    for row in _all_rows(payloads, "deb_packages", "packages"):
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
            )
        )
    return _dedupe_models(packages, key=lambda item: (item.name, item.version))


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
) -> dict[str, object]:
    config: dict[str, object] = {}
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
    sysctl = _sysctl_from_commands(command_payloads)
    if sysctl:
        config["sysctl"] = sysctl
    sudo = _sudo_config_from_evidence(payloads, command_payloads)
    if sudo:
        config["sudo"] = sudo
    return config


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
    if stdout is None:
        return {}
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
    return {
        "source": "apt_upgradable",
        "upgradable": updates,
        "security_count": sum(1 for update in updates if update["security"]),
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
