from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from piranesi.models.finding import ConfirmedFinding


class ComplianceMappingMetadata(BaseModel):
    model_config = ConfigDict(extra="forbid")

    framework_name: str
    framework_version: str | None = None
    control_id: str
    mapping_rationale: str
    last_reviewed: str | None = None
    reviewer: str | None = None
    source: str | None = None
    confidence: float = Field(default=0.7, ge=0.0, le=1.0)


class RegulatoryObligation(BaseModel):
    model_config = ConfigDict(extra="forbid")

    framework: str
    section: str
    obligation_text: str
    data_categories_affected: list[str]
    penalty_range: str
    notification_timeline: str | None = None
    enforcement_precedents: list[str]
    rule_id: str | None = None
    consequences: list[str] = Field(default_factory=list)
    severity_modifier: str | None = None
    evidence_role: Literal["compliance_support"] = "compliance_support"
    mapping_metadata: ComplianceMappingMetadata | None = None


class LegalAssessment(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding: ConfirmedFinding
    obligations: list[RegulatoryObligation]
    risk_tier: str
    memo_markdown: str
