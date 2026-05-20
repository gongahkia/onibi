from __future__ import annotations

import json
import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from piranesi.pff import PFF_SCHEMA_VERSION, validate_pff_document
from piranesi.workspace import Confidence, FindingStatus, Severity

SECRET_PATTERN = re.compile(
    r"(?i)(token|secret|password|passwd|api[_-]?key|session|cookie)\s*[:=]\s*([^\s]+)"
)


class AdapterSdkError(ValueError):
    """Raised when an adapter SDK document cannot be built safely."""


@dataclass(frozen=True, slots=True)
class AdapterSourceReference:
    tool: str
    input_sha256: str
    raw_path: str
    locator: str | None = None
    metadata: dict[str, Any] = field(default_factory=dict)

    def as_pff(self) -> dict[str, Any]:
        return {
            "tool": self.tool,
            "input_sha256": self.input_sha256,
            "raw_path": self.raw_path,
            "locator": self.locator,
            "metadata": self.metadata,
        }


@dataclass(frozen=True, slots=True)
class AdapterEvidence:
    kind: str
    value: str
    redacted: bool
    locator: str | None = None

    def as_pff(self) -> dict[str, Any]:
        return {
            "kind": self.kind,
            "value": self.value,
            "redacted": self.redacted,
            "locator": self.locator,
        }


class PffAdapterBuilder:
    def __init__(
        self,
        *,
        producer_name: str,
        producer_version: str,
        engagement: dict[str, Any] | None = None,
    ) -> None:
        if not producer_name.strip():
            raise AdapterSdkError("producer_name is required")
        if not producer_version.strip():
            raise AdapterSdkError("producer_version is required")
        self._producer = {"name": producer_name, "version": producer_version}
        self._engagement = dict(engagement or {})
        self._findings: list[dict[str, Any]] = []
        self._known_gaps: list[str] = []

    def add_known_gap(self, gap: str) -> None:
        if gap.strip():
            self._known_gaps.append(gap.strip())

    def add_finding(
        self,
        *,
        finding_id: str,
        title: str,
        severity: Severity = "info",
        confidence: Confidence = "tool-observed",
        status: FindingStatus = "open",
        asset: str | None = None,
        description: str | None = None,
        remediation: str | None = None,
        evidence: list[AdapterEvidence] | None = None,
        source_references: list[AdapterSourceReference] | None = None,
        weakness_ids: list[str] | None = None,
        references: list[str] | None = None,
        tags: list[str] | None = None,
        provenance: dict[str, Any] | None = None,
    ) -> None:
        if not finding_id.strip():
            raise AdapterSdkError("finding_id is required")
        if not title.strip():
            raise AdapterSdkError("title is required")
        if not source_references:
            raise AdapterSdkError("source_references are required for PFF adapter findings")
        assets = [asset] if asset else []
        self._findings.append(
            {
                "id": finding_id,
                "title": title,
                "severity": severity,
                "confidence": confidence,
                "status": status,
                "description": redact_text(description) if description else None,
                "remediation": redact_text(remediation) if remediation else None,
                "assets": assets,
                "service": None,
                "affected_instances": [{"asset": asset, "metadata": {}}] if asset else [],
                "weakness_ids": weakness_ids or [],
                "references": references or [],
                "tags": sorted(set(tags or [])),
                "evidence": [item.as_pff() for item in evidence or []],
                "source_references": [item.as_pff() for item in source_references],
                "provenance": {
                    **(provenance or {}),
                    "adapter_sdk": True,
                },
                "chain_of_custody": None,
                "retest_status": None,
            }
        )

    def to_document(self) -> dict[str, Any]:
        document = {
            "schema_version": PFF_SCHEMA_VERSION,
            "producer": self._producer,
            "engagement": self._engagement,
            "findings": self._findings,
            "known_gaps": self._known_gaps,
        }
        validate_pff_document(document)
        return document

    def write_json(self, path: Path | str) -> Path:
        output = Path(path)
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(
            json.dumps(self.to_document(), indent=2, sort_keys=True) + "\n",
            encoding="utf-8",
        )
        return output


def source_reference(
    *,
    tool: str,
    input_sha256: str,
    raw_path: str,
    locator: str | None = None,
    metadata: dict[str, Any] | None = None,
) -> AdapterSourceReference:
    return AdapterSourceReference(
        tool=tool,
        input_sha256=input_sha256,
        raw_path=raw_path,
        locator=locator,
        metadata=dict(metadata or {}),
    )


def evidence_snippet(
    *,
    kind: str,
    value: str,
    locator: str | None = None,
    redact: bool = True,
) -> AdapterEvidence:
    detected_redaction = redact_text(value)
    redacted_value = detected_redaction if redact or detected_redaction != value else value
    return AdapterEvidence(
        kind=kind,
        value=redacted_value,
        redacted=redact or redacted_value != value,
        locator=locator,
    )


def redact_text(value: str) -> str:
    return SECRET_PATTERN.sub(r"\1=[redacted]", value)


__all__ = [
    "AdapterEvidence",
    "AdapterSdkError",
    "AdapterSourceReference",
    "PffAdapterBuilder",
    "evidence_snippet",
    "redact_text",
    "source_reference",
]
