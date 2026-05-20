from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from piranesi.timeline import TimelineConfidence


class C2ParseError(ValueError):
    """Raised when neutral C2 JSONL cannot be parsed into timeline events."""


@dataclass(frozen=True)
class C2TimelineEvent:
    timestamp: str
    summary: str
    actor: str | None
    details: str | None
    tags: list[str]
    confidence: TimelineConfidence


@dataclass(frozen=True)
class C2ParseResult:
    events: list[C2TimelineEvent]
    warnings: list[str]
    metadata: dict[str, Any]


def parse_c2_jsonl_file(
    input_path: Path,
    *,
    input_sha256: str,
    raw_path: str,
) -> C2ParseResult:
    try:
        lines = input_path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        raise C2ParseError(f"cannot read C2 JSONL: {exc}") from exc

    warnings: list[str] = []
    events: list[C2TimelineEvent] = []
    malformed_lines = 0
    event_types: set[str] = set()

    for line_number, line in enumerate(lines, start=1):
        stripped = line.strip()
        if not stripped:
            continue
        try:
            payload = json.loads(stripped)
        except json.JSONDecodeError as exc:
            malformed_lines += 1
            warnings.append(f"line {line_number}: invalid JSON ({exc.msg})")
            continue
        if not isinstance(payload, dict):
            malformed_lines += 1
            warnings.append(f"line {line_number}: expected JSON object")
            continue
        event = _event_from_payload(payload, line_number=line_number, warnings=warnings)
        if event is None:
            malformed_lines += 1
            continue
        events.append(event)
        event_types.add(str(payload.get("event") or payload.get("type") or "event"))

    if not lines or (not events and malformed_lines == 0):
        raise C2ParseError("empty C2 JSONL: input contains no event records")
    if not events:
        raise C2ParseError("C2 JSONL contained no valid event records")

    return C2ParseResult(
        events=events,
        warnings=warnings,
        metadata={
            "format": "neutral-c2-jsonl",
            "input_sha256": input_sha256,
            "raw_path": raw_path,
            "records": len(events) + malformed_lines,
            "valid_records": len(events),
            "malformed_records": malformed_lines,
            "event_types": sorted(event_types),
            "summary": {"events": len(events), "warnings": len(warnings)},
        },
    )


def _event_from_payload(
    payload: dict[str, Any],
    *,
    line_number: int,
    warnings: list[str],
) -> C2TimelineEvent | None:
    timestamp = _as_str(payload.get("timestamp") or payload.get("time"))
    event_type = _as_str(payload.get("event") or payload.get("type"))
    if timestamp is None:
        warnings.append(f"line {line_number}: missing timestamp")
        return None
    if event_type is None and _as_str(payload.get("summary")) is None:
        warnings.append(f"line {line_number}: missing event or summary")
        return None

    actor = _as_str(payload.get("operator") or payload.get("actor"))
    target = _as_str(payload.get("target") or payload.get("host"))
    session = _as_str(payload.get("session") or payload.get("session_id"))
    source = _as_str(payload.get("source"))
    command_summary = _as_str(payload.get("command_summary"))
    output_locator = _as_str(payload.get("output_locator"))
    summary = _as_str(payload.get("summary")) or _summary(
        event_type=event_type or "event",
        target=target,
        session=session,
    )
    details = _details(
        event_type=event_type,
        source=source,
        target=target,
        session=session,
        command_summary=command_summary,
        output_locator=output_locator,
    )
    tags = {
        "c2",
        *(item for item in [event_type, source] if item),
    }
    return C2TimelineEvent(
        timestamp=timestamp,
        summary=summary,
        actor=actor,
        details=details,
        tags=sorted(tags),
        confidence=_confidence(payload.get("confidence")),
    )


def _summary(*, event_type: str, target: str | None, session: str | None) -> str:
    subject = target or session
    if subject:
        return f"C2 {event_type} for {subject}"
    return f"C2 {event_type}"


def _details(
    *,
    event_type: str | None,
    source: str | None,
    target: str | None,
    session: str | None,
    command_summary: str | None,
    output_locator: str | None,
) -> str | None:
    parts = [
        ("Event", event_type),
        ("Source", source),
        ("Target", target),
        ("Session", session),
        ("Command summary", command_summary),
        ("Output locator", output_locator),
    ]
    lines = [f"{label}: {value}" for label, value in parts if value]
    return "\n".join(lines) if lines else None


def _confidence(value: object) -> TimelineConfidence:
    if value in {"low", "medium", "high", "confirmed"}:
        return value  # type: ignore[return-value]
    return "medium"


def _as_str(value: object) -> str | None:
    if isinstance(value, str) and value.strip():
        return value.strip()
    return None


__all__ = ["C2ParseError", "C2ParseResult", "C2TimelineEvent", "parse_c2_jsonl_file"]
