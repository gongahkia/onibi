from __future__ import annotations

import json
import socket
from pathlib import Path

from piranesi.verify.launch import (
    LaunchCandidate,
    infer_launch_plan,
    probe_launch_candidate,
    render_target_profile_snippet,
    write_target_profile,
)


def test_infer_launch_plan_detects_package_json_start_script(tmp_path: Path) -> None:
    (tmp_path / "package.json").write_text(
        json.dumps(
            {
                "scripts": {"start": "node app.js"},
                "dependencies": {"express": "^4.18.0"},
            }
        ),
        encoding="utf-8",
    )

    plan = infer_launch_plan(tmp_path)

    assert len(plan.candidates) == 1
    candidate = plan.candidates[0]
    assert candidate.command == "npm run start"
    assert candidate.base_url == "http://127.0.0.1:3000"
    assert candidate.env["PORT"] == "3000"


def test_infer_launch_plan_detects_python_http_server_port(tmp_path: Path) -> None:
    (tmp_path / "package.json").write_text(
        json.dumps(
            {
                "scripts": {"start": "python3 -m http.server 8123 --bind 127.0.0.1"},
                "dependencies": {},
            }
        ),
        encoding="utf-8",
    )

    plan = infer_launch_plan(tmp_path)

    assert plan.candidates[0].base_url == "http://127.0.0.1:8123"
    assert plan.candidates[0].env["PORT"] == "8123"


def test_render_target_profile_snippet_escapes_command() -> None:
    snippet = render_target_profile_snippet(
        infer_launch_plan(Path("examples/vuln-express")).candidates[0],
        profile_name="local",
    )

    assert 'target_profile = "local"' in snippet
    assert "[verify.target_profiles.local]" in snippet
    assert 'command = "npm run start"' in snippet


def test_write_target_profile_appends_profile_to_config(tmp_path: Path) -> None:
    config = tmp_path / "piranesi.toml"
    candidate = LaunchCandidate(
        name="npm:start",
        command="npm run start",
        base_url="http://127.0.0.1:3000",
        readiness_url="/",
        env={"PORT": "3000"},
        source="package.json",
        reason="test",
    )

    result = write_target_profile(config, candidate, profile_name="auto")

    assert result.written is True
    text = config.read_text(encoding="utf-8")
    assert 'target_profile = "auto"' in text
    assert "[verify.target_profiles.auto]" in text
    assert 'command = "npm run start"' in text
    assert "[verify.target_profiles.auto.env]" in text


def test_write_target_profile_requires_force_to_replace(tmp_path: Path) -> None:
    config = tmp_path / "piranesi.toml"
    candidate = LaunchCandidate(
        name="npm:start",
        command="npm run start",
        base_url="http://127.0.0.1:3000",
        readiness_url="/",
        env={"PORT": "3000"},
        source="package.json",
        reason="test",
    )

    write_target_profile(config, candidate, profile_name="auto")
    try:
        write_target_profile(config, candidate, profile_name="auto")
    except ValueError as exc:
        assert "--force" in str(exc)
    else:  # pragma: no cover - defensive assertion
        raise AssertionError("expected duplicate profile write to fail")

    replacement = candidate.model_copy(update={"command": "npm run dev"})
    result = write_target_profile(config, replacement, profile_name="auto", force=True)

    assert result.replaced is True
    text = config.read_text(encoding="utf-8")
    assert 'command = "npm run dev"' in text
    assert 'command = "npm run start"' not in text


def test_probe_launch_candidate_starts_local_server(tmp_path: Path) -> None:
    (tmp_path / "index.html").write_text("ok", encoding="utf-8")
    port = _free_port()
    candidate = LaunchCandidate(
        name="python:http",
        command=f"python3 -m http.server {port} --bind 127.0.0.1",
        base_url=f"http://127.0.0.1:{port}",
        readiness_url="/",
        source="test",
        reason="test",
    )

    result = probe_launch_candidate(tmp_path, candidate, output_dir=tmp_path / "out")

    assert result.ready is True
    assert Path(result.log_path).is_file()


def _free_port() -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind(("127.0.0.1", 0))
        return int(sock.getsockname()[1])
