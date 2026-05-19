from __future__ import annotations

import json
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, ValidationError

from piranesi.evidence import load_evidence_index
from piranesi.workspace import (
    TIMELINE_FILE,
    WorkspaceState,
    deterministic_finding_id,
    utc_now,
    workspace_path,
)

TimelineConfidence = Literal["low", "medium", "high", "confirmed"]


class TimelineError(ValueError):
    """Raised when timeline events cannot be stored or loaded safely."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class TimelineEvent(_StrictModel):
    id: str
    timestamp: str
    phase: str | None = None
    actor: str | None = None
    summary: str
    details: str | None = None
    evidence_ids: list[str] = Field(default_factory=list)
    finding_ids: list[str] = Field(default_factory=list)
    objective_ids: list[str] = Field(default_factory=list)
    tags: list[str] = Field(default_factory=list)
    confidence: TimelineConfidence = "medium"


def append_timeline_event(
    state: WorkspaceState,
    *,
    summary: str,
    timestamp: str | None = None,
    phase: str | None = None,
    actor: str | None = None,
    details: str | None = None,
    evidence_ids: list[str] | None = None,
    finding_ids: list[str] | None = None,
    objective_ids: list[str] | None = None,
    tags: list[str] | None = None,
    confidence: TimelineConfidence = "medium",
) -> TimelineEvent:
    if not summary.strip():
        raise TimelineError("timeline summary cannot be empty")
    event = TimelineEvent(
        id=deterministic_finding_id("timeline", timestamp or utc_now(), summary, prefix="event"),
        timestamp=timestamp or utc_now(),
        phase=phase,
        actor=actor,
        summary=summary,
        details=details,
        evidence_ids=sorted(set(evidence_ids or [])),
        finding_ids=sorted(set(finding_ids or [])),
        objective_ids=sorted(set(objective_ids or [])),
        tags=sorted(set(tags or [])),
        confidence=confidence,
    )
    _validate_references(state, event)
    path = workspace_path(state.root, TIMELINE_FILE, allowed_roots=("timeline",))
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as handle:
        handle.write(json.dumps(event.model_dump(mode="json"), sort_keys=True))
        handle.write("\n")
    return event


def load_timeline_events(root: Path | str) -> list[TimelineEvent]:
    path = workspace_path(root, TIMELINE_FILE, allowed_roots=("timeline",))
    if not path.exists():
        return []
    events: list[TimelineEvent] = []
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        stripped = line.strip()
        if not stripped:
            continue
        try:
            payload = json.loads(stripped)
            events.append(TimelineEvent.model_validate(payload))
        except (json.JSONDecodeError, ValidationError) as exc:
            raise TimelineError(f"invalid timeline event on line {line_number}: {exc}") from exc
    return sorted(events, key=lambda event: (event.timestamp, event.id))


def _validate_references(state: WorkspaceState, event: TimelineEvent) -> None:
    if event.evidence_ids:
        evidence_ids = {record.id for record in load_evidence_index(state.root).evidence}
        missing = sorted(set(event.evidence_ids) - evidence_ids)
        if missing:
            raise TimelineError(f"unknown evidence id(s): {', '.join(missing)}")
    if event.finding_ids:
        finding_ids = {finding.id for finding in state.findings.findings}
        missing = sorted(set(event.finding_ids) - finding_ids)
        if missing:
            raise TimelineError(f"unknown finding id(s): {', '.join(missing)}")


__all__ = [
    "TimelineConfidence",
    "TimelineError",
    "TimelineEvent",
    "append_timeline_event",
    "load_timeline_events",
]
