from __future__ import annotations

from pathlib import Path

import pytest

from piranesi.ai import (
    AIProviderError,
    StaticLocalAIProvider,
    build_redacted_prompt_payload,
    local_provider_config,
)
from piranesi.workspace import create_workspace


def test_local_provider_config_supports_endpoint_without_external_call() -> None:
    config = local_provider_config(
        provider_name="local-openai-compatible",
        model=None,
        endpoint_url="http://127.0.0.1:11434/v1/chat/completions",
        metadata={"runtime": "operator-managed"},
    )

    metadata = config.trace_metadata()

    assert metadata["type"] == "local"
    assert metadata["model"] is None
    assert metadata["external_call"] is False
    assert metadata["endpoint_url"] == "http://127.0.0.1:11434/v1/chat/completions"


def test_local_provider_config_supports_process_command() -> None:
    config = local_provider_config(
        provider_name="local-process",
        command=["local-llm", "--stdio"],
    )

    assert config.trace_metadata()["command"] == ["local-llm", "--stdio"]


def test_local_provider_requires_endpoint_or_command() -> None:
    with pytest.raises(AIProviderError, match="endpoint_url or command"):
        local_provider_config(provider_name="local")


def test_static_local_provider_accepts_only_redacted_prompt_payload(tmp_path: Path) -> None:
    state = create_workspace(tmp_path / "workspace")
    prompt = build_redacted_prompt_payload(state, purpose="unit-test")
    provider = StaticLocalAIProvider(
        config=local_provider_config(provider_name="fake-local", command=["fake-local"]),
        output_text="Draft from redacted prompt only.",
        output_metadata={"finish_reason": "stop"},
    )

    result = provider.complete(prompt)

    assert result.text == "Draft from redacted prompt only."
    assert result.metadata["prompt_schema"] == "piranesi.ai.prompt.v1"
    assert provider.trace_metadata["external_call"] is False
