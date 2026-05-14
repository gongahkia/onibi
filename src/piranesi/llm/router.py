from __future__ import annotations

import json
import logging
from collections.abc import Mapping, Sequence
from dataclasses import dataclass, field
from threading import Lock
from typing import TYPE_CHECKING, Any

from piranesi.config import PiranesiConfig
from piranesi.llm.cost import CostTracker

if TYPE_CHECKING:
    from piranesi.models import CandidateFinding

VALID_STAGES = frozenset({"scanner", "detector", "triage", "skeptic", "patcher"})

# Rough tokenizer estimate that biases conservative for budget gating.
_ESTIMATED_CHARS_PER_TOKEN = 4
_MESSAGE_OVERHEAD_TOKENS = 4
_MIN_TRUNCATED_MESSAGE_CHARS = 64

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


class TokenBudgetExceededError(RuntimeError):
    """Raised when the configured LLM token budget cannot accommodate a request."""


@dataclass(frozen=True, slots=True)
class TokenBudgetReservation:
    stage: str
    messages: tuple[dict[str, Any], ...]
    max_tokens: int
    prompt_tokens_estimate: int
    reserved_tokens: int
    context_omitted: bool
    max_tokens_clamped: bool


@dataclass(slots=True)
class ModelRouter:
    config: PiranesiConfig
    cost_tracker: CostTracker
    _warned: bool = field(default=False, init=False, repr=False)
    _token_budget_used: int = field(default=0, init=False, repr=False)
    _lock: Lock = field(default_factory=Lock, init=False, repr=False)
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

    @property
    def used_tokens(self) -> int:
        with self._lock:
            return self._token_budget_used

    @property
    def remaining_tokens(self) -> int:
        with self._lock:
            return max(0, self.config.budget.max_tokens - self._token_budget_used)

    def estimate_prompt_tokens(self, messages: Sequence[Mapping[str, Any]]) -> int:
        total = 0
        for message in messages:
            role = str(message.get("role", ""))
            total += _MESSAGE_OVERHEAD_TOKENS + _estimate_text_tokens(role)
            content = _normalize_message_content(message.get("content", ""))
            total += _estimate_text_tokens(content)
        return max(total, 1)

    def reserve_completion(
        self,
        *,
        stage: str,
        messages: Sequence[Mapping[str, Any]],
        requested_max_tokens: int,
        min_completion_tokens: int = 64,
    ) -> TokenBudgetReservation:
        self._validate_stage(stage)
        self._check_budget()
        normalized_messages = [dict(message) for message in messages]
        requested = max(1, int(requested_max_tokens))
        minimum_completion = max(1, int(min_completion_tokens))

        with self._lock:
            remaining_before = self.config.budget.max_tokens - self._token_budget_used
            if remaining_before <= 0:
                raise TokenBudgetExceededError(
                    f"token budget exhausted for stage={stage}: "
                    f"used={self._token_budget_used} / max={self.config.budget.max_tokens}"
                )
            if remaining_before <= minimum_completion:
                raise TokenBudgetExceededError(
                    "token budget exhausted before minimum completion allocation for "
                    f"stage={stage} (remaining={remaining_before}, "
                    f"minimum_completion_tokens={minimum_completion})"
                )
            max_prompt_tokens = max(1, remaining_before - minimum_completion)
            prompt_tokens = self.estimate_prompt_tokens(normalized_messages)
            adjusted_messages = [dict(message) for message in normalized_messages]
            if prompt_tokens > max_prompt_tokens:
                adjusted_messages = self._truncate_messages_to_fit(
                    adjusted_messages,
                    prompt_token_limit=max_prompt_tokens,
                )
                prompt_tokens = self.estimate_prompt_tokens(adjusted_messages)
            context_omitted = adjusted_messages != normalized_messages
            if prompt_tokens > max_prompt_tokens:
                raise TokenBudgetExceededError(
                    "token budget exhausted: prompt context remains too large "
                    f"for stage={stage} (estimated_prompt_tokens={prompt_tokens}, "
                    f"remaining={remaining_before})"
                )
            allowed_completion = min(requested, remaining_before - prompt_tokens)
            if allowed_completion < minimum_completion:
                raise TokenBudgetExceededError(
                    "token budget exhausted before minimum completion allocation for "
                    f"stage={stage} (remaining={remaining_before}, "
                    f"estimated_prompt_tokens={prompt_tokens}, "
                    f"min_completion_tokens={minimum_completion})"
                )

            reserved_tokens = prompt_tokens + allowed_completion
            self._token_budget_used += reserved_tokens
            remaining_after = max(0, self.config.budget.max_tokens - self._token_budget_used)

        max_tokens_clamped = allowed_completion < requested
        if context_omitted or max_tokens_clamped:
            self._logger.warning(
                "LLM token budget adjusted: stage=%s prompt_est=%d completion_cap=%d "
                "requested_completion=%d remaining_tokens=%d context_omitted=%s",
                stage,
                prompt_tokens,
                allowed_completion,
                requested,
                remaining_after,
                context_omitted,
                extra={
                    "event": "llm_token_budget_adjusted",
                    "stage": stage,
                    "prompt_tokens_estimate": prompt_tokens,
                    "completion_tokens_cap": allowed_completion,
                    "requested_completion_tokens": requested,
                    "remaining_tokens": remaining_after,
                    "context_omitted": context_omitted,
                },
            )

        return TokenBudgetReservation(
            stage=stage,
            messages=tuple(dict(message) for message in adjusted_messages),
            max_tokens=allowed_completion,
            prompt_tokens_estimate=prompt_tokens,
            reserved_tokens=reserved_tokens,
            context_omitted=context_omitted,
            max_tokens_clamped=max_tokens_clamped,
        )

    def settle_completion(
        self,
        reservation: TokenBudgetReservation,
        *,
        prompt_tokens: int,
        response_tokens: int,
    ) -> None:
        actual_total = max(0, int(prompt_tokens)) + max(0, int(response_tokens))
        if actual_total <= 0:
            return

        with self._lock:
            self._token_budget_used += actual_total - reservation.reserved_tokens
            if self._token_budget_used < 0:
                self._token_budget_used = 0
            used_tokens = self._token_budget_used
            max_tokens = self.config.budget.max_tokens

        if used_tokens > max_tokens:
            self._logger.warning(
                "LLM token budget estimate undercounted actual usage: used=%d max=%d over=%d",
                used_tokens,
                max_tokens,
                used_tokens - max_tokens,
                extra={
                    "event": "llm_token_budget_estimate_undercount",
                    "used_tokens": used_tokens,
                    "max_tokens": max_tokens,
                    "over_tokens": used_tokens - max_tokens,
                },
            )

    def _truncate_messages_to_fit(
        self,
        messages: list[dict[str, Any]],
        *,
        prompt_token_limit: int,
    ) -> list[dict[str, Any]]:
        adjusted = [dict(message) for message in messages]
        candidate_indexes = [
            index
            for index, message in enumerate(adjusted)
            if str(message.get("role", "")).lower() != "system"
        ]
        if not candidate_indexes:
            candidate_indexes = list(range(len(adjusted)))

        for index in reversed(candidate_indexes):
            current_prompt_tokens = self.estimate_prompt_tokens(adjusted)
            if current_prompt_tokens <= prompt_token_limit:
                break

            content = _normalize_message_content(adjusted[index].get("content", ""))
            if not content:
                continue

            over_tokens = current_prompt_tokens - prompt_token_limit
            trim_chars = max(
                _MIN_TRUNCATED_MESSAGE_CHARS,
                over_tokens * _ESTIMATED_CHARS_PER_TOKEN + 48,
            )
            if len(content) <= trim_chars + _MIN_TRUNCATED_MESSAGE_CHARS:
                adjusted[index]["content"] = "[context omitted due token budget]"
                continue

            keep_chars = max(_MIN_TRUNCATED_MESSAGE_CHARS, len(content) - trim_chars)
            adjusted[index]["content"] = (
                content[:keep_chars].rstrip() + "\n\n[context truncated due token budget]"
            )

        return adjusted

    def _check_budget(self) -> None:
        total_usd = self.cost_tracker.total_usd
        max_cost_usd = self.config.budget.max_cost_usd
        if total_usd >= max_cost_usd:
            raise BudgetExceededError(
                f"budget {max_cost_usd:.2f} USD exceeded, current spend: {total_usd:.4f} USD"
            )
        warn_at_usd = self.config.budget.warn_at_usd
        should_warn = False
        with self._lock:
            if warn_at_usd is not None and not self._warned and total_usd >= warn_at_usd:
                self._warned = True
                should_warn = True
        if not should_warn:
            return
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


def _estimate_text_tokens(text: str) -> int:
    if not text:
        return 0
    return max(1, (len(text) + (_ESTIMATED_CHARS_PER_TOKEN - 1)) // _ESTIMATED_CHARS_PER_TOKEN)


def _normalize_message_content(content: Any) -> str:
    if isinstance(content, str):
        return content
    if content is None:
        return ""
    return json.dumps(content, default=str, ensure_ascii=True, separators=(",", ":"))
