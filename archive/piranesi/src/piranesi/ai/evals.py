from __future__ import annotations

import re
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from piranesi.ai.redaction import (
    PromptRedactionContext,
    RedactedPromptPayload,
    redact_text_for_prompt,
)
from piranesi.ai.trace import AITraceRecord

AI_EVAL_SCHEMA_VERSION: Literal["piranesi.ai-eval.v1"] = "piranesi.ai-eval.v1"

_FINDING_ID_SEGMENT = r"[A-Za-z0-9_:-]+(?:\.[A-Za-z0-9_:-]+)*"
EVIDENCE_ID_PATTERN = re.compile(rf"\bfinding:{_FINDING_ID_SEGMENT}:evidence:\d+\b")
FINDING_ID_PATTERN = re.compile(rf"\bfinding:{_FINDING_ID_SEGMENT}\b")


class AIEvaluationError(ValueError):
    """Raised when AI output cannot be accepted safely."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class AIEvaluationFailure(_StrictModel):
    code: str
    message: str


class AIEvaluationResult(_StrictModel):
    schema_version: Literal["piranesi.ai-eval.v1"] = AI_EVAL_SCHEMA_VERSION
    passed: bool
    failures: list[AIEvaluationFailure] = Field(default_factory=list)

    def require_passed(self) -> None:
        if self.passed:
            return
        joined = "; ".join(f"{failure.code}: {failure.message}" for failure in self.failures)
        raise AIEvaluationError(joined)


def evaluate_ai_output(
    prompt: RedactedPromptPayload,
    *,
    output_text: str,
) -> AIEvaluationResult:
    failures: list[AIEvaluationFailure] = []
    redaction_context = PromptRedactionContext()
    redacted = redact_text_for_prompt(output_text, context=redaction_context, field="ai.eval")
    if redacted != output_text:
        categories = sorted({event.category for event in redaction_context.events})
        failures.append(
            AIEvaluationFailure(
                code="unredacted-sensitive-output",
                message=f"output still contains sensitive categories: {', '.join(categories)}",
            )
        )

    allowed_finding_ids = {finding.id for finding in prompt.findings}
    allowed_evidence_ids = {
        evidence.evidence_id for finding in prompt.findings for evidence in finding.evidence
    }
    output_evidence_ids = set(EVIDENCE_ID_PATTERN.findall(output_text))
    invented_evidence = sorted(output_evidence_ids - allowed_evidence_ids)
    if invented_evidence:
        failures.append(
            AIEvaluationFailure(
                code="invented-evidence-id",
                message=f"output references unknown evidence IDs: {', '.join(invented_evidence)}",
            )
        )

    output_without_evidence_ids = EVIDENCE_ID_PATTERN.sub("", output_text)
    output_finding_ids = set(FINDING_ID_PATTERN.findall(output_without_evidence_ids))
    invented_findings = sorted(output_finding_ids - allowed_finding_ids)
    if invented_findings:
        failures.append(
            AIEvaluationFailure(
                code="invented-finding-id",
                message=f"output references unknown finding IDs: {', '.join(invented_findings)}",
            )
        )

    return AIEvaluationResult(passed=not failures, failures=failures)


def require_trace_approved_for_report_change(
    trace: AITraceRecord,
    *,
    target_field: str,
) -> None:
    if trace.approval_state != "accepted":
        raise AIEvaluationError(
            f"AI trace {trace.trace_id} is {trace.approval_state}; "
            f"report field {target_field!r} requires accepted approval state"
        )
    raw_field = trace.target.get("field")
    if raw_field != target_field:
        raise AIEvaluationError(
            f"AI trace {trace.trace_id} targets {raw_field!r}; cannot apply it to {target_field!r}"
        )


__all__ = [
    "AI_EVAL_SCHEMA_VERSION",
    "AIEvaluationError",
    "AIEvaluationFailure",
    "AIEvaluationResult",
    "evaluate_ai_output",
    "require_trace_approved_for_report_change",
]
