from __future__ import annotations

from pathlib import Path

from piranesi.detect.ruby_patterns import extract_ruby_rails_findings
from piranesi.plugin import RailsFramework, RubyFramework, SinatraFramework
from piranesi.scan.framework import detect_frameworks, resolve_frameworks
from piranesi.scan.specs import (
    RUBY_SANITIZER_SPECS,
    RUBY_SINK_SPECS,
    RUBY_SOURCE_SPECS,
    get_sanitizer_specs,
    get_sink_specs,
    get_source_specs,
)

RUBY_FIXTURES = Path(__file__).resolve().parents[1] / "fixtures" / "ruby"
RAILS_APP_DIR = RUBY_FIXTURES / "rails_app"
SINATRA_APP_DIR = RUBY_FIXTURES / "sinatra_app"
RAW_APP_DIR = RUBY_FIXTURES / "raw_app"


def test_rails_plugin_specs_and_detection() -> None:
    plugin = RailsFramework()

    assert plugin.name() == "rails"
    assert plugin.detect(RAILS_APP_DIR) is True
    assert plugin.source_specs() == list(RUBY_SOURCE_SPECS)
    assert plugin.sink_specs() == list(RUBY_SINK_SPECS)
    assert plugin.sanitizer_specs() == list(RUBY_SANITIZER_SPECS)


def test_sinatra_plugin_specs_and_detection() -> None:
    plugin = SinatraFramework()

    assert plugin.name() == "sinatra"
    assert plugin.detect(SINATRA_APP_DIR) is True
    assert plugin.source_specs() == list(RUBY_SOURCE_SPECS)
    assert plugin.sink_specs() == list(RUBY_SINK_SPECS)
    assert plugin.sanitizer_specs() == list(RUBY_SANITIZER_SPECS)


def test_ruby_plugin_detects_raw_ruby_fixture() -> None:
    plugin = RubyFramework()

    assert plugin.name() == "ruby"
    assert plugin.detect(RAW_APP_DIR) is True


def test_detect_frameworks_for_ruby_fixtures() -> None:
    assert detect_frameworks(RAILS_APP_DIR) == ("rails",)
    assert detect_frameworks(SINATRA_APP_DIR) == ("sinatra",)
    assert detect_frameworks(RAW_APP_DIR) == ("ruby",)


def test_resolve_frameworks_auto_ruby_fixture() -> None:
    assert resolve_frameworks(RAILS_APP_DIR, ("auto",)) == ("rails",)
    assert resolve_frameworks(SINATRA_APP_DIR, ("auto",)) == ("sinatra",)
    assert resolve_frameworks(RAW_APP_DIR, ("auto",)) == ("ruby",)


def test_get_specs_with_rails_framework() -> None:
    source_names = {spec.name for spec in get_source_specs(frameworks=("rails",))}
    sink_names = {spec.name for spec in get_sink_specs(frameworks=("rails",))}
    sanitizer_names = {spec.name for spec in get_sanitizer_specs(frameworks=("rails",))}

    assert {"ruby_params", "ruby_request_body", "ruby_cookies", "ruby_env"} <= source_names
    assert {
        "ruby_active_record_string_query",
        "ruby_find_by_sql",
        "ruby_raw_helper",
        "ruby_html_safe",
        "ruby_system",
        "ruby_file_read",
        "ruby_yaml_load",
        "ruby_render_dynamic",
    } <= sink_names
    assert {
        "ruby_sanitize",
        "ruby_html_escape",
        "ruby_sanitize_sql",
        "ruby_permit",
        "ruby_shellwords_escape",
        "ruby_yaml_safe_load",
    } <= sanitizer_names


def test_extract_ruby_rails_findings_flags_unsafe_patterns_and_ignores_safe_ones() -> None:
    findings = extract_ruby_rails_findings(RAILS_APP_DIR)
    sink_keys = {
        (finding.vuln_class, finding.sink.api_name, finding.sink.location.line)
        for finding in findings
    }

    assert ("CWE-352", "skip_before_action", 2) in sink_keys
    assert ("CWE-89", "where", 5) in sink_keys
    assert ("CWE-89", "find_by_sql", 9) in sink_keys
    assert ("CWE-915", "create", 13) in sink_keys
    assert ("CWE-1336", "render", 21) in sink_keys
    assert ("CWE-78", "system", 29) in sink_keys
    assert ("CWE-22", "File.read", 33) in sink_keys
    assert ("CWE-502", "YAML.load", 37) in sink_keys
    assert ("CWE-79", "raw", 1) in sink_keys
    assert ("CWE-79", "html_safe", 4) in sink_keys

    assert ("CWE-89", "where", 45) not in sink_keys
    assert ("CWE-89", "where", 49) not in sink_keys
    assert ("CWE-915", "create", 17) not in sink_keys
    assert ("CWE-502", "YAML.load", 41) not in sink_keys
    assert ("CWE-79", "raw", 2) not in sink_keys


def test_extract_ruby_rails_findings_reports_missing_protect_from_forgery(tmp_path: Path) -> None:
    controller = tmp_path / "app" / "controllers" / "application_controller.rb"
    controller.parent.mkdir(parents=True)
    controller.write_text(
        "class ApplicationController < ActionController::Base\nend\n",
        encoding="utf-8",
    )

    findings = extract_ruby_rails_findings(tmp_path)

    assert any(
        finding.vuln_class == "CWE-352"
        and finding.sink.api_name == "protect_from_forgery"
        and finding.sink.location.file == str(controller.resolve())
        for finding in findings
    )
