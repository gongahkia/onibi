from __future__ import annotations

from collections.abc import Callable
from pathlib import Path
from typing import Any

import pytest

from piranesi.config import OutputConfig, PiranesiConfig


@pytest.fixture
def fixtures_dir() -> Path:
    return Path(__file__).parent / "fixtures"


@pytest.fixture
def ts_fixtures(fixtures_dir: Path) -> Path:
    return fixtures_dir / "typescript"


@pytest.fixture
def default_config(tmp_path: Path) -> PiranesiConfig:
    return PiranesiConfig(output=OutputConfig(output_dir=str(tmp_path / "output")))


@pytest.fixture
def mock_llm(monkeypatch: pytest.MonkeyPatch) -> dict[str, Any]:
    state: dict[str, Any] = {
        "response": {"choices": [{"message": {"content": "ok"}}]},
        "calls": [],
    }

    def _completion(*args: Any, **kwargs: Any) -> dict[str, Any]:
        state["calls"].append({"args": args, "kwargs": kwargs})
        return state["response"]

    import litellm

    monkeypatch.setattr(litellm, "completion", _completion)
    return state


@pytest.fixture
def config_file(tmp_path: Path) -> Callable[[str], Path]:
    def _create(content: str) -> Path:
        path = tmp_path / "piranesi.toml"
        path.write_text(content, encoding="utf-8")
        return path

    return _create
