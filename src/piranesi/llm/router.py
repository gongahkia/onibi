from __future__ import annotations

import logging
from dataclasses import dataclass, field
from typing import TYPE_CHECKING

from piranesi.config import PiranesiConfig
from piranesi.llm.cost import CostTracker

if TYPE_CHECKING:
    from piranesi.models import CandidateFinding

VALID_STAGES = frozenset({"scanner", "detector", "triage", "skeptic", "patcher"})

# CWE difficulty: well-understood vulns are "easy", context-dependent are "hard"
_CWE_DIFFICULTY: dict[str, float] = {
    "CWE-89": 0.2,  # sqli — well-understood
    "CWE-78": 0.3,  # cmdi
    "CWE-79": 0.4,  # xss — context matters
    "CWE-22": 0.3,  # path traversal
    "CWE-94": 0.5,  # code injection
    "CWE-918": 0.7,  # ssrf — context-dependent
    "CWE-942": 0.6,  # cors
    "CWE-1021": 0.5,  # clickjacking
}
_DEFAULT_CWE_DIFFICULTY = 0.5


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
        }

    def _fallback_models(self) -> dict[str, str | None]:
        return {
            "default": self.config.models_fallback.default,
            "scanner": self.config.models_fallback.scanner,
            "detector": self.config.models_fallback.detector,
            "triage": self.config.models_fallback.triage,
            "skeptic": self.config.models_fallback.skeptic,
            "patcher": self.config.models_fallback.patcher,
        }

    def select_triage_model(self, finding: CandidateFinding) -> str:
        """Cost-aware model selection: cheap model for easy findings, expensive for hard."""
        self._check_budget()
        difficulty = estimate_difficulty(finding)
        budget_remaining = self.config.budget.max_cost_usd - self.cost_tracker.total_usd
        triage_model = self._stage_models().get("triage") or "gpt-4o"
        fallback = self.resolve_fallback("triage")
        cheap_model = fallback or triage_model
        expensive_model = triage_model
        if difficulty < 0.3 and budget_remaining > 0.5:
            self._logger.debug(
                "routing easy finding (d=%.2f) to cheap model %s", difficulty, cheap_model
            )
            return cheap_model
        if difficulty > 0.7 or budget_remaining > 2.0:
            self._logger.debug(
                "routing hard finding (d=%.2f) to expensive model %s", difficulty, expensive_model
            )
            return expensive_model
        return triage_model


def estimate_difficulty(finding: CandidateFinding) -> float:
    """Estimate finding difficulty for cost-aware routing.

    Returns 0.0 (easy/cheap model) to 1.0 (hard/expensive model).
    Signals: CWE class, taint path length, sanitizer count.
    """
    cwe_id = finding.vuln_class.split(":")[0].strip() if ":" in finding.vuln_class else None
    cwe_score = _CWE_DIFFICULTY.get(cwe_id or "", _DEFAULT_CWE_DIFFICULTY)
    path_len = len(finding.taint_path)
    if path_len <= 2:
        path_score = 0.1
    elif path_len <= 4:
        path_score = 0.3
    else:
        path_score = min(0.5 + (path_len - 4) * 0.1, 1.0)
    sanitizer_count = sum(1 for step in finding.taint_path if step.sanitizer_applied is not None)
    if sanitizer_count == 0:
        sanitizer_score = 0.0
    elif sanitizer_count == 1:
        sanitizer_score = 0.3
    else:
        sanitizer_score = min(0.3 + (sanitizer_count - 1) * 0.2, 1.0)
    return 0.5 * cwe_score + 0.3 * path_score + 0.2 * sanitizer_score
