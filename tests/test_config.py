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


def test_load_config_from_file(fixtures_dir: Path) -> None:
    path = fixtures_dir / "configs" / "default.toml"

    config = load_config(path)

    assert config.models.scanner == "scanner-from-file"
    assert config.budget.max_cost_usd == 9.5
    assert config.output.output_dir == "./custom-output"
    assert config.verify.proof_mode == "unsafe"


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
