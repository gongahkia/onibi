from __future__ import annotations

import json
from collections import deque
from dataclasses import dataclass
from threading import Lock
from typing import Any

from piranesi.models import (
    CandidateFinding,
    SandboxResult,
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
)


@dataclass(slots=True)
class _Response:
    content: str
    prompt_tokens: int
    response_tokens: int
    cost_usd: float
    duration_ms: int
    model: str
    prompt_hash: str
    response_hash: str


class RecordingProvider:
    def __init__(self, responses: dict[tuple[str, str], list[dict[str, Any] | str]]) -> None:
        self._responses = {
            key: deque(
                json.dumps(payload, sort_keys=True) if isinstance(payload, dict) else payload
                for payload in values
            )
            for key, values in responses.items()
        }
        self._lock = Lock()
        self.calls: list[dict[str, Any]] = []

    def complete(
        self,
        *,
        stage: str,
        model: str | None = None,
        messages: list[dict[str, str]],
        **kwargs: Any,
    ) -> _Response:
        if model is None:
            raise AssertionError("tests expect an explicit model")
        with self._lock:
            queue = self._responses.get((stage, model))
            if queue is None or not queue:
                raise AssertionError(f"no queued response for {(stage, model)}")
            content = queue.popleft()
            call_index = len(self.calls) + 1
            self.calls.append(
                {
                    "stage": stage,
                    "model": model,
                    "messages": messages,
                    **kwargs,
                }
            )
        return _Response(
            content=content,
            prompt_tokens=0,
            response_tokens=0,
            cost_usd=0.0,
            duration_ms=0,
            model=model,
            prompt_hash=f"sha256:prompt-{call_index}",
            response_hash=f"sha256:response-{call_index}",
        )


def build_candidate_finding(vuln_class: str = "CWE-89: SQL Injection") -> CandidateFinding:
    source_location = SourceLocation(
        file="src/api/users.ts",
        line=11,
        column=15,
        snippet="const id = req.query.id; // upstream validation note\n",
    )
    step_location = SourceLocation(
        file="src/api/users.ts",
        line=14,
        column=9,
        snippet="const sql = `SELECT * FROM users WHERE id = ${id}`; /* query builder */\n",
    )
    sink_location = SourceLocation(
        file="src/api/users.ts",
        line=18,
        column=5,
        snippet="db.query(sql); // sink call\n",
    )
    return CandidateFinding(
        id="finding-1",
        vuln_class=vuln_class,
        source=TaintSource(
            location=source_location,
            source_type="req.query.id",
            data_categories=["identifier"],
            parameter_name="id",
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type="sql",
            api_name="db.query",
        ),
        taint_path=[
            TaintStep(
                location=step_location,
                operation="build_sql",
                taint_state="tainted",
                through_function="loadUser",
            )
        ],
        path_conditions=[],
        confidence=0.92,
        severity="high",
    )


def build_sandbox_result(*, confirmed: bool) -> SandboxResult:
    return SandboxResult(
        container_id="sandbox-1",
        request={"path": "/users?id=1"},
        response={"status": 200},
        timing_ms=14,
        side_effects=[],
        container_diff=[],
        stdout="",
        stderr="",
        exit_code=0,
        network_isolated=True,
        confirmed=confirmed,
    )
