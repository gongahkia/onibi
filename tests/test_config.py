from __future__ import annotations

from pathlib import Path

import pytest

from piranesi.config import ConfigError, load_config


def test_load_config_defaults(config_file) -> None:
    path = config_file("")

    config = load_config(path)

    assert config.models.scanner == "gpt-4o-mini"
    assert config.trace.file_path == ".piranesi-trace.jsonl"
    assert config.joern.binary_path == "joern"
    assert config.joern.query_timeout_seconds == 60
    assert config.output.output_dir == "./piranesi-output"


def test_load_config_from_file(fixtures_dir: Path) -> None:
    path = fixtures_dir / "configs" / "default.toml"

    config = load_config(path)

    assert config.models.scanner == "scanner-from-file"
    assert config.budget.max_cost_usd == 9.5
    assert config.output.output_dir == "./custom-output"


def test_environment_override(config_file, monkeypatch: pytest.MonkeyPatch) -> None:
    path = config_file("[models]\nscanner = 'base-model'\n")
    monkeypatch.setenv("PIRANESI_MODELS_SCANNER", "override-model")

    config = load_config(path)

    assert config.models.scanner == "override-model"


def test_invalid_toml_raises(config_file) -> None:
    path = config_file("[models\nscanner = 'oops'\n")

    with pytest.raises(ConfigError):
        load_config(path)


def test_missing_file_raises(tmp_path: Path) -> None:
    missing = tmp_path / "missing.toml"

    with pytest.raises(ConfigError):
        load_config(missing)


def test_nested_budget_block_is_normalized(config_file) -> None:
    path = config_file("[models.budget]\nmax_cost_usd = 7.25\nmax_tokens = 123\n")

    config = load_config(path)

    assert config.budget.max_cost_usd == 7.25
    assert config.budget.max_tokens == 123


def test_load_joern_config_from_file(config_file) -> None:
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
