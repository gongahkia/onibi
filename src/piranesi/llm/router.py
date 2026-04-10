from __future__ import annotations

import logging
from dataclasses import dataclass, field

from piranesi.config import PiranesiConfig
from piranesi.llm.cost import CostTracker

VALID_STAGES = frozenset({"scanner", "detector", "triage", "skeptic", "patcher", "legal_memo"})


class BudgetExceededError(RuntimeError):
    """Raised when cumulative LLM spend has exhausted the configured budget."""


@dataclass(slots=True)
class ModelRouter:
    config: PiranesiConfig
    cost_tracker: CostTracker
    _warned: bool = field(default=False, init=False, repr=False)
    _logger: logging.Logger = field(
        default_factory=lambda: logging.getLogger("piranesi.llm.router"),
        init=False,
        repr=False,
    )

    def resolve(self, stage: str) -> str:
        self._validate_stage(stage)
        self._check_budget()
        configured_model = self._stage_models().get(stage)
        if configured_model is not None:
            return configured_model
        fallback_model = self.resolve_fallback(stage)
        if fallback_model is None:
            raise ValueError(f"no model configured for stage {stage} and no default fallback")
        return fallback_model

    def resolve_fallback(self, stage: str) -> str | None:
        self._validate_stage(stage)
        fallback_models = self._fallback_models()
        return fallback_models.get(stage) or fallback_models["default"]

    @property
    def total_cost_usd(self) -> float:
        return self.cost_tracker.total_usd

    def _check_budget(self) -> None:
        total_usd = self.cost_tracker.total_usd
        max_cost_usd = self.config.budget.max_cost_usd
        if total_usd >= max_cost_usd:
            raise BudgetExceededError(
                f"budget {max_cost_usd:.2f} USD exceeded, current spend: {total_usd:.4f} USD"
            )
        warn_at_usd = self.config.budget.warn_at_usd
        if warn_at_usd is None or self._warned or total_usd < warn_at_usd:
            return
        self._warned = True
        self._logger.warning(
            "LLM budget warning threshold reached: %.4f / %.4f USD",
            total_usd,
            warn_at_usd,
            extra={
                "event": "llm_budget_warning",
                "total_cost_usd": total_usd,
                "warn_at_usd": warn_at_usd,
            },
        )

    def _validate_stage(self, stage: str) -> None:
        if stage not in VALID_STAGES:
            raise ValueError(f"unknown stage: {stage}")

    def _stage_models(self) -> dict[str, str | None]:
        return {
            "scanner": self.config.models.scanner,
            "detector": self.config.models.detector,
            "triage": self.config.models.triage,
            "skeptic": self.config.models.skeptic,
            "patcher": self.config.models.patcher,
            "legal_memo": self.config.models.legal_memo,
        }

    def _fallback_models(self) -> dict[str, str | None]:
        return {
            "default": self.config.models_fallback.default,
            "scanner": self.config.models_fallback.scanner,
            "detector": self.config.models_fallback.detector,
            "triage": self.config.models_fallback.triage,
            "skeptic": self.config.models_fallback.skeptic,
            "patcher": self.config.models_fallback.patcher,
            "legal_memo": self.config.models_fallback.legal_memo,
        }
