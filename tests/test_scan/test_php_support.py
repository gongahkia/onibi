from __future__ import annotations

import json
from pathlib import Path

from piranesi.plugin import LaravelFramework, PhpFramework, SymfonyFramework, WordPressFramework
from piranesi.scan.framework import detect_frameworks
from piranesi.scan.joern import (
    LANGUAGE_TO_JOERN_FRONTEND,
    LANGUAGE_TO_JOERN_IMPORT_MODULE,
    LANGUAGE_TO_JOERN_PARSE_LANGUAGE,
)
from piranesi.scan.specs import SinkType, SourceType, get_sanitizer_specs, get_sink_specs, get_source_specs


def _source_spec_by_name(specs, name: str):
    return next(spec for spec in specs if spec.name == name)


def _sink_spec_by_name(specs, name: str):
    return next(spec for spec in specs if spec.name == name)


def _sanitizer_spec_by_name(specs, name: str):
    return next(spec for spec in specs if spec.name == name)


def test_detect_frameworks_finds_laravel_from_composer(tmp_path: Path) -> None:
    (tmp_path / "composer.json").write_text(
        json.dumps({"require": {"laravel/framework": "^11.0"}}),
        encoding="utf-8",
    )
    (tmp_path / "routes").mkdir()
    (tmp_path / "routes" / "web.php").write_text("<?php\n", encoding="utf-8")

    assert detect_frameworks(tmp_path) == ("laravel",)


def test_detect_frameworks_finds_symfony_from_composer(tmp_path: Path) -> None:
    (tmp_path / "composer.json").write_text(
        json.dumps({"require": {"symfony/framework-bundle": "^7.0"}}),
        encoding="utf-8",
    )
    (tmp_path / "src").mkdir()
    (tmp_path / "src" / "Controller.php").write_text("<?php\n", encoding="utf-8")

    assert detect_frameworks(tmp_path) == ("symfony",)


def test_detect_frameworks_finds_wordpress_from_structure(tmp_path: Path) -> None:
    (tmp_path / "wp-config.php").write_text("<?php\n", encoding="utf-8")
    (tmp_path / "wp-content" / "plugins").mkdir(parents=True)
    (tmp_path / "wp-content" / "plugins" / "plugin.php").write_text("<?php\n", encoding="utf-8")

    assert detect_frameworks(tmp_path) == ("wordpress",)


def test_detect_frameworks_falls_back_to_raw_php(tmp_path: Path) -> None:
    (tmp_path / "index.php").write_text("<?php echo $_GET['name'];\n", encoding="utf-8")

    assert detect_frameworks(tmp_path) == ("php",)


def test_php_plugin_detects_raw_php_only_when_framework_specific_plugins_do_not_match(
    tmp_path: Path,
) -> None:
    (tmp_path / "index.php").write_text("<?php echo $_GET['name'];\n", encoding="utf-8")

    assert PhpFramework().detect(tmp_path) is True
    assert LaravelFramework().detect(tmp_path) is False
    assert SymfonyFramework().detect(tmp_path) is False
    assert WordPressFramework().detect(tmp_path) is False


def test_php_source_specs_include_superglobals() -> None:
    source_specs = get_source_specs(frameworks=("php",))

    assert _source_spec_by_name(source_specs, "php_get").source_type is SourceType.REQUEST_PARAM
    assert _source_spec_by_name(source_specs, "php_post").source_type is SourceType.REQUEST_BODY
    assert _source_spec_by_name(source_specs, "php_request").source_type is SourceType.REQUEST_PARAM
    assert _source_spec_by_name(source_specs, "php_cookie").source_type is SourceType.COOKIE


def test_laravel_specs_include_php_base_and_request_helpers() -> None:
    source_specs = get_source_specs(frameworks=("laravel",))
    sink_specs = get_sink_specs(frameworks=("laravel",))
    sanitizer_specs = get_sanitizer_specs(frameworks=("laravel",))

    assert _source_spec_by_name(source_specs, "php_get").source_type is SourceType.REQUEST_PARAM
    assert (
        _source_spec_by_name(source_specs, "laravel_request_input").source_type
        is SourceType.REQUEST_BODY
    )
    assert _sink_spec_by_name(sink_specs, "laravel_db_raw").sink_type is SinkType.SQL_QUERY
    assert _sink_spec_by_name(sink_specs, "laravel_http_client").sink_type is SinkType.HTTP_REQUEST
    assert "CWE-79" in _sanitizer_spec_by_name(sanitizer_specs, "laravel_e").mitigates


def test_symfony_specs_include_request_get_and_execute_query() -> None:
    source_specs = get_source_specs(frameworks=("symfony",))
    sink_specs = get_sink_specs(frameworks=("symfony",))
    sanitizer_specs = get_sanitizer_specs(frameworks=("symfony",))

    assert _source_spec_by_name(source_specs, "symfony_request_get").source_type is SourceType.REQUEST_PARAM
    assert _sink_spec_by_name(sink_specs, "symfony_execute_query").sink_type is SinkType.SQL_QUERY
    assert "CWE-89" in _sanitizer_spec_by_name(sanitizer_specs, "symfony_set_parameter").mitigates


def test_wordpress_specs_include_prepare_escaping_and_nonce_helpers() -> None:
    source_specs = get_source_specs(frameworks=("wordpress",))
    sink_specs = get_sink_specs(frameworks=("wordpress",))
    sanitizer_specs = get_sanitizer_specs(frameworks=("wordpress",))

    assert (
        _source_spec_by_name(source_specs, "wordpress_rest_get_param").source_type
        is SourceType.REQUEST_PARAM
    )
    assert _sink_spec_by_name(sink_specs, "wordpress_wpdb_query").sink_type is SinkType.SQL_QUERY
    assert "CWE-79" in _sanitizer_spec_by_name(sanitizer_specs, "wordpress_esc_html").mitigates
    assert "CWE-89" in _sanitizer_spec_by_name(sanitizer_specs, "wordpress_wpdb_prepare").mitigates
    assert "CWE-352" in _sanitizer_spec_by_name(
        sanitizer_specs, "wordpress_wp_nonce_field"
    ).mitigates


def test_php_sink_specs_cover_required_php_sinks() -> None:
    sink_specs = get_sink_specs(frameworks=("php",))

    assert _sink_spec_by_name(sink_specs, "php_mysqli_query").cwe_id == "CWE-89"
    assert _sink_spec_by_name(sink_specs, "php_echo").cwe_id == "CWE-79"
    assert _sink_spec_by_name(sink_specs, "php_exec").cwe_id == "CWE-78"
    assert _sink_spec_by_name(sink_specs, "php_include").cwe_id == "CWE-22"
    assert _sink_spec_by_name(sink_specs, "php_unserialize").cwe_id == "CWE-502"
    assert _sink_spec_by_name(sink_specs, "php_curl_exec").cwe_id == "CWE-918"


def test_php_joern_language_mappings_are_registered() -> None:
    assert LANGUAGE_TO_JOERN_FRONTEND["php"] == "php2cpg"
    assert LANGUAGE_TO_JOERN_IMPORT_MODULE["php"] == "php"
    assert LANGUAGE_TO_JOERN_PARSE_LANGUAGE["php"] == "php"
