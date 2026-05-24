from __future__ import annotations

import json
import subprocess
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any

import yaml

from piranesi.host.models import (
    CollectionCapabilityHealth,
    CollectionHealth,
    EvidenceItem,
    HostRiskScore,
    Severity,
)
from piranesi.infrastructure.models import (
    InfrastructureFinding,
    InfrastructureReport,
    KubernetesContainerSpec,
    KubernetesService,
    KubernetesSnapshot,
    KubernetesWorkload,
    infrastructure_finding_id,
)

_WORKLOAD_KINDS = {
    "CronJob",
    "DaemonSet",
    "Deployment",
    "Job",
    "Pod",
    "ReplicaSet",
    "ReplicationController",
    "StatefulSet",
}
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


class KubernetesInputError(RuntimeError):
    """Raised when Kubernetes evidence cannot be loaded safely."""


def load_kubernetes_snapshot(path: str | Path) -> KubernetesSnapshot:
    root = Path(path).expanduser().resolve(strict=False)
    if not root.exists():
        raise KubernetesInputError(f"Kubernetes manifest path does not exist: {root}")
    documents: list[dict[str, Any]] = []
    files = [root] if root.is_file() else sorted(root.rglob("*"))
    for candidate in files:
        if not candidate.is_file() or candidate.suffix.lower() not in {".yaml", ".yml", ".json"}:
            continue
        documents.extend(_load_manifest_documents(candidate))
    return snapshot_from_kubernetes_documents(documents, source=str(root))


def snapshot_from_kubernetes_documents(
    documents: list[dict[str, Any]],
    *,
    source: str,
) -> KubernetesSnapshot:
    workloads: list[KubernetesWorkload] = []
    services: list[KubernetesService] = []
    for document in documents:
        kind = str(document.get("kind") or "")
        if not kind:
            continue
        if kind in _WORKLOAD_KINDS:
            workloads.append(_parse_workload(document))
        elif kind == "Service":
            services.append(_parse_service(document))
    return KubernetesSnapshot(
        source=source,
        workloads=workloads,
        services=services,
        raw_evidence={"resource_count": len(documents)},
    )


def collect_kubernetes_api_snapshot(
    *,
    kubeconfig: str | Path,
    namespace: str | None = None,
) -> KubernetesSnapshot:
    command = [
        "kubectl",
        "--kubeconfig",
        str(Path(kubeconfig).expanduser()),
        "get",
        "pods,deployments,statefulsets,daemonsets,jobs,cronjobs,services",
        "-o",
        "json",
    ]
    if namespace:
        command.extend(["--namespace", namespace])
    else:
        command.append("--all-namespaces")
    try:
        result = subprocess.run(
            command,
            check=False,
            capture_output=True,
            text=True,
            timeout=60,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise KubernetesInputError(
            f"unable to query Kubernetes API read-only snapshot: {exc}"
        ) from exc
    if result.returncode != 0:
        raise KubernetesInputError(result.stderr.strip() or "kubectl get failed")
    payload = json.loads(result.stdout)
    items = payload.get("items") if isinstance(payload, dict) else None
    documents = (
        [item for item in items if isinstance(item, dict)] if isinstance(items, list) else []
    )
    return snapshot_from_kubernetes_documents(documents, source="kubernetes-api")


def assess_kubernetes_snapshot(snapshot: KubernetesSnapshot) -> InfrastructureReport:
    findings: list[InfrastructureFinding] = []
    for workload in snapshot.workloads:
        findings.extend(_workload_findings(workload))
    for service in snapshot.services:
        findings.extend(_service_findings(service))
    ranked = _rank_findings(findings)
    return InfrastructureReport(
        surface="kubernetes",
        target=snapshot.source,
        posture_score=_posture_score(ranked),
        summary=_summary(ranked),
        evidence_inventory={
            "workloads": len(snapshot.workloads),
            "containers": sum(len(workload.containers) for workload in snapshot.workloads),
            "services": len(snapshot.services),
        },
        collection_health=_collection_health(snapshot),
        top_actions=_top_actions(ranked),
        findings=ranked,
        snapshots={"kubernetes": snapshot.model_dump(mode="json")},
        known_limitations=[
            "Kubernetes posture is read-only manifest/API analysis, not admission control.",
            "Cluster runtime state is only represented when explicitly collected through kubectl.",
        ],
    )


def _collection_health(snapshot: KubernetesSnapshot) -> CollectionHealth:
    source_name = (
        "kubernetes_api" if snapshot.source == "kubernetes-api" else "kubernetes_manifests"
    )
    message = (
        "Loaded read-only Kubernetes API snapshot."
        if source_name == "kubernetes_api"
        else "Loaded local Kubernetes manifests."
    )
    return CollectionHealth(
        manifest_present=True,
        status_counts={"ok": 1},
        required={
            source_name: CollectionCapabilityHealth(
                status="ok",
                required=True,
                commands_by_status={"ok": 1},
                command_names=[source_name],
                message=message,
            )
        },
        optional={},
        warnings=[],
    )


def _parse_workload(document: dict[str, Any]) -> KubernetesWorkload:
    metadata = _dict(document.get("metadata"))
    pod_spec = _pod_spec(document)
    containers = [
        _parse_container(item, pod_spec=_dict(pod_spec))
        for item in _list(pod_spec.get("containers"))
        if isinstance(item, dict)
    ]
    containers.extend(
        _parse_container(item, pod_spec=_dict(pod_spec))
        for item in _list(pod_spec.get("initContainers"))
        if isinstance(item, dict)
    )
    return KubernetesWorkload(
        kind=str(document.get("kind") or "Unknown"),
        name=str(metadata.get("name") or "unknown"),
        namespace=_string(metadata.get("namespace")),
        host_network=bool(pod_spec.get("hostNetwork")),
        containers=containers,
        raw=document,
    )


def _parse_container(
    container: dict[str, Any],
    *,
    pod_spec: dict[str, Any],
) -> KubernetesContainerSpec:
    security = _dict(container.get("securityContext"))
    pod_security = _dict(pod_spec.get("securityContext"))
    resources = _dict(container.get("resources"))
    limits = _dict(resources.get("limits"))
    requests = _dict(resources.get("requests"))
    env_names: list[str] = []
    secret_refs: list[str] = []
    for env in _list(container.get("env")):
        if not isinstance(env, dict):
            continue
        name = _string(env.get("name"))
        if name:
            env_names.append(name)
        value_from = _dict(env.get("valueFrom"))
        secret_key_ref = _dict(value_from.get("secretKeyRef"))
        if secret_key_ref:
            secret_refs.append(_string(secret_key_ref.get("name")) or name or "secret")
    for env_from in _list(container.get("envFrom")):
        if not isinstance(env_from, dict):
            continue
        secret_ref = _dict(env_from.get("secretRef"))
        if secret_ref:
            secret_refs.append(_string(secret_ref.get("name")) or "secret")
    run_as_user = security.get("runAsUser", pod_security.get("runAsUser"))
    return KubernetesContainerSpec(
        name=str(container.get("name") or "container"),
        image=_string(container.get("image")),
        privileged=bool(security.get("privileged")),
        run_as_user=int(run_as_user) if isinstance(run_as_user, int) else None,
        run_as_non_root=_bool_or_none(
            security.get("runAsNonRoot", pod_security.get("runAsNonRoot"))
        ),
        env=env_names,
        env_secret_refs=secret_refs,
        volume_mounts=[
            str(item.get("name"))
            for item in _list(container.get("volumeMounts"))
            if isinstance(item, dict) and item.get("name")
        ],
        has_resource_limits=bool(limits),
        has_resource_requests=bool(requests),
    )


def _parse_service(document: dict[str, Any]) -> KubernetesService:
    metadata = _dict(document.get("metadata"))
    spec = _dict(document.get("spec"))
    return KubernetesService(
        name=str(metadata.get("name") or "unknown"),
        namespace=_string(metadata.get("namespace")),
        service_type=str(spec.get("type") or "ClusterIP"),
        ports=[
            port["port"]
            for port in _list(spec.get("ports"))
            if isinstance(port, dict) and isinstance(port.get("port"), int)
        ],
        raw=document,
    )


def _workload_findings(workload: KubernetesWorkload) -> list[InfrastructureFinding]:
    findings: list[InfrastructureFinding] = []
    resource = _resource_name(workload)
    if workload.host_network:
        findings.append(
            _finding(
                rule_id="k8s.workload.host_network",
                title="Kubernetes workload uses hostNetwork",
                category="network",
                severity="high",
                resource=resource,
                evidence=[EvidenceItem(source="manifest", key="hostNetwork", value="true")],
                remediation=(
                    "Disable hostNetwork unless the workload has a documented "
                    "node-level networking requirement."
                ),
            )
        )
    for container in workload.containers:
        container_resource = f"{resource}/{container.name}"
        if container.privileged:
            findings.append(
                _finding(
                    rule_id="k8s.workload.privileged_container",
                    title="Kubernetes container is privileged",
                    category="escape-risk",
                    severity="critical",
                    resource=container_resource,
                    evidence=[EvidenceItem(source="manifest", key="privileged", value="true")],
                    remediation=(
                        "Set securityContext.privileged to false and grant only "
                        "required capabilities."
                    ),
                )
            )
        if container.run_as_user == 0 or (
            container.run_as_user is None and container.run_as_non_root is not True
        ):
            findings.append(
                _finding(
                    rule_id="k8s.workload.runs_as_root",
                    title="Kubernetes container may run as root",
                    category="identity",
                    severity="medium",
                    resource=container_resource,
                    evidence=[
                        EvidenceItem(
                            source="manifest",
                            key="runAsNonRoot",
                            value=str(container.run_as_non_root),
                        )
                    ],
                    remediation="Set runAsNonRoot true and runAsUser to a non-zero UID.",
                )
            )
        if not container.has_resource_limits:
            findings.append(
                _finding(
                    rule_id="k8s.workload.missing_resource_limits",
                    title="Kubernetes container is missing resource limits",
                    category="resilience",
                    severity="medium",
                    resource=container_resource,
                    evidence=[
                        EvidenceItem(
                            source="manifest",
                            key="resources.limits",
                            value="missing",
                        )
                    ],
                    remediation="Set CPU and memory limits appropriate for the workload.",
                )
            )
        if container.env_secret_refs:
            findings.append(
                _finding(
                    rule_id="k8s.workload.env_secret_ref",
                    title="Kubernetes manifest exposes secret through environment variables",
                    category="secret-handling",
                    severity="medium",
                    resource=container_resource,
                    evidence=[
                        EvidenceItem(
                            source="manifest",
                            key="secret_refs",
                            value=", ".join(sorted(set(container.env_secret_refs))),
                        )
                    ],
                    remediation=(
                        "Prefer mounted secret files with narrow access and avoid "
                        "exposing secrets as process environment variables."
                    ),
                )
            )
    return findings


def _service_findings(service: KubernetesService) -> list[InfrastructureFinding]:
    if service.service_type not in {"LoadBalancer", "NodePort"}:
        return []
    return [
        _finding(
            rule_id="k8s.service.public_exposure",
            title=f"Kubernetes service exposes {service.service_type}",
            category="exposure",
            severity="high",
            resource=_service_name(service),
            evidence=[
                EvidenceItem(source="manifest", key="service.type", value=service.service_type),
                EvidenceItem(
                    source="manifest",
                    key="ports",
                    value=", ".join(map(str, service.ports)),
                ),
            ],
            remediation=(
                "Use ClusterIP by default or restrict public services with "
                "explicit ingress and network policy."
            ),
        )
    ]


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
        id=infrastructure_finding_id("k8s", rule_id, resource, title),
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


def _pod_spec(document: dict[str, Any]) -> dict[str, Any]:
    kind = document.get("kind")
    spec = _dict(document.get("spec"))
    if kind == "Pod":
        return spec
    if kind == "CronJob":
        return _dict(
            _dict(_dict(_dict(spec.get("jobTemplate")).get("spec")).get("template")).get("spec")
        )
    return _dict(_dict(spec.get("template")).get("spec"))


def _load_manifest_documents(path: Path) -> list[dict[str, Any]]:
    try:
        if path.suffix.lower() == ".json":
            payload = json.loads(path.read_text(encoding="utf-8"))
            if isinstance(payload, dict) and isinstance(payload.get("items"), list):
                return [item for item in payload["items"] if isinstance(item, dict)]
            return [payload] if isinstance(payload, dict) else []
        documents = yaml.safe_load_all(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError, yaml.YAMLError) as exc:
        raise KubernetesInputError(f"invalid Kubernetes manifest {path}: {exc}") from exc
    return [item for item in documents if isinstance(item, dict)]


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
    return sorted(
        actions,
        key=lambda item: (
            float(item["risk_total"]) if isinstance(item["risk_total"], (int, float, str)) else 0.0
        ),
        reverse=True,
    )[:5]


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
        rationale=["Infrastructure finding derived from local normalized Kubernetes evidence."],
    )


def _resource_name(workload: KubernetesWorkload) -> str:
    namespace = f"{workload.namespace}/" if workload.namespace else ""
    return f"{namespace}{workload.kind}/{workload.name}"


def _service_name(service: KubernetesService) -> str:
    namespace = f"{service.namespace}/" if service.namespace else ""
    return f"{namespace}Service/{service.name}"


def _dict(value: object) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def _list(value: object) -> list[Any]:
    return value if isinstance(value, list) else []


def _string(value: object) -> str | None:
    if value is None:
        return None
    rendered = str(value)
    return rendered if rendered else None


def _bool_or_none(value: object) -> bool | None:
    return value if isinstance(value, bool) else None
