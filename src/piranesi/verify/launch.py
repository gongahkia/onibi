from __future__ import annotations

import json
import os
import re
import subprocess
import time
from pathlib import Path
from urllib.parse import urljoin

import requests
from pydantic import BaseModel, ConfigDict, Field

_PORT_RE = re.compile(r"(?:PORT=|--port\s+|-p\s+|http\.server\s+)(?P<port>\d{2,5})")
_PROFILE_HEADER_RE = re.compile(r"^\[verify\.target_profiles\.(?P<name>[A-Za-z0-9_.-]+)(?:\..*)?]$")
_TOP_LEVEL_TARGET_PROFILE_RE = re.compile(r'^target_profile\s*=\s*".*"\s*$')


class LaunchCandidate(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    command: str
    cwd: str = "."
    base_url: str
    readiness_url: str = "/"
    env: dict[str, str] = Field(default_factory=dict)
    confidence: str = "medium"
    source: str
    reason: str


class LaunchPlan(BaseModel):
    model_config = ConfigDict(extra="forbid")

    target_dir: str
    candidates: list[LaunchCandidate] = Field(default_factory=list)
    warnings: list[str] = Field(default_factory=list)


class LaunchProfileWriteResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    profile_name: str
    config_path: str
    written: bool
    replaced: bool = False


class LaunchProbeResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    candidate_name: str
    command: str
    cwd: str
    base_url: str
    readiness_url: str
    ready: bool
    log_path: str
    exit_code: int | None = None
    startup_error: str | None = None


def infer_launch_plan(target_dir: str | Path) -> LaunchPlan:
    root = Path(target_dir).expanduser().resolve(strict=False)
    candidates: list[LaunchCandidate] = []
    warnings: list[str] = []
    if not root.is_dir():
        return LaunchPlan(
            target_dir=str(root),
            warnings=[f"target directory does not exist: {root}"],
        )
    candidates.extend(_node_candidates(root))
    candidates.extend(_python_candidates(root))
    if (root / "docker-compose.yml").is_file() or (root / "compose.yml").is_file():
        warnings.append("docker compose file detected; configure a target profile manually for now")
    if (root / "Dockerfile").is_file() and not candidates:
        warnings.append("Dockerfile detected but no local launch command inferred")
    return LaunchPlan(target_dir=str(root), candidates=candidates, warnings=warnings)


def render_launch_plan(plan: LaunchPlan) -> str:
    lines = [f"Piranesi launch plan: {plan.target_dir}"]
    if not plan.candidates:
        lines.append("No launch candidates inferred.")
    for index, candidate in enumerate(plan.candidates, start=1):
        lines.extend(
            [
                "",
                f"{index}. {candidate.name} ({candidate.confidence})",
                f"   command: {candidate.command}",
                f"   cwd: {candidate.cwd}",
                f"   base_url: {candidate.base_url}",
                f"   readiness_url: {candidate.readiness_url}",
                f"   source: {candidate.source}",
                f"   reason: {candidate.reason}",
            ]
        )
    if plan.warnings:
        lines.append("")
        lines.append("Warnings:")
        lines.extend(f"- {warning}" for warning in plan.warnings)
    if plan.candidates:
        lines.append("")
        lines.append("Config snippet:")
        lines.append(render_target_profile_snippet(plan.candidates[0]))
    return "\n".join(lines) + "\n"


def render_target_profile_snippet(candidate: LaunchCandidate, *, profile_name: str = "auto") -> str:
    return _render_target_profile_snippet(
        candidate,
        profile_name=profile_name,
        include_selector=True,
    )


def write_target_profile(
    config_path: str | Path,
    candidate: LaunchCandidate,
    *,
    profile_name: str = "auto",
    force: bool = False,
) -> LaunchProfileWriteResult:
    path = Path(config_path).expanduser().resolve(strict=False)
    original = path.read_text(encoding="utf-8") if path.exists() else ""
    existing = _profile_exists(original, profile_name)
    if existing and not force:
        raise ValueError(
            f"verify target profile {profile_name!r} already exists; rerun with --force"
        )
    updated = _remove_profile_blocks(original, profile_name) if existing else original
    updated = _set_top_level_target_profile(updated, profile_name)
    table = _render_target_profile_snippet(
        candidate,
        profile_name=profile_name,
        include_selector=False,
    )
    if updated and not updated.endswith("\n"):
        updated += "\n"
    updated += "\n" + table + "\n"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(updated.lstrip("\n"), encoding="utf-8")
    return LaunchProfileWriteResult(
        profile_name=profile_name,
        config_path=str(path),
        written=True,
        replaced=existing,
    )


def probe_launch_candidate(
    target_dir: str | Path,
    candidate: LaunchCandidate,
    *,
    output_dir: str | Path = "./piranesi-output",
    timeout_seconds: int = 30,
) -> LaunchProbeResult:
    root = Path(target_dir).expanduser().resolve(strict=False)
    cwd = root / candidate.cwd if not Path(candidate.cwd).is_absolute() else Path(candidate.cwd)
    debug_dir = Path(output_dir).expanduser().resolve(strict=False) / "debug"
    debug_dir.mkdir(parents=True, exist_ok=True)
    log_path = debug_dir / f"launch-{_safe_name(candidate.name)}.log"
    env = dict(os.environ)
    env.update(candidate.env)
    ready = False
    startup_error: str | None = None
    process: subprocess.Popen[str] | None = None
    with log_path.open("w", encoding="utf-8") as log_handle:
        log_handle.write(f"$ {candidate.command}\n")
        log_handle.flush()
        try:
            process = subprocess.Popen(  # noqa: S602
                candidate.command,
                shell=True,
                cwd=str(cwd),
                env=env,
                stdout=log_handle,
                stderr=subprocess.STDOUT,
                text=True,
            )
            ready = _wait_for_candidate_ready(candidate, process, timeout_seconds)
            if not ready:
                if process.poll() is not None:
                    startup_error = f"process exited before readiness ({process.returncode})"
                else:
                    startup_error = "readiness timeout"
        except Exception as exc:
            startup_error = str(exc)
        finally:
            if process is not None and process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
    return LaunchProbeResult(
        candidate_name=candidate.name,
        command=candidate.command,
        cwd=str(cwd),
        base_url=candidate.base_url,
        readiness_url=candidate.readiness_url,
        ready=ready,
        log_path=str(log_path),
        exit_code=None if process is None else process.poll(),
        startup_error=startup_error,
    )


def render_probe_result(result: LaunchProbeResult) -> str:
    lines = [
        f"Piranesi launch probe: {result.candidate_name}",
        f"Command: {result.command}",
        f"Cwd: {result.cwd}",
        f"Base URL: {result.base_url}",
        f"Readiness URL: {result.readiness_url}",
        f"Ready: {'yes' if result.ready else 'no'}",
        f"Log: {result.log_path}",
    ]
    if result.exit_code is not None:
        lines.append(f"Exit code: {result.exit_code}")
    if result.startup_error:
        lines.append(f"Startup error: {result.startup_error}")
    return "\n".join(lines) + "\n"


def _render_target_profile_snippet(
    candidate: LaunchCandidate,
    *,
    profile_name: str,
    include_selector: bool,
) -> str:
    lines = [
        f"[verify.target_profiles.{profile_name}]",
        f'command = "{_toml_escape(candidate.command)}"',
        f'cwd = "{_toml_escape(candidate.cwd)}"',
        f'base_url = "{_toml_escape(candidate.base_url)}"',
        f'readiness_url = "{_toml_escape(candidate.readiness_url)}"',
        'teardown = "always"',
    ]
    if candidate.env:
        lines.append("")
        lines.append(f"[verify.target_profiles.{profile_name}.env]")
        for key, value in sorted(candidate.env.items()):
            lines.append(f'{key} = "{_toml_escape(value)}"')
    if include_selector:
        lines = [f'target_profile = "{profile_name}"', "", *lines]
    return "\n".join(lines)


def _node_candidates(root: Path) -> list[LaunchCandidate]:
    package_json = root / "package.json"
    if not package_json.is_file():
        return []
    try:
        payload = json.loads(package_json.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return []
    scripts = payload.get("scripts", {})
    if not isinstance(scripts, dict):
        return []
    dependencies = payload.get("dependencies", {})
    dev_dependencies = payload.get("devDependencies", {})
    deps = {
        **(dependencies if isinstance(dependencies, dict) else {}),
        **(dev_dependencies if isinstance(dev_dependencies, dict) else {}),
    }
    candidates: list[LaunchCandidate] = []
    for script_name in ("start", "dev", "serve"):
        script = scripts.get(script_name)
        if not isinstance(script, str) or not script.strip():
            continue
        port = _infer_port(script, deps)
        candidates.append(
            LaunchCandidate(
                name=f"npm:{script_name}",
                command=f"npm run {script_name}",
                cwd=".",
                base_url=f"http://127.0.0.1:{port}",
                readiness_url=_node_readiness_url(deps),
                env={"PORT": str(port)},
                confidence="high" if script_name == "start" else "medium",
                source="package.json",
                reason=f"package.json defines scripts.{script_name}",
            )
        )
    return candidates


def _python_candidates(root: Path) -> list[LaunchCandidate]:
    files = {path.name for path in root.iterdir() if path.is_file()}
    candidates: list[LaunchCandidate] = []
    if "manage.py" in files:
        candidates.append(
            LaunchCandidate(
                name="django:runserver",
                command="python manage.py runserver 127.0.0.1:8000",
                base_url="http://127.0.0.1:8000",
                readiness_url="/",
                env={"PORT": "8000"},
                confidence="medium",
                source="manage.py",
                reason="Django manage.py detected",
            )
        )
    for module_name in ("main.py", "app.py"):
        module = root / module_name
        if not module.is_file():
            continue
        text = module.read_text(encoding="utf-8", errors="ignore")
        if "FastAPI(" in text:
            candidates.append(
                LaunchCandidate(
                    name=f"fastapi:{module.stem}",
                    command=f"python -m uvicorn {module.stem}:app --host 127.0.0.1 --port 8000",
                    base_url="http://127.0.0.1:8000",
                    readiness_url="/docs",
                    env={"PORT": "8000"},
                    confidence="high",
                    source=module_name,
                    reason="FastAPI app object detected",
                )
            )
        elif "Flask(" in text:
            candidates.append(
                LaunchCandidate(
                    name=f"flask:{module.stem}",
                    command=f"python {module_name}",
                    base_url="http://127.0.0.1:5000",
                    readiness_url="/",
                    env={"PORT": "5000", "FLASK_ENV": "development"},
                    confidence="medium",
                    source=module_name,
                    reason="Flask app object detected",
                )
            )
    return candidates


def _infer_port(script: str, deps: dict[str, object]) -> int:
    match = _PORT_RE.search(script)
    if match is not None:
        return int(match.group("port"))
    if "next" in deps:
        return 3000
    if "vite" in deps:
        return 5173
    return 3000


def _node_readiness_url(deps: dict[str, object]) -> str:
    if "next" in deps or "vite" in deps:
        return "/"
    return "/"


def _toml_escape(value: str) -> str:
    return value.replace("\\", "\\\\").replace('"', '\\"')


def _profile_exists(text: str, profile_name: str) -> bool:
    return any(
        line.strip() == f"[verify.target_profiles.{profile_name}]" for line in text.splitlines()
    )


def _remove_profile_blocks(text: str, profile_name: str) -> str:
    kept: list[str] = []
    skipping = False
    for line in text.splitlines():
        stripped = line.strip()
        match = _PROFILE_HEADER_RE.match(stripped)
        if match is not None:
            skipping = match.group("name") == profile_name
        elif stripped.startswith("[") and stripped.endswith("]"):
            skipping = False
        if not skipping:
            kept.append(line)
    return "\n".join(kept).rstrip() + ("\n" if kept else "")


def _set_top_level_target_profile(text: str, profile_name: str) -> str:
    lines = text.splitlines()
    for index, line in enumerate(lines):
        if _TOP_LEVEL_TARGET_PROFILE_RE.match(line.strip()):
            lines[index] = f'target_profile = "{profile_name}"'
            return "\n".join(lines).rstrip() + "\n"
    body = "\n".join(lines).rstrip() + "\n" if lines else ""
    return f'target_profile = "{profile_name}"\n' + body


def _wait_for_candidate_ready(
    candidate: LaunchCandidate,
    process: subprocess.Popen[str],
    timeout_seconds: int,
) -> bool:
    deadline = time.monotonic() + max(1, timeout_seconds)
    readiness_url = urljoin(
        candidate.base_url.rstrip("/") + "/",
        candidate.readiness_url.lstrip("/"),
    )
    while time.monotonic() < deadline:
        if process.poll() is not None:
            return False
        try:
            response = requests.get(readiness_url, timeout=2)
        except requests.RequestException:
            time.sleep(0.25)
            continue
        if response.status_code < 500:
            return True
        time.sleep(0.25)
    return False


def _safe_name(value: str) -> str:
    return "".join(ch.lower() if ch.isalnum() else "-" for ch in value).strip("-") or "app"


__all__ = [
    "LaunchCandidate",
    "LaunchPlan",
    "LaunchProbeResult",
    "LaunchProfileWriteResult",
    "infer_launch_plan",
    "probe_launch_candidate",
    "render_launch_plan",
    "render_probe_result",
    "render_target_profile_snippet",
    "write_target_profile",
]
