from __future__ import annotations

import logging
import math
from concurrent.futures import ThreadPoolExecutor
from dataclasses import dataclass, field
from pathlib import Path
from typing import TYPE_CHECKING, Any, Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.llm.prompts import triage_classify
from piranesi.llm.router import TokenBudgetExceededError
from piranesi.llm.sanitize import strip_comments
from piranesi.models import CandidateFinding, SandboxResult, TriagedFinding

if TYPE_CHECKING:
    from collections.abc import Mapping

    from piranesi.llm.provider import LLMProvider
    from piranesi.llm.router import ModelRouter
    from piranesi.triage.skeptic import SkepticAgent, SkepticResult

_EPSILON = 1e-6
_TP_THRESHOLD = 0.7
_FP_THRESHOLD = 0.3
_logger = logging.getLogger(__name__)


class _TriagePayload(BaseModel):
    model_config = ConfigDict(extra="forbid")

    verdict: Literal["true_positive", "false_positive"]
    confidence: float = Field(ge=0.0, le=1.0)
    explanation: str


class ModelVote(BaseModel):
    model_config = ConfigDict(extra="forbid")

    model: str
    verdict: Literal["true_positive", "false_positive"]
    confidence: float = Field(ge=0.0, le=1.0)
    explanation: str
    raw_true_positive_score: float = Field(ge=0.0, le=1.0)
    calibrated_true_positive_score: float | None = Field(default=None, ge=0.0, le=1.0)
    weight: float = Field(default=1.0, ge=0.0)
    temperature: float | None = Field(default=None, gt=0.0)


class EnsembleDecision(BaseModel):
    model_config = ConfigDict(extra="forbid")

    verdict: Literal["true_positive", "false_positive", "uncertain"]
    ensemble_score: float = Field(ge=0.0, le=1.0)
    escalated: bool
    votes: list[ModelVote]
    escalation_model: str | None = None


@dataclass(slots=True)
class CalibratedEnsembleVoter:
    provider: LLMProvider
    router: ModelRouter | None = None
    models: tuple[str, ...] = ()
    num_models: int = 3
    calibration_temperatures: Mapping[str, float] | None = None
    historical_precision: Mapping[str, Mapping[str, float]] | None = None
    escalation_model: str | None = None
    max_workers: int | None = None
    calibration_dir: Path | None = None
    _calibration_cache: dict[str, Any] = field(default_factory=dict, init=False, repr=False)

    def classify(self, finding: CandidateFinding) -> EnsembleDecision:
        models = self._resolve_models()
        if len(models) == 1:
            vote = self._run_model(models[0], finding)
            vote = self._apply_platt_if_available(vote, finding)
            return EnsembleDecision(
                verdict=vote.verdict,
                ensemble_score=vote.calibrated_true_positive_score or vote.raw_true_positive_score,
                escalated=False,
                votes=[vote],
            )

        votes = self._collect_votes(models, finding)
        # apply Platt calibration from eval/calibration/ if available
        platt_votes = [self._apply_platt_if_available(v, finding) for v in votes]
        has_platt = any(v.calibrated_true_positive_score is not None for v in platt_votes)
        if not self._has_calibration(models) and not has_platt:
            tp_votes = sum(1 for vote in platt_votes if vote.verdict == "true_positive")
            fp_votes = len(platt_votes) - tp_votes
            score = tp_votes / len(platt_votes)
            if tp_votes > fp_votes:
                return EnsembleDecision(
                    verdict="true_positive",
                    ensemble_score=score,
                    escalated=False,
                    votes=platt_votes,
                )
            if fp_votes > tp_votes:
                return EnsembleDecision(
                    verdict="false_positive",
                    ensemble_score=score,
                    escalated=False,
                    votes=platt_votes,
                )
            return self._escalate_or_defer(finding, votes=platt_votes, score=score)

        calibrated_votes = [
            self._apply_calibration(vote, finding.vuln_class) for vote in platt_votes
        ]
        score = _weighted_average(calibrated_votes)
        # use optimized thresholds from calibration data if available
        tp_thresh, fp_thresh = self._resolve_thresholds(models)
        verdict = _threshold_verdict(score, tp_threshold=tp_thresh, fp_threshold=fp_thresh)
        if verdict != "uncertain":
            return EnsembleDecision(
                verdict=verdict,
                ensemble_score=score,
                escalated=False,
                votes=calibrated_votes,
            )
        return self._escalate_or_defer(finding, votes=calibrated_votes, score=score)

    def triage_finding(
        self,
        finding: CandidateFinding,
        *,
        skeptic: SkepticAgent | None = None,
        sandbox_result: SandboxResult | None = None,
    ) -> TriagedFinding:
        if sandbox_result is not None and sandbox_result.confirmed:
            return TriagedFinding(
                finding=finding,
                triage_verdict="confirmed",
                skeptic_analysis="",
                ensemble_score=1.0,
                escalated=False,
                triage_override_logged=True,
            )

        skeptic_result: SkepticResult | None = None
        if skeptic is None:
            ensemble_result = self.classify(finding)
        else:
            with ThreadPoolExecutor(max_workers=2) as executor:
                ensemble_future = executor.submit(self.classify, finding)
                skeptic_future = executor.submit(skeptic.analyze, finding)
                ensemble_result = ensemble_future.result()
                skeptic_result = skeptic_future.result()

        triage_verdict = self._combine_verdicts(
            ensemble_verdict=ensemble_result.verdict,
            skeptic_verdict=None if skeptic_result is None else skeptic_result.verdict,
        )
        skeptic_analysis = "" if skeptic_result is None else skeptic_result.as_audit_record()
        return TriagedFinding(
            finding=finding,
            triage_verdict=triage_verdict,
            skeptic_analysis=skeptic_analysis,
            ensemble_score=ensemble_result.ensemble_score,
            escalated=ensemble_result.escalated,
        )

    def _combine_verdicts(
        self,
        *,
        ensemble_verdict: Literal["true_positive", "false_positive", "uncertain"],
        skeptic_verdict: Literal["genuine", "false_positive", "uncertain"] | None,
    ) -> str:
        if skeptic_verdict is None:
            if ensemble_verdict == "uncertain":
                return "true_positive"
            return ensemble_verdict
        if ensemble_verdict == "false_positive" and skeptic_verdict == "false_positive":
            return "false_positive"
        return "true_positive"

    def _collect_votes(self, models: tuple[str, ...], finding: CandidateFinding) -> list[ModelVote]:
        worker_count = self.max_workers or len(models)
        with ThreadPoolExecutor(max_workers=min(worker_count, len(models))) as executor:
            futures = [executor.submit(self._run_model, model, finding) for model in models]
            return [future.result() for future in futures]

    def _run_model(self, model: str, finding: CandidateFinding) -> ModelVote:
        try:
            response = self.provider.complete(
                stage="triage",
                model=model,
                messages=self._build_messages(finding),
                tools=[triage_classify.TOOL_SPEC],
                tool_choice={"type": "function", "function": {"name": triage_classify.TOOL_NAME}},
                max_tokens=512,
            )
        except TokenBudgetExceededError as exc:
            _logger.warning(
                "triage: token budget exhausted for model=%s finding=%s; "
                "using conservative true-positive fallback",
                model,
                finding.id,
                extra={
                    "event": "triage_token_budget_exhausted",
                    "model": model,
                    "finding_id": finding.id,
                },
            )
            return ModelVote(
                model=model,
                verdict="true_positive",
                confidence=0.5,
                explanation=f"LLM triage skipped due to token budget constraints: {exc}",
                raw_true_positive_score=0.5,
            )

        payload = _parse_triage_payload(response.content)
        return ModelVote(
            model=model,
            verdict=payload.verdict,
            confidence=payload.confidence,
            explanation=payload.explanation,
            raw_true_positive_score=_true_positive_probability(payload),
        )

    def _build_messages(self, finding: CandidateFinding) -> list[dict[str, str]]:
        code_context = strip_comments(_collect_code_context(finding))
        return triage_classify.render(
            finding_summary=_finding_summary(finding),
            taint_path=_taint_path_summary(finding),
            code_context=code_context,
            language=_language_for_path(finding.source.location.file),
        )

    def _apply_calibration(self, vote: ModelVote, vuln_class: str) -> ModelVote:
        temperatures = self.calibration_temperatures or {}
        temperature = temperatures[vote.model]
        weight = _precision_weight(self.historical_precision, vuln_class, vote.model)
        calibrated = _temperature_scale(vote.raw_true_positive_score, temperature)
        return vote.model_copy(
            update={
                "calibrated_true_positive_score": calibrated,
                "temperature": temperature,
                "weight": weight,
            }
        )

    def _load_platt_calibration(self, model: str) -> Any | None:
        """Load Platt calibration data for a model from eval/calibration/."""
        if model in self._calibration_cache:
            return self._calibration_cache[model]
        try:
            from eval.calibrate import load_calibration

            cal = load_calibration(model, self.calibration_dir)
            self._calibration_cache[model] = cal
            return cal
        except (ImportError, Exception):
            self._calibration_cache[model] = None
            return None

    def _apply_platt_if_available(self, vote: ModelVote, finding: CandidateFinding) -> ModelVote:
        """Apply Platt scaling from calibration data if available."""
        if vote.calibrated_true_positive_score is not None:
            return vote  # already calibrated
        cal = self._load_platt_calibration(vote.model)
        if cal is None:
            return vote
        cwe_id = finding.vuln_class.split(":")[0].strip() if ":" in finding.vuln_class else None
        calibrated = cal.calibrate(vote.raw_true_positive_score, cwe_id)
        return vote.model_copy(update={"calibrated_true_positive_score": calibrated})

    def _resolve_thresholds(self, models: tuple[str, ...]) -> tuple[float, float]:
        """Get optimized thresholds from calibration data, or defaults."""
        for model in models:
            cal = self._load_platt_calibration(model)
            if cal is not None:
                return cal.optimal_tp_threshold, cal.optimal_fp_threshold
        return _TP_THRESHOLD, _FP_THRESHOLD

    def _escalate_or_defer(
        self,
        finding: CandidateFinding,
        *,
        votes: list[ModelVote],
        score: float,
    ) -> EnsembleDecision:
        escalation_model = self._resolve_escalation_model()
        if escalation_model is None:
            return EnsembleDecision(
                verdict="uncertain",
                ensemble_score=score,
                escalated=False,
                votes=votes,
            )
        escalation_vote = self._run_model(escalation_model, finding)
        return EnsembleDecision(
            verdict=escalation_vote.verdict,
            ensemble_score=escalation_vote.raw_true_positive_score,
            escalated=True,
            votes=[*votes, escalation_vote],
            escalation_model=escalation_model,
        )

    def _has_calibration(self, models: tuple[str, ...]) -> bool:
        temperatures = self.calibration_temperatures
        if not temperatures:
            return False
        return all(model in temperatures for model in models)

    def _resolve_models(self) -> tuple[str, ...]:
        if self.models:
            return self.models[: max(1, self.num_models)]
        if self.router is None:
            raise ValueError("models or router is required to run the ensemble")

        configured = self.router.config.models.triage
        if "," in configured:
            parsed = tuple(model.strip() for model in configured.split(",") if model.strip())
            if parsed:
                return parsed[: max(1, self.num_models)]

        primary = self.router.resolve("triage")
        fallback = self.router.resolve_fallback("triage")
        models = [primary]
        if fallback is not None and fallback != primary:
            models.append(fallback)
        return tuple(models[: max(1, self.num_models)])

    def _resolve_escalation_model(self) -> str | None:
        if self.escalation_model is not None:
            return self.escalation_model
        if self.router is None:
            return None
        return self.router.resolve_fallback("triage")


def _parse_triage_payload(content: str) -> _TriagePayload:
    try:
        return _TriagePayload.model_validate_json(content)
    except (ValidationError, ValueError):
        return _TriagePayload(
            verdict="true_positive",
            confidence=0.5,
            explanation="Malformed structured triage response; treating finding as uncertain.",
        )


def _true_positive_probability(payload: _TriagePayload) -> float:
    if payload.verdict == "true_positive":
        return payload.confidence
    return 1.0 - payload.confidence


def _temperature_scale(probability: float, temperature: float) -> float:
    if temperature <= 0:
        raise ValueError("temperature must be positive")
    bounded = min(max(probability, _EPSILON), 1.0 - _EPSILON)
    logit = math.log(bounded / (1.0 - bounded))
    return 1.0 / (1.0 + math.exp(-(logit / temperature)))


def _weighted_average(votes: list[ModelVote]) -> float:
    numerator = 0.0
    denominator = 0.0
    for vote in votes:
        calibrated = (
            vote.raw_true_positive_score
            if vote.calibrated_true_positive_score is None
            else vote.calibrated_true_positive_score
        )
        weight = vote.weight if vote.weight > 0 else 1.0
        numerator += weight * calibrated
        denominator += weight
    if denominator == 0:
        return 0.5
    return numerator / denominator


def _threshold_verdict(
    score: float,
    *,
    tp_threshold: float = _TP_THRESHOLD,
    fp_threshold: float = _FP_THRESHOLD,
) -> Literal["true_positive", "false_positive", "uncertain"]:
    if score >= tp_threshold:
        return "true_positive"
    if score <= fp_threshold:
        return "false_positive"
    return "uncertain"


def _precision_weight(
    precision_table: Mapping[str, Mapping[str, float]] | None,
    vuln_class: str,
    model: str,
) -> float:
    if precision_table is None:
        return 1.0
    by_model = precision_table.get(vuln_class)
    if by_model is None:
        return 1.0
    weight = by_model.get(model, 1.0)
    return weight if weight > 0 else 1.0


def _finding_summary(finding: CandidateFinding) -> str:
    return (
        f"{finding.vuln_class} from {finding.source.source_type} to "
        f"{finding.sink.api_name} in {finding.sink.location.file}:{finding.sink.location.line}"
    )


def _taint_path_summary(finding: CandidateFinding) -> str:
    path = [finding.source.source_type]
    path.extend(step.operation for step in finding.taint_path)
    path.append(finding.sink.api_name)
    return " -> ".join(path)


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


__all__ = [
    "CalibratedEnsembleVoter",
    "EnsembleDecision",
    "ModelVote",
    "_temperature_scale",
]
