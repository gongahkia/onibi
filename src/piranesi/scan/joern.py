from __future__ import annotations

import json
import logging
import os
import shlex
import shutil
import socket
import subprocess
import time
from pathlib import Path
from typing import Any
from urllib import error as urllib_error
from urllib import parse as urllib_parse
from urllib import request as urllib_request

from piranesi.config import JoernConfig
from piranesi.observability import log_error_context, run_subprocess

JsonDict = dict[str, Any]

JOERN_HOST = "127.0.0.1"
JOERN_PORT_MIN = 8080
JOERN_PORT_MAX = 8089
JOERN_SHUTDOWN_TIMEOUT_SECONDS = 5
JOERN_INSTALL_INSTRUCTIONS = (
    "Joern is required. Install via: brew install joern (macOS) "
    "or see https://github.com/joernio/joern"
)
JVM_INSTALL_INSTRUCTIONS = "JVM 11+ is required. Install via: brew install openjdk@11"
_HEALTHCHECK_QUERY = "val __piranesi_healthcheck = 1"


class JoernError(RuntimeError):
    """Raised when the Joern server lifecycle or API interaction fails."""


class JoernQueryTimeoutError(JoernError):
    """Raised when a Joern query exceeds the configured timeout."""


class _JoernTransportError(JoernError):
    """Internal transport-level Joern HTTP failure."""


def is_joern_installed(binary_path: str = "joern") -> bool:
    """Return True when the configured Joern binary can be resolved locally."""

    candidate = Path(binary_path).expanduser()
    if candidate.is_absolute() or candidate.parent != Path("."):
        return candidate.is_file() and os.access(candidate, os.X_OK)
    return shutil.which(binary_path) is not None


class JoernServer:
    """Manage a local Joern server subprocess and its REST API lifecycle."""

    def __init__(
        self,
        *,
        config: JoernConfig | None = None,
        binary_path: str | None = None,
        port: int | None = None,
        startup_timeout_seconds: int | None = None,
        query_timeout_seconds: int | None = None,
        jvm_memory: str | None = None,
        logger: logging.Logger | None = None,
    ) -> None:
        effective_config = config or JoernConfig()
        self.binary_path = binary_path or effective_config.binary_path
        self.port = port if port is not None else effective_config.server_port
        self.startup_timeout_seconds = (
            startup_timeout_seconds
            if startup_timeout_seconds is not None
            else effective_config.startup_timeout_seconds
        )
        self.query_timeout_seconds = (
            query_timeout_seconds
            if query_timeout_seconds is not None
            else effective_config.query_timeout_seconds
        )
        self.jvm_memory = jvm_memory or effective_config.jvm_memory
        self._requested_port = self.port
        self._logger = logger or logging.getLogger("piranesi.joern")
        self._process: subprocess.Popen[str] | None = None
        self._resolved_binary_path: str | None = None
        self._restart_count = 0
        self._imported_project_path: Path | None = None
        self._captured_stdout = ""
        self._captured_stderr = ""

    @property
    def process(self) -> subprocess.Popen[str] | None:
        return self._process

    @property
    def base_url(self) -> str:
        return f"http://{JOERN_HOST}:{self.port}"

    def __enter__(self) -> JoernServer:
        self._ensure_prerequisites()
        self._start_server(preferred_port=self._requested_port)
        return self

    def __exit__(self, exc_type: object, exc: object, traceback: object) -> None:
        self._stop_server()

    def import_project(self, path: str | Path) -> JsonDict:
        project_path = Path(path).expanduser().resolve()
        if not project_path.exists():
            raise FileNotFoundError(f"Joern import path does not exist: {project_path}")

        self._logger.info(
            "importing project into Joern",
            extra={
                "event": "joern_import_start",
                "path": str(project_path),
                "port": self.port,
            },
        )
        response = self._execute_cpgql(
            self._build_import_query(project_path),
            timeout_seconds=self.query_timeout_seconds,
            event="joern_import",
        )
        if response.get("success") is True:
            self._imported_project_path = project_path
        return response

    def query(self, cpgql: str) -> JsonDict:
        return self._execute_cpgql(
            cpgql,
            timeout_seconds=self.query_timeout_seconds,
            event="joern_query",
        )

    def _ensure_prerequisites(self) -> None:
        if self._resolved_binary_path is not None:
            return

        self._resolved_binary_path = self._resolve_binary_path()
        self._validate_java()
        self._validate_joern_binary(self._resolved_binary_path)

    def _resolve_binary_path(self) -> str:
        candidate = Path(self.binary_path).expanduser()
        if candidate.is_absolute() or candidate.parent != Path("."):
            resolved_candidate = candidate.resolve()
            if resolved_candidate.is_file() and os.access(resolved_candidate, os.X_OK):
                self._logger.debug(
                    "resolved Joern binary from configured path",
                    extra={
                        "event": "joern_binary_resolved",
                        "binary_path": str(resolved_candidate),
                    },
                )
                return str(resolved_candidate)
            raise JoernError(
                f"{JOERN_INSTALL_INSTRUCTIONS}. configured_binary_path={candidate}"
            )

        resolved = shutil.which(self.binary_path)
        if resolved is None:
            raise JoernError(JOERN_INSTALL_INSTRUCTIONS)

        self._logger.debug(
            "resolved Joern binary from PATH",
            extra={
                "event": "joern_binary_resolved",
                "binary_path": resolved,
            },
        )
        return resolved

    def _validate_java(self) -> None:
        try:
            result = run_subprocess(["java", "-version"], timeout=10, logger=self._logger)
        except FileNotFoundError as exc:
            raise JoernError(JVM_INSTALL_INSTRUCTIONS) from exc

        if result.returncode != 0:
            raise JoernError(JVM_INSTALL_INSTRUCTIONS)

    def _validate_joern_binary(self, binary_path: str) -> None:
        try:
            result = run_subprocess([binary_path, "--help"], timeout=15, logger=self._logger)
        except FileNotFoundError as exc:
            raise JoernError(JOERN_INSTALL_INSTRUCTIONS) from exc

        if result.returncode != 0:
            raise JoernError(
                f"Unable to execute Joern at {binary_path}. stderr={result.stderr.strip()}"
            )

    def _start_server(self, *, preferred_port: int) -> None:
        last_error: Exception | None = None
        attempted_ports: list[int] = []

        for candidate_port in self._candidate_ports(preferred_port):
            attempted_ports.append(candidate_port)
            if not self._port_is_available(candidate_port):
                self._logger.warning(
                    "Joern port unavailable, trying next candidate",
                    extra={
                        "event": "joern_port_conflict",
                        "port": candidate_port,
                    },
                )
                continue

            self.port = candidate_port
            self._start_process(candidate_port)
            try:
                self._wait_until_ready()
            except Exception as exc:
                last_error = exc
                self._logger.warning(
                    "Joern server start attempt failed",
                    extra={
                        "event": "joern_start_failed",
                        "port": candidate_port,
                        "error": str(exc),
                    },
                )
                self._stop_server()
                continue

            self._logger.info(
                "Joern server ready",
                extra={
                    "event": "joern_ready",
                    "port": candidate_port,
                    "pid": self._process.pid if self._process is not None else None,
                },
            )
            return

        if last_error is not None:
            raise JoernError(
                "Unable to start Joern server after trying ports "
                f"{attempted_ports}: {last_error}"
            ) from last_error

        raise JoernError(
            "Unable to start Joern server because no candidate port was available in "
            f"{attempted_ports}"
        )

    def _start_process(self, port: int) -> None:
        if self._resolved_binary_path is None:
            raise JoernError("Joern prerequisites were not initialized before startup")

        cmd = self._build_command(port)
        command_text = shlex.join(cmd)
        self._captured_stdout = ""
        self._captured_stderr = ""
        self._logger.info(
            "starting Joern server",
            extra={
                "event": "joern_start",
                "cmd": command_text,
                "port": port,
                "host": JOERN_HOST,
            },
        )
        try:
            self._process = subprocess.Popen(
                cmd,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )
        except FileNotFoundError as exc:
            raise JoernError(JOERN_INSTALL_INSTRUCTIONS) from exc

    def _build_command(self, port: int) -> list[str]:
        if self._resolved_binary_path is None:
            raise JoernError("Joern binary has not been resolved")

        command = [self._resolved_binary_path]
        if self.jvm_memory:
            command.append(f"-J-Xmx{self.jvm_memory}")
        command.extend(
            [
                "--nocolors",
                "--server",
                "--server-host",
                JOERN_HOST,
                "--server-port",
                str(port),
            ]
        )
        return command

    def _wait_until_ready(self) -> None:
        deadline = time.monotonic() + self.startup_timeout_seconds
        sleep_seconds = 0.25
        last_error: Exception | None = None
        attempt = 0

        while time.monotonic() < deadline:
            attempt += 1
            if self._process is None:
                raise JoernError("Joern process handle disappeared during startup")

            if self._process.poll() is not None:
                stdout, stderr = self._collect_process_output(self._process)
                raise JoernError(
                    "Joern server exited during startup "
                    f"with exit_code={self._process.returncode}; "
                    f"stdout={_truncate(stdout, 500)!r}; stderr={_truncate(stderr, 500)!r}"
                )

            try:
                response = self._request_json(
                    "/query-sync",
                    {"query": _HEALTHCHECK_QUERY},
                    timeout_seconds=min(2.0, self.startup_timeout_seconds),
                    event="joern_healthcheck",
                    log_level=logging.DEBUG,
                    log_failures=False,
                )
            except (JoernQueryTimeoutError, _JoernTransportError) as exc:
                last_error = exc
                self._logger.debug(
                    "Joern health probe not ready yet",
                    extra={
                        "event": "joern_healthcheck_retry",
                        "attempt": attempt,
                        "port": self.port,
                        "error": str(exc),
                    },
                )
            else:
                if response.get("success") is True:
                    return
                last_error = JoernError(
                    f"Joern health probe returned success={response.get('success')!r}"
                )

            remaining_seconds = deadline - time.monotonic()
            if remaining_seconds <= 0:
                break
            time.sleep(min(sleep_seconds, remaining_seconds))
            sleep_seconds = min(sleep_seconds * 2, 2.0)

        if last_error is not None:
            raise JoernError(
                "Timed out waiting for Joern server readiness "
                f"after {self.startup_timeout_seconds}s: {last_error}"
            ) from last_error

        raise JoernError(
            f"Timed out waiting for Joern server readiness after {self.startup_timeout_seconds}s"
        )

    def _execute_cpgql(
        self,
        cpgql: str,
        *,
        timeout_seconds: int,
        event: str,
        allow_restart: bool = True,
    ) -> JsonDict:
        self._ensure_server_running(allow_restart=allow_restart)

        try:
            return self._request_json(
                "/query-sync",
                {"query": cpgql},
                timeout_seconds=timeout_seconds,
                event=event,
            )
        except JoernQueryTimeoutError:
            raise
        except _JoernTransportError as exc:
            if allow_restart and self._process is not None and self._process.poll() is not None:
                self._logger.warning(
                    "Joern transport failed after process crash, restarting once",
                    extra={
                        "event": "joern_restart_after_transport_failure",
                        "port": self.port,
                        "error": str(exc),
                    },
                )
                self._ensure_server_running(allow_restart=True)
                return self._execute_cpgql(
                    cpgql,
                    timeout_seconds=timeout_seconds,
                    event=event,
                    allow_restart=False,
                )
            raise

    def _ensure_server_running(self, *, allow_restart: bool) -> None:
        if self._process is None:
            raise JoernError("Joern server is not running")

        if self._process.poll() is None:
            return

        exit_code = self._process.returncode
        stdout, stderr = self._collect_process_output(self._process)
        if not allow_restart or self._restart_count >= 1:
            raise JoernError(
                "Joern server crashed and restart limit was exceeded. "
                f"exit_code={exit_code}; stdout={_truncate(stdout, 500)!r}; "
                f"stderr={_truncate(stderr, 500)!r}"
            )

        self._restart_count += 1
        self._logger.warning(
            "Joern server crashed, restarting once",
            extra={
                "event": "joern_restart",
                "port": self.port,
                "exit_code": exit_code,
                "stdout": _truncate(stdout, 500),
                "stderr": _truncate(stderr, 500),
            },
        )
        self._restart_server()

    def _restart_server(self) -> None:
        preferred_port = self.port
        self._stop_server()
        self._start_server(preferred_port=preferred_port)
        if self._imported_project_path is not None:
            self._logger.info(
                "re-importing project after Joern restart",
                extra={
                    "event": "joern_reimport_after_restart",
                    "path": str(self._imported_project_path),
                    "port": self.port,
                },
            )
            response = self._execute_cpgql(
                self._build_import_query(self._imported_project_path),
                timeout_seconds=self.query_timeout_seconds,
                event="joern_import_after_restart",
                allow_restart=False,
            )
            if response.get("success") is not True:
                raise JoernError(
                    "Joern server restarted but the active project could not be re-imported. "
                    f"response={response}"
                )

    def _request_json(
        self,
        endpoint: str,
        payload: JsonDict,
        *,
        timeout_seconds: float,
        event: str,
        log_level: int = logging.INFO,
        log_failures: bool = True,
    ) -> JsonDict:
        url = f"{self.base_url}{endpoint}"
        parsed_url = urllib_parse.urlparse(url)
        if parsed_url.scheme != "http" or parsed_url.hostname != JOERN_HOST:
            raise JoernError(f"Refusing to send Joern HTTP request to unexpected URL: {url}")
        body = json.dumps(payload).encode("utf-8")
        self._logger.log(
            log_level,
            "sending Joern HTTP request",
            extra={
                "event": event,
                "url": url,
                "payload": payload,
                "timeout_seconds": timeout_seconds,
            },
        )

        request = urllib_request.Request(  # noqa: S310 - URL is validated as local http above.
            url,
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )

        try:
            with urllib_request.urlopen(  # noqa: S310 - URL is validated as local http above.
                request,
                timeout=timeout_seconds,
            ) as response:
                raw_response = response.read().decode("utf-8")
                status_code = response.status
        except TimeoutError as exc:
            self._log_timeout(event, url, timeout_seconds, payload)
            raise JoernQueryTimeoutError(
                f"Joern query timed out after {timeout_seconds}s: {payload.get('query', '')}"
            ) from exc
        except urllib_error.HTTPError as exc:
            raw_response = exc.read().decode("utf-8", errors="replace")
            if log_failures:
                log_error_context(
                    self._logger,
                    event=f"{event}_http_error",
                    what="joern_http_request",
                    on_what=url,
                    why=f"http_status={exc.code}",
                    next_step="propagating request failure",
                    debug=f"payload={payload}; response={raw_response}",
                    status_code=exc.code,
                )
            raise _JoernTransportError(
                f"Joern HTTP request failed with status {exc.code}: {raw_response}"
            ) from exc
        except urllib_error.URLError as exc:
            if isinstance(exc.reason, TimeoutError):
                self._log_timeout(event, url, timeout_seconds, payload)
                raise JoernQueryTimeoutError(
                    f"Joern query timed out after {timeout_seconds}s: {payload.get('query', '')}"
                ) from exc
            if log_failures:
                log_error_context(
                    self._logger,
                    event=f"{event}_transport_error",
                    what="joern_http_request",
                    on_what=url,
                    why=str(exc.reason),
                    next_step="propagating transport failure",
                    debug=f"payload={payload}",
                )
            raise _JoernTransportError(
                f"Joern HTTP request failed for {url}: {exc.reason}"
            ) from exc

        try:
            parsed = json.loads(raw_response)
        except json.JSONDecodeError as exc:
            if log_failures:
                log_error_context(
                    self._logger,
                    event=f"{event}_invalid_json",
                    what="joern_http_response_parse",
                    on_what=url,
                    why="invalid JSON response",
                    next_step="raising parse error",
                    debug=f"response={raw_response}",
                )
            raise JoernError(f"Joern returned invalid JSON: {raw_response}") from exc

        if not isinstance(parsed, dict):
            raise JoernError(f"Joern returned unexpected JSON payload: {parsed!r}")

        self._logger.log(
            log_level,
            "received Joern HTTP response",
            extra={
                "event": f"{event}_response",
                "url": url,
                "status_code": status_code,
                "success": parsed.get("success"),
                "uuid": parsed.get("uuid"),
                "stdout": _truncate(str(parsed.get("stdout", "")), 500),
                "stderr": _truncate(str(parsed.get("stderr", "")), 500),
            },
        )
        return parsed

    def _stop_server(self) -> None:
        process = self._process
        if process is None:
            return

        self._logger.info(
            "stopping Joern server",
            extra={
                "event": "joern_stop",
                "port": self.port,
                "pid": process.pid,
            },
        )

        if process.poll() is None:
            process.terminate()
            try:
                process.wait(timeout=JOERN_SHUTDOWN_TIMEOUT_SECONDS)
            except subprocess.TimeoutExpired:
                self._logger.warning(
                    "Joern server did not stop after SIGTERM, sending SIGKILL",
                    extra={
                        "event": "joern_kill",
                        "port": self.port,
                        "pid": process.pid,
                    },
                )
                process.kill()
                process.wait(timeout=JOERN_SHUTDOWN_TIMEOUT_SECONDS)

        stdout, stderr = self._collect_process_output(process)
        self._logger.info(
            "Joern server stopped",
            extra={
                "event": "joern_stopped",
                "port": self.port,
                "pid": process.pid,
                "exit_code": process.returncode,
                "stdout": _truncate(stdout, 500),
                "stderr": _truncate(stderr, 500),
            },
        )
        self._process = None

    def _collect_process_output(self, process: subprocess.Popen[str]) -> tuple[str, str]:
        try:
            stdout, stderr = process.communicate(timeout=0.1)
        except subprocess.TimeoutExpired:
            return self._captured_stdout, self._captured_stderr

        if stdout:
            self._captured_stdout += stdout
        if stderr:
            self._captured_stderr += stderr
        return self._captured_stdout, self._captured_stderr

    def _build_import_query(self, project_path: Path) -> str:
        return f"importCode({json.dumps(str(project_path))})"

    def _candidate_ports(self, preferred_port: int) -> list[int]:
        if JOERN_PORT_MIN <= preferred_port <= JOERN_PORT_MAX:
            ordered = list(range(preferred_port, JOERN_PORT_MAX + 1))
            ordered.extend(range(JOERN_PORT_MIN, preferred_port))
            return ordered

        ordered = [preferred_port]
        ordered.extend(range(JOERN_PORT_MIN, JOERN_PORT_MAX + 1))
        return _dedupe(ordered)

    def _port_is_available(self, port: int) -> bool:
        with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
            try:
                sock.bind((JOERN_HOST, port))
            except OSError:
                return False
        return True

    def _log_timeout(
        self,
        event: str,
        url: str,
        timeout_seconds: float,
        payload: JsonDict,
    ) -> None:
        log_error_context(
            self._logger,
            event=f"{event}_timeout",
            what="joern_http_request",
            on_what=url,
            why=f"timeout after {timeout_seconds}s",
            next_step="raising timeout",
            debug=f"payload={payload}",
            timeout_seconds=timeout_seconds,
        )


def _dedupe(values: list[int]) -> list[int]:
    deduped: list[int] = []
    seen: set[int] = set()
    for value in values:
        if value in seen:
            continue
        deduped.append(value)
        seen.add(value)
    return deduped


def _truncate(value: str, limit: int) -> str:
    if len(value) <= limit:
        return value
    return f"{value[:limit]}...<truncated>"
