from __future__ import annotations

import json
import subprocess
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any

from pydantic import ValidationError

from piranesi.host.models import (
    CollectionCapabilityHealth,
    CollectionHealth,
    EvidenceItem,
    HostRiskScore,
    Severity,
)
from piranesi.infrastructure.models import (
    ContainerImagePackage,
    ContainerImageSnapshot,
    ContainerMount,
    InfrastructureFinding,
    InfrastructureReport,
    RunningContainerSnapshot,
    infrastructure_finding_id,
)

_DANGEROUS_MOUNT_PREFIXES = ("/", "/var/run/docker.sock", "/etc", "/proc", "/sys")
_SEVERITY_SCORE = {
    "informational": 0.05,
    "low": 0.25,
    "medium": 0.55,
    "high": 0.8,
    "critical": 1.0,
}
_SEVERITY_PENALTIES = {
    "critical": 30,
    "high": 18,
    "medium": 8,
    "low": 3,
    "informational": 1,
}


class ContainerInputError(RuntimeError):
    """Raised when container evidence cannot be loaded safely."""


def load_container_image_snapshot(path: str | Path) -> ContainerImageSnapshot:
    payload = _load_json(path)
    try:
        return ContainerImageSnapshot.model_validate(payload)
    except ValidationError:
        return parse_trivy_image_json(payload)


def parse_trivy_image_json(payload: dict[str, Any]) -> ContainerImageSnapshot:
    image_ref = str(payload.get("ArtifactName") or payload.get("Target") or "container-image")
    image_id = _string(payload.get("ArtifactID"))
    packages: list[ContainerImagePackage] = []
    os_family: str | None = None
    for result in _list(payload.get("Results")):
        if isinstance(result, dict):
            os_family = os_family or _string(result.get("Class")) or _string(result.get("Type"))
            for vuln in _list(result.get("Vulnerabilities")):
                if not isinstance(vuln, dict):
                    continue
                packages.append(
                    ContainerImagePackage(
                        name=_string(vuln.get("PkgName")) or "unknown",
                        version=_string(vuln.get("InstalledVersion")),
                        fixed_version=_string(vuln.get("FixedVersion")),
                        vulnerability_id=_string(vuln.get("VulnerabilityID")),
                        severity=_severity(vuln.get("Severity")),
                        source="trivy",
                    )
                )
    metadata = payload.get("Metadata")
    config_user = None
    env: list[str] = []
    if isinstance(metadata, dict):
        image_config = metadata.get("ImageConfig")
        if isinstance(image_config, dict):
            config = image_config.get("config") or image_config.get("Config")
            if isinstance(config, dict):
                config_user = _string(config.get("User"))
                env = [str(item) for item in _list(config.get("Env"))]
    return ContainerImageSnapshot(
        image_ref=image_ref,
        image_id=image_id,
        os_family=os_family,
        packages=packages,
        config_user=config_user,
        env=env,
        raw_evidence={"trivy_image": payload},
    )


def load_running_container_snapshots(path: str | Path) -> list[RunningContainerSnapshot]:
    payload = _load_json(path)
    if isinstance(payload, dict) and "containers" in payload:
        raw_containers = _list(payload["containers"])
    else:
        raw_containers = payload if isinstance(payload, list) else [payload]
    return [
        _parse_docker_container_payload(item) for item in raw_containers if isinstance(item, dict)
    ]


def parse_docker_inspect(payload: dict[str, Any]) -> RunningContainerSnapshot:
    host_config = _dict(payload.get("HostConfig"))
    config = _dict(payload.get("Config"))
    network_settings = _dict(payload.get("NetworkSettings"))
    mounts = [
        ContainerMount(
            source=_string(item.get("Source")) or _string(item.get("Name")) or "",
            destination=_string(item.get("Destination")) or "",
            mode=_string(item.get("Mode")),
            read_write=bool(item.get("RW")) if "RW" in item else None,
        )
        for item in _list(payload.get("Mounts"))
        if isinstance(item, dict)
    ]
    return RunningContainerSnapshot(
        container_id=_string(payload.get("Id")) or _string(payload.get("ID")) or "unknown",
        name=(_string(payload.get("Name")) or _string(payload.get("Names")) or "unknown").lstrip(
            "/"
        ),
        image=_string(config.get("Image")) or _string(payload.get("Image")) or "unknown",
        image_id=_string(payload.get("ImageID")),
        privileged=bool(host_config.get("Privileged")),
        network_mode=_string(host_config.get("NetworkMode")),
        user=_string(config.get("User")),
        env=[str(item) for item in _list(config.get("Env"))],
        mounts=mounts,
        ports=_ports(network_settings),
        raw_evidence={"docker_inspect": payload},
    )


def parse_docker_container_list(payload: dict[str, Any]) -> RunningContainerSnapshot:
    ports = _string(payload.get("Ports"))
    return RunningContainerSnapshot(
        container_id=_string(payload.get("ID")) or _string(payload.get("Id")) or "unknown",
        name=_string(payload.get("Names")) or _string(payload.get("Name")) or "unknown",
        image=_string(payload.get("Image")) or "unknown",
        image_id=_string(payload.get("ImageID")),
        privileged=False,
        network_mode=_string(payload.get("Networks")),
        user=None,
        env=[],
        mounts=[],
        ports=[ports] if ports else [],
        raw_evidence={"docker_container_list": payload},
    )


def _parse_docker_container_payload(payload: dict[str, Any]) -> RunningContainerSnapshot:
    if "HostConfig" in payload or "Config" in payload or "Mounts" in payload:
        return parse_docker_inspect(payload)
    return parse_docker_container_list(payload)


def assess_container_image(snapshot: ContainerImageSnapshot) -> InfrastructureReport:
    findings = _rank_findings(_image_findings(snapshot))
    return _container_report(
        target=snapshot.image_ref,
        findings=findings,
        evidence_inventory={
            "images": 1,
            "image_packages": len(snapshot.packages),
            "running_containers": 0,
        },
        snapshots={"image": snapshot.model_dump(mode="json")},
        collection_health=_collection_health(
            "trivy_image",
            "Loaded Trivy image vulnerability/config evidence.",
        ),
    )


def assess_running_containers(
    snapshots: list[RunningContainerSnapshot],
    *,
    target: str = "local-docker",
) -> InfrastructureReport:
    findings: list[InfrastructureFinding] = []
    for snapshot in snapshots:
        findings.extend(_running_container_findings(snapshot))
    ranked = _rank_findings(findings)
    return _container_report(
        target=target,
        findings=ranked,
        evidence_inventory={
            "images": len({snapshot.image for snapshot in snapshots}),
            "image_packages": 0,
            "running_containers": len(snapshots),
            "mounts": sum(len(snapshot.mounts) for snapshot in snapshots),
            "published_ports": sum(len(snapshot.ports) for snapshot in snapshots),
        },
        snapshots={"running_containers": [item.model_dump(mode="json") for item in snapshots]},
        collection_health=_collection_health(
            "docker_inspect",
            "Loaded read-only Docker container list/inspect evidence.",
        ),
    )


def collect_local_docker_snapshots() -> list[RunningContainerSnapshot]:
    try:
        ps = subprocess.run(
            ["docker", "ps", "-q"],
            check=False,
            capture_output=True,
            text=True,
            timeout=30,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise ContainerInputError(f"unable to query local Docker: {exc}") from exc
    if ps.returncode != 0:
        raise ContainerInputError(ps.stderr.strip() or "docker ps failed")
    ids = [line.strip() for line in ps.stdout.splitlines() if line.strip()]
    if not ids:
        return []
    inspect = subprocess.run(
        ["docker", "inspect", *ids],
        check=False,
        capture_output=True,
        text=True,
        timeout=60,
    )
    if inspect.returncode != 0:
        raise ContainerInputError(inspect.stderr.strip() or "docker inspect failed")
    payload = json.loads(inspect.stdout)
    return [parse_docker_inspect(item) for item in payload if isinstance(item, dict)]


def collect_trivy_image_snapshot(image: str) -> ContainerImageSnapshot:
    image_path = Path(image).expanduser()
    if image_path.is_file():
        return load_container_image_snapshot(image_path)
    try:
        result = subprocess.run(
            ["trivy", "image", "--format", "json", "--quiet", image],
            check=False,
            capture_output=True,
            text=True,
            timeout=300,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise ContainerInputError(f"unable to run Trivy image scan: {exc}") from exc
    if result.returncode != 0:
        raise ContainerInputError(result.stderr.strip() or "trivy image failed")
    return parse_trivy_image_json(json.loads(result.stdout))


def _image_findings(snapshot: ContainerImageSnapshot) -> list[InfrastructureFinding]:
    findings: list[InfrastructureFinding] = []
    for package in snapshot.packages:
        if package.vulnerability_id is None:
            continue
        severity = package.severity or "medium"
        findings.append(
            InfrastructureFinding(
                id=infrastructure_finding_id(
                    "container",
                    "vulnerability",
                    snapshot.image_ref,
                    package.name,
                    package.vulnerability_id,
                ),
                rule_id="container.image.vulnerable_package",
                title=f"{package.name} has {package.vulnerability_id}",
                category="vulnerability",
                severity=severity,
                confidence=0.95,
                affected_resource=snapshot.image_ref,
                evidence=[
                    EvidenceItem(source="trivy", key="package", value=package.name),
                    EvidenceItem(
                        source="trivy",
                        key="vulnerability",
                        value=package.vulnerability_id,
                    ),
                    EvidenceItem(
                        source="trivy",
                        key="installed_version",
                        value=package.version or "unknown",
                    ),
                ],
                remediation=(
                    f"Rebuild {snapshot.image_ref} with a fixed {package.name} package"
                    + (
                        f" ({package.fixed_version})."
                        if package.fixed_version
                        else " when a fixed version is available."
                    )
                ),
                source_tool="trivy",
                risk=_risk(severity, 0.95),
            )
        )
    if _runs_as_root(snapshot.config_user):
        findings.append(
            InfrastructureFinding(
                id=infrastructure_finding_id("container", "image-root", snapshot.image_ref),
                rule_id="container.image.runs_as_root",
                title="Container image defaults to root",
                category="identity",
                severity="medium",
                confidence=0.8,
                affected_resource=snapshot.image_ref,
                evidence=[
                    EvidenceItem(
                        source="image_config",
                        key="user",
                        value=snapshot.config_user or "root",
                    )
                ],
                remediation=(
                    "Set a non-root USER in the image and ensure filesystem permissions support it."
                ),
                risk=_risk("medium", 0.8),
            )
        )
    return findings


def _running_container_findings(snapshot: RunningContainerSnapshot) -> list[InfrastructureFinding]:
    findings: list[InfrastructureFinding] = []
    resource = snapshot.name or snapshot.container_id
    if snapshot.privileged:
        findings.append(
            _finding(
                rule_id="container.runtime.privileged",
                title="Container is running privileged",
                category="escape-risk",
                severity="critical",
                resource=resource,
                evidence=[EvidenceItem(source="docker", key="Privileged", value="true")],
                remediation=(
                    "Run the container without --privileged and grant only required capabilities."
                ),
            )
        )
    if snapshot.network_mode == "host":
        findings.append(
            _finding(
                rule_id="container.runtime.host_network",
                title="Container uses host network mode",
                category="network",
                severity="high",
                resource=resource,
                evidence=[EvidenceItem(source="docker", key="NetworkMode", value="host")],
                remediation=(
                    "Use bridge or isolated networking unless host networking is "
                    "explicitly required."
                ),
            )
        )
    if _runs_as_root(snapshot.user):
        findings.append(
            _finding(
                rule_id="container.runtime.runs_as_root",
                title="Container runs as root",
                category="identity",
                severity="medium",
                resource=resource,
                evidence=[EvidenceItem(source="docker", key="User", value=snapshot.user or "root")],
                remediation=(
                    "Run the container with a non-root user and least-privilege "
                    "filesystem permissions."
                ),
            )
        )
    for mount in snapshot.mounts:
        if _dangerous_mount(mount):
            findings.append(
                _finding(
                    rule_id="container.runtime.dangerous_mount",
                    title=f"Dangerous host mount {mount.destination}",
                    category="filesystem",
                    severity="high",
                    resource=resource,
                    evidence=[
                        EvidenceItem(source="docker", key="Source", value=mount.source),
                        EvidenceItem(
                            source="docker",
                            key="Destination",
                            value=mount.destination,
                        ),
                    ],
                    remediation=(
                        "Remove host-sensitive mounts or make narrowly scoped read-only mounts."
                    ),
                )
            )
    return findings


def _finding(
    *,
    rule_id: str,
    title: str,
    category: str,
    severity: Severity,
    resource: str,
    evidence: list[EvidenceItem],
    remediation: str,
) -> InfrastructureFinding:
    return InfrastructureFinding(
        id=infrastructure_finding_id("container", rule_id, resource, title),
        rule_id=rule_id,
        title=title,
        category=category,
        severity=severity,
        confidence=0.9,
        affected_resource=resource,
        evidence=evidence,
        remediation=remediation,
        risk=_risk(severity, 0.9),
    )


def _container_report(
    *,
    target: str,
    findings: list[InfrastructureFinding],
    evidence_inventory: dict[str, int],
    snapshots: dict[str, object],
    collection_health: CollectionHealth,
) -> InfrastructureReport:
    return InfrastructureReport(
        surface="container",
        target=target,
        posture_score=_posture_score(findings),
        summary=_summary(findings),
        evidence_inventory=evidence_inventory,
        collection_health=collection_health,
        top_actions=_top_actions(findings),
        findings=findings,
        snapshots=snapshots,
        known_limitations=[
            "Container posture is local evidence normalization, not runtime monitoring.",
            (
                "Image vulnerability findings depend on supplied Trivy evidence "
                "or local Trivy availability."
            ),
        ],
    )


def _collection_health(name: str, message: str) -> CollectionHealth:
    return CollectionHealth(
        manifest_present=True,
        status_counts={"ok": 1},
        required={
            name: CollectionCapabilityHealth(
                status="ok",
                required=True,
                commands_by_status={"ok": 1},
                command_names=[name],
                message=message,
            )
        },
        optional={},
        warnings=[],
    )


def _rank_findings(findings: list[InfrastructureFinding]) -> list[InfrastructureFinding]:
    return sorted(findings, key=lambda item: (-(item.risk.total if item.risk else 0), item.id))


def _summary(findings: list[InfrastructureFinding]) -> dict[str, object]:
    by_severity = Counter(finding.severity for finding in findings)
    by_category = Counter(finding.category for finding in findings)
    risk_totals = [finding.risk.total for finding in findings if finding.risk is not None]
    return {
        "findings_total": len(findings),
        "by_severity": dict(sorted(by_severity.items())),
        "by_category": dict(sorted(by_category.items())),
        "risk": {
            "max_total": max(risk_totals) if risk_totals else 0.0,
            "average_total": round(sum(risk_totals) / len(risk_totals), 2) if risk_totals else 0.0,
        },
    }


def _top_actions(findings: list[InfrastructureFinding]) -> list[dict[str, object]]:
    grouped: dict[str, list[InfrastructureFinding]] = defaultdict(list)
    for finding in findings:
        grouped[finding.category].append(finding)
    actions = []
    for category, group in grouped.items():
        top = group[0]
        actions.append(
            {
                "category": category,
                "action": top.remediation,
                "finding_count": len(group),
                "risk_total": top.risk.total if top.risk else 0.0,
            }
        )
    return sorted(actions, key=lambda item: float(item["risk_total"]), reverse=True)[:5]


def _posture_score(findings: list[InfrastructureFinding]) -> int:
    penalty = sum(_SEVERITY_PENALTIES[finding.severity] for finding in findings)
    return max(0, 100 - penalty)


def _risk(severity: Severity, confidence: float) -> HostRiskScore:
    sev = _SEVERITY_SCORE[severity]
    return HostRiskScore(
        total=round(min(100.0, max(1.0, sev * 72.0 + confidence * 20.0)), 1),
        severity=sev,
        confidence=confidence,
        exploitability=max(0.1, sev * 0.8),
        blast_radius=max(0.1, sev * 0.75),
        remediation_urgency=max(0.1, sev * 0.8),
        evidence_quality=confidence,
        rationale=["Infrastructure finding derived from local normalized evidence."],
    )


def _dangerous_mount(mount: ContainerMount) -> bool:
    source = mount.source.rstrip("/") or "/"
    destination = mount.destination.rstrip("/") or "/"
    if source == "/var/run/docker.sock" or destination == "/var/run/docker.sock":
        return True
    if source == "/" or destination == "/":
        return True
    if source in {"/etc", "/proc", "/sys"}:
        return mount.read_write is not False
    return any(source.startswith(prefix + "/") for prefix in _DANGEROUS_MOUNT_PREFIXES[1:])


def _runs_as_root(user: str | None) -> bool:
    if user is None or user == "":
        return True
    return user in {"0", "root"}


def _ports(network_settings: dict[str, Any]) -> list[str]:
    ports = network_settings.get("Ports")
    if not isinstance(ports, dict):
        return []
    return [str(key) for key, value in ports.items() if value]


def _load_json(path: str | Path) -> dict[str, Any] | list[Any]:
    candidate = Path(path).expanduser().resolve(strict=False)
    try:
        text = candidate.read_text(encoding="utf-8")
    except OSError as exc:
        raise ContainerInputError(f"invalid container JSON evidence {candidate}: {exc}") from exc
    try:
        return json.loads(text)
    except json.JSONDecodeError as exc:
        lines = [line for line in text.splitlines() if line.strip()]
        if not lines:
            message = f"invalid container JSON evidence {candidate}: {exc}"
            raise ContainerInputError(message) from exc
        try:
            return [json.loads(line) for line in lines]
        except json.JSONDecodeError as line_exc:
            raise ContainerInputError(
                f"invalid container JSON evidence {candidate}: {line_exc}"
            ) from line_exc


def _severity(value: object) -> Severity | None:
    rendered = str(value or "").casefold()
    if rendered in {"informational", "low", "medium", "high", "critical"}:
        return rendered  # type: ignore[return-value]
    return None


def _string(value: object) -> str | None:
    if value is None:
        return None
    rendered = str(value)
    return rendered if rendered else None


def _dict(value: object) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def _list(value: object) -> list[Any]:
    return value if isinstance(value, list) else []
