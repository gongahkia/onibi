from __future__ import annotations

import json
import re
from pathlib import Path

from pydantic import BaseModel, ConfigDict, Field

_PORT_RE = re.compile(r"(?:PORT=|--port\s+|-p\s+)(?P<port>\d{2,5})")


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
    lines = [
        f'target_profile = "{profile_name}"',
        "",
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


__all__ = [
    "LaunchCandidate",
    "LaunchPlan",
    "infer_launch_plan",
    "render_launch_plan",
    "render_target_profile_snippet",
]
