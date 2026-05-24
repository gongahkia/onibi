from __future__ import annotations

import importlib.util
import shutil
from dataclasses import dataclass


class RescanRuntimeError(RuntimeError):
    """Raised when optional rescan runtime support is unavailable."""


@dataclass(frozen=True, slots=True)
class ContainerRuntimeStatus:
    docker_python_available: bool
    docker_cli_path: str | None

    @property
    def available(self) -> bool:
        return self.docker_python_available and self.docker_cli_path is not None

    @property
    def install_hint(self) -> str:
        return (
            "Install optional rescan support with `pip install 'piranesi[rescan]'` "
            "or `uv sync --extra rescan`, and ensure the Docker CLI/daemon is available."
        )


def detect_container_runtime() -> ContainerRuntimeStatus:
    return ContainerRuntimeStatus(
        docker_python_available=importlib.util.find_spec("docker") is not None,
        docker_cli_path=shutil.which("docker"),
    )


def ensure_container_runtime() -> ContainerRuntimeStatus:
    status = detect_container_runtime()
    if status.available:
        return status
    missing = []
    if not status.docker_python_available:
        missing.append("docker Python package")
    if status.docker_cli_path is None:
        missing.append("Docker CLI")
    raise RescanRuntimeError(
        "rescan container runtime is unavailable: "
        f"missing {', '.join(missing)}. {status.install_hint}"
    )


__all__ = [
    "ContainerRuntimeStatus",
    "RescanRuntimeError",
    "detect_container_runtime",
    "ensure_container_runtime",
]
