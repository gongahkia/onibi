from __future__ import annotations

from datetime import UTC, datetime
from hashlib import sha256
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

ExternalTool = Literal["sarif", "codeql_sarif", "semgrep", "trivy", "zap"]


class AdapterDiagnostic(BaseModel):
    model_config = ConfigDict(extra="forbid")

    level: Literal["warning", "error"]
    message: str
    context: dict[str, object] = Field(default_factory=dict)


class ExternalRawFinding(BaseModel):
    model_config = ConfigDict(extra="forbid")

    tool: ExternalTool
    external_id: str | None = None
    rule_id: str | None = None
    title: str
    description: str | None = None
    severity: Literal["critical", "high", "medium", "low", "informational"] = "medium"
    confidence: float | None = None
    cwe_ids: list[str] = Field(default_factory=list)
    category: str | None = None
    file_path: str | None = None
    line: int | None = None
    column: int | None = None
    package_name: str | None = None
    endpoint: str | None = None
    metadata: dict[str, object] = Field(default_factory=dict)

    def stable_key(self) -> str:
        payload = "|".join(
            [
                self.tool,
                self.rule_id or "",
                self.external_id or "",
                self.file_path or "",
                str(self.line or 0),
                self.title,
            ]
        )
        return sha256(payload.encode("utf-8")).hexdigest()


class AdapterParseResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    tool: ExternalTool
    source_path: str
    parsed_at: str
    findings: list[ExternalRawFinding] = Field(default_factory=list)
    diagnostics: list[AdapterDiagnostic] = Field(default_factory=list)

    @classmethod
    def empty(cls, *, tool: ExternalTool, source_path: Path) -> AdapterParseResult:
        return cls(
            tool=tool,
            source_path=str(source_path.resolve(strict=False)),
            parsed_at=datetime.now(UTC).isoformat(),
            findings=[],
            diagnostics=[],
        )
