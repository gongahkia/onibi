from __future__ import annotations

from pydantic import BaseModel, ConfigDict, Field

from piranesi.models.finding import ConfirmedFinding


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


class LegalAssessment(BaseModel):
    model_config = ConfigDict(extra="forbid")

    finding: ConfirmedFinding
    obligations: list[RegulatoryObligation]
    risk_tier: str
    memo_markdown: str
