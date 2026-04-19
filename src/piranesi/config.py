from __future__ import annotations

import json
import os
import tomllib
import types
from collections.abc import Mapping
from copy import deepcopy
from hashlib import sha256
from pathlib import Path
from typing import Any, Literal, Union, get_args, get_origin

from pydantic import BaseModel, ConfigDict, Field, ValidationError


class ConfigError(RuntimeError):
    """Raised when Piranesi configuration cannot be loaded."""


class ModelsConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    scanner: str = "gpt-4o-mini"
    detector: str = "gpt-4o-mini"
    triage: str = "gpt-4o"
    skeptic: str | None = None
    patcher: str = "claude-sonnet-4-20250514"


class ModelFallbackConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    default: str | None = None
    scanner: str | None = None
    detector: str | None = None
    triage: str | None = None
    skeptic: str | None = None
    patcher: str | None = None


class BudgetConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    max_cost_usd: float = 5.0
    warn_at_usd: float | None = None
    max_tokens: int = 500_000


class SandboxConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    docker_image: str = "piranesi-sandbox:latest"
    timeout_seconds: int = 30
    network_enabled: bool = False


class OutputConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    format: Literal[
        "json",
        "markdown",
        "both",
        "sarif",
        "junit",
        "csv",
        "tui",
        "compliance",
    ] = "both"
    output_dir: str = "./piranesi-output"


class TraceConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    enabled: bool = True
    file_path: str = ".piranesi-trace.jsonl"
    log_prompts: bool = False


class JoernConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    binary_path: str = "joern"
    server_port: int = 8080
    startup_timeout_seconds: int = 30
    query_timeout_seconds: int = 60
    jvm_memory: str = "2g"


class CustomSourceConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    patterns: list[str] = Field(default_factory=list)
    source_type: str = "custom"


class CustomSinkConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    patterns: list[str] = Field(default_factory=list)
    sink_type: str = "custom"
    cwe_id: str | None = None
    include_receivers: list[str] = Field(default_factory=list)
    exclude_receivers: list[str] = Field(default_factory=list)


class ScanConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    include_patterns: list[str] = Field(
        default_factory=lambda: ["**/*.ts", "**/*.tsx", "**/*.js", "**/*.jsx", "**/*.go"]
    )
    exclude_patterns: list[str] = Field(
        default_factory=lambda: [
            "**/node_modules/**",
            "**/dist/**",
            "**/*.d.ts",
            "**/vendor/**",
            # piranesi output dirs
            "**/piranesi-output/**",
            "**/.piranesi-cache/**",
            "**/.piranesi-out/**",
            "**/.piranesi-trace*",
        ]
    )
    max_file_size: int = 1_048_576
    include_tests: bool = False
    frameworks: list[str] = Field(default_factory=lambda: ["auto"])
    incremental: bool = False
    incremental_threshold: int = 20
    incremental_invalidation_depth: int = 3
    cpg_cache_max_mb: int = 500
    sbom_format: Literal["spdx", "cyclonedx"] | None = None
    custom_sources: CustomSourceConfig = Field(default_factory=CustomSourceConfig)
    custom_sinks: CustomSinkConfig = Field(default_factory=CustomSinkConfig)


class DetectConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    context_sensitivity: Literal[0, 1, 2] = 1
    max_contexts: int = Field(default=1000, ge=1)
    hot_threshold: int = Field(default=50, ge=1)
    context_timeout: int = Field(default=300, ge=1)


class PluginsConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    disabled: list[str] = Field(default_factory=list)


class RulesConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    paths: list[str] = Field(default_factory=lambda: ["./rules", "~/.piranesi/rules/*"])
    disabled_rules: list[str] = Field(default_factory=list)
    require_signatures: bool = False
    trusted_keys: list[str] = Field(default_factory=list)


class HooksConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    pre_commit: bool = True
    fail_severity: Literal["low", "medium", "high", "critical"] = "high"
    timeout: int = Field(default=60, ge=1)
    staged_only: bool = True


class TriageConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    ml_prefilter: bool = True
    ml_threshold: float = 0.5
    ml_model_path: str | None = None
    ml_conservative: bool = False


class ReachabilityConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    include_unreachable: bool = False
    dead_code_report: bool = False


class SuppressionConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    fail_on_invalid: bool = True
    fail_on_expired: bool = False
    fail_on_stale: bool = False


class BaselineConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    fail_on_new: bool = False
    fail_on_new_severity: Literal["low", "medium", "high", "critical"] = "low"


class VerifyConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    proof_mode: Literal["safe", "unsafe"] = "safe"
    target_profile: str | None = None
    target_profiles: dict[str, VerifyTargetProfileConfig] = Field(default_factory=dict)


class VerifyTargetProfileConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    command: str | None = None
    cwd: str | None = None
    env: dict[str, str] = Field(default_factory=dict)
    startup_timeout_seconds: int = Field(default=30, ge=1)
    readiness_url: str | None = None
    readiness_command: str | None = None
    base_url: str | None = None
    teardown: Literal["always", "on_success", "never"] = "always"
    logs_path: str | None = None


class RolloutPolicyProfileConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    verify_proof_mode: Literal["safe", "unsafe"] | None = None
    verify_target_profile: str | None = None
    max_cost_usd: float | None = None
    max_tokens: int | None = None
    trace_log_prompts: bool | None = None
    suppression_fail_on_invalid: bool | None = None
    suppression_fail_on_expired: bool | None = None
    suppression_fail_on_stale: bool | None = None
    allowed_models: list[str] = Field(default_factory=list)


class RolloutConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    environment: Literal["dev", "staging", "prod"] | None = None
    policy_profile: str | None = None
    policy_profiles: dict[str, RolloutPolicyProfileConfig] = Field(default_factory=dict)


class OwnershipPathMappingConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    path: str
    service: str | None = None
    system: str | None = None
    team: str | None = None
    owner: str | None = None
    repository: str | None = None
    environment: str | None = None
    control_owner: str | None = None


class OwnershipPackageMappingConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    package: str
    service: str | None = None
    system: str | None = None
    team: str | None = None
    owner: str | None = None
    repository: str | None = None
    environment: str | None = None
    control_owner: str | None = None


class OwnershipControlMappingConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    framework: str
    control: str
    owner: str


class OwnershipConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    service: str | None = None
    system: str | None = None
    team: str | None = None
    owner: str | None = None
    repository: str | None = None
    environment: str | None = None
    control_owner: str | None = None
    autodetect_repository: bool = True
    autodetect_service: bool = True
    path_mappings: list[OwnershipPathMappingConfig] = Field(default_factory=list)
    package_mappings: list[OwnershipPackageMappingConfig] = Field(default_factory=list)
    control_mappings: list[OwnershipControlMappingConfig] = Field(default_factory=list)


class LspConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    enabled: bool = True
    scan_on_save: bool = True
    debounce_ms: int = 1000
    max_findings_per_file: int = 50
    severity_filter: Literal["informational", "low", "medium", "high", "critical"] = "medium"


class PiranesiConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    models: ModelsConfig = Field(default_factory=ModelsConfig)
    models_fallback: ModelFallbackConfig = Field(default_factory=ModelFallbackConfig)
    budget: BudgetConfig = Field(default_factory=BudgetConfig)
    sandbox: SandboxConfig = Field(default_factory=SandboxConfig)
    output: OutputConfig = Field(default_factory=OutputConfig)
    trace: TraceConfig = Field(default_factory=TraceConfig)
    joern: JoernConfig = Field(default_factory=JoernConfig)
    scan: ScanConfig = Field(default_factory=ScanConfig)
    detect: DetectConfig = Field(default_factory=DetectConfig)
    triage: TriageConfig = Field(default_factory=TriageConfig)
    reachability: ReachabilityConfig = Field(default_factory=ReachabilityConfig)
    suppression: SuppressionConfig = Field(default_factory=SuppressionConfig)
    baseline: BaselineConfig = Field(default_factory=BaselineConfig)
    verify: VerifyConfig = Field(default_factory=VerifyConfig)
    rollout: RolloutConfig = Field(default_factory=RolloutConfig)
    ownership: OwnershipConfig = Field(default_factory=OwnershipConfig)
    lsp: LspConfig = Field(default_factory=LspConfig)
    plugins: PluginsConfig = Field(default_factory=PluginsConfig)
    rules: RulesConfig = Field(default_factory=RulesConfig)
    hooks: HooksConfig = Field(default_factory=HooksConfig)


def load_config(
    config_path: str | Path,
    *,
    env: Mapping[str, str] | None = None,
    cli_overrides: Mapping[str, Any] | None = None,
) -> PiranesiConfig:
    path = Path(config_path)
    data = _read_toml(path)
    data = _normalize_file_data(data)
    data = _apply_env_overrides(data, env or os.environ)
    data = _apply_cli_overrides(data, cli_overrides or {})
    try:
        config = PiranesiConfig.model_validate(data)
    except ValidationError as exc:
        raise ConfigError(f"invalid config at {path}: {exc}") from exc
    return _apply_rollout_policy_profile(config)


def config_hash(config: PiranesiConfig) -> str:
    payload = json.dumps(config.model_dump(mode="json"), sort_keys=True, separators=(",", ":"))
    return sha256(payload.encode("utf-8")).hexdigest()


def _apply_rollout_policy_profile(config: PiranesiConfig) -> PiranesiConfig:
    profile_name = config.rollout.policy_profile
    if profile_name is None:
        return config

    profile = config.rollout.policy_profiles.get(profile_name)
    if profile is None:
        raise ConfigError(
            "invalid rollout policy profile: "
            f"{profile_name!r} not found in [rollout.policy_profiles]"
        )

    if profile.verify_proof_mode is not None:
        config.verify.proof_mode = profile.verify_proof_mode
    if profile.verify_target_profile is not None:
        config.verify.target_profile = profile.verify_target_profile
    if profile.max_cost_usd is not None:
        config.budget.max_cost_usd = profile.max_cost_usd
    if profile.max_tokens is not None:
        config.budget.max_tokens = profile.max_tokens
    if profile.trace_log_prompts is not None:
        config.trace.log_prompts = profile.trace_log_prompts
    if profile.suppression_fail_on_invalid is not None:
        config.suppression.fail_on_invalid = profile.suppression_fail_on_invalid
    if profile.suppression_fail_on_expired is not None:
        config.suppression.fail_on_expired = profile.suppression_fail_on_expired
    if profile.suppression_fail_on_stale is not None:
        config.suppression.fail_on_stale = profile.suppression_fail_on_stale

    if profile.allowed_models:
        selected_models = {
            "scanner": config.models.scanner,
            "detector": config.models.detector,
            "triage": config.models.triage,
            "skeptic": config.models.skeptic,
            "patcher": config.models.patcher,
        }
        disallowed = [
            f"{stage}={model}"
            for stage, model in selected_models.items()
            if model is not None and model not in profile.allowed_models
        ]
        if disallowed:
            allowed = ", ".join(profile.allowed_models)
            joined = ", ".join(disallowed)
            raise ConfigError(
                "rollout policy profile rejected configured models: "
                f"{joined}. Allowed models: {allowed}"
            )
    return config


def _read_toml(path: Path) -> dict[str, Any]:
    if not path.exists():
        raise ConfigError(
            f"config file not found: {path}. "
            "run `piranesi init` to generate a default configuration."
        )
    try:
        with path.open("rb") as handle:
            loaded = tomllib.load(handle)
    except tomllib.TOMLDecodeError as exc:
        raise ConfigError(f"invalid TOML in {path}: {exc}") from exc
    if not isinstance(loaded, dict):
        raise ConfigError(f"invalid TOML structure in {path}: expected a table at the root")
    return loaded


def _normalize_file_data(data: dict[str, Any]) -> dict[str, Any]:
    normalized = deepcopy(data)
    models_section = normalized.get("models")
    if (
        isinstance(models_section, dict)
        and "budget" in models_section
        and "budget" not in normalized
    ):
        normalized["budget"] = models_section.pop("budget")
    if (
        isinstance(models_section, dict)
        and "fallback" in models_section
        and "models_fallback" not in normalized
    ):
        normalized["models_fallback"] = models_section.pop("fallback")
    return normalized


def _apply_env_overrides(data: dict[str, Any], env: Mapping[str, str]) -> dict[str, Any]:
    merged = deepcopy(data)
    for env_name, path, annotation in _iter_override_targets(PiranesiConfig):
        if env_name in env:
            _set_dotted_value(merged, path, _parse_env_value(annotation, env[env_name]))
    return merged


def _apply_cli_overrides(data: dict[str, Any], cli_overrides: Mapping[str, Any]) -> dict[str, Any]:
    merged = deepcopy(data)
    for path, value in cli_overrides.items():
        if value is not None:
            _set_dotted_value(merged, path, value)
    return merged


def _iter_override_targets(
    model_type: type[BaseModel],
    prefix: str = "",
) -> list[tuple[str, str, Any]]:
    targets: list[tuple[str, str, Any]] = []
    for field_name, field_info in model_type.model_fields.items():
        field_path = f"{prefix}.{field_name}" if prefix else field_name
        annotation = field_info.annotation
        nested_model = _extract_model_type(annotation)
        if nested_model is not None:
            targets.extend(_iter_override_targets(nested_model, field_path))
            continue
        env_name = f"PIRANESI_{field_path.replace('.', '_').upper()}"
        targets.append((env_name, field_path, annotation))
    return targets


def _extract_model_type(annotation: Any) -> type[BaseModel] | None:
    if isinstance(annotation, type) and issubclass(annotation, BaseModel):
        return annotation
    origin = get_origin(annotation)
    if origin is None:
        return None
    for candidate in get_args(annotation):
        if isinstance(candidate, type) and issubclass(candidate, BaseModel):
            return candidate
    return None


def _parse_env_value(annotation: Any, raw_value: str) -> Any:
    optional_annotation = _strip_optional(annotation)
    origin = get_origin(optional_annotation)
    if origin is list:
        if raw_value.strip().startswith("["):
            parsed = json.loads(raw_value)
            if not isinstance(parsed, list):
                raise ConfigError(f"expected a list override, got: {raw_value}")
            return parsed
        return [item.strip() for item in raw_value.split(",") if item.strip()]
    if optional_annotation is bool:
        lowered = raw_value.strip().lower()
        if lowered in {"1", "true", "yes", "on"}:
            return True
        if lowered in {"0", "false", "no", "off"}:
            return False
        raise ConfigError(f"invalid boolean override: {raw_value}")
    if optional_annotation is int:
        return int(raw_value)
    if optional_annotation is float:
        return float(raw_value)
    if optional_annotation is str:
        return raw_value
    if raw_value.strip().startswith(("{", "[")):
        return json.loads(raw_value)
    return raw_value


def _strip_optional(annotation: Any) -> Any:
    origin = get_origin(annotation)
    if origin is None:
        return annotation
    if origin not in {types.UnionType, Union}:
        return annotation
    non_none = [candidate for candidate in get_args(annotation) if candidate is not type(None)]
    return non_none[0] if len(non_none) == 1 else annotation


def _set_dotted_value(data: dict[str, Any], dotted_path: str, value: Any) -> None:
    current = data
    parts = dotted_path.split(".")
    for part in parts[:-1]:
        current = current.setdefault(part, {})
    current[parts[-1]] = value
