from __future__ import annotations

import importlib.machinery

import pytest

from piranesi.rescan.runtime import (
    RescanRuntimeError,
    detect_container_runtime,
    ensure_container_runtime,
)


def test_detect_container_runtime_reports_optional_dependencies(monkeypatch) -> None:
    spec = importlib.machinery.ModuleSpec("docker", loader=None)
    monkeypatch.setattr("importlib.util.find_spec", lambda name: spec if name == "docker" else None)
    monkeypatch.setattr(
        "shutil.which",
        lambda name: "/usr/local/bin/docker" if name == "docker" else None,
    )

    status = detect_container_runtime()

    assert status.available is True
    assert status.docker_python_available is True
    assert status.docker_cli_path == "/usr/local/bin/docker"


def test_ensure_container_runtime_raises_actionable_error_without_optional_deps(
    monkeypatch,
) -> None:
    monkeypatch.setattr("importlib.util.find_spec", lambda _name: None)
    monkeypatch.setattr("shutil.which", lambda _name: None)

    with pytest.raises(RescanRuntimeError) as exc_info:
        ensure_container_runtime()

    message = str(exc_info.value)
    assert "docker Python package" in message
    assert "Docker CLI" in message
    assert "piranesi[rescan]" in message
    assert "uv sync --extra rescan" in message
