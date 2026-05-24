from __future__ import annotations

import os
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any, Literal, Protocol

from pydantic import BaseModel, ConfigDict, Field, field_validator

from piranesi.ai.redaction import RedactedPromptPayload

AI_PROVIDER_SCHEMA_VERSION: Literal["piranesi.ai-provider.v1"] = "piranesi.ai-provider.v1"


class AIProviderError(ValueError):
    """Raised when an AI provider cannot be used safely."""


class _StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class CloudAIProviderConfig(_StrictModel):
    schema_version: Literal["piranesi.ai-provider.v1"] = AI_PROVIDER_SCHEMA_VERSION
    provider_type: Literal["cloud"] = "cloud"
    provider_name: str
    model: str
    api_key_env: str
    base_url: str | None = None
    external_calls_enabled: bool = False
    privacy_mode: bool = True
    metadata: dict[str, Any] = Field(default_factory=dict)

    @field_validator("provider_name", "model", "api_key_env")
    @classmethod
    def _non_empty(cls, value: str) -> str:
        if not value.strip():
            raise ValueError("value must not be empty")
        return value.strip()

    def trace_metadata(self) -> dict[str, Any]:
        return {
            "schema_version": self.schema_version,
            "type": self.provider_type,
            "name": self.provider_name,
            "model": self.model,
            "base_url": self.base_url,
            "external_call": self.external_calls_enabled and not self.privacy_mode,
            "privacy_mode": self.privacy_mode,
            "api_key_env": self.api_key_env,
            "metadata": self.metadata,
        }


class LocalAIProviderConfig(_StrictModel):
    schema_version: Literal["piranesi.ai-provider.v1"] = AI_PROVIDER_SCHEMA_VERSION
    provider_type: Literal["local"] = "local"
    provider_name: str
    model: str | None = None
    endpoint_url: str | None = None
    command: list[str] = Field(default_factory=list)
    privacy_mode: bool = True
    metadata: dict[str, Any] = Field(default_factory=dict)

    @field_validator("provider_name")
    @classmethod
    def _non_empty(cls, value: str) -> str:
        if not value.strip():
            raise ValueError("value must not be empty")
        return value.strip()

    def trace_metadata(self) -> dict[str, Any]:
        return {
            "schema_version": self.schema_version,
            "type": self.provider_type,
            "name": self.provider_name,
            "model": self.model,
            "endpoint_url": self.endpoint_url,
            "command": self.command,
            "external_call": False,
            "privacy_mode": self.privacy_mode,
            "metadata": self.metadata,
        }


class AICompletionResult(_StrictModel):
    text: str
    metadata: dict[str, Any] = Field(default_factory=dict)


class AIProvider(Protocol):
    @property
    def trace_metadata(self) -> dict[str, Any]: ...

    def complete(self, prompt: RedactedPromptPayload) -> AICompletionResult: ...


@dataclass(frozen=True, slots=True)
class ProviderRuntimeAuth:
    api_key: str
    api_key_env: str


@dataclass(frozen=True, slots=True)
class StaticLocalAIProvider:
    config: LocalAIProviderConfig
    output_text: str
    output_metadata: Mapping[str, Any] | None = None

    @property
    def trace_metadata(self) -> dict[str, Any]:
        return self.config.trace_metadata()

    def complete(self, prompt: RedactedPromptPayload) -> AICompletionResult:
        if not isinstance(prompt, RedactedPromptPayload):
            raise AIProviderError("AI providers require a RedactedPromptPayload")
        return AICompletionResult(
            text=self.output_text,
            metadata={
                "provider": self.config.provider_name,
                "prompt_schema": prompt.schema_version,
                **dict(self.output_metadata or {}),
            },
        )


def cloud_provider_config(
    *,
    provider_name: str,
    model: str,
    api_key_env: str,
    base_url: str | None = None,
    external_calls_enabled: bool = False,
    privacy_mode: bool = True,
    metadata: Mapping[str, Any] | None = None,
) -> CloudAIProviderConfig:
    return CloudAIProviderConfig(
        provider_name=provider_name,
        model=model,
        api_key_env=api_key_env,
        base_url=base_url,
        external_calls_enabled=external_calls_enabled,
        privacy_mode=privacy_mode,
        metadata=dict(metadata or {}),
    )


def local_provider_config(
    *,
    provider_name: str,
    model: str | None = None,
    endpoint_url: str | None = None,
    command: list[str] | None = None,
    privacy_mode: bool = True,
    metadata: Mapping[str, Any] | None = None,
) -> LocalAIProviderConfig:
    configured_command = list(command or [])
    if endpoint_url is None and not configured_command:
        raise AIProviderError("local provider requires endpoint_url or command")
    return LocalAIProviderConfig(
        provider_name=provider_name,
        model=model,
        endpoint_url=endpoint_url,
        command=configured_command,
        privacy_mode=privacy_mode,
        metadata=dict(metadata or {}),
    )


def require_cloud_provider_ready(
    config: CloudAIProviderConfig,
    *,
    env: Mapping[str, str] | None = None,
) -> ProviderRuntimeAuth:
    active_env = env if env is not None else os.environ
    if config.privacy_mode:
        raise AIProviderError("privacy mode disables external model calls")
    if not config.external_calls_enabled:
        raise AIProviderError("external model calls require explicit enablement")
    api_key = active_env.get(config.api_key_env)
    if not api_key:
        raise AIProviderError(f"BYOK environment variable {config.api_key_env!r} is required")
    return ProviderRuntimeAuth(api_key=api_key, api_key_env=config.api_key_env)


__all__ = [
    "AI_PROVIDER_SCHEMA_VERSION",
    "AICompletionResult",
    "AIProvider",
    "AIProviderError",
    "CloudAIProviderConfig",
    "LocalAIProviderConfig",
    "ProviderRuntimeAuth",
    "StaticLocalAIProvider",
    "cloud_provider_config",
    "local_provider_config",
    "require_cloud_provider_ready",
]
