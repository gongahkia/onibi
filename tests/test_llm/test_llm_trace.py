from __future__ import annotations

import json
from hashlib import sha256
from pathlib import Path

import pytest

from piranesi.config import BudgetConfig, TraceConfig
from piranesi.llm.trace import TraceLogger, detect_nondeterminism
from piranesi.trace import TraceEntry, TraceWriter


def test_trace_logger_writes_expected_jsonl_fields(tmp_path: Path) -> None:
    trace_path = tmp_path / "llm-trace.jsonl"
    writer = TraceWriter(TraceConfig(file_path=str(trace_path), log_prompts=True), BudgetConfig())
    logger = TraceLogger(writer, log_prompts=True)
    messages = [
        {"role": "system", "content": "Return JSON only."},
        {"role": "user", "content": "Classify this finding."},
    ]
    response = '{"verdict":"true_positive"}'

    entry = logger.log_call(
        stage="triage",
        model="openai/gpt-4o-mini",
        messages=messages,
        response_content=response,
        prompt_tokens=123,
        response_tokens=45,
        cost_usd=0.0123,
        duration_ms=678,
        cache_hit=False,
        finding_id="finding-1",
        verdict="true_positive",
    )

    expected_prompt = json.dumps(messages, ensure_ascii=True, separators=(",", ":"), sort_keys=True)
    assert entry.prompt_hash == f"sha256:{sha256(expected_prompt.encode('utf-8')).hexdigest()}"
    assert entry.response_hash == f"sha256:{sha256(response.encode('utf-8')).hexdigest()}"

    parsed = json.loads(trace_path.read_text(encoding="utf-8").splitlines()[0])
    assert parsed["timestamp"].endswith("Z")
    assert parsed["stage"] == "triage"
    assert parsed["model"] == "openai/gpt-4o-mini"
    assert parsed["prompt_tokens"] == 123
    assert parsed["response_tokens"] == 45
    assert parsed["cost_usd"] == 0.0123
    assert parsed["duration_ms"] == 678
    assert parsed["cache_hit"] is False
    assert parsed["finding_id"] == "finding-1"
    assert parsed["verdict"] == "true_positive"
    assert parsed["prompt"] == expected_prompt
    assert parsed["response"] == response


def test_trace_logger_scrubs_prompt_and_response_when_disabled(tmp_path: Path) -> None:
    trace_path = tmp_path / "llm-trace.jsonl"
    writer = TraceWriter(TraceConfig(file_path=str(trace_path), log_prompts=False), BudgetConfig())
    logger = TraceLogger(writer, log_prompts=False)

    logger.log_call(
        stage="scanner",
        model="openai/gpt-4o-mini",
        messages=[{"role": "user", "content": "Find sources."}],
        response_content='{"sources":[]}',
        prompt_tokens=10,
        response_tokens=20,
        cost_usd=0.001,
        duration_ms=30,
        cache_hit=True,
    )

    parsed = json.loads(trace_path.read_text(encoding="utf-8").splitlines()[0])
    assert parsed["prompt"] is None
    assert parsed["response"] is None
    assert parsed["cache_hit"] is True


def test_detect_nondeterminism_when_same_prompt_produces_different_response() -> None:
    entries = [
        TraceEntry(
            timestamp="2026-04-09T00:00:00.000Z",
            stage="triage",
            model="anthropic/claude-sonnet-4-6",
            prompt_hash="sha256:same-prompt",
            response_hash="sha256:response-a",
            prompt_tokens=10,
            response_tokens=5,
            cost_usd=0.01,
            duration_ms=100,
            cache_hit=False,
            finding_id="finding-1",
            verdict="true_positive",
        ),
        TraceEntry(
            timestamp="2026-04-09T00:00:01.000Z",
            stage="triage",
            model="anthropic/claude-sonnet-4-6",
            prompt_hash="sha256:same-prompt",
            response_hash="sha256:response-b",
            prompt_tokens=10,
            response_tokens=5,
            cost_usd=0.01,
            duration_ms=100,
            cache_hit=False,
            finding_id="finding-1",
            verdict="false_positive",
        ),
    ]

    events = detect_nondeterminism(entries)

    assert len(events) == 1
    assert events[0].stage == "triage"
    assert events[0].model == "anthropic/claude-sonnet-4-6"
    assert events[0].prompt_hash == "sha256:same-prompt"
    assert events[0].previous_response_hash == "sha256:response-a"
    assert events[0].current_response_hash == "sha256:response-b"
    assert events[0].verdict_changed is True


def test_trace_writer_summary_aggregates_multiple_entries(tmp_path: Path) -> None:
    trace_path = tmp_path / "llm-trace.jsonl"
    writer = TraceWriter(TraceConfig(file_path=str(trace_path), log_prompts=False), BudgetConfig())
    logger = TraceLogger(writer, log_prompts=False)

    logger.log_call(
        stage="scanner",
        model="openai/gpt-4o-mini",
        messages=[{"role": "user", "content": "scan"}],
        response_content='{"sources":[]}',
        prompt_tokens=10,
        response_tokens=5,
        cost_usd=0.001,
        duration_ms=100,
    )
    logger.log_call(
        stage="triage",
        model="anthropic/claude-sonnet-4-6",
        messages=[{"role": "user", "content": "triage"}],
        response_content='{"verdict":"true_positive"}',
        prompt_tokens=20,
        response_tokens=8,
        cost_usd=0.021,
        duration_ms=250,
        verdict="true_positive",
    )

    summary = writer.summary()

    assert summary.entry_count == 2
    assert summary.prompt_tokens == 30
    assert summary.response_tokens == 13
    assert summary.total_cost_usd == pytest.approx(0.022)
