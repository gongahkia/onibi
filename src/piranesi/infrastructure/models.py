from __future__ import annotations

from datetime import UTC, datetime
from hashlib import sha256
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from piranesi.host.models import CollectionHealth, EvidenceItem, HostRiskScore, Severity

InfrastructureSurface = Literal["container", "kubernetes"]


class ContainerImagePackage(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    version: str | None = None
    fixed_version: str | None = None
    vulnerability_id: str | None = None
    severity: Severity | None = None
    source: str = "trivy"


class ContainerImageSnapshot(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = 1
    collected_at: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    image_ref: str
    image_id: str | None = None
    os_family: str | None = None
    packages: list[ContainerImagePackage] = Field(default_factory=list)
    config_user: str | None = None
    env: list[str] = Field(default_factory=list)
    raw_evidence: dict[str, object] = Field(default_factory=dict)


class ContainerMount(BaseModel):
    model_config = ConfigDict(extra="forbid")

    source: str
    destination: str
    mode: str | None = None
    read_write: bool | None = None


class RunningContainerSnapshot(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = 1
    collected_at: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    container_id: str
    name: str
    image: str
    image_id: str | None = None
    privileged: bool = False
    network_mode: str | None = None
    user: str | None = None
    env: list[str] = Field(default_factory=list)
    mounts: list[ContainerMount] = Field(default_factory=list)
    ports: list[str] = Field(default_factory=list)
    raw_evidence: dict[str, object] = Field(default_factory=dict)


class KubernetesContainerSpec(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    image: str | None = None
    privileged: bool = False
    run_as_user: int | None = None
    run_as_non_root: bool | None = None
    env: list[str] = Field(default_factory=list)
    env_secret_refs: list[str] = Field(default_factory=list)
    volume_mounts: list[str] = Field(default_factory=list)
    has_resource_limits: bool = False
    has_resource_requests: bool = False


class KubernetesWorkload(BaseModel):
    model_config = ConfigDict(extra="forbid")

    kind: str
    name: str
    namespace: str | None = None
    host_network: bool = False
    containers: list[KubernetesContainerSpec] = Field(default_factory=list)
    raw: dict[str, object] = Field(default_factory=dict)


class KubernetesService(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    namespace: str | None = None
    service_type: str = "ClusterIP"
    ports: list[int] = Field(default_factory=list)
    raw: dict[str, object] = Field(default_factory=dict)


class KubernetesSnapshot(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = 1
    collected_at: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    source: str
    workloads: list[KubernetesWorkload] = Field(default_factory=list)
    services: list[KubernetesService] = Field(default_factory=list)
    raw_evidence: dict[str, object] = Field(default_factory=dict)


class InfrastructureFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    rule_id: str
    title: str
    category: str
    severity: Severity
    confidence: float = Field(ge=0.0, le=1.0)
    affected_resource: str
    evidence: list[EvidenceItem] = Field(default_factory=list)
    remediation: str
    source_tool: str = "deterministic"
    risk: HostRiskScore | None = None
    suppressed: bool = False


class InfrastructureReport(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = 1
    generated_at: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    surface: InfrastructureSurface
    target: str
    posture_score: int = Field(ge=0, le=100)
    summary: dict[str, object]
    evidence_inventory: dict[str, int] = Field(default_factory=dict)
    collection_health: CollectionHealth | None = None
    top_actions: list[dict[str, object]] = Field(default_factory=list)
    findings: list[InfrastructureFinding] = Field(default_factory=list)
    snapshots: dict[str, object] = Field(default_factory=dict)
    known_limitations: list[str] = Field(default_factory=list)


def infrastructure_finding_id(*parts: object) -> str:
    material = "|".join(str(part).strip().lower() for part in parts)
    return "infra-" + sha256(material.encode("utf-8")).hexdigest()[:16]
