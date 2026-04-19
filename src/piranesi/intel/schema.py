from __future__ import annotations

from datetime import UTC, datetime
from hashlib import sha256
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from piranesi.adapters.models import ExternalRawFinding, ExternalTool

TrustLevel = Literal["verified", "trusted", "untrusted"]


class IntelSourceProvenance(BaseModel):
    model_config = ConfigDict(extra="forbid")

    source_name: str
    tool: ExternalTool
    snapshot_path: str
    snapshot_sha256: str
    ingested_at: str
    collected_at: str | None = None
    stale_after_hours: int = 168
    trust_level: TrustLevel = "trusted"
    schema_version: str = "1.0"
    metadata: dict[str, object] = Field(default_factory=dict)

    @classmethod
    def from_snapshot(
        cls,
        *,
        source_name: str,
        tool: ExternalTool,
        snapshot_path: Path,
        trust_level: TrustLevel,
        stale_after_hours: int,
        collected_at: str | None = None,
        metadata: dict[str, object] | None = None,
    ) -> IntelSourceProvenance:
        payload = snapshot_path.read_bytes()
        return cls(
            source_name=source_name,
            tool=tool,
            snapshot_path=str(snapshot_path.resolve(strict=False)),
            snapshot_sha256=sha256(payload).hexdigest(),
            ingested_at=datetime.now(UTC).isoformat(),
            collected_at=collected_at,
            stale_after_hours=stale_after_hours,
            trust_level=trust_level,
            metadata={} if metadata is None else metadata,
        )


class NormalizedExternalFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding_id: str
    tool: ExternalTool
    source_name: str
    external_id: str | None = None
    rule_id: str | None = None
    title: str
    description: str | None = None
    severity: Literal["critical", "high", "medium", "low", "informational"]
    confidence: float
    cwe_ids: list[str] = Field(default_factory=list)
    category: str | None = None
    file_path: str | None = None
    line: int | None = None
    column: int | None = None
    package_name: str | None = None
    endpoint: str | None = None
    provenance: IntelSourceProvenance
    trust_score: float
    staleness_score: float
    metadata: dict[str, object] = Field(default_factory=dict)


class NormalizationBundle(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: str = "1.0"
    generated_at: str
    source: IntelSourceProvenance
    findings: list[NormalizedExternalFinding] = Field(default_factory=list)
    diagnostics: list[str] = Field(default_factory=list)

    @classmethod
    def create(
        cls,
        *,
        source: IntelSourceProvenance,
        findings: list[NormalizedExternalFinding],
        diagnostics: list[str] | None = None,
    ) -> NormalizationBundle:
        return cls(
            generated_at=datetime.now(UTC).isoformat(),
            source=source,
            findings=findings,
            diagnostics=[] if diagnostics is None else diagnostics,
        )


def normalized_finding_id(raw: ExternalRawFinding, source_name: str) -> str:
    payload = "|".join(
        [
            source_name,
            raw.tool,
            raw.rule_id or "",
            raw.external_id or "",
            raw.file_path or "",
            str(raw.line or 0),
            raw.title,
        ]
    )
    return f"intel-{sha256(payload.encode('utf-8')).hexdigest()[:20]}"
