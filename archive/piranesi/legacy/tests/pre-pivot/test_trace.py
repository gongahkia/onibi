from __future__ import annotations

import json
from pathlib import Path

from piranesi.config import BudgetConfig, TraceConfig
from piranesi.trace import TraceEntry, TraceWriter


def _entry() -> TraceEntry:
    return TraceEntry(
        timestamp="2026-04-09T14:23:01.442Z",
        stage="triage",
        model="gpt-4o",
        prompt_hash="abc",
        response_hash="def",
        prompt_tokens=10,
        response_tokens=5,
        cost_usd=0.25,
        duration_ms=1200,
        cache_hit=False,
        prompt="prompt body",
        response="response body",
    )


def test_trace_writer_writes_entry(tmp_path: Path) -> None:
    trace_path = tmp_path / "trace.jsonl"
    writer = TraceWriter(TraceConfig(file_path=str(trace_path), log_prompts=True), BudgetConfig())

    writer.open()
    writer.write(_entry())
    writer.close()

    lines = trace_path.read_text(encoding="utf-8").strip().splitlines()
    assert len(lines) == 1
    assert writer.summary().entry_count == 1
    assert writer.summary().total_cost_usd == 0.25


def test_trace_writer_outputs_valid_jsonl(tmp_path: Path) -> None:
    trace_path = tmp_path / "trace.jsonl"
    writer = TraceWriter(TraceConfig(file_path=str(trace_path), log_prompts=True), BudgetConfig())

    writer.write(_entry())
    writer.close()

    parsed = json.loads(trace_path.read_text(encoding="utf-8").splitlines()[0])
    assert parsed["stage"] == "triage"
    assert parsed["model"] == "gpt-4o"


def test_trace_writer_scrubs_prompts_when_disabled(tmp_path: Path) -> None:
    trace_path = tmp_path / "trace.jsonl"
    writer = TraceWriter(TraceConfig(file_path=str(trace_path), log_prompts=False), BudgetConfig())

    writer.write(_entry())
    writer.close()

    parsed = json.loads(trace_path.read_text(encoding="utf-8").splitlines()[0])
    assert parsed["prompt"] is None
    assert parsed["response"] is None
