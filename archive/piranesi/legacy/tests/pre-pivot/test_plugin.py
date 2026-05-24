from __future__ import annotations

import json
from pathlib import Path
from unittest.mock import patch

import pytest

from piranesi.plugin import (
    ChiFramework,
    EchoFramework,
    ExpressFramework,
    FastifyFramework,
    FrameworkPlugin,
    GinFramework,
    GoStdlibFramework,
    NestJSFramework,
    NextJSFramework,
    ReporterPlugin,
    RulePlugin,
    SpringBootFramework,
    discover_framework_plugins,
    get_framework_plugins_by_name,
    plugin_api_manifest,
)
from piranesi.scan.specs import (
    BUILTIN_SANITIZER_SPECS,
    BUILTIN_SINK_SPECS,
    BUILTIN_SOURCE_SPECS,
    CHI_SOURCE_SPECS,
    ECHO_SOURCE_SPECS,
    FASTIFY_SANITIZER_SPECS,
    FASTIFY_SINK_SPECS,
    FASTIFY_SOURCE_SPECS,
    GIN_SOURCE_SPECS,
    GO_SANITIZER_SPECS,
    GO_SINK_SPECS,
    GO_STDLIB_SOURCE_SPECS,
    NESTJS_SOURCE_SPECS,
    NEXTJS_SOURCE_SPECS,
    SPRINGBOOT_SANITIZER_SPECS,
    SPRINGBOOT_SINK_SPECS,
    SPRINGBOOT_SOURCE_SPECS,
    SanitizerSpec,
    SinkSpec,
    SourceSpec,
    get_sanitizer_specs,
    get_sink_specs,
    get_source_specs,
)

# --- ABC enforcement ---


def test_framework_plugin_cannot_be_instantiated_directly() -> None:
    with pytest.raises(TypeError):
        FrameworkPlugin()  # type: ignore[abstract]


def test_rule_plugin_cannot_be_instantiated_directly() -> None:
    with pytest.raises(TypeError):
        RulePlugin()  # type: ignore[abstract]


def test_reporter_plugin_cannot_be_instantiated_directly() -> None:
    with pytest.raises(TypeError):
        ReporterPlugin()  # type: ignore[abstract]


def test_incomplete_framework_subclass_raises() -> None:
    class Incomplete(FrameworkPlugin):
        def name(self) -> str:
            return "incomplete"

    with pytest.raises(TypeError):
        Incomplete()  # type: ignore[abstract]


# --- built-in plugin specs ---


def test_express_plugin_provides_builtin_specs() -> None:
    p = ExpressFramework()
    assert p.name() == "express"
    assert p.source_specs() == list(BUILTIN_SOURCE_SPECS)
    assert p.sink_specs() == list(BUILTIN_SINK_SPECS)
    assert p.sanitizer_specs() == list(BUILTIN_SANITIZER_SPECS)
    assert p.tsconfig_overrides() == {}


def test_nestjs_plugin_provides_nestjs_specs() -> None:
    p = NestJSFramework()
    assert p.name() == "nestjs"
    names = {s.name for s in p.source_specs()}
    for spec in NESTJS_SOURCE_SPECS:
        assert spec.name in names
    for spec in BUILTIN_SOURCE_SPECS:
        assert spec.name in names
    assert p.sink_specs() == list(BUILTIN_SINK_SPECS)
    assert p.sanitizer_specs() == list(BUILTIN_SANITIZER_SPECS)


def test_nextjs_plugin_provides_nextjs_specs() -> None:
    p = NextJSFramework()
    assert p.name() == "nextjs"
    names = {s.name for s in p.source_specs()}
    for spec in NEXTJS_SOURCE_SPECS:
        assert spec.name in names
    for spec in BUILTIN_SOURCE_SPECS:
        assert spec.name in names
    assert p.sink_specs() == list(BUILTIN_SINK_SPECS)
    assert p.sanitizer_specs() == list(BUILTIN_SANITIZER_SPECS)


def test_fastify_plugin_provides_fastify_specs() -> None:
    p = FastifyFramework()
    assert p.name() == "fastify"
    src_names = {s.name for s in p.source_specs()}
    for spec in FASTIFY_SOURCE_SPECS:
        assert spec.name in src_names
    for spec in BUILTIN_SOURCE_SPECS:
        assert spec.name in src_names
    sink_names = {s.name for s in p.sink_specs()}
    for spec in FASTIFY_SINK_SPECS:
        assert spec.name in sink_names
    for spec in BUILTIN_SINK_SPECS:
        assert spec.name in sink_names
    san_names = {s.name for s in p.sanitizer_specs()}
    for spec in FASTIFY_SANITIZER_SPECS:
        assert spec.name in san_names
    for spec in BUILTIN_SANITIZER_SPECS:
        assert spec.name in san_names


# --- detect ---


def test_express_detect(tmp_path: Path) -> None:
    pkg = tmp_path / "package.json"
    pkg.write_text(json.dumps({"dependencies": {"express": "^4.0.0"}}))
    assert ExpressFramework().detect(tmp_path) is True


def test_express_detect_missing(tmp_path: Path) -> None:
    pkg = tmp_path / "package.json"
    pkg.write_text(json.dumps({"dependencies": {"fastify": "^4.0.0"}}))
    assert ExpressFramework().detect(tmp_path) is False


def test_nestjs_detect(tmp_path: Path) -> None:
    pkg = tmp_path / "package.json"
    pkg.write_text(json.dumps({"dependencies": {"@nestjs/core": "^10.0.0"}}))
    assert NestJSFramework().detect(tmp_path) is True


def test_nextjs_detect_requires_config(tmp_path: Path) -> None:
    pkg = tmp_path / "package.json"
    pkg.write_text(json.dumps({"dependencies": {"next": "^14.0.0"}}))
    assert NextJSFramework().detect(tmp_path) is False  # no next.config.*
    (tmp_path / "next.config.js").write_text("module.exports = {}")
    assert NextJSFramework().detect(tmp_path) is True


def test_fastify_detect(tmp_path: Path) -> None:
    pkg = tmp_path / "package.json"
    pkg.write_text(json.dumps({"devDependencies": {"fastify": "^4.0.0"}}))
    assert FastifyFramework().detect(tmp_path) is True


# --- discovery ---


def test_discover_framework_plugins_returns_builtins() -> None:
    plugins = discover_framework_plugins()
    names = [p.name() for p in plugins]
    assert "express" in names
    assert "nestjs" in names
    assert "nextjs" in names
    assert "fastify" in names


def test_disabled_plugin_filtered_out() -> None:
    plugins = discover_framework_plugins(disabled=frozenset({"nestjs", "fastify"}))
    names = [p.name() for p in plugins]
    assert "express" in names
    assert "nextjs" in names
    assert "nestjs" not in names
    assert "fastify" not in names


def test_get_framework_plugins_by_name_filters() -> None:
    plugins = get_framework_plugins_by_name(frozenset({"express", "fastify"}))
    names = {p.name() for p in plugins}
    assert names == {"express", "fastify"}


# --- mock external plugin discovery ---


class _FakeEntryPoint:
    def __init__(self, name: str, cls: type) -> None:
        self._name = name
        self._cls = cls

    @property
    def name(self) -> str:
        return self._name

    def load(self) -> type:
        return self._cls


class _ExternalFramework(FrameworkPlugin):
    def name(self) -> str:
        return "external-fw"

    def detect(self, project_root: Path) -> bool:
        return False

    def source_specs(self) -> list[SourceSpec]:
        return []

    def sink_specs(self) -> list[SinkSpec]:
        return []

    def sanitizer_specs(self) -> list[SanitizerSpec]:
        return []


def test_discover_loads_external_framework_plugin() -> None:
    fake_eps = [_FakeEntryPoint("external-fw", _ExternalFramework)]
    with patch("piranesi.plugin.entry_points", return_value=fake_eps):
        plugins = discover_framework_plugins()
    names = [p.name() for p in plugins]
    assert "external-fw" in names
    assert "express" in names


def test_discover_skips_disabled_external_plugin() -> None:
    fake_eps = [_FakeEntryPoint("external-fw", _ExternalFramework)]
    with patch("piranesi.plugin.entry_points", return_value=fake_eps):
        plugins = discover_framework_plugins(disabled=frozenset({"external-fw"}))
    names = [p.name() for p in plugins]
    assert "external-fw" not in names


def test_discover_skips_non_framework_entry_point() -> None:
    class NotAPlugin:
        pass

    fake_eps = [_FakeEntryPoint("bad", NotAPlugin)]
    with patch("piranesi.plugin.entry_points", return_value=fake_eps):
        plugins = discover_framework_plugins()
    names = [p.name() for p in plugins]
    assert "bad" not in names


def test_plugin_api_manifest_snapshot() -> None:
    assert plugin_api_manifest() == {
        "version": "1.0",
        "stable": {
            "entry_point_groups": [
                "piranesi.frameworks",
                "piranesi.rules",
                "piranesi.reporters",
            ],
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
            ]
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


def test_discover_warns_when_framework_uses_experimental_api(
    caplog: pytest.LogCaptureFixture,
) -> None:
    class _ExperimentalFramework(_ExternalFramework):
        def name(self) -> str:
            return "experimental-fw"

        def tsconfig_overrides(self) -> dict[str, object]:
            return {"strict": True}

    fake_eps = [_FakeEntryPoint("experimental-fw", _ExperimentalFramework)]
    with (
        caplog.at_level("WARNING"),
        patch("piranesi.plugin.entry_points", return_value=fake_eps),
    ):
        plugins = discover_framework_plugins()
    names = [p.name() for p in plugins]
    assert "experimental-fw" in names
    assert "uses experimental API 'tsconfig_overrides'" in caplog.text


# --- get_*_specs backward compat through plugins ---


def test_get_source_specs_default_returns_express_base() -> None:
    specs = get_source_specs()
    names = {s.name for s in specs}
    assert "express_req_body" in names
    assert "express_req_origin_header" in names


def test_get_sink_specs_default_includes_cors_reflection_sink() -> None:
    specs = get_sink_specs()
    names = {s.name for s in specs}
    assert "cors_allow_origin_reflection" in names


def test_get_source_specs_with_nestjs_adds_nestjs() -> None:
    specs = get_source_specs(frameworks=("nestjs",))
    names = {s.name for s in specs}
    assert "express_req_body" in names
    assert "nestjs_body" in names


def test_get_sink_specs_with_fastify_adds_fastify() -> None:
    specs = get_sink_specs(frameworks=("fastify",))
    names = {s.name for s in specs}
    assert "raw_sql_query" in names
    assert "fastify_reply_send" in names


def test_get_sanitizer_specs_with_fastify() -> None:
    specs = get_sanitizer_specs(frameworks=("fastify",))
    names = {s.name for s in specs}
    assert "html_escape" in names
    assert "fastify_schema_validation" in names


def test_get_source_specs_disabled_express() -> None:
    specs = get_source_specs(disabled_plugins=frozenset({"express"}))
    names = {s.name for s in specs}
    assert "express_req_body" not in names


# --- Go plugin specs ---


def test_gin_plugin_provides_gin_specs() -> None:
    p = GinFramework()
    assert p.name() == "gin"
    assert p.source_specs() == list(GIN_SOURCE_SPECS)
    assert p.sink_specs() == list(GO_SINK_SPECS)
    assert p.sanitizer_specs() == list(GO_SANITIZER_SPECS)


def test_echo_plugin_provides_echo_specs() -> None:
    p = EchoFramework()
    assert p.name() == "echo"
    assert p.source_specs() == list(ECHO_SOURCE_SPECS)
    assert p.sink_specs() == list(GO_SINK_SPECS)
    assert p.sanitizer_specs() == list(GO_SANITIZER_SPECS)


def test_chi_plugin_provides_chi_and_stdlib_sources() -> None:
    p = ChiFramework()
    assert p.name() == "chi"
    expected_sources = list(CHI_SOURCE_SPECS) + list(GO_STDLIB_SOURCE_SPECS)
    assert p.source_specs() == expected_sources
    assert p.sink_specs() == list(GO_SINK_SPECS)


def test_go_stdlib_plugin_provides_stdlib_specs() -> None:
    p = GoStdlibFramework()
    assert p.name() == "go-stdlib"
    assert p.source_specs() == list(GO_STDLIB_SOURCE_SPECS)
    assert p.sink_specs() == list(GO_SINK_SPECS)
    assert p.sanitizer_specs() == list(GO_SANITIZER_SPECS)


# --- Go detect ---


def _write_gomod(tmp_path: Path, deps: str) -> None:
    gomod = tmp_path / "go.mod"
    gomod.write_text(f"module example.com/app\n\ngo 1.21\n\nrequire (\n{deps}\n)\n")
    (tmp_path / "main.go").write_text("package main\n")


def test_gin_detect(tmp_path: Path) -> None:
    _write_gomod(tmp_path, "\tgithub.com/gin-gonic/gin v1.9.1")
    assert GinFramework().detect(tmp_path) is True


def test_gin_detect_missing(tmp_path: Path) -> None:
    _write_gomod(tmp_path, "\tgithub.com/labstack/echo/v4 v4.11.0")
    assert GinFramework().detect(tmp_path) is False


def test_echo_detect(tmp_path: Path) -> None:
    _write_gomod(tmp_path, "\tgithub.com/labstack/echo/v4 v4.11.0")
    assert EchoFramework().detect(tmp_path) is True


def test_chi_detect(tmp_path: Path) -> None:
    _write_gomod(tmp_path, "\tgithub.com/go-chi/chi/v5 v5.0.10")
    assert ChiFramework().detect(tmp_path) is True


def test_go_stdlib_detect(tmp_path: Path) -> None:
    _write_gomod(tmp_path, "")
    assert GoStdlibFramework().detect(tmp_path) is True


def test_go_stdlib_detect_no_go_files(tmp_path: Path) -> None:
    gomod = tmp_path / "go.mod"
    gomod.write_text("module example.com/app\n\ngo 1.21\n")
    assert GoStdlibFramework().detect(tmp_path) is False


def test_gin_detect_no_gomod(tmp_path: Path) -> None:
    (tmp_path / "main.go").write_text("package main\n")
    assert GinFramework().detect(tmp_path) is False


# --- Go discovery integration ---


def test_discover_includes_go_plugins() -> None:
    plugins = discover_framework_plugins()
    names = [p.name() for p in plugins]
    assert "gin" in names
    assert "echo" in names
    assert "chi" in names
    assert "go-stdlib" in names


def test_get_sink_specs_with_gin() -> None:
    specs = get_sink_specs(frameworks=("gin",))
    names = {s.name for s in specs}
    assert "go_sql_query_sprintf" in names
    assert "go_exec_command" in names


def test_get_source_specs_with_echo() -> None:
    specs = get_source_specs(frameworks=("echo",))
    names = {s.name for s in specs}
    assert "echo_query_param" in names


# --- Spring Boot plugin specs ---


def test_springboot_plugin_provides_specs() -> None:
    p = SpringBootFramework()
    assert p.name() == "springboot"
    assert p.source_specs() == list(SPRINGBOOT_SOURCE_SPECS)
    assert p.sink_specs() == list(SPRINGBOOT_SINK_SPECS)
    assert p.sanitizer_specs() == list(SPRINGBOOT_SANITIZER_SPECS)
    assert p.tsconfig_overrides() == {}


def test_springboot_source_spec_count() -> None:
    assert len(SPRINGBOOT_SOURCE_SPECS) == 6


def test_springboot_sink_spec_count() -> None:
    assert len(SPRINGBOOT_SINK_SPECS) == 12


def test_springboot_sanitizer_spec_count() -> None:
    assert len(SPRINGBOOT_SANITIZER_SPECS) == 4


# --- Spring Boot detect ---


def _write_pom(tmp_path: Path, artifact_ids: list[str]) -> None:
    deps = "\n".join(
        f"<dependency><groupId>org.springframework.boot</groupId>"
        f"<artifactId>{aid}</artifactId></dependency>"
        for aid in artifact_ids
    )
    pom = tmp_path / "pom.xml"
    pom.write_text(f"<project><dependencies>{deps}</dependencies></project>")
    src = tmp_path / "src" / "main" / "java"
    src.mkdir(parents=True)
    (src / "App.java").write_text("public class App {}")


def _write_gradle(tmp_path: Path, deps: list[str]) -> None:
    lines = "\n".join(f"    implementation '{d}'" for d in deps)
    gradle = tmp_path / "build.gradle"
    gradle.write_text(f"dependencies {{\n{lines}\n}}")
    src = tmp_path / "src"
    src.mkdir(parents=True)
    (src / "App.java").write_text("public class App {}")


def test_springboot_detect_pom(tmp_path: Path) -> None:
    _write_pom(tmp_path, ["spring-boot-starter-web"])
    assert SpringBootFramework().detect(tmp_path) is True


def test_springboot_detect_gradle(tmp_path: Path) -> None:
    _write_gradle(tmp_path, ["org.springframework.boot:spring-boot-starter-web:3.2.0"])
    assert SpringBootFramework().detect(tmp_path) is True


def test_springboot_detect_missing_dep(tmp_path: Path) -> None:
    _write_pom(tmp_path, ["spring-boot-starter-data-jpa"])
    assert SpringBootFramework().detect(tmp_path) is False


def test_springboot_detect_no_java_files(tmp_path: Path) -> None:
    pom = tmp_path / "pom.xml"
    pom.write_text(
        "<project><dependencies><dependency>"
        "<artifactId>spring-boot-starter-web</artifactId>"
        "</dependency></dependencies></project>"
    )
    assert SpringBootFramework().detect(tmp_path) is False


# --- Spring Boot discovery integration ---


def test_discover_includes_springboot() -> None:
    plugins = discover_framework_plugins()
    names = [p.name() for p in plugins]
    assert "springboot" in names


def test_get_source_specs_with_springboot() -> None:
    specs = get_source_specs(frameworks=("springboot",))
    names = {s.name for s in specs}
    assert "spring_request_body" in names
    assert "spring_path_variable" in names
    assert "spring_servlet_get_parameter" in names


def test_get_sink_specs_with_springboot() -> None:
    specs = get_sink_specs(frameworks=("springboot",))
    names = {s.name for s in specs}
    assert "spring_jdbc_query" in names
    assert "spring_jpa_native_query_concat" in names
    assert "java_runtime_exec" in names
    assert "spring_rest_template" in names


def test_get_sanitizer_specs_with_springboot() -> None:
    specs = get_sanitizer_specs(frameworks=("springboot",))
    names = {s.name for s in specs}
    assert "spring_security_context" in names
    assert "spring_valid_annotation" in names
    assert "spring_pre_authorize_access_control" in names
    assert "spring_secured_access_control" in names


def test_springboot_disabled() -> None:
    plugins = discover_framework_plugins(disabled=frozenset({"springboot"}))
    names = [p.name() for p in plugins]
    assert "springboot" not in names


# --- CLI ---


def test_plugins_list_shows_builtins() -> None:
    from typer.testing import CliRunner

    from piranesi.cli import app

    runner = CliRunner()
    result = runner.invoke(app, ["plugins", "list"])
    assert result.exit_code == 0
    assert "express" in result.stdout
    assert "nestjs" in result.stdout
    assert "fastify" in result.stdout


# --- config ---


def test_plugins_config_disabled_default() -> None:
    from piranesi.config import PluginsConfig

    cfg = PluginsConfig()
    assert cfg.disabled == []


def test_plugins_config_roundtrip() -> None:
    from piranesi.config import PiranesiConfig

    cfg = PiranesiConfig(plugins={"disabled": ["nestjs"]})  # type: ignore[arg-type]
    assert "nestjs" in cfg.plugins.disabled
