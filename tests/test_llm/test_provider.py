from __future__ import annotations

import json
from pathlib import Path

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

    monkeypatch.setattr(LLMProvider._complete_with_retry.retry, "wait", wait_none())

    def _completion(*, model: str, messages: list[dict[str, str]], **kwargs: object):
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
