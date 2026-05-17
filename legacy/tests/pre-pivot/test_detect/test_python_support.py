from __future__ import annotations

from pathlib import Path

from piranesi.plugin import (
    DjangoFramework,
    FastAPIFramework,
    FlaskFramework,
)
from piranesi.scan.framework import detect_frameworks, resolve_frameworks
from piranesi.scan.specs import (
    DJANGO_SOURCE_SPECS,
    FASTAPI_SOURCE_SPECS,
    FLASK_SOURCE_SPECS,
    PYTHON_SANITIZER_SPECS,
    PYTHON_SINK_SPECS,
    get_sanitizer_specs,
    get_sink_specs,
    get_source_specs,
)

FLASK_APP_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "python" / "flask_app"
DJANGO_APP_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "python" / "django_app"


# --- Flask plugin ---


def test_flask_plugin_name() -> None:
    assert FlaskFramework().name() == "flask"


def test_flask_plugin_specs() -> None:
    p = FlaskFramework()
    assert p.source_specs() == list(FLASK_SOURCE_SPECS)
    assert p.sink_specs() == list(PYTHON_SINK_SPECS)
    assert p.sanitizer_specs() == list(PYTHON_SANITIZER_SPECS)
    assert p.tsconfig_overrides() == {}


def test_flask_detect_requirements_txt(tmp_path: Path) -> None:
    (tmp_path / "requirements.txt").write_text("flask==3.0.0\n")
    assert FlaskFramework().detect(tmp_path) is True


def test_flask_detect_pyproject_toml(tmp_path: Path) -> None:
    (tmp_path / "pyproject.toml").write_text('[project]\ndependencies = ["flask>=3.0"]\n')
    assert FlaskFramework().detect(tmp_path) is True


def test_flask_detect_setup_py(tmp_path: Path) -> None:
    (tmp_path / "setup.py").write_text("install_requires=['flask']\n")
    assert FlaskFramework().detect(tmp_path) is True


def test_flask_detect_missing(tmp_path: Path) -> None:
    (tmp_path / "requirements.txt").write_text("django==5.0\n")
    assert FlaskFramework().detect(tmp_path) is False


def test_flask_detect_no_files(tmp_path: Path) -> None:
    assert FlaskFramework().detect(tmp_path) is False


# --- Django plugin ---


def test_django_plugin_name() -> None:
    assert DjangoFramework().name() == "django"


def test_django_plugin_specs() -> None:
    p = DjangoFramework()
    assert p.source_specs() == list(DJANGO_SOURCE_SPECS)
    assert p.sink_specs() == list(PYTHON_SINK_SPECS)
    assert p.sanitizer_specs() == list(PYTHON_SANITIZER_SPECS)


def test_django_detect_requirements_txt(tmp_path: Path) -> None:
    (tmp_path / "requirements.txt").write_text("django==5.0\npsycopg2>=2.9\n")
    assert DjangoFramework().detect(tmp_path) is True


def test_django_detect_missing(tmp_path: Path) -> None:
    (tmp_path / "requirements.txt").write_text("flask==3.0\n")
    assert DjangoFramework().detect(tmp_path) is False


# --- FastAPI plugin ---


def test_fastapi_plugin_name() -> None:
    assert FastAPIFramework().name() == "fastapi"


def test_fastapi_plugin_specs() -> None:
    p = FastAPIFramework()
    assert p.source_specs() == list(FASTAPI_SOURCE_SPECS)
    assert p.sink_specs() == list(PYTHON_SINK_SPECS)
    assert p.sanitizer_specs() == list(PYTHON_SANITIZER_SPECS)


def test_fastapi_detect_requirements_txt(tmp_path: Path) -> None:
    (tmp_path / "requirements.txt").write_text("fastapi==0.110.0\nuvicorn>=0.25\n")
    assert FastAPIFramework().detect(tmp_path) is True


def test_fastapi_detect_pyproject_toml(tmp_path: Path) -> None:
    (tmp_path / "pyproject.toml").write_text('[project]\ndependencies = ["fastapi"]\n')
    assert FastAPIFramework().detect(tmp_path) is True


def test_fastapi_detect_missing(tmp_path: Path) -> None:
    (tmp_path / "requirements.txt").write_text("flask==3.0\n")
    assert FastAPIFramework().detect(tmp_path) is False


# --- framework detection integration ---


def test_detect_frameworks_flask_fixture() -> None:
    assert "flask" in detect_frameworks(FLASK_APP_DIR)


def test_detect_frameworks_django_fixture() -> None:
    assert "django" in detect_frameworks(DJANGO_APP_DIR)


def test_resolve_frameworks_auto_flask_fixture() -> None:
    frameworks = resolve_frameworks(FLASK_APP_DIR, ("auto",))
    assert "flask" in frameworks


def test_resolve_frameworks_explicit_flask() -> None:
    frameworks = resolve_frameworks(Path("/nonexistent"), ("flask",))
    assert "flask" in frameworks


# --- get_*_specs with Python frameworks ---


def test_get_source_specs_with_flask() -> None:
    specs = get_source_specs(frameworks=("flask",))
    names = {s.name for s in specs}
    assert "flask_request_form" in names
    assert "flask_request_args" in names
    assert "flask_request_json" in names
    assert "flask_request_headers" in names


def test_get_source_specs_with_django() -> None:
    specs = get_source_specs(frameworks=("django",))
    names = {s.name for s in specs}
    assert "django_request_post" in names
    assert "django_request_get" in names
    assert "django_request_body" in names


def test_get_source_specs_with_fastapi() -> None:
    specs = get_source_specs(frameworks=("fastapi",))
    names = {s.name for s in specs}
    assert "fastapi_body" in names
    assert "fastapi_query" in names
    assert "fastapi_path" in names


def test_get_sink_specs_with_flask() -> None:
    specs = get_sink_specs(frameworks=("flask",))
    names = {s.name for s in specs}
    assert "python_sql_execute" in names
    assert "python_os_system" in names
    assert "python_subprocess_run" in names
    assert "python_eval" in names
    assert "python_open" in names
    assert "python_render_template_string" in names
    assert "python_requests_get" in names


def test_get_sanitizer_specs_with_flask() -> None:
    specs = get_sanitizer_specs(frameworks=("flask",))
    names = {s.name for s in specs}
    assert "python_parameterized_query" in names
    assert "python_shlex_quote" in names
    assert "python_markupsafe_escape" in names
    assert "python_bleach_clean" in names
    assert "python_path_realpath_startswith" in names


# --- source spec properties ---


def test_flask_source_types() -> None:
    by_name = {s.name: s for s in FLASK_SOURCE_SPECS}
    assert by_name["flask_request_form"].source_type == "request_body"
    assert by_name["flask_request_args"].source_type == "url_param"
    assert by_name["flask_request_json"].source_type == "request_body"
    assert by_name["flask_request_headers"].source_type == "header"
    assert by_name["flask_request_cookies"].source_type == "cookie"


def test_django_source_types() -> None:
    by_name = {s.name: s for s in DJANGO_SOURCE_SPECS}
    assert by_name["django_request_post"].source_type == "request_body"
    assert by_name["django_request_get"].source_type == "url_param"
    assert by_name["django_request_body"].source_type == "request_body"
    assert by_name["django_request_cookies"].source_type == "cookie"


def test_fastapi_source_types() -> None:
    by_name = {s.name: s for s in FASTAPI_SOURCE_SPECS}
    assert by_name["fastapi_body"].source_type == "request_body"
    assert by_name["fastapi_query"].source_type == "url_param"
    assert by_name["fastapi_path"].source_type == "request_param"
    assert by_name["fastapi_header"].source_type == "header"
    assert by_name["fastapi_cookie"].source_type == "cookie"


# --- sink spec properties ---


def test_python_sink_cwe_ids() -> None:
    by_name = {s.name: s for s in PYTHON_SINK_SPECS}
    assert by_name["python_sql_execute"].cwe_id == "CWE-89"
    assert by_name["python_os_system"].cwe_id == "CWE-78"
    assert by_name["python_subprocess_run"].cwe_id == "CWE-78"
    assert by_name["python_eval"].cwe_id == "CWE-94"
    assert by_name["python_open"].cwe_id == "CWE-22"
    assert by_name["python_render_template_string"].cwe_id == "CWE-79"
    assert by_name["python_requests_get"].cwe_id == "CWE-918"


# --- sanitizer spec properties ---


def test_python_sanitizer_mitigations() -> None:
    by_name = {s.name: s for s in PYTHON_SANITIZER_SPECS}
    assert "CWE-89" in by_name["python_parameterized_query"].mitigates
    assert "CWE-78" in by_name["python_shlex_quote"].mitigates
    assert "CWE-79" in by_name["python_markupsafe_escape"].mitigates
    assert "CWE-79" in by_name["python_bleach_clean"].mitigates
    assert "CWE-22" in by_name["python_path_realpath_startswith"].mitigates


# --- discovery includes Python frameworks ---


def test_discover_includes_python_frameworks() -> None:
    from piranesi.plugin import discover_framework_plugins

    plugins = discover_framework_plugins()
    names = [p.name() for p in plugins]
    assert "flask" in names
    assert "django" in names
    assert "fastapi" in names


def test_disabled_python_plugin() -> None:
    from piranesi.plugin import discover_framework_plugins

    plugins = discover_framework_plugins(disabled=frozenset({"flask", "django"}))
    names = [p.name() for p in plugins]
    assert "flask" not in names
    assert "django" not in names
    assert "fastapi" in names


# --- requirements.txt edge cases ---


def test_flask_detect_with_extras(tmp_path: Path) -> None:
    (tmp_path / "requirements.txt").write_text("flask[async]==3.0.0\n")
    assert FlaskFramework().detect(tmp_path) is True


def test_flask_detect_comment_line(tmp_path: Path) -> None:
    (tmp_path / "requirements.txt").write_text("# flask is great\nrequests==2.31\n")
    assert FlaskFramework().detect(tmp_path) is False


def test_flask_detect_version_specifiers(tmp_path: Path) -> None:
    (tmp_path / "requirements.txt").write_text("flask>=3.0,<4.0\n")
    assert FlaskFramework().detect(tmp_path) is True
