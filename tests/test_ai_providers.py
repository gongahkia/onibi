from __future__ import annotations

import json

import pytest

from piranesi.ai import (
    AIProviderError,
    cloud_provider_config,
    require_cloud_provider_ready,
)


def test_cloud_provider_requires_privacy_mode_off_and_explicit_external_call() -> None:
    config = cloud_provider_config(
        provider_name="openai-compatible",
        model="gpt-test",
        api_key_env="PIRANESI_TEST_API_KEY",
        external_calls_enabled=True,
        privacy_mode=True,
    )

    with pytest.raises(AIProviderError, match="privacy mode disables external model calls"):
        require_cloud_provider_ready(config, env={"PIRANESI_TEST_API_KEY": "secret-key"})

    blocked = config.model_copy(update={"privacy_mode": False, "external_calls_enabled": False})
    with pytest.raises(AIProviderError, match="explicit enablement"):
        require_cloud_provider_ready(blocked, env={"PIRANESI_TEST_API_KEY": "secret-key"})


def test_cloud_provider_uses_byok_env_without_serializing_key() -> None:
    config = cloud_provider_config(
        provider_name="openai-compatible",
        model="gpt-test",
        api_key_env="PIRANESI_TEST_API_KEY",
        external_calls_enabled=True,
        privacy_mode=False,
        metadata={"purpose": "unit-test"},
    )

    auth = require_cloud_provider_ready(config, env={"PIRANESI_TEST_API_KEY": "secret-key"})
    encoded_config = json.dumps(config.model_dump(mode="json"), sort_keys=True)
    encoded_trace = json.dumps(config.trace_metadata(), sort_keys=True)

    assert auth.api_key == "secret-key"
    assert "secret-key" not in encoded_config
    assert "secret-key" not in encoded_trace
    assert config.trace_metadata()["external_call"] is True
    assert config.trace_metadata()["api_key_env"] == "PIRANESI_TEST_API_KEY"


def test_cloud_provider_requires_configured_byok_variable() -> None:
    config = cloud_provider_config(
        provider_name="openai-compatible",
        model="gpt-test",
        api_key_env="PIRANESI_TEST_API_KEY",
        external_calls_enabled=True,
        privacy_mode=False,
    )

    with pytest.raises(AIProviderError, match="BYOK environment variable"):
        require_cloud_provider_ready(config, env={})
