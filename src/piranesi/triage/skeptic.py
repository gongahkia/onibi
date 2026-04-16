from __future__ import annotations

import json
import logging
from dataclasses import dataclass
from typing import TYPE_CHECKING, Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.llm.prompts import skeptic_challenge
from piranesi.llm.router import TokenBudgetExceededError
from piranesi.llm.sanitize import strip_comments
from piranesi.models import CandidateFinding

if TYPE_CHECKING:
    from piranesi.llm.provider import LLMProvider
    from piranesi.llm.router import ModelRouter

_logger = logging.getLogger(__name__)


class SkepticResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    model: str
    verdict: Literal["genuine", "false_positive", "uncertain"]
    confidence: float = Field(ge=0.0, le=1.0)
    reasoning: str
    mitigations_found: list[str]
    remaining_risk: str

    def as_audit_record(self) -> str:
        return json.dumps(self.model_dump(mode="json"), sort_keys=True)


class _SkepticPayload(BaseModel):
    model_config = ConfigDict(extra="forbid")

    verdict: Literal["genuine", "false_positive", "uncertain"]
    confidence: float = Field(ge=0.0, le=1.0)
    reasoning: str
    mitigations_found: list[str]
    remaining_risk: str


@dataclass(slots=True)
class SkepticAgent:
    provider: LLMProvider
    router: ModelRouter | None = None
    model: str | None = None
    detector_model: str | None = None

    def analyze(self, finding: CandidateFinding) -> SkepticResult:
        model = self._resolve_model()
        try:
            response = self.provider.complete(
                stage="skeptic",
                model=model,
                messages=self.build_messages(finding),
                tools=[skeptic_challenge.TOOL_SPEC],
                tool_choice={"type": "function", "function": {"name": skeptic_challenge.TOOL_NAME}},
                max_tokens=384,
            )
        except TokenBudgetExceededError as exc:
            _logger.warning(
                "skeptic: token budget exhausted for model=%s finding=%s; "
                "returning uncertain fallback",
                model,
                finding.id,
                extra={
                    "event": "skeptic_token_budget_exhausted",
                    "model": model,
                    "finding_id": finding.id,
                },
            )
            return SkepticResult(
                model=model,
                verdict="uncertain",
                confidence=0.0,
                reasoning=f"Skeptic LLM stage skipped due to token budget constraints: {exc}",
                mitigations_found=[],
                remaining_risk="Budget-constrained triage fallback; manual review recommended.",
            )

        payload = _parse_skeptic_payload(response.content)
        return SkepticResult(
            model=model,
            verdict=payload.verdict,
            confidence=payload.confidence,
            reasoning=payload.reasoning,
            mitigations_found=payload.mitigations_found,
            remaining_risk=payload.remaining_risk,
        )

    def build_messages(self, finding: CandidateFinding) -> list[dict[str, str]]:
        cwe_id, cwe_name = _split_cwe(finding.vuln_class)
        return skeptic_challenge.render(
            cwe_id=cwe_id,
            cwe_name=cwe_name,
            file_path=finding.sink.location.file,
            line_number=finding.sink.location.line,
            source=finding.source.source_type,
            sink=finding.sink.api_name,
            code_context=strip_comments(_collect_code_context(finding)),
            language=_language_for_path(finding.source.location.file),
        )

    def _resolve_model(self) -> str:
        detector_model = self._resolve_detector_model()
        if self.model is not None:
            if detector_model is not None and self.model == detector_model:
                raise ValueError("skeptic model must differ from detector model")
            return self.model

        if self.router is None:
            raise ValueError("model or router is required to run the skeptic")

        configured = self.router.config.models.skeptic
        if configured is not None:
            if detector_model is not None and configured == detector_model:
                raise ValueError("skeptic model must differ from detector model")
            return configured

        candidates = (
            self.router.resolve_fallback("skeptic"),
            self.router.config.models.triage,
            self.router.resolve_fallback("triage"),
            self.router.resolve_fallback("detector"),
        )
        for candidate in candidates:
            if candidate is None:
                continue
            if detector_model is None:
                return candidate
            if candidate != detector_model and _provider_family(candidate) != _provider_family(
                detector_model
            ):
                return candidate
        raise ValueError("unable to resolve a skeptic model distinct from the detector model")

    def _resolve_detector_model(self) -> str | None:
        if self.detector_model is not None:
            return self.detector_model
        if self.router is None:
            return None
        return self.router.resolve("detector")


def _parse_skeptic_payload(content: str) -> _SkepticPayload:
    try:
        return _SkepticPayload.model_validate_json(content)
    except (ValidationError, ValueError):
        return _SkepticPayload(
            verdict="uncertain",
            confidence=0.0,
            reasoning="Malformed structured skeptic response; escalating for manual verification.",
            mitigations_found=[],
            remaining_risk="Unable to parse skeptic output.",
        )


def _provider_family(model: str) -> str:
    if "/" not in model:
        return model
    return model.split("/", 1)[0]


def _split_cwe(vuln_class: str) -> tuple[str, str]:
    if "(" in vuln_class and vuln_class.endswith(")"):
        head, _, tail = vuln_class.partition("(")
        return head.strip(), tail[:-1].strip()
    if ":" in vuln_class:
        head, _, tail = vuln_class.partition(":")
        return head.strip(), tail.strip()
    return vuln_class, vuln_class


def _collect_code_context(finding: CandidateFinding) -> str:
    snippets: list[str] = []
    seen: set[tuple[str, int, int]] = set()
    locations = [
        finding.source.location,
        *(step.location for step in finding.taint_path),
        finding.sink.location,
    ]
    for location in locations:
        key = (location.file, location.line, location.column)
        if key in seen:
            continue
        seen.add(key)
        snippets.append(location.snippet)
    return "\n\n".join(snippets)


def _language_for_path(file_path: str) -> str:
    if file_path.endswith(".tsx"):
        return "tsx"
    if file_path.endswith(".ts"):
        return "typescript"
    if file_path.endswith(".jsx"):
        return "jsx"
    if file_path.endswith(".js"):
        return "javascript"
    return "text"


__all__ = ["SkepticAgent", "SkepticResult"]
