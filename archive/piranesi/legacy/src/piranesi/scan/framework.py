from __future__ import annotations

import json
from collections.abc import Sequence
from dataclasses import dataclass
from pathlib import Path

_PACKAGE_DEPENDENCY_KEYS = (
    "dependencies",
    "devDependencies",
    "peerDependencies",
    "optionalDependencies",
)
_SUPPORTED_ROUTE_EXTENSIONS = frozenset({".ts", ".js"})


@dataclass(frozen=True, slots=True)
class NextJsRoute:
    file: Path
    kind: str
    route_pattern: str


def detect_framework(project_root: Path) -> list[str]:
    dependencies = _package_dependencies(project_root)
    next_config_exists = any(path.is_file() for path in project_root.glob("next.config.*"))
    python_deps = _python_dependencies(project_root)
    java_deps = _java_dependencies(project_root)
    go_deps = _go_dependencies(project_root)
    has_go_files = _has_go_source_files(project_root)
    php_framework = _php_framework(project_root)
    ruby_framework = _ruby_framework(project_root)

    detected: list[str] = []
    if "@nestjs/core" in dependencies:
        detected.append("nestjs")
    if "next" in dependencies and next_config_exists:
        detected.append("nextjs")
    if "fastify" in dependencies:
        detected.append("fastify")
    if "express" in dependencies:
        detected.append("express")
    if "koa" in dependencies:
        detected.append("koa")
    if "flask" in python_deps:
        detected.append("flask")
    if "django" in python_deps:
        detected.append("django")
    if "fastapi" in python_deps:
        detected.append("fastapi")
    if "spring-boot-starter-web" in java_deps and any(project_root.rglob("*.java")):
        detected.append("springboot")
    if any(dep.startswith("github.com/gin-gonic/gin") for dep in go_deps):
        detected.append("gin")
    if any(dep.startswith("github.com/labstack/echo") for dep in go_deps):
        detected.append("echo")
    if any(dep.startswith("github.com/go-chi/chi") for dep in go_deps):
        detected.append("chi")
    if has_go_files and (project_root / "go.mod").is_file():
        detected.append("go-stdlib")
    if php_framework is not None:
        detected.append(php_framework)
    if ruby_framework is not None:
        detected.append(ruby_framework)
    return detected


def detect_frameworks(project_root: Path) -> tuple[str, ...]:
    return tuple(detect_framework(project_root))


def resolve_frameworks(
    project_root: Path,
    requested_frameworks: Sequence[str] | None,
) -> tuple[str, ...]:
    resolved: list[str] = []
    normalized_requested = [framework.lower() for framework in requested_frameworks or ("auto",)]

    if not normalized_requested or "auto" in normalized_requested:
        resolved.extend(detect_frameworks(project_root))

    for framework in normalized_requested:
        if framework == "auto" or framework in resolved:
            continue
        resolved.append(framework)

    return tuple(resolved)


def discover_nextjs_routes(project_root: Path) -> tuple[NextJsRoute, ...]:
    root = Path(project_root).resolve(strict=False)
    routes: list[NextJsRoute] = []

    pages_api_dir = root / "pages" / "api"
    if pages_api_dir.is_dir():
        for path in sorted(pages_api_dir.rglob("*")):
            if not _is_supported_route_file(path):
                continue
            routes.append(
                NextJsRoute(
                    file=path.resolve(strict=False),
                    kind="pages_router",
                    route_pattern=_pages_api_route_pattern(root, path),
                )
            )

    app_dir = root / "app"
    if app_dir.is_dir():
        for path in sorted(app_dir.rglob("*")):
            if not _is_supported_route_file(path):
                continue
            if path.name.startswith("route."):
                routes.append(
                    NextJsRoute(
                        file=path.resolve(strict=False),
                        kind="app_router",
                        route_pattern=_app_route_pattern(root, path),
                    )
                )
                continue
            if path.name.startswith("actions."):
                routes.append(
                    NextJsRoute(
                        file=path.resolve(strict=False),
                        kind="server_action",
                        route_pattern=_actions_route_pattern(root, path),
                    )
                )

    return tuple(routes)


def _java_dependencies(project_root: Path) -> set[str]:
    """Extract artifact IDs from pom.xml or build.gradle."""
    deps: set[str] = set()
    pom = Path(project_root) / "pom.xml"
    if pom.is_file():
        try:
            import re

            content = pom.read_text(encoding="utf-8")
            for m in re.finditer(r"<artifactId>\s*([^<]+?)\s*</artifactId>", content):
                deps.add(m.group(1).strip())
        except OSError:
            pass
    for gradle_name in ("build.gradle", "build.gradle.kts"):
        gradle = Path(project_root) / gradle_name
        if not gradle.is_file():
            continue
        try:
            content = gradle.read_text(encoding="utf-8")
            for line in content.splitlines():
                line = line.strip()
                if not line or line.startswith("//"):
                    continue
                for token in line.replace("'", '"').split('"'):
                    parts = token.split(":")
                    if len(parts) >= 2:
                        deps.add(parts[1].strip())
        except OSError:
            pass
    return deps


def _python_dependencies(project_root: Path) -> set[str]:
    deps: set[str] = set()
    for req_file in ("requirements.txt", "requirements.in"):
        req_path = Path(project_root) / req_file
        if not req_path.is_file():
            continue
        try:
            content = req_path.read_text(encoding="utf-8")
        except OSError:
            continue
        for line in content.splitlines():
            line = line.split("#", 1)[0].strip()
            if not line or line.startswith("-"):
                continue
            dep = (
                line.split("==")[0]
                .split(">=")[0]
                .split("<=")[0]
                .split("~=")[0]
                .split("!=")[0]
                .split("[")[0]
                .strip()
                .lower()
            )
            if dep:
                deps.add(dep)
    for manifest in ("pyproject.toml", "setup.py"):
        manifest_path = Path(project_root) / manifest
        if manifest_path.is_file():
            try:
                content = manifest_path.read_text(encoding="utf-8").lower()
            except OSError:
                continue
            for candidate in ("flask", "django", "fastapi"):
                if candidate in content:
                    deps.add(candidate)
    return deps


def _go_dependencies(project_root: Path) -> set[str]:
    gomod = Path(project_root) / "go.mod"
    if not gomod.is_file():
        return set()

    try:
        content = gomod.read_text(encoding="utf-8")
    except OSError:
        return set()

    deps: set[str] = set()
    for line in content.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("//"):
            continue
        parts = stripped.split()
        if len(parts) >= 3 and parts[0] == "require" and "." in parts[1]:
            deps.add(parts[1])
            continue
        if len(parts) >= 2 and "." in parts[0]:
            deps.add(parts[0])
    return deps


def _package_dependencies(project_root: Path) -> set[str]:
    package_json_path = Path(project_root) / "package.json"
    if not package_json_path.is_file():
        return set()

    try:
        payload = json.loads(package_json_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return set()

    if not isinstance(payload, dict):
        return set()

    dependencies: set[str] = set()
    for key in _PACKAGE_DEPENDENCY_KEYS:
        section = payload.get(key)
        if isinstance(section, dict):
            dependencies.update(
                name for name, version in section.items() if isinstance(name, str) and version
            )
    return dependencies


def _has_go_source_files(project_root: Path) -> bool:
    return any(path.is_file() and "vendor" not in path.parts for path in project_root.rglob("*.go"))


def _is_supported_route_file(path: Path) -> bool:
    return path.is_file() and path.suffix in _SUPPORTED_ROUTE_EXTENSIONS


def _php_framework(project_root: Path) -> str | None:
    if (project_root / "wp-config.php").is_file() or (project_root / "wp-content").is_dir():
        return "wordpress"

    composer_path = project_root / "composer.json"
    if composer_path.is_file():
        try:
            payload = json.loads(composer_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            payload = {}
        flattened = json.dumps(payload).lower() if isinstance(payload, dict) else ""
        if "laravel" in flattened:
            return "laravel"
        if "symfony" in flattened:
            return "symfony"

    if any(path.is_file() for path in project_root.rglob("*.php")):
        return "php"
    return None


def _ruby_framework(project_root: Path) -> str | None:
    gemfile_path = project_root / "Gemfile"
    if gemfile_path.is_file():
        try:
            content = gemfile_path.read_text(encoding="utf-8").lower()
        except OSError:
            content = ""
        if "rails" in content:
            return "rails"
        if "sinatra" in content:
            return "sinatra"
        return "ruby"

    if any(path.is_file() for path in project_root.rglob("*.rb")):
        return "ruby"
    return None


def _pages_api_route_pattern(project_root: Path, path: Path) -> str:
    relative = path.resolve(strict=False).relative_to(project_root.resolve(strict=False))
    parts = list(relative.parts)
    route_parts = ["api", *_strip_index(_strip_extension(parts[2:]))]
    return _render_route_pattern(route_parts)


def _app_route_pattern(project_root: Path, path: Path) -> str:
    relative = path.resolve(strict=False).relative_to(project_root.resolve(strict=False))
    route_parts = _normalize_app_route_parts(relative.parts[1:-1])
    return _render_route_pattern(route_parts)


def _actions_route_pattern(project_root: Path, path: Path) -> str:
    relative = path.resolve(strict=False).relative_to(project_root.resolve(strict=False))
    route_parts = _normalize_app_route_parts(relative.parts[1:-1])
    return _render_route_pattern(route_parts)


def _normalize_app_route_parts(parts: tuple[str, ...] | list[str]) -> list[str]:
    normalized: list[str] = []
    for part in parts:
        if not part:
            continue
        if part.startswith("(") and part.endswith(")"):
            continue
        if part.startswith("@"):
            continue
        normalized.append(part)
    return normalized


def _strip_extension(parts: list[str]) -> list[str]:
    if not parts:
        return []
    stripped = list(parts)
    stripped[-1] = Path(stripped[-1]).stem
    return stripped


def _strip_index(parts: list[str]) -> list[str]:
    if parts and parts[-1] == "index":
        return parts[:-1]
    return parts


def _render_route_pattern(parts: list[str]) -> str:
    if not parts:
        return "/"
    return "/" + "/".join(parts)


__all__ = [
    "NextJsRoute",
    "detect_framework",
    "detect_frameworks",
    "discover_nextjs_routes",
    "resolve_frameworks",
]
