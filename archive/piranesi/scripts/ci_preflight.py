from __future__ import annotations

import argparse
import json
import os
import platform
import shutil
import subprocess
import sys
from dataclasses import dataclass
from datetime import UTC, datetime
from importlib.util import find_spec
from pathlib import Path
from typing import Any


@dataclass(frozen=True, slots=True)
class CapabilityStatus:
    available: bool
    detail: str

    def as_dict(self) -> dict[str, object]:
        return {
            "available": self.available,
            "detail": self.detail,
        }


def _python_check() -> CapabilityStatus:
    major = sys.version_info.major
    minor = sys.version_info.minor
    if (major, minor) >= (3, 12):
        return CapabilityStatus(True, f"python {major}.{minor}")
    return CapabilityStatus(False, f"python {major}.{minor} is below required 3.12")


def _binary_check(binary: str) -> CapabilityStatus:
    resolved = shutil.which(binary)
    if resolved is None:
        return CapabilityStatus(False, f"{binary} not found on PATH")
    return CapabilityStatus(True, f"{binary} found at {resolved}")


def _docker_daemon_check() -> CapabilityStatus:
    docker_binary = shutil.which("docker")
    if docker_binary is None:
        return CapabilityStatus(False, "docker binary missing")
    try:
        completed = subprocess.run(
            [docker_binary, "info", "--format", "{{.ServerVersion}}"],
            check=True,
            capture_output=True,
            text=True,
            timeout=10,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        return CapabilityStatus(False, f"docker daemon unavailable: {exc}")
    version = completed.stdout.strip() or "unknown"
    return CapabilityStatus(True, f"docker daemon ready ({version})")


def _optional_dependency_check(module: str) -> CapabilityStatus:
    if find_spec(module) is None:
        return CapabilityStatus(False, f"optional dependency {module} not installed")
    return CapabilityStatus(True, f"optional dependency {module} installed")


def _runtime_constraints_check() -> CapabilityStatus:
    os_name = platform.system().lower()
    if os_name not in {"linux", "darwin"}:
        return CapabilityStatus(False, f"unsupported runtime OS {os_name}")
    return CapabilityStatus(True, f"runtime OS {os_name}")


def _build_payload() -> dict[str, Any]:
    python = _python_check()
    joern = _binary_check("joern")
    docker_binary = _binary_check("docker")
    docker_daemon = _docker_daemon_check()
    runtime = _runtime_constraints_check()
    optional = {
        module: _optional_dependency_check(module).as_dict()
        for module in ("watchfiles", "pygls", "textual")
    }

    joern_ready = joern.available
    docker_ready = docker_binary.available and docker_daemon.available
    integration_ready = joern_ready or docker_ready
    core_ready = python.available and runtime.available

    return {
        "generated_at": datetime.now(UTC).isoformat(),
        "core_ready": core_ready,
        "integration_ready": integration_ready,
        "joern_available": joern_ready,
        "docker_available": docker_ready,
        "checks": {
            "python": python.as_dict(),
            "joern_binary": joern.as_dict(),
            "docker_binary": docker_binary.as_dict(),
            "docker_daemon": docker_daemon.as_dict(),
            "runtime_constraints": runtime.as_dict(),
            "optional_dependencies": optional,
        },
    }


def _write_github_outputs(payload: dict[str, Any]) -> None:
    github_output = os.getenv("GITHUB_OUTPUT")
    if not github_output:
        return
    output_path = Path(github_output)
    with output_path.open("a", encoding="utf-8") as handle:
        handle.write(f"core_ready={str(payload['core_ready']).lower()}\n")
        handle.write(f"integration_ready={str(payload['integration_ready']).lower()}\n")
        handle.write(f"joern_available={str(payload['joern_available']).lower()}\n")
        handle.write(f"docker_available={str(payload['docker_available']).lower()}\n")


def _parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Compute CI capability preflight for lane routing.",
    )
    parser.add_argument(
        "--write-json",
        type=Path,
        help="Write capability payload to this JSON file.",
    )
    parser.add_argument(
        "--emit-github-outputs",
        action="store_true",
        help="Emit lane capability booleans to $GITHUB_OUTPUT when available.",
    )
    parser.add_argument(
        "--require-core",
        action="store_true",
        help="Exit non-zero when core-required constraints are not met.",
    )
    return parser.parse_args()


def main() -> int:
    args = _parse_args()
    payload = _build_payload()

    if args.write_json is not None:
        args.write_json.parent.mkdir(parents=True, exist_ok=True)
        args.write_json.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")

    if args.emit_github_outputs:
        _write_github_outputs(payload)

    print(json.dumps(payload, indent=2, sort_keys=True))

    if args.require_core and not payload["core_ready"]:
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
