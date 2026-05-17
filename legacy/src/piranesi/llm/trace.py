from __future__ import annotations

import json
from collections.abc import Mapping, Sequence
from datetime import UTC, datetime
from hashlib import sha256
from typing import Any

from pydantic import BaseModel, ConfigDict

from piranesi.trace import TraceEntry, TraceWriter


def _json_default(value: Any) -> Any:
    if hasattr(value, "model_dump"):
        return value.model_dump(mode="json")
    if isinstance(value, set):
        return sorted(value)
    return str(value)


class TraceLogger:
    def __init__(self, writer: TraceWriter, *, log_prompts: bool = False) -> None:
        self._writer = writer
        self._log_prompts = log_prompts

    def log_call(
        self,
        *,
        stage: str,
        model: str,
        messages: Sequence[Mapping[str, Any]],
        response_content: str,
        prompt_tokens: int,
        response_tokens: int,
        cost_usd: float,
        duration_ms: int,
        cache_hit: bool = False,
        finding_id: str | None = None,
        verdict: str | None = None,
    ) -> TraceEntry:
        prompt = self.serialize_messages(messages)
        response = response_content
        entry = TraceEntry(
            timestamp=_utc_now(),
            stage=stage,
            model=model,
            prompt_hash=self.hash_text(prompt),
            response_hash=self.hash_text(response),
            prompt_tokens=prompt_tokens,
            response_tokens=response_tokens,
            cost_usd=cost_usd,
            duration_ms=duration_ms,
            cache_hit=cache_hit,
            finding_id=finding_id,
            verdict=verdict,
            prompt=prompt if self._log_prompts else None,
            response=response if self._log_prompts else None,
        )
        self._writer.write(entry)
        return entry

    @staticmethod
    def hash_text(value: str) -> str:
        digest = sha256(value.encode("utf-8")).hexdigest()
        return f"sha256:{digest}"

    @staticmethod
    def serialize_messages(messages: Sequence[Mapping[str, Any]]) -> str:
        return json.dumps(
            list(messages),
            default=_json_default,
            ensure_ascii=True,
            separators=(",", ":"),
            sort_keys=True,
        )


class TraceNondeterminismEvent(BaseModel):
    model_config = ConfigDict(extra="forbid")

    event: str = "nondeterminism"
    stage: str
    model: str
    prompt_hash: str
    previous_response_hash: str
    current_response_hash: str
    verdict_changed: bool = False


def _utc_now() -> str:
    return datetime.now(UTC).isoformat(timespec="milliseconds").replace("+00:00", "Z")


def detect_nondeterminism(entries: Sequence[TraceEntry]) -> list[TraceNondeterminismEvent]:
    events: list[TraceNondeterminismEvent] = []
    previous_by_prompt: dict[tuple[str, str, str], TraceEntry] = {}

    for entry in entries:
        key = (entry.stage, entry.model, entry.prompt_hash)
        previous = previous_by_prompt.get(key)
        if previous is not None and previous.response_hash != entry.response_hash:
            events.append(
                TraceNondeterminismEvent(
                    stage=entry.stage,
                    model=entry.model,
                    prompt_hash=entry.prompt_hash,
                    previous_response_hash=previous.response_hash,
                    current_response_hash=entry.response_hash,
                    verdict_changed=_verdict_changed(previous, entry),
                )
            )
        previous_by_prompt[key] = entry

    return events


def _verdict_changed(previous: TraceEntry, current: TraceEntry) -> bool:
    if previous.verdict is None or current.verdict is None:
        return False
    return previous.verdict != current.verdict


__all__ = ["TraceLogger", "TraceNondeterminismEvent", "detect_nondeterminism"]
