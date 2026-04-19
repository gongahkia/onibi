from __future__ import annotations

from collections.abc import Callable
from pathlib import Path

import pytest

from piranesi.config import ConfigError, load_config


def test_load_config_defaults(config_file: Callable[[str], Path]) -> None:
    path = config_file("")

    config = load_config(path)

    assert config.models.scanner == "gpt-4o-mini"
    assert config.trace.file_path == ".piranesi-trace.jsonl"
    assert config.joern.binary_path == "joern"
    assert config.joern.query_timeout_seconds == 60
    assert config.output.output_dir == "./piranesi-output"
    assert config.hooks.pre_commit is True
    assert config.hooks.fail_severity == "high"
    assert config.hooks.timeout == 60
    assert config.hooks.staged_only is True
    assert config.lsp.enabled is True
    assert config.lsp.scan_on_save is True
    assert config.lsp.debounce_ms == 1000
    assert config.verify.proof_mode == "safe"
    assert config.suppression.fail_on_invalid is True
    assert config.suppression.fail_on_expired is False
    assert config.suppression.fail_on_stale is False
    assert config.baseline.fail_on_new is False
    assert config.baseline.fail_on_new_severity == "low"
    assert config.rollout.environment is None
    assert config.rollout.policy_profile is None


def test_load_config_from_file(fixtures_dir: Path) -> None:
    path = fixtures_dir / "configs" / "default.toml"

    config = load_config(path)

    assert config.models.scanner == "scanner-from-file"
    assert config.budget.max_cost_usd == 9.5
    assert config.output.output_dir == "./custom-output"
    assert config.verify.proof_mode == "unsafe"


def test_load_baseline_config_from_file(config_file: Callable[[str], Path]) -> None:
    path = config_file(
        "\n".join(
            [
                "[baseline]",
                "fail_on_new = true",
                'fail_on_new_severity = "high"',
            ]
        )
    )

    config = load_config(path)

    assert config.baseline.fail_on_new is True
    assert config.baseline.fail_on_new_severity == "high"


def test_load_ownership_config_from_file(config_file: Callable[[str], Path]) -> None:
    path = config_file(
        "\n".join(
            [
                "[ownership]",
                'service = "payments-api"',
                'system = "checkout-platform"',
                'team = "payments-eng"',
                'owner = "payments-oncall"',
                'repository = "acme/payments"',
                'environment = "production"',
                'control_owner = "grc-controls"',
                "autodetect_repository = false",
                "autodetect_service = false",
                "",
                "[[ownership.path_mappings]]",
                'path = "src/payments/**"',
                'team = "payments-eng"',
                'owner = "payments-api-owner"',
                "",
                "[[ownership.package_mappings]]",
                'package = "@acme/auth"',
                'owner = "identity-team"',
                'control_owner = "identity-grc"',
                "",
                "[[ownership.control_mappings]]",
                'framework = "SOC2"',
                'control = "CC6.6"',
                'owner = "security-governance"',
            ]
        )
    )

    config = load_config(path)

    assert config.ownership.service == "payments-api"
    assert config.ownership.system == "checkout-platform"
    assert config.ownership.team == "payments-eng"
    assert config.ownership.owner == "payments-oncall"
    assert config.ownership.repository == "acme/payments"
    assert config.ownership.environment == "production"
    assert config.ownership.control_owner == "grc-controls"
    assert config.ownership.autodetect_repository is False
    assert config.ownership.autodetect_service is False
    assert config.ownership.path_mappings[0].path == "src/payments/**"
    assert config.ownership.path_mappings[0].owner == "payments-api-owner"
    assert config.ownership.package_mappings[0].package == "@acme/auth"
    assert config.ownership.package_mappings[0].control_owner == "identity-grc"
    assert config.ownership.control_mappings[0].framework == "SOC2"
    assert config.ownership.control_mappings[0].control == "CC6.6"
    assert config.ownership.control_mappings[0].owner == "security-governance"


def test_load_config_accepts_compliance_report_format(
    config_file: Callable[[str], Path],
) -> None:
    path = config_file("[output]\nformat = 'compliance'\n")

    config = load_config(path)

    assert config.output.format == "compliance"


def test_environment_override(
    config_file: Callable[[str], Path], monkeypatch: pytest.MonkeyPatch
) -> None:
    path = config_file("[models]\nscanner = 'base-model'\n")
    monkeypatch.setenv("PIRANESI_MODELS_SCANNER", "override-model")

    config = load_config(path)

    assert config.models.scanner == "override-model"


def test_invalid_toml_raises(config_file: Callable[[str], Path]) -> None:
    path = config_file("[models\nscanner = 'oops'\n")

    with pytest.raises(ConfigError):
        load_config(path)


def test_missing_file_raises(tmp_path: Path) -> None:
    missing = tmp_path / "missing.toml"

    with pytest.raises(ConfigError):
        load_config(missing)


def test_nested_budget_block_is_normalized(config_file: Callable[[str], Path]) -> None:
    path = config_file("[models.budget]\nmax_cost_usd = 7.25\nmax_tokens = 123\n")

    config = load_config(path)

    assert config.budget.max_cost_usd == 7.25
    assert config.budget.max_tokens == 123


def test_load_joern_config_from_file(config_file: Callable[[str], Path]) -> None:
    path = config_file(
        "\n".join(
            [
                "[joern]",
                "binary_path = '/opt/joern/bin/joern'",
                "server_port = 8087",
                "startup_timeout_seconds = 45",
                "query_timeout_seconds = 90",
                "jvm_memory = '4g'",
            ]
        )
    )

    config = load_config(path)

    assert config.joern.binary_path == "/opt/joern/bin/joern"
    assert config.joern.server_port == 8087
    assert config.joern.startup_timeout_seconds == 45
    assert config.joern.query_timeout_seconds == 90
    assert config.joern.jvm_memory == "4g"


def test_load_rules_config_from_file(config_file: Callable[[str], Path]) -> None:
    path = config_file(
        "\n".join(
            [
                "[rules]",
                'paths = ["./rules", "~/.piranesi/rules/*"]',
                'disabled_rules = ["noisy-rule-001", "org-rules:experimental-*"]',
                "require_signatures = true",
                'trusted_keys = ["~/.piranesi/trusted-keys"]',
            ]
        )
    )

    config = load_config(path)

    assert config.rules.paths == ["./rules", "~/.piranesi/rules/*"]
    assert config.rules.disabled_rules == ["noisy-rule-001", "org-rules:experimental-*"]
    assert config.rules.require_signatures is True
    assert config.rules.trusted_keys == ["~/.piranesi/trusted-keys"]


def test_load_hooks_config_from_file(config_file: Callable[[str], Path]) -> None:
    path = config_file(
        "\n".join(
            [
                "[hooks]",
                "pre_commit = false",
                'fail_severity = "critical"',
                "timeout = 15",
                "staged_only = false",
            ]
        )
    )

    config = load_config(path)

    assert config.hooks.pre_commit is False
    assert config.hooks.fail_severity == "critical"
    assert config.hooks.timeout == 15
    assert config.hooks.staged_only is False


def test_load_lsp_config_from_file(config_file: Callable[[str], Path]) -> None:
    path = config_file(
        "\n".join(
            [
                "[lsp]",
                "enabled = false",
                "scan_on_save = false",
                "debounce_ms = 250",
                "max_findings_per_file = 10",
                'severity_filter = "high"',
            ]
        )
    )

    config = load_config(path)

    assert config.lsp.enabled is False
    assert config.lsp.scan_on_save is False
    assert config.lsp.debounce_ms == 250
    assert config.lsp.max_findings_per_file == 10
    assert config.lsp.severity_filter == "high"


def test_load_verify_target_profiles_from_file(config_file: Callable[[str], Path]) -> None:
    path = config_file(
        "\n".join(
            [
                "[verify]",
                'proof_mode = "safe"',
                'target_profile = "express_dev"',
                "",
                "[verify.target_profiles.express_dev]",
                'command = "npm run dev"',
                'cwd = "examples/vuln-express"',
                "startup_timeout_seconds = 45",
                'readiness_url = "/healthz"',
                'base_url = "http://127.0.0.1:{port}"',
                'teardown = "on_success"',
                'logs_path = "verify/logs/express-dev.log"',
                "",
                "[verify.target_profiles.express_dev.env]",
                'PORT = "4010"',
                'NODE_ENV = "development"',
            ]
        )
    )

    config = load_config(path)

    assert config.verify.target_profile == "express_dev"
    profile = config.verify.target_profiles["express_dev"]
    assert profile.command == "npm run dev"
    assert profile.cwd == "examples/vuln-express"
    assert profile.startup_timeout_seconds == 45
    assert profile.readiness_url == "/healthz"
    assert profile.base_url == "http://127.0.0.1:{port}"
    assert profile.teardown == "on_success"
    assert profile.logs_path == "verify/logs/express-dev.log"
    assert profile.env["PORT"] == "4010"


def test_rollout_policy_profile_applies_verification_and_llm_controls(
    config_file: Callable[[str], Path],
) -> None:
    path = config_file(
        "\n".join(
            [
                "[models]",
                'scanner = "gpt-4o-mini"',
                'detector = "gpt-4o-mini"',
                'triage = "gpt-4o-mini"',
                'patcher = "gpt-4o-mini"',
                "",
                "[verify]",
                'proof_mode = "unsafe"',
                "",
                "[budget]",
                "max_cost_usd = 7.5",
                "max_tokens = 250000",
                "",
                "[suppression]",
                "fail_on_invalid = false",
                "fail_on_expired = false",
                "fail_on_stale = false",
                "",
                "[rollout]",
                'environment = "prod"',
                'policy_profile = "prod_strict"',
                "",
                "[rollout.policy_profiles.prod_strict]",
                'verify_proof_mode = "safe"',
                "max_cost_usd = 2.5",
                "max_tokens = 100000",
                "suppression_fail_on_invalid = true",
                "suppression_fail_on_expired = true",
                "suppression_fail_on_stale = true",
                'allowed_models = ["gpt-4o-mini"]',
            ]
        )
    )

    config = load_config(path)

    assert config.rollout.environment == "prod"
    assert config.rollout.policy_profile == "prod_strict"
    assert config.verify.proof_mode == "safe"
    assert config.budget.max_cost_usd == 2.5
    assert config.budget.max_tokens == 100000
    assert config.suppression.fail_on_invalid is True
    assert config.suppression.fail_on_expired is True
    assert config.suppression.fail_on_stale is True


def test_rollout_policy_profile_requires_existing_profile(
    config_file: Callable[[str], Path],
) -> None:
    path = config_file(
        "\n".join(
            [
                "[rollout]",
                'policy_profile = "missing_profile"',
            ]
        )
    )

    with pytest.raises(ConfigError, match="invalid rollout policy profile"):
        load_config(path)


def test_rollout_policy_profile_rejects_disallowed_models(
    config_file: Callable[[str], Path],
) -> None:
    path = config_file(
        "\n".join(
            [
                "[models]",
                'scanner = "gpt-4o"',
                "",
                "[rollout]",
                'policy_profile = "strict_model_list"',
                "",
                "[rollout.policy_profiles.strict_model_list]",
                'allowed_models = ["gpt-4o-mini"]',
            ]
        )
    )

    with pytest.raises(ConfigError, match="rollout policy profile rejected configured models"):
        load_config(path)
