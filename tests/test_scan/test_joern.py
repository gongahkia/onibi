from __future__ import annotations

import json
import socket
import subprocess
from pathlib import Path

import pytest

from piranesi.scan.joern import (
    JOERN_PORT_MAX,
    JOERN_PORT_MIN,
    JoernError,
    JoernQueryTimeoutError,
    JoernServer,
    is_joern_installed,
)


class FakeProcess:
    _pid = 9000

    def __init__(
        self,
        *,
        alive: bool,
        returncode: int = 0,
        stdout: str = "",
        stderr: str = "",
        wait_raises_timeout: bool = False,
    ) -> None:
        type(self)._pid += 1
        self.pid = type(self)._pid
        self._alive = alive
        self.returncode = None if alive else returncode
        self._stdout = stdout
        self._stderr = stderr
        self._wait_raises_timeout = wait_raises_timeout
        self.terminated = False
        self.killed = False

    def poll(self) -> int | None:
        return None if self._alive else self.returncode

    def terminate(self) -> None:
        self.terminated = True
        if not self._wait_raises_timeout:
            self._alive = False
            self.returncode = 0

    def kill(self) -> None:
        self.killed = True
        self._alive = False
        self.returncode = -9

    def wait(self, timeout: float | None = None) -> int:
        if self._alive and self._wait_raises_timeout:
            raise subprocess.TimeoutExpired(cmd="fake-joern", timeout=timeout or 0.0)
        self._alive = False
        if self.returncode is None:
            self.returncode = 0
        return self.returncode

    def communicate(self, timeout: float | None = None) -> tuple[str, str]:
        if self._alive:
            raise subprocess.TimeoutExpired(cmd="fake-joern", timeout=timeout or 0.0)
        return self._stdout, self._stderr


def test_import_project_uses_import_code_query(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    project_dir = tmp_path / "project"
    project_dir.mkdir()
    server = JoernServer(binary_path="joern")
    captured: dict[str, object] = {}

    def fake_execute(
        cpgql: str,
        *,
        timeout_seconds: int,
        event: str,
        allow_restart: bool = True,
    ) -> dict[str, object]:
        captured["cpgql"] = cpgql
        captured["timeout_seconds"] = timeout_seconds
        captured["event"] = event
        captured["allow_restart"] = allow_restart
        return {"success": True}

    monkeypatch.setattr(server, "_execute_cpgql", fake_execute)

    response = server.import_project(project_dir)

    assert response["success"] is True
    assert captured["event"] == "joern_import"
    assert captured["timeout_seconds"] == server.query_timeout_seconds
    assert captured["cpgql"] == f"importCode({json.dumps(str(project_dir.resolve()))})"
    assert server._imported_project_path == project_dir.resolve()


def test_import_project_uses_language_alias_when_requested(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    project_dir = tmp_path / "project"
    project_dir.mkdir()
    server = JoernServer(binary_path="joern")
    captured: dict[str, object] = {}

    def fake_execute(
        cpgql: str,
        *,
        timeout_seconds: int,
        event: str,
        allow_restart: bool = True,
    ) -> dict[str, object]:
        captured["cpgql"] = cpgql
        captured["timeout_seconds"] = timeout_seconds
        captured["event"] = event
        captured["allow_restart"] = allow_restart
        return {"success": True}

    monkeypatch.setattr(server, "_execute_cpgql", fake_execute)

    response = server.import_project(project_dir, language="java")

    assert response["success"] is True
    assert captured["event"] == "joern_import"
    assert captured["cpgql"] == f"importCode.java({json.dumps(str(project_dir.resolve()))})"
    assert server._imported_project_path == project_dir.resolve()
    assert server._imported_language == "java"


def test_import_project_uses_language_alias_with_project_name(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    project_dir = tmp_path / "project"
    project_dir.mkdir()
    server = JoernServer(binary_path="joern")
    captured: dict[str, object] = {}

    def fake_execute(
        cpgql: str,
        *,
        timeout_seconds: int,
        event: str,
        allow_restart: bool = True,
    ) -> dict[str, object]:
        captured["cpgql"] = cpgql
        captured["timeout_seconds"] = timeout_seconds
        captured["event"] = event
        captured["allow_restart"] = allow_restart
        return {"success": True}

    monkeypatch.setattr(server, "_execute_cpgql", fake_execute)

    response = server.import_project(project_dir, language="python", project_name="py-sample")

    assert response["success"] is True
    assert captured["event"] == "joern_import"
    assert captured["timeout_seconds"] == server.query_timeout_seconds
    assert captured["cpgql"] == (
        f'importCode.python({json.dumps(str(project_dir.resolve()))}, "py-sample")'
    )
    assert server._imported_project_path == project_dir.resolve()
    assert server._imported_language == "python"
    assert server._imported_project_name == "py-sample"


def test_import_project_with_frontend_args_generates_cpg_before_import(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    project_dir = tmp_path / "project"
    project_dir.mkdir()
    server = JoernServer(binary_path="joern", query_timeout_seconds=45)
    server._resolved_binary_path = "/opt/homebrew/opt/joern/libexec/joern"
    captured: dict[str, object] = {}

    def fake_run_subprocess(
        cmd: list[str],
        *,
        timeout: int,
        logger: object,
    ) -> subprocess.CompletedProcess[str]:
        _ = (timeout, logger)
        captured["cmd"] = cmd
        output_index = cmd.index("--output") + 1
        output_path = Path(cmd[output_index])
        output_path.write_text("cpg", encoding="utf-8")
        return subprocess.CompletedProcess(cmd, 0, stdout="", stderr="")

    def fake_execute(
        cpgql: str,
        *,
        timeout_seconds: int,
        event: str,
        allow_restart: bool = True,
    ) -> dict[str, object]:
        captured["cpgql"] = cpgql
        captured["timeout_seconds"] = timeout_seconds
        captured["event"] = event
        captured["allow_restart"] = allow_restart
        return {"success": True}

    monkeypatch.setattr("piranesi.scan.joern.run_subprocess", fake_run_subprocess)
    monkeypatch.setattr(server, "_execute_cpgql", fake_execute)

    response = server.import_project(
        project_dir,
        language="java",
        frontend_args=["--exclude", "src/test"],
    )

    assert response["success"] is True
    assert captured["event"] == "joern_import"
    assert captured["timeout_seconds"] == server.query_timeout_seconds
    assert str(captured["cmd"][0]).endswith("joern-parse")
    assert captured["cmd"][1:5] == [
        "--output",
        str(server._imported_cpg_path),
        "--language",
        "javasrc",
    ]
    assert "--frontend-args" in captured["cmd"]
    assert captured["cmd"][5] == str(project_dir.resolve())
    assert captured["cmd"][-3:] == ["--frontend-args", "--exclude", "src/test"]
    assert captured["cpgql"] == f"importCpg({json.dumps(str(server._imported_cpg_path))})"
    assert server._imported_project_path is None
    assert server._imported_language is None
    assert server._imported_cpg_path is not None

    server._cleanup_temporary_artifacts()
    assert not server._temporary_artifacts


def test_version_uses_joern_scan_help_output(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    server = JoernServer(binary_path="joern")
    server._resolved_binary_path = "/opt/homebrew/bin/joern"

    def fake_run_subprocess(
        cmd: list[str],
        *,
        timeout: int,
        logger: object,
    ) -> subprocess.CompletedProcess[str]:
        _ = (timeout, logger)
        assert cmd == ["/opt/homebrew/bin/joern-scan", "--help"]
        return subprocess.CompletedProcess(
            cmd,
            0,
            stdout="Version: `HEAD+20260325-0833`\nUsage: joern-scan [options] [src]\n",
            stderr="",
        )

    monkeypatch.setattr("piranesi.scan.joern.run_subprocess", fake_run_subprocess)

    assert server.version() == "HEAD+20260325-0833"


def test_import_project_with_go_frontend_args_uses_golang_parse_mode(
    tmp_path: Path,
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    project_dir = tmp_path / "project"
    project_dir.mkdir()
    server = JoernServer(binary_path="joern", query_timeout_seconds=45)
    server._resolved_binary_path = "/opt/homebrew/opt/joern/libexec/joern"
    captured: dict[str, object] = {}

    def fake_run_subprocess(
        cmd: list[str],
        *,
        timeout: int,
        logger: object,
    ) -> subprocess.CompletedProcess[str]:
        _ = (timeout, logger)
        captured["cmd"] = cmd
        output_index = cmd.index("--output") + 1
        output_path = Path(cmd[output_index])
        output_path.write_text("cpg", encoding="utf-8")
        return subprocess.CompletedProcess(cmd, 0, stdout="", stderr="")

    def fake_execute(
        cpgql: str,
        *,
        timeout_seconds: int,
        event: str,
        allow_restart: bool = True,
    ) -> dict[str, object]:
        captured["cpgql"] = cpgql
        captured["timeout_seconds"] = timeout_seconds
        captured["event"] = event
        captured["allow_restart"] = allow_restart
        return {"success": True}

    monkeypatch.setattr("piranesi.scan.joern.run_subprocess", fake_run_subprocess)
    monkeypatch.setattr(server, "_execute_cpgql", fake_execute)

    response = server.import_project(
        project_dir,
        language="go",
        frontend_args=["--exclude", "vendor"],
    )

    assert response["success"] is True
    assert captured["cmd"][1:5] == [
        "--output",
        str(server._imported_cpg_path),
        "--language",
        "golang",
    ]
    assert captured["cmd"][-3:] == ["--frontend-args", "--exclude", "vendor"]
    assert captured["cpgql"] == f"importCpg({json.dumps(str(server._imported_cpg_path))})"


def test_query_timeout_raises(monkeypatch: pytest.MonkeyPatch) -> None:
    server = JoernServer(binary_path="joern", port=8086)
    server._process = FakeProcess(alive=True)  # type: ignore[assignment]

    def fake_request_json(*args: object, **kwargs: object) -> dict[str, object]:
        raise JoernQueryTimeoutError("timed out")

    monkeypatch.setattr(server, "_request_json", fake_request_json)

    with pytest.raises(JoernQueryTimeoutError, match="timed out"):
        server.query("cpg.method.l")


def test_query_restarts_once_after_server_crash(monkeypatch: pytest.MonkeyPatch) -> None:
    server = JoernServer(binary_path="joern", port=8086)
    server._process = FakeProcess(alive=False, returncode=1, stderr="boom")  # type: ignore[assignment]
    restart_calls: list[int] = []

    def fake_restart() -> None:
        restart_calls.append(server.port)
        server._process = FakeProcess(alive=True)  # type: ignore[assignment]

    def fake_request_json(*args: object, **kwargs: object) -> dict[str, object]:
        return {"success": True, "stdout": "ok"}

    monkeypatch.setattr(server, "_restart_server", fake_restart)
    monkeypatch.setattr(server, "_request_json", fake_request_json)

    response = server.query("cpg.method.l")

    assert response["success"] is True
    assert restart_calls == [8086]
    assert server._restart_count == 1


def test_query_fails_after_restart_budget_is_spent() -> None:
    server = JoernServer(binary_path="joern", port=8086)
    server._process = FakeProcess(alive=False, returncode=1, stderr="boom")  # type: ignore[assignment]
    server._restart_count = 1

    with pytest.raises(JoernError, match="restart limit"):
        server.query("cpg.method.l")


def test_stop_server_kills_hung_process() -> None:
    server = JoernServer(binary_path="joern", port=8086)
    process = FakeProcess(alive=True, wait_raises_timeout=True)
    server._process = process  # type: ignore[assignment]

    server._stop_server()

    assert process.terminated is True
    assert process.killed is True
    assert server.process is None


@pytest.fixture
def require_joern() -> None:
    if not is_joern_installed():
        pytest.skip("Joern is not installed in PATH")


def _port_is_free(port: int) -> bool:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        try:
            sock.bind(("127.0.0.1", port))
        except OSError:
            return False
    return True


def _find_free_joern_port() -> int:
    for port in range(JOERN_PORT_MIN, JOERN_PORT_MAX + 1):
        if _port_is_free(port):
            return port
    pytest.skip("No free Joern test port available in 8080-8089")


def _find_conflict_port_with_fallback() -> int:
    for port in range(JOERN_PORT_MIN, JOERN_PORT_MAX + 1):
        if not _port_is_free(port):
            continue
        ordered_fallbacks = list(range(port + 1, JOERN_PORT_MAX + 1))
        ordered_fallbacks.extend(range(JOERN_PORT_MIN, port))
        if any(_port_is_free(candidate) for candidate in ordered_fallbacks):
            return port
    pytest.skip("No free Joern conflict port with an available fallback in 8080-8089")


@pytest.mark.joern
@pytest.mark.integration
def test_joern_server_lifecycle(require_joern: None) -> None:
    port = _find_free_joern_port()

    with JoernServer(port=port, startup_timeout_seconds=30, query_timeout_seconds=10) as server:
        process = server.process
        response = server.query("val x = 1")

        assert process is not None
        assert response["success"] is True
        assert server.port == port
        assert process.poll() is None

    assert process is not None
    assert process.poll() is not None


@pytest.mark.joern
@pytest.mark.integration
def test_joern_server_uses_fallback_port_on_conflict(require_joern: None) -> None:
    conflicted_port = _find_conflict_port_with_fallback()

    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as blocker:
        blocker.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        blocker.bind(("127.0.0.1", conflicted_port))
        blocker.listen(1)

        with JoernServer(
            port=conflicted_port,
            startup_timeout_seconds=30,
            query_timeout_seconds=10,
        ) as server:
            response = server.query("val x = 1")

            assert response["success"] is True
            assert server.port != conflicted_port
            assert JOERN_PORT_MIN <= server.port <= JOERN_PORT_MAX
