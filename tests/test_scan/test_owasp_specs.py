"""Tests for CWE-502, CWE-601, CWE-434 source/sink/sanitizer specs."""

from __future__ import annotations

from piranesi.scan.specs import (
    BUILTIN_SANITIZER_SPECS,
    BUILTIN_SINK_SPECS,
    GO_SINK_SPECS,
    PYTHON_SANITIZER_SPECS,
    PYTHON_SINK_SPECS,
    SPRINGBOOT_SINK_SPECS,
    SinkType,
)

# --- CWE-502: Unsafe Deserialization ---


def test_builtin_sinks_include_json_parse() -> None:
    names = {s.name for s in BUILTIN_SINK_SPECS}
    assert "json_parse_user_input" in names


def test_json_parse_sink_has_correct_cwe() -> None:
    spec = next(s for s in BUILTIN_SINK_SPECS if s.name == "json_parse_user_input")
    assert spec.cwe_id == "CWE-502"
    assert spec.sink_type == SinkType.DESERIALIZATION


def test_python_sinks_include_pickle_loads() -> None:
    names = {s.name for s in PYTHON_SINK_SPECS}
    assert "python_pickle_loads" in names
    assert "python_pickle_load" in names


def test_python_pickle_sink_severity() -> None:
    spec = next(s for s in PYTHON_SINK_SPECS if s.name == "python_pickle_loads")
    assert spec.cwe_id == "CWE-502"
    assert spec.severity == "critical"


def test_python_sinks_include_yaml_load() -> None:
    spec = next(s for s in PYTHON_SINK_SPECS if s.name == "python_yaml_load_unsafe")
    assert spec.cwe_id == "CWE-502"
    assert spec.sink_type == SinkType.DESERIALIZATION


def test_python_sinks_include_marshal_loads() -> None:
    spec = next(s for s in PYTHON_SINK_SPECS if s.name == "python_marshal_loads")
    assert spec.cwe_id == "CWE-502"
    assert spec.severity == "critical"


def test_go_sinks_include_xml_unmarshal() -> None:
    names = {s.name for s in GO_SINK_SPECS}
    assert "go_xml_unmarshal" in names
    assert "go_json_unmarshal" in names
    assert "go_gob_decode" in names


def test_go_deserialization_sink_cwe() -> None:
    spec = next(s for s in GO_SINK_SPECS if s.name == "go_xml_unmarshal")
    assert spec.cwe_id == "CWE-502"
    assert spec.sink_type == SinkType.DESERIALIZATION


def test_java_sinks_include_object_input_stream() -> None:
    names = {s.name for s in SPRINGBOOT_SINK_SPECS}
    assert "java_object_input_stream" in names
    assert "java_xml_decoder" in names


def test_java_object_input_stream_severity() -> None:
    spec = next(s for s in SPRINGBOOT_SINK_SPECS if s.name == "java_object_input_stream")
    assert spec.cwe_id == "CWE-502"
    assert spec.severity == "critical"


# --- CWE-601: Open Redirect ---


def test_builtin_sinks_include_redirect() -> None:
    names = {s.name for s in BUILTIN_SINK_SPECS}
    assert "express_redirect" in names
    assert "location_header_set" in names


def test_express_redirect_sink_cwe() -> None:
    spec = next(s for s in BUILTIN_SINK_SPECS if s.name == "express_redirect")
    assert spec.cwe_id == "CWE-601"
    assert spec.sink_type == SinkType.REDIRECT


def test_python_sinks_include_redirect() -> None:
    spec = next(s for s in PYTHON_SINK_SPECS if s.name == "python_redirect")
    assert spec.cwe_id == "CWE-601"
    assert spec.sink_type == SinkType.REDIRECT


def test_go_sinks_include_redirect() -> None:
    spec = next(s for s in GO_SINK_SPECS if s.name == "go_http_redirect")
    assert spec.cwe_id == "CWE-601"
    assert spec.sink_type == SinkType.REDIRECT


def test_java_sinks_include_redirect() -> None:
    names = {s.name for s in SPRINGBOOT_SINK_SPECS}
    assert "java_send_redirect" in names
    assert "spring_redirect" in names


def test_java_send_redirect_cwe() -> None:
    spec = next(s for s in SPRINGBOOT_SINK_SPECS if s.name == "java_send_redirect")
    assert spec.cwe_id == "CWE-601"


# --- CWE-434: Unrestricted File Upload ---


def test_builtin_sinks_include_file_upload() -> None:
    names = {s.name for s in BUILTIN_SINK_SPECS}
    assert "multer_file_write" in names
    assert "multer_file_path_concat" in names


def test_multer_file_write_sink_cwe() -> None:
    spec = next(s for s in BUILTIN_SINK_SPECS if s.name == "multer_file_write")
    assert spec.cwe_id == "CWE-434"
    assert spec.sink_type == SinkType.FILE_UPLOAD
    assert spec.severity == "high"


# --- Sanitizers ---


def test_python_sanitizers_include_yaml_safe_load() -> None:
    spec = next(s for s in PYTHON_SANITIZER_SPECS if s.name == "python_yaml_safe_load")
    assert "CWE-502" in spec.mitigates
    assert spec.confidence >= 0.9


def test_python_sanitizers_include_url_check() -> None:
    spec = next(s for s in PYTHON_SANITIZER_SPECS if s.name == "python_url_startswith_check")
    assert "CWE-601" in spec.mitigates


def test_builtin_sanitizers_include_json_schema() -> None:
    spec = next(s for s in BUILTIN_SANITIZER_SPECS if s.name == "json_schema_validate")
    assert "CWE-502" in spec.mitigates


def test_builtin_sanitizers_include_url_origin_check() -> None:
    spec = next(s for s in BUILTIN_SANITIZER_SPECS if s.name == "url_origin_check")
    assert "CWE-601" in spec.mitigates


def test_builtin_sanitizers_include_file_extension_check() -> None:
    spec = next(s for s in BUILTIN_SANITIZER_SPECS if s.name == "file_extension_check")
    assert "CWE-434" in spec.mitigates


def test_builtin_sanitizers_include_multer_file_filter() -> None:
    spec = next(s for s in BUILTIN_SANITIZER_SPECS if s.name == "multer_file_filter")
    assert "CWE-434" in spec.mitigates
    assert spec.confidence >= 0.8


# --- SinkType enum ---


def test_sink_type_has_deserialization() -> None:
    assert SinkType.DESERIALIZATION == "deserialization"


def test_sink_type_has_redirect() -> None:
    assert SinkType.REDIRECT == "redirect"


def test_sink_type_has_file_upload() -> None:
    assert SinkType.FILE_UPLOAD == "file_upload"


def test_sink_type_has_prototype_pollution() -> None:
    assert SinkType.PROTOTYPE_POLLUTION == "prototype_pollution"


def test_builtin_sinks_include_prototype_pollution_specs() -> None:
    names = {s.name for s in BUILTIN_SINK_SPECS}
    assert "prototype_pollution_object_assign" in names
    assert "prototype_pollution_lodash_merge" in names
    assert "prototype_pollution_defaults_deep" in names


def test_prototype_pollution_specs_have_correct_cwe() -> None:
    spec = next(s for s in BUILTIN_SINK_SPECS if s.name == "prototype_pollution_object_assign")
    assert spec.cwe_id == "CWE-1321"
    assert spec.sink_type == SinkType.PROTOTYPE_POLLUTION
    assert spec.severity == "high"


# --- Completeness checks ---


def _all_sinks():
    return (
        list(BUILTIN_SINK_SPECS)
        + list(PYTHON_SINK_SPECS)
        + list(GO_SINK_SPECS)
        + list(SPRINGBOOT_SINK_SPECS)
    )


def test_at_least_3_deserialization_sinks_per_language() -> None:
    sinks = _all_sinks()
    deser_sinks = [s for s in sinks if s.cwe_id == "CWE-502"]
    assert len(deser_sinks) >= 3


def test_redirect_sinks_across_languages() -> None:
    sinks = _all_sinks()
    redirect_sinks = [s for s in sinks if s.cwe_id == "CWE-601"]
    assert len(redirect_sinks) >= 4  # JS + Python + Go + Java


def test_file_upload_sinks_exist() -> None:
    upload_sinks = [s for s in BUILTIN_SINK_SPECS if s.cwe_id == "CWE-434"]
    assert len(upload_sinks) >= 2
