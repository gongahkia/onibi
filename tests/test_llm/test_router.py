from __future__ import annotations

import logging
from typing import Any

import pytest

from piranesi.config import (
    BudgetConfig,
    ModelFallbackConfig,
    ModelsConfig,
    PiranesiConfig,
    load_config,
)
from piranesi.llm.cost import CostTracker
from piranesi.llm.router import BudgetExceededError, ModelRouter


def test_router_resolves_models_from_stage_config_and_fallback(config_file: Any) -> None:
    path = config_file(
        """
[models]
scanner = "scanner-model"
detector = "detector-model"
triage = "triage-model"
patcher = "patcher-model"

[models.fallback]
default = "fallback-default"
skeptic = "skeptic-fallback"

[models.budget]
max_cost_usd = 8.5
warn_at_usd = 4.25
"""
    )
    config = load_config(path)
    router = ModelRouter(config=config, cost_tracker=CostTracker())

    assert config.budget.max_cost_usd == 8.5
    assert config.budget.warn_at_usd == 4.25
    assert router.resolve("scanner") == "scanner-model"
    assert router.resolve("detector") == "detector-model"
    assert router.resolve("skeptic") == "skeptic-fallback"
    assert router.resolve_fallback("triage") == "fallback-default"


def test_router_warns_once_when_budget_threshold_is_reached(
    caplog: pytest.LogCaptureFixture,
) -> None:
    cost_tracker = CostTracker()
    config = PiranesiConfig(
        models=ModelsConfig(scanner="scanner-model"),
        budget=BudgetConfig(max_cost_usd=10.0, warn_at_usd=1.0),
    )
    router = ModelRouter(config=config, cost_tracker=cost_tracker)
    cost_tracker.add(1.0, "scanner")

    with caplog.at_level(logging.WARNING, logger="piranesi.llm.router"):
        assert router.resolve("scanner") == "scanner-model"
        assert router.resolve("scanner") == "scanner-model"

    warnings = [record for record in caplog.records if record.msg.startswith("LLM budget warning")]
    assert len(warnings) == 1


def test_router_raises_budget_exceeded_at_limit() -> None:
    cost_tracker = CostTracker()
    config = PiranesiConfig(
        models=ModelsConfig(scanner="scanner-model"),
        budget=BudgetConfig(max_cost_usd=1.0),
    )
    router = ModelRouter(config=config, cost_tracker=cost_tracker)
    cost_tracker.add(1.0, "scanner")

    with pytest.raises(BudgetExceededError, match=r"budget 1\.00 USD exceeded"):
        router.resolve("scanner")


def test_router_rejects_unknown_stage() -> None:
    router = ModelRouter(
        config=PiranesiConfig(models=ModelsConfig(scanner="scanner-model")),
        cost_tracker=CostTracker(),
    )

    with pytest.raises(ValueError, match="unknown stage: unknown"):
        router.resolve("unknown")


def test_router_prefers_stage_specific_fallback_over_default() -> None:
    router = ModelRouter(
        config=PiranesiConfig(
            models=ModelsConfig(skeptic=None),
            models_fallback=ModelFallbackConfig(
                default="fallback-default",
                skeptic="skeptic-fallback",
            ),
        ),
        cost_tracker=CostTracker(),
    )

    assert router.resolve("skeptic") == "skeptic-fallback"
    assert router.resolve_fallback("skeptic") == "skeptic-fallback"


def test_router_raises_when_no_stage_model_or_fallback_exists() -> None:
    router = ModelRouter(
        config=PiranesiConfig(
            models=ModelsConfig(skeptic=None),
            models_fallback=ModelFallbackConfig(),
        ),
        cost_tracker=CostTracker(),
    )

    with pytest.raises(ValueError, match="no model configured for stage skeptic"):
        router.resolve("skeptic")


def test_router_exposes_accumulated_total_cost() -> None:
    tracker = CostTracker()
    tracker.add(0.25, "scanner")
    tracker.add(0.75, "triage")
    router = ModelRouter(
        config=PiranesiConfig(models=ModelsConfig(scanner="scanner-model")),
        cost_tracker=tracker,
    )

    assert router.total_cost_usd == pytest.approx(1.0)
