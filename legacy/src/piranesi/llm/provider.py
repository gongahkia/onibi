from __future__ import annotations

import json
import logging
import time
from collections.abc import Mapping, Sequence
from dataclasses import dataclass
from types import SimpleNamespace
from typing import TYPE_CHECKING, Any

from pydantic import BaseModel
from tenacity import retry, retry_if_exception_type, stop_after_attempt, wait_exponential_jitter

from piranesi.llm.cost import CostTracker
from piranesi.llm.router import TokenBudgetReservation
from piranesi.llm.sanitize import redact_prompt_messages
from piranesi.llm.trace import TraceLogger

try:
    import litellm
except ImportError:  # pragma: no cover - depends on local environment.

    def _missing_completion(*args: Any, **kwargs: Any) -> Any:
        raise ImportError("litellm is required to execute LLM-backed pipeline stages")

    litellm = SimpleNamespace(completion=_missing_completion)  # type: ignore[assignment]

if TYPE_CHECKING:
    from piranesi.llm.router import ModelRouter


def _litellm_exception_types(*names: str) -> tuple[type[BaseException], ...]:
    exception_types: list[type[BaseException]] = []
    for name in names:
        candidate = getattr(litellm, name, None)
        if isinstance(candidate, type) and issubclass(candidate, BaseException):
            exception_types.append(candidate)
    return tuple(exception_types)


RETRYABLE_EXCEPTIONS: tuple[type[BaseException], ...] = (
    *_litellm_exception_types(
        "RateLimitError",
        "Timeout",
        "APIError",
        "APIConnectionError",
        "InternalServerError",
        "ServiceUnavailableError",
    ),
    TimeoutError,
    ConnectionError,
)


@dataclass(frozen=True, slots=True)
class LLMResponse:
    content: str
    prompt_tokens: int
    response_tokens: int
    cost_usd: float
    duration_ms: int
    model: str
    prompt_hash: str
    response_hash: str


class LLMProvider:
    def __init__(
        self,
        tracer: TraceLogger,
        cost_tracker: CostTracker,
        *,
        router: ModelRouter | None = None,
    ) -> None:
        self._tracer = tracer
        self._cost = cost_tracker
        self._router = router
        self._logger = logging.getLogger("piranesi.llm.provider")

    def complete(
        self,
        *,
        stage: str,
        messages: Sequence[Mapping[str, Any]],
        model: str | None = None,
        response_format: dict[str, Any] | type[BaseModel] | None = None,
        tools: list[dict[str, Any]] | None = None,
        tool_choice: str | dict[str, Any] | None = None,
        functions: list[dict[str, Any]] | None = None,
        function_call: str | None = None,
        temperature: float = 0.0,
        max_tokens: int = 4096,
        timeout: int = 60,
        api_key: str | None = None,
        **kwargs: Any,
    ) -> LLMResponse:
        selected_model = model or self._resolve_model(stage)
        try:
            return self._complete_with_retry(
                model=selected_model,
                messages=messages,
                stage=stage,
                response_format=response_format,
                tools=tools,
                tool_choice=tool_choice,
                functions=functions,
                function_call=function_call,
                temperature=temperature,
                max_tokens=max_tokens,
                timeout=timeout,
                api_key=api_key,
                kwargs=kwargs,
            )
        except RETRYABLE_EXCEPTIONS as exc:
            fallback_model = self._resolve_fallback(stage, primary_model=selected_model)
            if fallback_model is None:
                raise
            self._logger.warning(
                "LLM call failed for stage=%s model=%s; retrying with fallback=%s",
                stage,
                selected_model,
                fallback_model,
                extra={
                    "event": "llm_fallback",
                    "stage": stage,
                    "model": selected_model,
                    "fallback_model": fallback_model,
                    "failure_type": type(exc).__name__,
                },
            )
            return self._complete_once(
                model=fallback_model,
                messages=messages,
                stage=stage,
                response_format=response_format,
                tools=tools,
                tool_choice=tool_choice,
                functions=functions,
                function_call=function_call,
                temperature=temperature,
                max_tokens=max_tokens,
                timeout=timeout,
                api_key=api_key,
                kwargs=kwargs,
            )

    @retry(
        retry=retry_if_exception_type(RETRYABLE_EXCEPTIONS),
        stop=stop_after_attempt(3),
        wait=wait_exponential_jitter(initial=1, max=30, jitter=5),
        reraise=True,
    )
    def _complete_with_retry(
        self,
        *,
        model: str,
        messages: Sequence[Mapping[str, Any]],
        stage: str,
        response_format: dict[str, Any] | type[BaseModel] | None,
        tools: list[dict[str, Any]] | None,
        tool_choice: str | dict[str, Any] | None,
        functions: list[dict[str, Any]] | None,
        function_call: str | None,
        temperature: float,
        max_tokens: int,
        timeout: int,
        api_key: str | None,
        kwargs: Mapping[str, Any],
    ) -> LLMResponse:
        return self._complete_once(
            model=model,
            messages=messages,
            stage=stage,
            response_format=response_format,
            tools=tools,
            tool_choice=tool_choice,
            functions=functions,
            function_call=function_call,
            temperature=temperature,
            max_tokens=max_tokens,
            timeout=timeout,
            api_key=api_key,
            kwargs=kwargs,
        )

    def _complete_once(
        self,
        *,
        model: str,
        messages: Sequence[Mapping[str, Any]],
        stage: str,
        response_format: dict[str, Any] | type[BaseModel] | None,
        tools: list[dict[str, Any]] | None,
        tool_choice: str | dict[str, Any] | None,
        functions: list[dict[str, Any]] | None,
        function_call: str | None,
        temperature: float,
        max_tokens: int,
        timeout: int,
        api_key: str | None,
        kwargs: Mapping[str, Any],
    ) -> LLMResponse:
        normalized_messages = [dict(message) for message in messages]
        effective_max_tokens = max_tokens
        reservation: TokenBudgetReservation | None = None
        if self._router is not None:
            reservation = self._router.reserve_completion(
                stage=stage,
                messages=normalized_messages,
                requested_max_tokens=max_tokens,
            )
            normalized_messages = [dict(message) for message in reservation.messages]
            effective_max_tokens = reservation.max_tokens
        redacted_messages = redact_prompt_messages(normalized_messages)
        started_at = time.perf_counter()
        response = litellm.completion(
            model=model,
            messages=redacted_messages,
            temperature=temperature,
            max_tokens=effective_max_tokens,
            timeout=timeout,
            response_format=response_format,
            tools=tools,
            tool_choice=tool_choice,
            functions=functions,
            function_call=function_call,
            api_key=api_key,
            **dict(kwargs),
        )
        duration_ms = int((time.perf_counter() - started_at) * 1000)
        prompt_tokens, response_tokens = _extract_usage(response)
        if self._router is not None and reservation is not None:
            self._router.settle_completion(
                reservation,
                prompt_tokens=prompt_tokens,
                response_tokens=response_tokens,
            )
        response_content = _extract_response_content(response)
        cost_usd = _completion_cost(response)
        self._cost.add(cost_usd, stage)
        trace_entry = self._tracer.log_call(
            stage=stage,
            model=model,
            messages=redacted_messages,
            response_content=response_content,
            prompt_tokens=prompt_tokens,
            response_tokens=response_tokens,
            cost_usd=cost_usd,
            duration_ms=duration_ms,
            cache_hit=_extract_cache_hit(response),
        )
        return LLMResponse(
            content=response_content,
            prompt_tokens=prompt_tokens,
            response_tokens=response_tokens,
            cost_usd=cost_usd,
            duration_ms=duration_ms,
            model=model,
            prompt_hash=trace_entry.prompt_hash,
            response_hash=trace_entry.response_hash,
        )

    def _resolve_model(self, stage: str) -> str:
        if self._router is None:
            raise ValueError("model is required when no router is configured")
        return self._router.resolve(stage)

    def _resolve_fallback(self, stage: str, *, primary_model: str) -> str | None:
        if self._router is None:
            return None
        fallback_model = self._router.resolve_fallback(stage)
        if fallback_model is None or fallback_model == primary_model:
            return None
        return fallback_model


def _extract_usage(response: Any) -> tuple[int, int]:
    usage = _read_field(response, "usage")
    prompt_tokens = _read_int_field(usage, "prompt_tokens")
    response_tokens = _read_int_field(usage, "completion_tokens")
    if response_tokens == 0:
        response_tokens = _read_int_field(usage, "response_tokens")
    return prompt_tokens, response_tokens


def _extract_cache_hit(response: Any) -> bool:
    cache_hit = _read_field(response, "cache_hit")
    if isinstance(cache_hit, bool):
        return cache_hit
    hidden_params = _read_field(response, "_hidden_params")
    if isinstance(hidden_params, Mapping):
        hidden_cache_hit = hidden_params.get("cache_hit")
        if isinstance(hidden_cache_hit, bool):
            return hidden_cache_hit
    return False


def _extract_response_content(response: Any) -> str:
    choices = _read_field(response, "choices")
    if isinstance(choices, Sequence) and not isinstance(choices, (str, bytes)) and choices:
        message = _read_field(choices[0], "message")
        tool_calls = _read_field(message, "tool_calls")
        tool_content = _extract_tool_call_content(tool_calls)
        if tool_content is not None:
            return tool_content
        content = _read_field(message, "content")
        if isinstance(content, str):
            return content
        if isinstance(content, Sequence) and not isinstance(content, (str, bytes)):
            text_parts = [_extract_content_part(part) for part in content]
            return "".join(part for part in text_parts if part)
    return ""


def _extract_tool_call_content(tool_calls: Any) -> str | None:
    if not isinstance(tool_calls, Sequence) or isinstance(tool_calls, (str, bytes)):
        return None
    payloads: list[str] = []
    for tool_call in tool_calls:
        function = _read_field(tool_call, "function")
        arguments = _read_field(function, "arguments")
        if isinstance(arguments, str):
            payloads.append(arguments)
        elif arguments is not None:
            payloads.append(
                json.dumps(arguments, default=str, ensure_ascii=True, separators=(",", ":"))
            )
    if not payloads:
        return None
    if len(payloads) == 1:
        return payloads[0]
    return json.dumps(payloads, ensure_ascii=True, separators=(",", ":"))


def _extract_content_part(part: Any) -> str:
    if isinstance(part, str):
        return part
    if isinstance(part, Mapping):
        text = part.get("text")
        if isinstance(text, str):
            return text
        content = part.get("content")
        if isinstance(content, str):
            return content
    text = _read_field(part, "text")
    if isinstance(text, str):
        return text
    content = _read_field(part, "content")
    if isinstance(content, str):
        return content
    return ""


def _completion_cost(response: Any) -> float:
    try:
        cost = litellm.completion_cost(response)
    except Exception:
        return 0.0
    return float(cost)


def _read_int_field(obj: Any, field: str) -> int:
    value = _read_field(obj, field)
    if isinstance(value, bool):
        return int(value)
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        return int(value)
    return 0


def _read_field(obj: Any, field: str) -> Any:
    if isinstance(obj, Mapping):
        return obj.get(field)
    return getattr(obj, field, None)
