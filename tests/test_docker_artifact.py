from __future__ import annotations

from pathlib import Path


def test_dockerfile_configures_non_root_and_deterministic_install() -> None:
    root = Path(__file__).resolve().parents[1]
    dockerfile = (root / "Dockerfile").read_text(encoding="utf-8")

    assert "USER piranesi" in dockerfile
    assert 'ENTRYPOINT ["piranesi"]' in dockerfile
    assert "uv pip install --system --no-cache --locked ." in dockerfile
    assert "OPENAI_API_KEY" not in dockerfile
    assert "ANTHROPIC_API_KEY" not in dockerfile


def test_dockerignore_excludes_large_dev_context() -> None:
    root = Path(__file__).resolve().parents[1]
    dockerignore = (root / ".dockerignore").read_text(encoding="utf-8")

    assert ".git" in dockerignore
    assert "tests" in dockerignore
    assert "eval" in dockerignore


def test_docker_smoke_script_exists() -> None:
    root = Path(__file__).resolve().parents[1]
    script = root / "scripts" / "docker_smoke_check.sh"
    payload = script.read_text(encoding="utf-8")

    assert script.exists()
    assert "docker build" in payload
    assert "docker run" in payload
