from __future__ import annotations

from datetime import UTC, datetime
from hashlib import sha256
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

Severity = Literal["informational", "low", "medium", "high", "critical"]
AnalysisMode = Literal["deterministic", "llm"]


def _default_analysis_modes() -> list[AnalysisMode]:
    return ["deterministic"]


class EvidenceItem(BaseModel):
    model_config = ConfigDict(extra="forbid")

    source: str
    key: str
    value: str


class HostIdentity(BaseModel):
    model_config = ConfigDict(extra="forbid")

    hostname: str
    host_id: str | None = None
    ip_addresses: list[str] = Field(default_factory=list)


class OsRelease(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str = "unknown"
    version: str | None = None
    id: str | None = None
    version_id: str | None = None
    pretty_name: str | None = None


class HostPackage(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    version: str
    source: str = "snapshot"
    architecture: str | None = None


class ListeningPort(BaseModel):
    model_config = ConfigDict(extra="forbid")

    protocol: str
    address: str
    port: int
    process: str | None = None
    pid: int | None = None


class HostProcess(BaseModel):
    model_config = ConfigDict(extra="forbid")

    pid: int
    name: str
    path: str | None = None
    cmdline: str | None = None
    user: str | None = None


class NetworkInterface(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    address: str
    family: str | None = None
    mask: str | None = None


class ServiceState(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    enabled: bool | None = None
    running: bool | None = None
    source: str = "snapshot"


class UserAccount(BaseModel):
    model_config = ConfigDict(extra="forbid")

    username: str
    uid: int | None = None
    gid: int | None = None
    shell: str | None = None
    groups: list[str] = Field(default_factory=list)
    last_login: str | None = None


class HostSnapshot(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = 1
    collected_at: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    identity: HostIdentity
    os: OsRelease = Field(default_factory=OsRelease)
    kernel: str | None = None
    packages: list[HostPackage] = Field(default_factory=list)
    network_interfaces: list[NetworkInterface] = Field(default_factory=list)
    listening_ports: list[ListeningPort] = Field(default_factory=list)
    processes: list[HostProcess] = Field(default_factory=list)
    services: list[ServiceState] = Field(default_factory=list)
    users: list[UserAccount] = Field(default_factory=list)
    config: dict[str, object] = Field(default_factory=dict)
    tool_provenance: dict[str, str] = Field(default_factory=dict)
    raw_evidence: dict[str, object] = Field(default_factory=dict)


class HostFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    title: str
    category: str
    severity: Severity
    confidence: float = Field(ge=0.0, le=1.0)
    affected_component: str | None = None
    cve_ids: list[str] = Field(default_factory=list)
    control_refs: list[str] = Field(default_factory=list)
    evidence: list[EvidenceItem] = Field(default_factory=list)
    remediation: str
    source_tool: str
    analysis_mode: AnalysisMode = "deterministic"
    rationale: str | None = None


class CollectionCapabilityHealth(BaseModel):
    model_config = ConfigDict(extra="forbid")

    status: Literal["ok", "warn", "fail", "skipped"]
    required: bool = False
    commands_by_status: dict[str, int] = Field(default_factory=dict)
    command_names: list[str] = Field(default_factory=list)
    message: str
    remediation: str | None = None


class CollectionHealth(BaseModel):
    model_config = ConfigDict(extra="forbid")

    manifest_present: bool = True
    status_counts: dict[str, int] = Field(default_factory=dict)
    required: dict[str, CollectionCapabilityHealth] = Field(default_factory=dict)
    optional: dict[str, CollectionCapabilityHealth] = Field(default_factory=dict)
    warnings: list[str] = Field(default_factory=list)


class HostPostureReport(BaseModel):
    model_config = ConfigDict(extra="forbid")

    target: str
    generated_at: str = Field(default_factory=lambda: datetime.now(UTC).isoformat())
    analysis_modes: list[AnalysisMode] = Field(default_factory=_default_analysis_modes)
    posture_score: int = Field(ge=0, le=100)
    summary: dict[str, object]
    host_metadata: dict[str, object] = Field(default_factory=dict)
    top_actions: list[dict[str, object]] = Field(default_factory=list)
    findings: list[HostFinding] = Field(default_factory=list)
    evidence_inventory: dict[str, int] = Field(default_factory=dict)
    collection_health: CollectionHealth | None = None
    known_limitations: list[str] = Field(default_factory=list)
    snapshot: HostSnapshot


def host_finding_id(*parts: object) -> str:
    material = "|".join(str(part).strip().lower() for part in parts)
    return "host-" + sha256(material.encode("utf-8")).hexdigest()[:16]
