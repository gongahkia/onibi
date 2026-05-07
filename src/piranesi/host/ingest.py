from __future__ import annotations

import json
from collections.abc import Callable
from pathlib import Path
from typing import Any

from pydantic import ValidationError

from piranesi.host.models import (
    HostIdentity,
    HostPackage,
    HostProcess,
    HostSnapshot,
    ListeningPort,
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
        return _load_snapshot_file(input_path)
    if input_path.is_dir():
        snapshot_file = input_path / "host_snapshot.json"
        if snapshot_file.is_file():
            return _load_snapshot_file(snapshot_file)
        return _load_tool_bundle(input_path)
    raise HostInputError(f"host input does not exist: {input_path}")


def _load_snapshot_file(path: Path) -> HostSnapshot:
    try:
        payload = json.loads(path.read_text(encoding="utf-8"))
        snapshot = HostSnapshot.model_validate(payload)
    except (OSError, json.JSONDecodeError, ValidationError) as exc:
        raise HostInputError(f"invalid host snapshot {path}: {exc}") from exc
    provenance = dict(snapshot.tool_provenance)
    provenance.setdefault("piranesi_snapshot", str(path))
    return snapshot.model_copy(update={"tool_provenance": provenance})


def _load_tool_bundle(root: Path) -> HostSnapshot:
    osquery_payloads = _load_json_files(root / "osquery")
    trivy_payloads = _load_json_files(root / "trivy")
    if not osquery_payloads and not trivy_payloads:
        raise HostInputError(
            f"raw host bundle at {root} must contain host_snapshot.json, "
            "osquery/*.json, or trivy/*.json"
        )

    identity = _identity_from_osquery(osquery_payloads) or HostIdentity(hostname=root.name)
    os_release = _os_from_osquery(osquery_payloads)
    kernel = _kernel_from_osquery(osquery_payloads)
    packages = _packages_from_osquery(osquery_payloads)
    listening_ports = _listening_ports_from_osquery(osquery_payloads)
    processes = _processes_from_osquery(osquery_payloads)
    users = _users_from_osquery(osquery_payloads)
    services = _services_from_osquery(osquery_payloads)
    config = _config_from_osquery(osquery_payloads)

    raw_evidence: dict[str, object] = {}
    if osquery_payloads:
        raw_evidence["osquery"] = osquery_payloads
    if trivy_payloads:
        raw_evidence["trivy"] = trivy_payloads

    return HostSnapshot(
        identity=identity,
        os=os_release,
        kernel=kernel,
        packages=packages,
        listening_ports=listening_ports,
        processes=processes,
        services=services,
        users=users,
        config=config,
        tool_provenance={
            "bundle": str(root),
            "osquery": str(root / "osquery") if osquery_payloads else "",
            "trivy": str(root / "trivy") if trivy_payloads else "",
        },
        raw_evidence=raw_evidence,
    )


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


def _identity_from_osquery(payloads: dict[str, object]) -> HostIdentity | None:
    info = _first(_all_rows(payloads, "system_info", "hostname"))
    if info is None:
        return None
    hostname = _string(info.get("hostname")) or _string(info.get("computer_name"))
    if not hostname:
        return None
    return HostIdentity(
        hostname=hostname,
        host_id=_string(info.get("uuid")) or _string(info.get("hardware_serial")),
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


def _listening_ports_from_osquery(payloads: dict[str, object]) -> list[ListeningPort]:
    ports: list[ListeningPort] = []
    for row in _all_rows(payloads, "listening_ports", "process_open_sockets"):
        port = _int(row.get("port") or row.get("local_port"))
        if port is None:
            continue
        ports.append(
            ListeningPort(
                protocol=(_string(row.get("protocol")) or "tcp").lower(),
                address=(
                    _string(row.get("address"))
                    or _string(row.get("local_address"))
                    or _DEFAULT_LISTEN_ADDRESS
                ),
                port=port,
                process=_string(row.get("process_name")) or _string(row.get("name")),
                pid=_int(row.get("pid")),
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


def _config_from_osquery(payloads: dict[str, object]) -> dict[str, object]:
    config: dict[str, object] = {}
    ssh_rows = _all_rows(payloads, "ssh_config", "sshd_config")
    if ssh_rows:
        ssh_config: dict[str, str] = {}
        for row in ssh_rows:
            key = _string(row.get("key")) or _string(row.get("option"))
            value = _string(row.get("value"))
            if key and value is not None:
                ssh_config[key] = value
        config["ssh"] = ssh_config
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
