from __future__ import annotations

from pathlib import Path
from typing import Any, Literal

from piranesi import __version__
from piranesi.workspace import NormalizedFinding, WorkspaceState, load_workspace

PFF_SCHEMA_VERSION: Literal["piranesi.pff.v0"] = "piranesi.pff.v0"
PFF_SCHEMA_PATH = Path(__file__).resolve().parents[2] / "docs" / "schemas" / "pff-v0.schema.json"


def build_pff_document(root_or_state: Path | str | WorkspaceState) -> dict[str, Any]:
    state = (
        root_or_state
        if isinstance(root_or_state, WorkspaceState)
        else load_workspace(root_or_state)
    )
    return {
        "schema_version": PFF_SCHEMA_VERSION,
        "producer": {
            "name": "piranesi",
            "version": __version__,
        },
        "engagement": state.workspace.engagement.model_dump(mode="json"),
        "findings": [_pff_finding(finding) for finding in state.findings.findings],
        "known_gaps": [],
    }


def _pff_finding(finding: NormalizedFinding) -> dict[str, Any]:
    assets = [finding.asset] if finding.asset is not None else []
    return {
        "id": finding.id,
        "title": finding.title,
        "severity": finding.severity,
        "confidence": finding.confidence,
        "status": finding.status,
        "description": finding.description,
        "remediation": finding.remediation,
        "assets": assets,
        "service": finding.service.model_dump(mode="json") if finding.service else None,
        "affected_instances": [
            instance.model_dump(mode="json") for instance in finding.affected_instances
        ],
        "weakness_ids": finding.weakness_ids,
        "references": finding.references,
        "tags": finding.tags,
        "evidence": [item.model_dump(mode="json") for item in finding.evidence],
        "source_references": [
            reference.model_dump(mode="json") for reference in finding.source_references
        ],
        "provenance": finding.provenance,
        "chain_of_custody": None,
        "retest_status": finding.provenance.get("retest_status"),
    }


__all__ = ["PFF_SCHEMA_PATH", "PFF_SCHEMA_VERSION", "build_pff_document"]
