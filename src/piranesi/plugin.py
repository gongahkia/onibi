from __future__ import annotations

import logging
from abc import ABC, abstractmethod
from copy import deepcopy
from importlib.metadata import entry_points
from pathlib import Path

from piranesi.scan.specs import (
    BUILTIN_SANITIZER_SPECS,
    BUILTIN_SINK_SPECS,
    BUILTIN_SOURCE_SPECS,
    CHI_SOURCE_SPECS,
    DJANGO_SOURCE_SPECS,
    ECHO_SOURCE_SPECS,
    FASTAPI_SOURCE_SPECS,
    FASTIFY_SANITIZER_SPECS,
    FASTIFY_SINK_SPECS,
    FASTIFY_SOURCE_SPECS,
    FLASK_SOURCE_SPECS,
    GIN_SOURCE_SPECS,
    GO_SANITIZER_SPECS,
    GO_SINK_SPECS,
    GO_STDLIB_SOURCE_SPECS,
    LARAVEL_SANITIZER_SPECS,
    LARAVEL_SINK_SPECS,
    LARAVEL_SOURCE_SPECS,
    NESTJS_SOURCE_SPECS,
    NEXTJS_SOURCE_SPECS,
    PHP_SANITIZER_SPECS,
    PHP_SINK_SPECS,
    PHP_SOURCE_SPECS,
    PYTHON_SANITIZER_SPECS,
    PYTHON_SINK_SPECS,
    RUBY_SANITIZER_SPECS,
    RUBY_SINK_SPECS,
    RUBY_SOURCE_SPECS,
    SPRINGBOOT_SANITIZER_SPECS,
    SPRINGBOOT_SINK_SPECS,
    SPRINGBOOT_SOURCE_SPECS,
    SYMFONY_SANITIZER_SPECS,
    SYMFONY_SINK_SPECS,
    SYMFONY_SOURCE_SPECS,
    WORDPRESS_SANITIZER_SPECS,
    WORDPRESS_SINK_SPECS,
    WORDPRESS_SOURCE_SPECS,
    SanitizerSpec,
    SinkSpec,
    SourceSpec,
)

logger = logging.getLogger(__name__)

PLUGIN_API_VERSION = "1.0"

_PLUGIN_API_MANIFEST: dict[str, object] = {
    "version": PLUGIN_API_VERSION,
    "stable": {
        "entry_point_groups": ["piranesi.frameworks", "piranesi.rules", "piranesi.reporters"],
        "framework_plugin_interface": [
            "name(self) -> str",
            "detect(self, project_root: Path) -> bool",
            "source_specs(self) -> list[SourceSpec]",
            "sink_specs(self) -> list[SinkSpec]",
            "sanitizer_specs(self) -> list[SanitizerSpec]",
        ],
        "rule_plugin_interface": [
            "name(self) -> str",
            "rule_files(self) -> list[Path]",
        ],
        "reporter_plugin_interface": [
            "name(self) -> str",
            "format_id(self) -> str",
            "render(self, report: object, output_dir: Path) -> Path",
        ],
        "discovery_helpers": [
            "discover_framework_plugins",
            "discover_rule_plugins",
            "discover_reporter_plugins",
            "get_framework_plugins_by_name",
            "collect_source_specs",
            "collect_sink_specs",
            "collect_sanitizer_specs",
            "plugin_api_manifest",
        ],
    },
    "experimental": {
        "framework_plugin_hooks": [
            "tsconfig_overrides(self) -> dict[str, object]",
        ],
    },
    "internal": {
        "built_in_plugin_classes": [
            "ExpressFramework",
            "NestJSFramework",
            "NextJSFramework",
            "FastifyFramework",
            "FlaskFramework",
            "DjangoFramework",
            "FastAPIFramework",
            "SpringBootFramework",
            "GinFramework",
            "EchoFramework",
            "ChiFramework",
            "GoStdlibFramework",
            "PhpFramework",
            "LaravelFramework",
            "SymfonyFramework",
            "WordPressFramework",
            "RubyFramework",
            "RailsFramework",
            "SinatraFramework",
        ],
        "registry_constant": "_BUILTIN_FRAMEWORK_PLUGINS",
    },
}


class FrameworkPlugin(ABC):
    @abstractmethod
    def name(self) -> str: ...
    @abstractmethod
    def detect(self, project_root: Path) -> bool: ...
    @abstractmethod
    def source_specs(self) -> list[SourceSpec]: ...
    @abstractmethod
    def sink_specs(self) -> list[SinkSpec]: ...
    @abstractmethod
    def sanitizer_specs(self) -> list[SanitizerSpec]: ...
    def tsconfig_overrides(self) -> dict[str, object]:
        return {}


class RulePlugin(ABC):
    @abstractmethod
    def name(self) -> str: ...
    @abstractmethod
    def rule_files(self) -> list[Path]: ...


class ReporterPlugin(ABC):
    @abstractmethod
    def name(self) -> str: ...
    @abstractmethod
    def format_id(self) -> str: ...
    @abstractmethod
    def render(self, report: object, output_dir: Path) -> Path: ...


# --- built-in framework plugins ---


def _check_package_dep(project_root: Path, package_name: str) -> bool:
    import json as _json

    pkg = project_root / "package.json"
    if not pkg.is_file():
        return False
    try:
        data = _json.loads(pkg.read_text(encoding="utf-8"))
    except (OSError, _json.JSONDecodeError):
        return False
    if not isinstance(data, dict):
        return False
    for key in ("dependencies", "devDependencies", "peerDependencies", "optionalDependencies"):
        section = data.get(key)
        if isinstance(section, dict) and package_name in section:
            return True
    return False


def _check_python_dep(project_root: Path, package_name: str) -> bool:
    for req_file in ("requirements.txt", "requirements.in"):
        req_path = project_root / req_file
        if req_path.is_file():
            try:
                content = req_path.read_text(encoding="utf-8").lower()
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
                )
                if dep == package_name:
                    return True
    pyproject = project_root / "pyproject.toml"
    if pyproject.is_file():
        try:
            content = pyproject.read_text(encoding="utf-8").lower()
            if package_name in content:
                return True
        except OSError:
            pass
    setup = project_root / "setup.py"
    if setup.is_file():
        try:
            content = setup.read_text(encoding="utf-8").lower()
            if package_name in content:
                return True
        except OSError:
            pass
    return False


class FlaskFramework(FrameworkPlugin):
    def name(self) -> str:
        return "flask"

    def detect(self, project_root: Path) -> bool:
        return _check_python_dep(project_root, "flask")

    def source_specs(self) -> list[SourceSpec]:
        return list(FLASK_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(PYTHON_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(PYTHON_SANITIZER_SPECS)


class DjangoFramework(FrameworkPlugin):
    def name(self) -> str:
        return "django"

    def detect(self, project_root: Path) -> bool:
        return _check_python_dep(project_root, "django")

    def source_specs(self) -> list[SourceSpec]:
        return list(DJANGO_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(PYTHON_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(PYTHON_SANITIZER_SPECS)


class FastAPIFramework(FrameworkPlugin):
    def name(self) -> str:
        return "fastapi"

    def detect(self, project_root: Path) -> bool:
        return _check_python_dep(project_root, "fastapi")

    def source_specs(self) -> list[SourceSpec]:
        return list(FASTAPI_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(PYTHON_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(PYTHON_SANITIZER_SPECS)


class ExpressFramework(FrameworkPlugin):
    def name(self) -> str:
        return "express"

    def detect(self, project_root: Path) -> bool:
        return _check_package_dep(project_root, "express")

    def source_specs(self) -> list[SourceSpec]:
        return list(BUILTIN_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(BUILTIN_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(BUILTIN_SANITIZER_SPECS)


class NestJSFramework(FrameworkPlugin):
    def name(self) -> str:
        return "nestjs"

    def detect(self, project_root: Path) -> bool:
        return _check_package_dep(project_root, "@nestjs/core")

    def source_specs(self) -> list[SourceSpec]:
        return list(BUILTIN_SOURCE_SPECS) + list(NESTJS_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(BUILTIN_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(BUILTIN_SANITIZER_SPECS)


class NextJSFramework(FrameworkPlugin):
    def name(self) -> str:
        return "nextjs"

    def detect(self, project_root: Path) -> bool:
        if not _check_package_dep(project_root, "next"):
            return False
        return any(p.is_file() for p in project_root.glob("next.config.*"))

    def source_specs(self) -> list[SourceSpec]:
        return list(NEXTJS_SOURCE_SPECS) + list(BUILTIN_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(BUILTIN_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(BUILTIN_SANITIZER_SPECS)


class FastifyFramework(FrameworkPlugin):
    def name(self) -> str:
        return "fastify"

    def detect(self, project_root: Path) -> bool:
        return _check_package_dep(project_root, "fastify")

    def source_specs(self) -> list[SourceSpec]:
        return list(BUILTIN_SOURCE_SPECS) + list(FASTIFY_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(BUILTIN_SINK_SPECS) + list(FASTIFY_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(BUILTIN_SANITIZER_SPECS) + list(FASTIFY_SANITIZER_SPECS)


def _check_java_dep(project_root: Path, artifact_id: str) -> bool:
    """Check pom.xml or build.gradle for a Java/Maven/Gradle dependency."""
    pom = project_root / "pom.xml"
    if pom.is_file():
        try:
            content = pom.read_text(encoding="utf-8")
            if artifact_id in content:
                return True
        except OSError:
            pass
    for gradle_name in ("build.gradle", "build.gradle.kts"):
        gradle = project_root / gradle_name
        if gradle.is_file():
            try:
                content = gradle.read_text(encoding="utf-8")
                if artifact_id in content:
                    return True
            except OSError:
                pass
    return False


def _has_java_files(project_root: Path) -> bool:
    return any(project_root.rglob("*.java"))


class SpringBootFramework(FrameworkPlugin):
    def name(self) -> str:
        return "springboot"

    def detect(self, project_root: Path) -> bool:
        if not _has_java_files(project_root):
            return False
        return _check_java_dep(project_root, "spring-boot-starter-web")

    def source_specs(self) -> list[SourceSpec]:
        return list(SPRINGBOOT_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(SPRINGBOOT_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(SPRINGBOOT_SANITIZER_SPECS)


def _check_gomod_dep(project_root: Path, module_path: str) -> bool:
    gomod = project_root / "go.mod"
    if not gomod.is_file():
        return False
    try:
        content = gomod.read_text(encoding="utf-8")
    except OSError:
        return False
    return module_path in content


def _has_go_files(project_root: Path) -> bool:
    return any(path.is_file() and "vendor" not in path.parts for path in project_root.rglob("*.go"))


class GinFramework(FrameworkPlugin):
    def name(self) -> str:
        return "gin"

    def detect(self, project_root: Path) -> bool:
        return _check_gomod_dep(project_root, "github.com/gin-gonic/gin")

    def source_specs(self) -> list[SourceSpec]:
        return list(GIN_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(GO_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(GO_SANITIZER_SPECS)


class EchoFramework(FrameworkPlugin):
    def name(self) -> str:
        return "echo"

    def detect(self, project_root: Path) -> bool:
        return _check_gomod_dep(project_root, "github.com/labstack/echo")

    def source_specs(self) -> list[SourceSpec]:
        return list(ECHO_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(GO_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(GO_SANITIZER_SPECS)


class ChiFramework(FrameworkPlugin):
    def name(self) -> str:
        return "chi"

    def detect(self, project_root: Path) -> bool:
        return _check_gomod_dep(project_root, "github.com/go-chi/chi")

    def source_specs(self) -> list[SourceSpec]:
        return list(CHI_SOURCE_SPECS) + list(GO_STDLIB_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(GO_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(GO_SANITIZER_SPECS)


class GoStdlibFramework(FrameworkPlugin):
    def name(self) -> str:
        return "go-stdlib"

    def detect(self, project_root: Path) -> bool:
        if not _has_go_files(project_root):
            return False
        gomod = project_root / "go.mod"
        return gomod.is_file()

    def source_specs(self) -> list[SourceSpec]:
        return list(GO_STDLIB_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(GO_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(GO_SANITIZER_SPECS)


class PhpFramework(FrameworkPlugin):
    def name(self) -> str:
        return "php"

    def detect(self, project_root: Path) -> bool:
        if WordPressFramework().detect(project_root):
            return False
        if LaravelFramework().detect(project_root):
            return False
        if SymfonyFramework().detect(project_root):
            return False
        if any(project_root.rglob("*.php")):
            return True
        return (project_root / "composer.json").is_file()

    def source_specs(self) -> list[SourceSpec]:
        return list(PHP_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(PHP_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(PHP_SANITIZER_SPECS)


class LaravelFramework(FrameworkPlugin):
    def name(self) -> str:
        return "laravel"

    def detect(self, project_root: Path) -> bool:
        composer = project_root / "composer.json"
        if not composer.is_file():
            return False
        try:
            return "laravel" in composer.read_text(encoding="utf-8").lower()
        except OSError:
            return False

    def source_specs(self) -> list[SourceSpec]:
        return list(LARAVEL_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(LARAVEL_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(LARAVEL_SANITIZER_SPECS)


class SymfonyFramework(FrameworkPlugin):
    def name(self) -> str:
        return "symfony"

    def detect(self, project_root: Path) -> bool:
        composer = project_root / "composer.json"
        if not composer.is_file():
            return False
        try:
            return "symfony" in composer.read_text(encoding="utf-8").lower()
        except OSError:
            return False

    def source_specs(self) -> list[SourceSpec]:
        return list(SYMFONY_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(SYMFONY_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(SYMFONY_SANITIZER_SPECS)


class WordPressFramework(FrameworkPlugin):
    def name(self) -> str:
        return "wordpress"

    def detect(self, project_root: Path) -> bool:
        return (project_root / "wp-config.php").is_file() or (project_root / "wp-content").is_dir()

    def source_specs(self) -> list[SourceSpec]:
        return list(WORDPRESS_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(WORDPRESS_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(WORDPRESS_SANITIZER_SPECS)


class RubyFramework(FrameworkPlugin):
    def name(self) -> str:
        return "ruby"

    def detect(self, project_root: Path) -> bool:
        if RailsFramework().detect(project_root) or SinatraFramework().detect(project_root):
            return False
        if any(project_root.rglob("*.rb")):
            return True
        return (project_root / "Gemfile").is_file()

    def source_specs(self) -> list[SourceSpec]:
        return list(RUBY_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(RUBY_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(RUBY_SANITIZER_SPECS)


class RailsFramework(FrameworkPlugin):
    def name(self) -> str:
        return "rails"

    def detect(self, project_root: Path) -> bool:
        gemfile = project_root / "Gemfile"
        if not gemfile.is_file():
            return False
        try:
            return "rails" in gemfile.read_text(encoding="utf-8").lower()
        except OSError:
            return False

    def source_specs(self) -> list[SourceSpec]:
        return list(RUBY_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(RUBY_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(RUBY_SANITIZER_SPECS)


class SinatraFramework(FrameworkPlugin):
    def name(self) -> str:
        return "sinatra"

    def detect(self, project_root: Path) -> bool:
        gemfile = project_root / "Gemfile"
        if not gemfile.is_file():
            return False
        try:
            return "sinatra" in gemfile.read_text(encoding="utf-8").lower()
        except OSError:
            return False

    def source_specs(self) -> list[SourceSpec]:
        return list(RUBY_SOURCE_SPECS)

    def sink_specs(self) -> list[SinkSpec]:
        return list(RUBY_SINK_SPECS)

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return list(RUBY_SANITIZER_SPECS)


# --- plugin registry ---

_BUILTIN_FRAMEWORK_PLUGINS: tuple[type[FrameworkPlugin], ...] = (
    ExpressFramework,
    NestJSFramework,
    NextJSFramework,
    FastifyFramework,
    FlaskFramework,
    DjangoFramework,
    FastAPIFramework,
    SpringBootFramework,
    GinFramework,
    EchoFramework,
    ChiFramework,
    GoStdlibFramework,
    PhpFramework,
    LaravelFramework,
    SymfonyFramework,
    WordPressFramework,
    RubyFramework,
    RailsFramework,
    SinatraFramework,
)


def plugin_api_manifest() -> dict[str, object]:
    """Return the documented plugin API stability manifest."""
    return deepcopy(_PLUGIN_API_MANIFEST)


def _warn_experimental_framework_usage(instance: FrameworkPlugin) -> None:
    if type(instance).tsconfig_overrides is FrameworkPlugin.tsconfig_overrides:
        return
    logger.warning(
        "framework plugin '%s' uses experimental API 'tsconfig_overrides'; "
        "compatibility may change",
        instance.name(),
    )


def discover_framework_plugins(
    *,
    disabled: frozenset[str] = frozenset(),
) -> list[FrameworkPlugin]:
    plugins: list[FrameworkPlugin] = []
    seen: set[str] = set()
    for cls in _BUILTIN_FRAMEWORK_PLUGINS:
        instance = cls()
        if instance.name() in disabled:
            continue
        if instance.name() in seen:
            logger.warning("duplicate framework plugin name: %s", instance.name())
            continue
        seen.add(instance.name())
        _warn_experimental_framework_usage(instance)
        plugins.append(instance)
    for ep in entry_points(group="piranesi.frameworks"):
        try:
            cls = ep.load()
            instance = cls()
        except Exception:
            logger.warning("failed to load framework plugin: %s", ep.name, exc_info=True)
            continue
        if not isinstance(instance, FrameworkPlugin):
            logger.warning("entry point %s is not a FrameworkPlugin, skipping", ep.name)
            continue
        if instance.name() in disabled:
            continue
        if instance.name() in seen:
            logger.warning("duplicate framework plugin name: %s", instance.name())
            continue
        seen.add(instance.name())
        _warn_experimental_framework_usage(instance)
        plugins.append(instance)
    return plugins


def discover_rule_plugins(
    *,
    disabled: frozenset[str] = frozenset(),
) -> list[RulePlugin]:
    plugins: list[RulePlugin] = []
    seen: set[str] = set()
    for ep in entry_points(group="piranesi.rules"):
        try:
            cls = ep.load()
            instance = cls()
        except Exception:
            logger.warning("failed to load rule plugin: %s", ep.name, exc_info=True)
            continue
        if not isinstance(instance, RulePlugin):
            logger.warning("entry point %s is not a RulePlugin, skipping", ep.name)
            continue
        if instance.name() in disabled:
            continue
        if instance.name() in seen:
            logger.warning("duplicate rule plugin name: %s", instance.name())
            continue
        seen.add(instance.name())
        plugins.append(instance)
    return plugins


def discover_reporter_plugins(
    *,
    disabled: frozenset[str] = frozenset(),
) -> list[ReporterPlugin]:
    plugins: list[ReporterPlugin] = []
    seen: set[str] = set()
    for ep in entry_points(group="piranesi.reporters"):
        try:
            cls = ep.load()
            instance = cls()
        except Exception:
            logger.warning("failed to load reporter plugin: %s", ep.name, exc_info=True)
            continue
        if not isinstance(instance, ReporterPlugin):
            logger.warning("entry point %s is not a ReporterPlugin, skipping", ep.name)
            continue
        if instance.name() in disabled:
            continue
        if instance.name() in seen:
            logger.warning("duplicate reporter plugin name: %s", instance.name())
            continue
        seen.add(instance.name())
        plugins.append(instance)
    return plugins


def get_framework_plugins_by_name(
    framework_names: frozenset[str],
    *,
    disabled: frozenset[str] = frozenset(),
) -> list[FrameworkPlugin]:
    """Return framework plugins whose name is in *framework_names*."""
    all_plugins = discover_framework_plugins(disabled=disabled)
    return [p for p in all_plugins if p.name() in framework_names]


def collect_source_specs(
    framework_names: frozenset[str],
    *,
    disabled: frozenset[str] = frozenset(),
) -> list[SourceSpec]:
    """Aggregate source specs from all matching framework plugins."""
    specs: list[SourceSpec] = []
    for plugin in get_framework_plugins_by_name(framework_names, disabled=disabled):
        specs.extend(plugin.source_specs())
    return specs


def collect_sink_specs(
    framework_names: frozenset[str],
    *,
    disabled: frozenset[str] = frozenset(),
) -> list[SinkSpec]:
    specs: list[SinkSpec] = []
    for plugin in get_framework_plugins_by_name(framework_names, disabled=disabled):
        specs.extend(plugin.sink_specs())
    return specs


def collect_sanitizer_specs(
    framework_names: frozenset[str],
    *,
    disabled: frozenset[str] = frozenset(),
) -> list[SanitizerSpec]:
    specs: list[SanitizerSpec] = []
    for plugin in get_framework_plugins_by_name(framework_names, disabled=disabled):
        specs.extend(plugin.sanitizer_specs())
    return specs


__all__ = [
    "ChiFramework",
    "DjangoFramework",
    "EchoFramework",
    "ExpressFramework",
    "FastAPIFramework",
    "FastifyFramework",
    "FlaskFramework",
    "FrameworkPlugin",
    "GinFramework",
    "GoStdlibFramework",
    "NestJSFramework",
    "NextJSFramework",
    "ReporterPlugin",
    "RulePlugin",
    "SpringBootFramework",
    "collect_sanitizer_specs",
    "collect_sink_specs",
    "collect_source_specs",
    "discover_framework_plugins",
    "discover_reporter_plugins",
    "discover_rule_plugins",
    "get_framework_plugins_by_name",
    "plugin_api_manifest",
]
