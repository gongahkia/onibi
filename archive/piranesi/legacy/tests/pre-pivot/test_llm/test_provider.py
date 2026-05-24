from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import litellm
import pytest
from tenacity import wait_none

from piranesi.config import (
    BudgetConfig,
    ModelFallbackConfig,
    ModelsConfig,
    PiranesiConfig,
    TraceConfig,
)
from piranesi.llm.cost import CostTracker
from piranesi.llm.provider import LLMProvider
from piranesi.llm.router import ModelRouter
from piranesi.llm.trace import TraceLogger
from piranesi.trace import TraceWriter


def _build_provider(
    tmp_path: Path,
    *,
    router: ModelRouter | None = None,
    log_prompts: bool = True,
) -> tuple[LLMProvider, CostTracker, Path]:
    trace_path = tmp_path / "llm-trace.jsonl"
    writer = TraceWriter(
        TraceConfig(file_path=str(trace_path), log_prompts=log_prompts),
        BudgetConfig(),
    )
    tracer = TraceLogger(writer, log_prompts=log_prompts)
    cost_tracker = router.cost_tracker if router is not None else CostTracker()
    return LLMProvider(tracer, cost_tracker, router=router), cost_tracker, trace_path


def test_provider_logs_trace_and_accumulates_cost(tmp_path: Path) -> None:
    provider, cost_tracker, trace_path = _build_provider(tmp_path)

    first = provider.complete(
        model="openai/gpt-4o-mini",
        stage="triage",
        messages=[{"role": "user", "content": "Return strict JSON."}],
        response_format={"type": "json_object"},
        mock_response='{"ok":true}',
    )
    second = provider.complete(
        model="openai/gpt-4o-mini",
        stage="triage",
        messages=[{"role": "user", "content": "Return strict JSON again."}],
        response_format={"type": "json_object"},
        mock_response='{"ok":false}',
    )

    assert first.model == "openai/gpt-4o-mini"
    assert first.prompt_hash.startswith("sha256:")
    assert first.response_hash.startswith("sha256:")
    assert first.cost_usd > 0
    assert second.cost_usd > 0
    assert cost_tracker.total_usd == pytest.approx(first.cost_usd + second.cost_usd)
    assert cost_tracker.total_for_stage("triage") == pytest.approx(first.cost_usd + second.cost_usd)

    lines = trace_path.read_text(encoding="utf-8").strip().splitlines()
    assert len(lines) == 2
    entry = json.loads(lines[0])
    assert entry["stage"] == "triage"
    assert entry["model"] == "openai/gpt-4o-mini"
    assert entry["prompt_hash"].startswith("sha256:")
    assert entry["response_hash"].startswith("sha256:")
    assert entry["prompt"] is not None
    assert entry["response"] == first.content


def test_provider_extracts_function_call_arguments(tmp_path: Path) -> None:
    provider, _, trace_path = _build_provider(tmp_path)

    response = provider.complete(
        model="openai/gpt-4o-mini",
        stage="triage",
        messages=[{"role": "user", "content": "Classify this finding."}],
        tools=[
            {
                "type": "function",
                "function": {
                    "name": "emit_json",
                    "description": "Return the structured verdict.",
                    "parameters": {"type": "object"},
                },
            }
        ],
        tool_choice="required",
        mock_tool_calls=[
            {
                "id": "call_1",
                "type": "function",
                "function": {
                    "name": "emit_json",
                    "arguments": '{"verdict":"true_positive","confidence":0.82}',
                },
            }
        ],
    )

    assert json.loads(response.content) == {"verdict": "true_positive", "confidence": 0.82}
    logged = json.loads(trace_path.read_text(encoding="utf-8").splitlines()[0])
    assert logged["response"] == response.content


def test_provider_uses_router_fallback_after_retryable_failure(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    router = ModelRouter(
        config=PiranesiConfig(
            models=ModelsConfig(scanner="openai/gpt-4o-mini"),
            models_fallback=ModelFallbackConfig(default="openai/gpt-4.1-mini"),
            budget=BudgetConfig(max_cost_usd=5.0),
        ),
        cost_tracker=CostTracker(),
    )
    provider, cost_tracker, _ = _build_provider(tmp_path, router=router)
    calls: list[str] = []

    monkeypatch.setattr(LLMProvider._complete_with_retry.retry, "wait", wait_none())  # type: ignore[attr-defined]

    def _completion(*, model: str, messages: list[dict[str, str]], **kwargs: Any) -> Any:
        calls.append(model)
        if model == "openai/gpt-4o-mini":
            return litellm.mock_completion(
                model=model,
                messages=messages,
                mock_response="litellm.RateLimitError",
                **kwargs,
            )
        return litellm.mock_completion(
            model=model,
            messages=messages,
            mock_response="fallback succeeded",
            **kwargs,
        )

    monkeypatch.setattr("piranesi.llm.provider.litellm.completion", _completion)

    response = provider.complete(
        stage="scanner",
        messages=[{"role": "user", "content": "Summarize sources and sinks."}],
    )

    assert calls == [
        "openai/gpt-4o-mini",
        "openai/gpt-4o-mini",
        "openai/gpt-4o-mini",
        "openai/gpt-4.1-mini",
    ]
    assert response.model == "openai/gpt-4.1-mini"
    assert response.content == "fallback succeeded"
    assert cost_tracker.total_usd == pytest.approx(response.cost_usd)


def test_provider_applies_router_token_budget_adjustments(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    router = ModelRouter(
        config=PiranesiConfig(
            models=ModelsConfig(scanner="openai/gpt-4o-mini"),
            budget=BudgetConfig(max_cost_usd=5.0, max_tokens=180),
        ),
        cost_tracker=CostTracker(),
    )
    provider, _, _ = _build_provider(tmp_path, router=router)
    observed_max_tokens: list[int] = []
    observed_messages: list[list[dict[str, str]]] = []

    def _completion(*, model: str, messages: list[dict[str, str]], **kwargs: Any) -> Any:
        _ = model
        observed_max_tokens.append(int(kwargs["max_tokens"]))
        observed_messages.append(messages)
        return litellm.mock_completion(
            model="openai/gpt-4o-mini",
            messages=messages,
            mock_response='{"ok":true}',
            **kwargs,
        )

    monkeypatch.setattr("piranesi.llm.provider.litellm.completion", _completion)

    response = provider.complete(
        stage="scanner",
        messages=[
            {"role": "system", "content": "return json only"},
            {"role": "user", "content": "X" * 4000},
        ],
        max_tokens=512,
    )

    assert response.content == '{"ok":true}'
    assert observed_max_tokens and observed_max_tokens[0] < 512
    assert "token budget" in observed_messages[0][1]["content"]
    assert router.used_tokens > 0


def test_provider_redacts_sensitive_values_before_llm_call(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    provider, _, _ = _build_provider(tmp_path)
    captured_messages: list[list[dict[str, str]]] = []

    def _completion(*, model: str, messages: list[dict[str, str]], **kwargs: Any) -> Any:
        _ = (model, kwargs)
        captured_messages.append(messages)
        return litellm.mock_completion(
            model="openai/gpt-4o-mini",
            messages=messages,
            mock_response='{"ok":true}',
        )

    monkeypatch.setattr("piranesi.llm.provider.litellm.completion", _completion)

    response = provider.complete(
        model="openai/gpt-4o-mini",
        stage="triage",
        messages=[
            {
                "role": "user",
                "content": (
                    "authorization: Bearer sk-abcdefghijklmnopqrstuvwx\n"
                    "cookie: sid=abc123\n"
                    "password=hunter2\n"
                ),
            }
        ],
    )

    assert response.content == '{"ok":true}'
    outbound = captured_messages[0][0]["content"]
    assert "sk-abcdefghijklmnopqrstuvwx" not in outbound
    assert "hunter2" not in outbound
    assert "sid=abc123" not in outbound
    assert "[REDACTED]" in outbound
