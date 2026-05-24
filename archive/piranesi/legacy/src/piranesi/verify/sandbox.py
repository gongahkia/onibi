from __future__ import annotations

import hashlib
import json
import logging
import os
import re
import shutil
import subprocess
import tempfile
import time
from collections.abc import Callable, Iterator, Mapping, Sequence
from contextlib import contextmanager, suppress
from dataclasses import dataclass, field
from pathlib import Path
from typing import TYPE_CHECKING, Any, Literal
from urllib.parse import urlsplit, urlunsplit

try:
    import docker as _docker
except ImportError:  # pragma: no cover - exercised only in underspecified local envs.
    _docker = None  # type: ignore[assignment]

try:
    import requests as _requests
except ImportError:  # pragma: no cover - exercised only in underspecified local envs.
    _requests = None  # type: ignore[assignment]

if TYPE_CHECKING:
    from docker.models.containers import Container
else:  # pragma: no cover - runtime-only fallback for missing optional deps.
    Container = Any

LOGGER = logging.getLogger("piranesi.verify.sandbox")

DEFAULT_PORT = 3000
DEFAULT_READY_WAIT = 30.0
INTERNAL_PORT_LABEL = "piranesi.internal_port"
START_COMMAND_LABEL = "piranesi.start_command"
GENERATED_LABEL = "piranesi.generated"
DOCKER_LABEL = "piranesi"
DOCKER_LABEL_VALUE = "sandbox"
DOCKER_SOCKET_PATH = "/var/run/docker.sock"
DOCKERFILE_NAME = "Dockerfile.piranesi"
_SOURCE_SUFFIXES = {".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx"}
_SKIP_DIRECTORIES = {".git", ".hg", ".svn", "node_modules", ".next", "dist", "build"}
_READY_DELAY_INITIAL = 0.5
_READY_DELAY_MAX = 5.0
_PORT_PATTERNS = (
    re.compile(r"\.listen\(\s*process\.env\.PORT\s*(?:\|\||\?\?)\s*(\d{2,5})\b"),
    re.compile(r"\.listen\(\s*(\d{2,5})\b"),
    re.compile(r"\bPORT\s*=\s*(\d{2,5})\b"),
    re.compile(r"\bport\s*=\s*(\d{2,5})\b"),
)
_DIFF_KIND_MAP = {0: "modified", 1: "added", 2: "deleted"}
_READY_PROBE_SCRIPT = (
    "const port = Number(process.argv[1]);"
    "fetch(`http://127.0.0.1:${port}/`, { method: 'GET' })"
    ".then((response) => process.exit(response.status < 500 ? 0 : 1))"
    ".catch(() => process.exit(1));"
)
_IN_CONTAINER_REQUEST_SCRIPT = """
const payload = JSON.parse(process.argv[1]);
const port = Number(process.argv[2]);
const started = Date.now();
const headers = { ...(payload.headers || {}) };
const requestDetails = {
  method: (payload.method || "GET").toUpperCase(),
  url: payload.url,
  headers,
  body: payload.body ?? null,
  encoding: payload.encoding || "json",
  payload_values: payload.payload_values || {},
};
const url = new URL(payload.url, `http://127.0.0.1:${port}`);
if (payload.encoding === "query" && payload.body && typeof payload.body === "object") {
  for (const [key, value] of Object.entries(payload.body)) {
    url.searchParams.append(key, String(value));
  }
}
const request = {
  method: requestDetails.method,
  headers,
};
if (payload.encoding === "json" && payload.body !== null && payload.body !== undefined) {
  if (!headers["content-type"] && !headers["Content-Type"]) {
    headers["content-type"] = "application/json";
  }
  request.body = JSON.stringify(payload.body);
} else if (
  payload.encoding === "urlencoded"
  && payload.body
  && typeof payload.body === "object"
) {
  if (!headers["content-type"] && !headers["Content-Type"]) {
    headers["content-type"] = "application/x-www-form-urlencoded";
  }
  request.body = new URLSearchParams(
    Object.entries(payload.body).map(([key, value]) => [key, String(value)])
  ).toString();
} else if (payload.encoding === "path" && payload.body !== null && payload.body !== undefined) {
  request.body = typeof payload.body === "string" ? payload.body : JSON.stringify(payload.body);
}

fetch(url, request)
  .then(async (response) => {
    const responseHeaders = {};
    response.headers.forEach((value, key) => {
      responseHeaders[key] = value;
    });
    const body = await response.text();
    console.log(JSON.stringify({
      status_code: response.status,
      headers: responseHeaders,
      body,
      elapsed_ms: Date.now() - started,
      request: requestDetails,
      error: null,
    }));
  })
  .catch((error) => {
    console.log(JSON.stringify({
      status_code: 0,
      headers: {},
      body: "",
      elapsed_ms: 0,
      request: requestDetails,
      error: String(error),
    }));
  });
""".strip()

PayloadEncoding = Literal["json", "urlencoded", "query", "path"]
TeardownMode = Literal["always", "on_success", "never"]


@dataclass(slots=True)
class SynthesizedPayload:
    method: str
    url: str
    headers: dict[str, str] = field(default_factory=dict)
    body: object | None = None
    payload_values: dict[str, str] = field(default_factory=dict)
    encoding: PayloadEncoding = "json"


@dataclass(slots=True)
class ExploitResult:
    status_code: int
    headers: dict[str, str]
    body: str
    elapsed_ms: float
    request: dict[str, object] = field(default_factory=dict)
    error: str | None = None

    @classmethod
    def unreachable(
        cls,
        *,
        error: str,
        request: dict[str, object] | None = None,
    ) -> ExploitResult:
        return cls(
            status_code=0,
            headers={},
            body="",
            elapsed_ms=0.0,
            request=request or {},
            error=error,
        )


@dataclass(slots=True)
class SandboxCapture:
    http_response: ExploitResult
    container_logs: str
    filesystem_diff: list[str]
    timing_ms: float
    container_id: str | None = None
    stdout: str = ""
    stderr: str = ""
    exit_code: int | None = None
    side_effects: list[str] = field(default_factory=list)
    network_isolated: bool = True
    launch_profile: str | None = None
    launch_log_path: str | None = None
    startup_error: str | None = None
    error: str | None = None

    @classmethod
    def app_not_ready(
        cls,
        *,
        container_id: str | None = None,
        container_logs: str = "",
        startup_error: str = "APP_NOT_READY",
        launch_profile: str | None = None,
        launch_log_path: str | None = None,
    ) -> SandboxCapture:
        return cls(
            http_response=ExploitResult.unreachable(error=startup_error),
            container_logs=container_logs,
            filesystem_diff=[],
            timing_ms=0.0,
            container_id=container_id,
            side_effects=[],
            network_isolated=True,
            launch_profile=launch_profile,
            launch_log_path=launch_log_path,
            startup_error=startup_error,
            error=startup_error,
        )


@dataclass(frozen=True, slots=True)
class TargetLaunchProfile:
    name: str
    command: str | None = None
    cwd: str | None = None
    env: dict[str, str] = field(default_factory=dict)
    startup_timeout_seconds: int = int(DEFAULT_READY_WAIT)
    readiness_url: str | None = None
    readiness_command: str | None = None
    base_url: str | None = None
    teardown: TeardownMode = "always"
    logs_path: str | None = None


@dataclass(slots=True)
class _PreparedBuildContext:
    root: Path
    dockerfile_name: str
    image_tag: str
    port: int
    start_command: str


def build_image(target_path: str) -> str:
    client = _docker_client()
    try:
        return _build_image(client, target_path)
    finally:
        _close_client(client)


def start_container(image: str) -> Container:
    client = _docker_client()
    return _start_container(client, image)


def wait_for_ready(
    host_port: int,
    max_wait: float = DEFAULT_READY_WAIT,
    *,
    readiness_url: str | None = None,
    base_url: str | None = None,
) -> bool:
    requests = _requests_module()
    url = _resolve_readiness_url(host_port, readiness_url=readiness_url, base_url=base_url)
    delay = _READY_DELAY_INITIAL
    started_at = time.perf_counter()
    attempt = 0

    while (time.perf_counter() - started_at) < max_wait:
        attempt += 1
        try:
            response = requests.get(url, timeout=2)
        except requests.RequestException:
            LOGGER.debug(
                "sandbox readiness probe failed",
                extra={
                    "event": "docker_wait_retry",
                    "url": url,
                    "host_port": host_port,
                    "attempt": attempt,
                    "delay_seconds": delay,
                },
            )
        else:
            if response.status_code < 500:
                LOGGER.info(
                    "sandbox application is ready",
                    extra={
                        "event": "docker_wait_ready",
                        "url": url,
                        "host_port": host_port,
                        "attempt": attempt,
                        "status_code": response.status_code,
                    },
                )
                return True

        time.sleep(delay)
        delay = min(delay * 2, _READY_DELAY_MAX)

    LOGGER.warning(
        "sandbox application did not become ready in time",
        extra={
            "event": "docker_wait_timeout",
            "host_port": host_port,
            "max_wait_seconds": max_wait,
        },
    )
    return False


def fire_payload(
    payload: SynthesizedPayload,
    host_port: int,
    *,
    base_url: str | None = None,
) -> ExploitResult:
    requests = _requests_module()
    request_url = _build_request_url(payload.url, host_port, base_url=base_url)
    request_details: dict[str, object] = {
        "method": payload.method.upper(),
        "url": request_url,
        "headers": dict(payload.headers),
        "body": payload.body,
        "encoding": payload.encoding,
        "payload_values": dict(payload.payload_values),
    }
    request_kwargs: dict[str, object] = {
        "method": payload.method.upper(),
        "url": request_url,
        "headers": payload.headers,
        "timeout": 30,
        "allow_redirects": False,
    }
    if payload.encoding == "json" and payload.body is not None:
        request_kwargs["json"] = payload.body
    elif payload.encoding == "urlencoded" and payload.body is not None:
        request_kwargs["data"] = payload.body
    elif payload.encoding == "query" and payload.body is not None:
        request_kwargs["params"] = payload.body
    elif payload.encoding == "path" and payload.body is not None:
        request_kwargs["data"] = payload.body

    LOGGER.info(
        "firing payload at sandbox",
        extra={
            "event": "docker_request_start",
            "host_port": host_port,
            "method": payload.method.upper(),
            "url": request_url,
            "encoding": payload.encoding,
        },
    )
    try:
        response = requests.request(**request_kwargs)
    except requests.RequestException as exc:
        LOGGER.warning(
            "payload request failed",
            extra={
                "event": "docker_request_error",
                "host_port": host_port,
                "method": payload.method.upper(),
                "url": request_url,
                "error": str(exc),
            },
        )
        return ExploitResult.unreachable(error=str(exc), request=request_details)

    elapsed_ms = response.elapsed.total_seconds() * 1000
    LOGGER.info(
        "payload request completed",
        extra={
            "event": "docker_request_complete",
            "host_port": host_port,
            "method": payload.method.upper(),
            "url": request_url,
            "status_code": response.status_code,
            "elapsed_ms": elapsed_ms,
        },
    )
    return ExploitResult(
        status_code=response.status_code,
        headers=dict(response.headers),
        body=response.text,
        elapsed_ms=elapsed_ms,
        request=request_details,
    )


def capture_results(container: Container, exploit_result: ExploitResult) -> SandboxCapture:
    container.reload()
    stdout = _decode_docker_output(container.logs(stdout=True, stderr=False))
    stderr = _decode_docker_output(container.logs(stdout=False, stderr=True))
    combined_logs = "\n".join(part for part in (stdout, stderr) if part)
    diff_entries = _serialize_container_diff(container.diff() or [])
    exit_code = _container_exit_code(container)

    LOGGER.info(
        "captured sandbox results",
        extra={
            "event": "docker_capture_complete",
            "container_id": getattr(container, "id", None),
            "timing_ms": exploit_result.elapsed_ms,
            "diff_entries": len(diff_entries),
            "exit_code": exit_code,
        },
    )
    return SandboxCapture(
        http_response=exploit_result,
        container_logs=combined_logs,
        filesystem_diff=diff_entries,
        timing_ms=exploit_result.elapsed_ms,
        container_id=getattr(container, "id", None),
        stdout=stdout,
        stderr=stderr,
        exit_code=exit_code,
        side_effects=list(diff_entries),
        network_isolated=True,
        error=exploit_result.error,
    )


def run_in_sandbox(
    target_path: str,
    payloads: Sequence[SynthesizedPayload],
    *,
    target_profile: TargetLaunchProfile | None = None,
    logs_base_dir: Path | None = None,
) -> list[SandboxCapture]:
    if not payloads:
        return []
    if target_profile is not None:
        return _run_with_target_profile(
            target_path=target_path,
            payloads=payloads,
            target_profile=target_profile,
            logs_base_dir=logs_base_dir,
        )

    client = _docker_client()
    container: Container | None = None
    network_ids: list[str] = []
    try:
        image = _build_image(client, target_path)
        container = _start_container(client, image)
        network_ids = _container_network_ids(container)
        request_executor: Callable[[SynthesizedPayload], ExploitResult]
        try:
            host_port = _get_host_port(container)
        except RuntimeError:
            internal_port = _image_internal_port(client, image)
            if not _wait_for_ready_in_container(container, internal_port):
                return [
                    SandboxCapture.app_not_ready(container_id=getattr(container, "id", None))
                    for _ in payloads
                ]

            def request_executor(
                payload: SynthesizedPayload,
                _c: Any = container,
                _p: int = internal_port,
            ) -> ExploitResult:
                return _fire_payload_in_container(_c, payload, internal_port=_p)
        else:
            if not wait_for_ready(host_port):
                return [
                    SandboxCapture.app_not_ready(container_id=getattr(container, "id", None))
                    for _ in payloads
                ]

            def request_executor(
                payload: SynthesizedPayload,
                _hp: int = host_port,
            ) -> ExploitResult:
                return fire_payload(payload, _hp)

        captures: list[SandboxCapture] = []
        for payload in payloads:
            result = request_executor(payload)
            captures.append(capture_results(container, result))
        return captures
    finally:
        if container is not None:
            _teardown_container(container)
        _teardown_networks(client, network_ids)
        _close_client(client)


def _run_with_target_profile(
    *,
    target_path: str,
    payloads: Sequence[SynthesizedPayload],
    target_profile: TargetLaunchProfile,
    logs_base_dir: Path | None,
) -> list[SandboxCapture]:
    startup_timeout = float(max(1, target_profile.startup_timeout_seconds))
    cwd = _resolve_profile_cwd(target_path=target_path, target_profile=target_profile)
    env = _resolve_profile_env(target_profile)
    process: subprocess.Popen[str] | None = None
    temp_log_path: Path | None = None
    temp_log_handle: Any | None = None
    completed = False

    try:
        if target_profile.command and target_profile.command.strip():
            with tempfile.NamedTemporaryFile(prefix="piranesi-launch-", delete=False) as handle:
                temp_log_path = Path(handle.name)
            temp_log_handle = temp_log_path.open("w", encoding="utf-8")
            process = subprocess.Popen(  # noqa: S602
                target_profile.command,
                shell=True,
                cwd=str(cwd),
                env=env,
                stdout=temp_log_handle,
                stderr=subprocess.STDOUT,
                text=True,
            )

        base_url = _resolve_profile_base_url(target_profile, env)
        if base_url is None:
            startup_error = "TARGET_PROFILE_BASE_URL_MISSING"
            launch_logs = _read_launch_logs(temp_log_path)
            launch_log_path = _persist_launch_logs(
                logs=launch_logs,
                target_profile=target_profile,
                logs_base_dir=logs_base_dir,
            )
            return [
                SandboxCapture.app_not_ready(
                    startup_error=startup_error,
                    container_logs=launch_logs,
                    launch_profile=target_profile.name,
                    launch_log_path=launch_log_path,
                )
                for _ in payloads
            ]

        ready, readiness_error = _wait_for_profile_ready(
            target_profile=target_profile,
            base_url=base_url,
            cwd=cwd,
            env=env,
            process=process,
            max_wait=startup_timeout,
        )
        if not ready:
            launch_logs = _read_launch_logs(temp_log_path)
            launch_log_path = _persist_launch_logs(
                logs=launch_logs,
                target_profile=target_profile,
                logs_base_dir=logs_base_dir,
            )
            return [
                SandboxCapture.app_not_ready(
                    startup_error=(
                        readiness_error
                        if readiness_error is not None
                        else "TARGET_PROFILE_NOT_READY"
                    ),
                    container_logs=launch_logs,
                    launch_profile=target_profile.name,
                    launch_log_path=launch_log_path,
                )
                for _ in payloads
            ]

        captures = [
            SandboxCapture(
                http_response=fire_payload(payload, 0, base_url=base_url),
                container_logs="",
                filesystem_diff=[],
                timing_ms=0.0,
                container_id=None,
                stdout="",
                stderr="",
                exit_code=0,
                side_effects=[],
                network_isolated=False,
                launch_profile=target_profile.name,
                launch_log_path=None,
                startup_error=None,
                error=None,
            )
            for payload in payloads
        ]
        launch_logs = _read_launch_logs(temp_log_path)
        launch_log_path = _persist_launch_logs(
            logs=launch_logs,
            target_profile=target_profile,
            logs_base_dir=logs_base_dir,
        )
        for capture in captures:
            capture.container_logs = launch_logs
            capture.stdout = launch_logs
            capture.error = capture.http_response.error
            capture.timing_ms = capture.http_response.elapsed_ms
            capture.launch_log_path = launch_log_path
        completed = True
        return captures
    finally:
        if temp_log_handle is not None:
            with suppress(Exception):
                temp_log_handle.close()
        if process is not None:
            _teardown_profile_process(
                process,
                teardown=target_profile.teardown,
                successful=completed,
            )
        if temp_log_path is not None and temp_log_path.exists():
            with suppress(OSError):
                temp_log_path.unlink()


def _resolve_profile_cwd(*, target_path: str, target_profile: TargetLaunchProfile) -> Path:
    target_root = Path(target_path).expanduser().resolve(strict=False)
    if target_profile.cwd is None:
        profile_cwd = target_root
    else:
        configured = Path(target_profile.cwd).expanduser()
        profile_cwd = (
            configured.resolve(strict=False)
            if configured.is_absolute()
            else (target_root / configured).resolve(strict=False)
        )
    if not profile_cwd.is_dir():
        raise ValueError(
            f"target profile '{target_profile.name}' cwd does not exist: {profile_cwd}"
        )
    return profile_cwd


def _resolve_profile_env(target_profile: TargetLaunchProfile) -> dict[str, str]:
    resolved = dict(os.environ)
    resolved.update({key: str(value) for key, value in target_profile.env.items()})
    return resolved


def _resolve_profile_base_url(
    target_profile: TargetLaunchProfile,
    env: Mapping[str, str],
) -> str | None:
    if target_profile.base_url and target_profile.base_url.strip():
        template = target_profile.base_url.strip()
        port = env.get("PORT", str(DEFAULT_PORT))
        try:
            return template.format(port=port)
        except KeyError:
            return template
    if target_profile.command and target_profile.command.strip():
        return f"http://127.0.0.1:{env.get('PORT', str(DEFAULT_PORT))}"
    return None


def _wait_for_profile_ready(
    *,
    target_profile: TargetLaunchProfile,
    base_url: str,
    cwd: Path,
    env: Mapping[str, str],
    process: subprocess.Popen[str] | None,
    max_wait: float,
) -> tuple[bool, str | None]:
    delay = _READY_DELAY_INITIAL
    started_at = time.perf_counter()
    last_error: str | None = None
    while (time.perf_counter() - started_at) < max_wait:
        if process is not None and process.poll() is not None:
            return False, f"TARGET_PROFILE_PROCESS_EXITED({process.returncode})"
        if target_profile.readiness_command and target_profile.readiness_command.strip():
            try:
                probe = subprocess.run(  # noqa: S602
                    target_profile.readiness_command,
                    shell=True,
                    cwd=str(cwd),
                    env=dict(env),
                    capture_output=True,
                    text=True,
                    check=False,
                    timeout=10,
                )
            except subprocess.TimeoutExpired:
                last_error = "TARGET_PROFILE_READINESS_COMMAND_TIMEOUT"
                time.sleep(delay)
                delay = min(delay * 2, _READY_DELAY_MAX)
                continue
            if probe.returncode == 0:
                return True, None
            stderr = probe.stderr.strip() if probe.stderr else ""
            stdout = probe.stdout.strip() if probe.stdout else ""
            last_error = f"TARGET_PROFILE_READINESS_COMMAND_FAILED({probe.returncode})" + (
                f": {stderr or stdout}" if (stderr or stdout) else ""
            )
        else:
            try:
                ready = _probe_readiness_url(
                    base_url=base_url,
                    readiness_url=target_profile.readiness_url,
                )
            except Exception as exc:
                last_error = f"TARGET_PROFILE_READINESS_ERROR({exc})"
            else:
                if ready:
                    return True, None
                last_error = "TARGET_PROFILE_READINESS_TIMEOUT"
        time.sleep(delay)
        delay = min(delay * 2, _READY_DELAY_MAX)
    return False, last_error or "TARGET_PROFILE_READINESS_TIMEOUT"


def _probe_readiness_url(*, base_url: str, readiness_url: str | None) -> bool:
    requests = _requests_module()
    parsed_base = urlsplit(base_url)
    if not parsed_base.scheme or not parsed_base.netloc:
        raise ValueError(f"invalid base_url for readiness checks: {base_url!r}")
    url = _resolve_readiness_url(0, readiness_url=readiness_url, base_url=base_url)
    response = requests.get(url, timeout=2)
    return bool(response.status_code < 500)


def _read_launch_logs(log_path: Path | None) -> str:
    if log_path is None or not log_path.exists():
        return ""
    try:
        return log_path.read_text(encoding="utf-8")
    except OSError:
        return ""


def _persist_launch_logs(
    *,
    logs: str,
    target_profile: TargetLaunchProfile,
    logs_base_dir: Path | None,
) -> str | None:
    if target_profile.logs_path is None or not target_profile.logs_path.strip():
        return None
    output_path = Path(target_profile.logs_path).expanduser()
    if not output_path.is_absolute():
        base_dir = logs_base_dir if logs_base_dir is not None else Path.cwd()
        output_path = (base_dir / output_path).resolve(strict=False)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(logs, encoding="utf-8")
    return str(output_path)


def _teardown_profile_process(
    process: subprocess.Popen[str],
    *,
    teardown: TeardownMode,
    successful: bool,
) -> None:
    if teardown == "never":
        return
    if teardown == "on_success" and not successful:
        return
    if process.poll() is not None:
        return
    with suppress(Exception):
        process.terminate()
    with suppress(Exception):
        process.wait(timeout=5)
    if process.poll() is None:
        with suppress(Exception):
            process.kill()


def _build_image(client: Any, target_path: str) -> str:
    target_dir = _resolve_target_dir(target_path)
    with _prepared_build_context(target_dir) as context:
        LOGGER.info(
            "building sandbox image",
            extra={
                "event": "docker_build_start",
                "target_path": str(target_dir),
                "image_tag": context.image_tag,
                "port": context.port,
                "start_command": context.start_command,
                "dockerfile": context.dockerfile_name,
            },
        )
        image, build_logs = client.images.build(
            path=str(context.root),
            dockerfile=context.dockerfile_name,
            tag=context.image_tag,
            rm=True,
            forcerm=True,
        )
        LOGGER.info(
            "built sandbox image",
            extra={
                "event": "docker_build_complete",
                "target_path": str(target_dir),
                "image_tag": context.image_tag,
                "image_id": getattr(image, "id", None),
                "port": context.port,
                "build_log_preview": _summarize_build_logs(build_logs),
            },
        )
        return context.image_tag


def _start_container(client: Any, image: str) -> Container:
    internal_port = _image_internal_port(client, image)
    network = client.networks.create(
        (
            "piranesi-sandbox-"
            f"{hashlib.sha256(f'{image}-{time.time_ns()}'.encode()).hexdigest()[:8]}"
        ),
        driver="bridge",
        internal=True,
        labels={DOCKER_LABEL: DOCKER_LABEL_VALUE},
    )
    LOGGER.info(
        "created sandbox network",
        extra={
            "event": "docker_network_create",
            "image": image,
            "network_id": getattr(network, "id", None),
            "internal": True,
        },
    )

    try:
        run_kwargs = _container_run_kwargs(
            image=image, network=network, internal_port=internal_port
        )
        _assert_no_host_mounts(run_kwargs)
        LOGGER.info(
            "starting sandbox container",
            extra={
                "event": "docker_container_start",
                "image": image,
                "network_id": getattr(network, "id", None),
                "port": internal_port,
            },
        )
        container: Container = client.containers.run(**run_kwargs)
        _assert_runtime_mounts(container)
        return container
    except Exception:
        _teardown_networks(client, [getattr(network, "id", "")])
        raise


@contextmanager
def _prepared_build_context(target_dir: Path) -> Iterator[_PreparedBuildContext]:
    image_tag = _image_tag(target_dir)
    start_command = _detect_start_command(target_dir)
    port = _detect_port(target_dir)

    with tempfile.TemporaryDirectory(prefix="piranesi-sandbox-") as temp_dir:
        build_root = Path(temp_dir)
        _copy_build_context(target_dir, build_root)
        _ensure_start_script(build_root / "package.json", start_command)
        dockerfile_path = build_root / DOCKERFILE_NAME
        dockerfile_path.write_text(
            _generate_dockerfile(port=port, start_command=start_command),
            encoding="utf-8",
        )
        LOGGER.debug(
            "prepared sandbox build context",
            extra={
                "event": "docker_context_prepared",
                "target_path": str(target_dir),
                "context_path": str(build_root),
                "image_tag": image_tag,
                "port": port,
                "start_command": start_command,
            },
        )
        yield _PreparedBuildContext(
            root=build_root,
            dockerfile_name=DOCKERFILE_NAME,
            image_tag=image_tag,
            port=port,
            start_command=start_command,
        )


def _generate_dockerfile(*, port: int, start_command: str) -> str:
    return (
        "FROM node:20-slim\n"
        "WORKDIR /app\n\n"
        f'LABEL {GENERATED_LABEL}="true"\n'
        f"LABEL {INTERNAL_PORT_LABEL}={json.dumps(str(port))}\n"
        f"LABEL {START_COMMAND_LABEL}={json.dumps(start_command)}\n\n"
        "RUN rm -f .npmrc\n\n"
        "COPY package*.json ./\n"
        "RUN npm install --production --ignore-scripts --registry https://registry.npmjs.org/\n\n"
        "COPY . .\n\n"
        "RUN rm -f Dockerfile* docker-compose* .npmrc .env\n\n"
        f"EXPOSE {port}\n"
        'CMD ["npm", "start"]\n'
    )


def _resolve_target_dir(target_path: str) -> Path:
    target_dir = Path(target_path).expanduser().resolve()
    if not target_dir.is_dir():
        raise ValueError(f"target_path must point to a directory: {target_path}")
    if not (target_dir / "package.json").is_file():
        raise ValueError(f"target_path must contain package.json: {target_path}")
    return target_dir


def _copy_build_context(source_root: Path, build_root: Path) -> None:
    for root, dirs, files in os.walk(source_root, topdown=True, followlinks=False):
        root_path = Path(root)
        dirs[:] = [
            dirname
            for dirname in dirs
            if dirname not in _SKIP_DIRECTORIES and not (root_path / dirname).is_symlink()
        ]

        relative_dir = root_path.relative_to(source_root)
        destination_dir = build_root / relative_dir
        destination_dir.mkdir(parents=True, exist_ok=True)

        for filename in files:
            source_file = root_path / filename
            if source_file.is_symlink() or _should_skip_file(source_file):
                continue
            destination_file = destination_dir / filename
            destination_file.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(source_file, destination_file)


def _should_skip_file(path: Path) -> bool:
    lower_name = path.name.lower()
    if lower_name in {".npmrc", ".env"}:
        return True
    if lower_name.startswith("dockerfile"):
        return True
    return lower_name.startswith("docker-compose")


def _ensure_start_script(package_json_path: Path, start_command: str) -> None:
    package_data = _read_json(package_json_path)
    scripts = package_data.get("scripts")
    if scripts is None:
        scripts = {}
        package_data["scripts"] = scripts
    if not isinstance(scripts, dict):
        raise ValueError("package.json scripts must be an object")
    current_start = scripts.get("start")
    if isinstance(current_start, str) and current_start.strip():
        return
    scripts["start"] = start_command
    package_json_path.write_text(f"{json.dumps(package_data, indent=2)}\n", encoding="utf-8")


def _detect_start_command(target_dir: Path) -> str:
    package_data = _read_json(target_dir / "package.json")
    scripts = package_data.get("scripts", {})
    if isinstance(scripts, dict):
        start_command = scripts.get("start")
        if isinstance(start_command, str) and start_command.strip():
            return start_command.strip()
    for entrypoint in ("index.js", "server.js"):
        if (target_dir / entrypoint).is_file():
            return f"node {entrypoint}"
    raise ValueError("package.json is missing scripts.start and no fallback entrypoint was found")


def _detect_port(target_dir: Path) -> int:
    saw_port_env = False
    for source_path in _iter_source_files(target_dir):
        try:
            source_text = source_path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            continue

        if "process.env.PORT" in source_text:
            saw_port_env = True

        for pattern in _PORT_PATTERNS:
            match = pattern.search(source_text)
            if match is None:
                continue
            port = int(match.group(1))
            if 1 <= port <= 65535:
                return port

    if saw_port_env:
        return DEFAULT_PORT
    return DEFAULT_PORT


def _iter_source_files(target_dir: Path) -> Iterator[Path]:
    for root, dirs, files in os.walk(target_dir, topdown=True, followlinks=False):
        root_path = Path(root)
        dirs[:] = [
            dirname
            for dirname in dirs
            if dirname not in _SKIP_DIRECTORIES and not (root_path / dirname).is_symlink()
        ]
        for filename in files:
            source_path = root_path / filename
            if source_path.is_symlink():
                continue
            if source_path.suffix.lower() in _SOURCE_SUFFIXES:
                yield source_path


def _image_tag(target_dir: Path) -> str:
    digest = hashlib.sha256(str(target_dir).encode("utf-8")).hexdigest()[:12]
    return f"piranesi-target:{digest}"


def _container_run_kwargs(*, image: str, network: Any, internal_port: int) -> dict[str, object]:
    return {
        "image": image,
        "detach": True,
        "network": getattr(network, "id", None) or getattr(network, "name", None),
        "read_only": True,
        "tmpfs": {"/tmp": "size=64m"},  # noqa: S108
        "cap_drop": ["ALL"],
        "security_opt": ["no-new-privileges"],
        "mem_limit": "512m",
        "cpu_period": 100000,
        "cpu_quota": 100000,
        "pids_limit": 256,
        "user": "node",
        "ports": {f"{internal_port}/tcp": None},
        "environment": {"NODE_ENV": "production", "PORT": str(internal_port)},
        "labels": {
            DOCKER_LABEL: DOCKER_LABEL_VALUE,
            "piranesi.network_id": getattr(network, "id", ""),
        },
        "log_config": {"type": "json-file", "config": {"max-size": "10m", "max-file": "1"}},
        "privileged": False,
    }


def _assert_no_host_mounts(run_kwargs: Mapping[str, object]) -> None:
    for mount_key in ("volumes", "mounts"):
        if run_kwargs.get(mount_key):
            raise AssertionError("sandbox containers must not use host or named volume mounts")
    for mount_key in ("volumes", "mounts"):
        if _contains_docker_socket(run_kwargs.get(mount_key)):
            raise AssertionError("sandbox containers must never mount the Docker socket")


def _assert_runtime_mounts(container: Container) -> None:
    container.reload()
    attrs = getattr(container, "attrs", {}) or {}
    mounts = attrs.get("Mounts") or []
    binds = (attrs.get("HostConfig") or {}).get("Binds") or []
    if mounts or binds:
        raise AssertionError("sandbox containers must not have host or named volume mounts")
    if _contains_docker_socket(mounts) or _contains_docker_socket(binds):
        raise AssertionError("sandbox containers must never mount the Docker socket")


def _contains_docker_socket(value: object) -> bool:
    if value is None:
        return False
    if isinstance(value, str):
        return DOCKER_SOCKET_PATH in value
    if isinstance(value, Mapping):
        return any(_contains_docker_socket(item) for item in value.items())
    if isinstance(value, tuple):
        return any(_contains_docker_socket(item) for item in value)
    if isinstance(value, Sequence) and not isinstance(value, (str, bytes, bytearray)):
        return any(_contains_docker_socket(item) for item in value)
    return False


def _image_internal_port(client: Any, image: str) -> int:
    image_obj = client.images.get(image)
    labels = getattr(image_obj, "labels", None)
    if not isinstance(labels, dict):
        labels = ((getattr(image_obj, "attrs", {}) or {}).get("Config") or {}).get("Labels") or {}
    raw_port = labels.get(INTERNAL_PORT_LABEL, str(DEFAULT_PORT))
    try:
        port = int(raw_port)
    except (TypeError, ValueError):
        return DEFAULT_PORT
    return port if 1 <= port <= 65535 else DEFAULT_PORT


def _extract_host_port(bindings_map: object) -> int | None:
    if not isinstance(bindings_map, Mapping):
        return None
    for bindings in bindings_map.values():
        if not bindings:
            continue
        if isinstance(bindings, Mapping):
            binding_items: Sequence[object] = (bindings,)
        elif isinstance(bindings, Sequence) and not isinstance(bindings, (str, bytes, bytearray)):
            binding_items = bindings
        else:
            continue
        for binding in binding_items:
            if not isinstance(binding, Mapping):
                continue
            raw_host_port = binding.get("HostPort")
            if raw_host_port is None:
                continue
            try:
                host_port = int(str(raw_host_port).strip())
            except (TypeError, ValueError):
                continue
            if 1 <= host_port <= 65535:
                return host_port
    return None


def _get_host_port(
    container: Container,
    *,
    max_wait: float = 5.0,
    poll_interval: float = 0.1,
) -> int:
    deadline = time.perf_counter() + max_wait
    while True:
        container.reload()
        attrs = getattr(container, "attrs", {}) or {}
        network_settings = attrs.get("NetworkSettings") or {}
        host_config = attrs.get("HostConfig") or {}

        port = _extract_host_port(network_settings.get("Ports"))
        if port is None:
            port = _extract_host_port(host_config.get("PortBindings"))
        if port is not None:
            return port

        state = attrs.get("State") or {}
        is_running = bool(state.get("Running", True))
        if not is_running or time.perf_counter() >= deadline:
            break
        time.sleep(poll_interval)
    raise RuntimeError("sandbox container did not expose a host port")


def _wait_for_ready_in_container(
    container: Container,
    internal_port: int,
    *,
    max_wait: float = DEFAULT_READY_WAIT,
) -> bool:
    delay = _READY_DELAY_INITIAL
    started_at = time.perf_counter()

    while (time.perf_counter() - started_at) < max_wait:
        exec_result = container.exec_run(["node", "-e", _READY_PROBE_SCRIPT, str(internal_port)])
        if int(getattr(exec_result, "exit_code", exec_result[0])) == 0:  # type: ignore[arg-type]
            return True
        time.sleep(delay)
        delay = min(delay * 2, _READY_DELAY_MAX)
    return False


def _fire_payload_in_container(
    container: Container,
    payload: SynthesizedPayload,
    *,
    internal_port: int,
) -> ExploitResult:
    request_details = {
        "method": payload.method.upper(),
        "url": payload.url,
        "headers": dict(payload.headers),
        "body": payload.body,
        "encoding": payload.encoding,
        "payload_values": dict(payload.payload_values),
    }
    exec_result = container.exec_run(
        [
            "node",
            "-e",
            _IN_CONTAINER_REQUEST_SCRIPT,
            json.dumps(request_details),
            str(internal_port),
        ]
    )
    exit_code = int(getattr(exec_result, "exit_code", exec_result[0]))  # type: ignore[arg-type]
    raw_output = _decode_exec_output(exec_result)
    payload_data = _parse_in_container_payload(raw_output, request_details)
    if exit_code != 0 and payload_data["error"] is None:
        payload_data["error"] = f"container exec failed with exit code {exit_code}"
    if payload_data["error"] is not None:
        return ExploitResult.unreachable(
            error=str(payload_data["error"]),
            request=request_details,
        )
    return ExploitResult(
        status_code=int(payload_data["status_code"]),  # type: ignore[call-overload]
        headers=dict(payload_data["headers"]),  # type: ignore[call-overload]
        body=str(payload_data["body"]),
        elapsed_ms=float(payload_data["elapsed_ms"]),  # type: ignore[arg-type]
        request=request_details,
        error=None,
    )


def _container_network_ids(container: Container) -> list[str]:
    container.reload()
    networks = ((getattr(container, "attrs", {}) or {}).get("NetworkSettings") or {}).get(
        "Networks"
    ) or {}
    return [details["NetworkID"] for details in networks.values() if details.get("NetworkID")]


def _teardown_container(container: Container) -> None:
    container_id = getattr(container, "id", None)
    LOGGER.info(
        "tearing down sandbox container",
        extra={"event": "docker_container_teardown", "container_id": container_id},
    )
    with suppress(Exception):
        container.stop(timeout=5)
    with suppress(Exception):
        container.remove(force=True)


def _teardown_networks(client: Any, network_ids: Sequence[str]) -> None:
    for network_id in network_ids:
        if not network_id:
            continue
        LOGGER.info(
            "tearing down sandbox network",
            extra={"event": "docker_network_teardown", "network_id": network_id},
        )
        with suppress(Exception):
            client.networks.get(network_id).remove()


def _resolve_readiness_url(
    host_port: int,
    *,
    readiness_url: str | None,
    base_url: str | None = None,
) -> str:
    readiness = readiness_url or "/"
    ready_parts = urlsplit(readiness)
    if ready_parts.scheme and ready_parts.netloc:
        return readiness
    base = base_url or f"http://127.0.0.1:{host_port}"
    base_parts = urlsplit(base)
    if not base_parts.scheme or not base_parts.netloc:
        raise ValueError(f"invalid readiness base URL: {base!r}")
    normalized_path = ready_parts.path or "/"
    if not normalized_path.startswith("/"):
        normalized_path = f"/{normalized_path}"
    return urlunsplit(
        (
            base_parts.scheme,
            base_parts.netloc,
            normalized_path,
            ready_parts.query,
            ready_parts.fragment,
        )
    )


def _build_request_url(path: str, host_port: int, *, base_url: str | None = None) -> str:
    parsed = urlsplit(path)
    if parsed.scheme or parsed.netloc:
        raise ValueError("payload.url must be a relative path inside the sandbox target")
    normalized_path = parsed.path if parsed.path.startswith("/") else f"/{parsed.path}"
    base = base_url or f"http://127.0.0.1:{host_port}"
    base_parts = urlsplit(base)
    if not base_parts.scheme or not base_parts.netloc:
        raise ValueError(f"invalid base URL for payload request: {base!r}")
    return urlunsplit(
        (
            base_parts.scheme,
            base_parts.netloc,
            normalized_path,
            parsed.query,
            parsed.fragment,
        )
    )


def _container_exit_code(container: Container) -> int | None:
    state = (getattr(container, "attrs", {}) or {}).get("State") or {}
    exit_code = state.get("ExitCode")
    if exit_code is None:
        return None
    try:
        return int(exit_code)
    except (TypeError, ValueError):
        return None


def _serialize_container_diff(diff_entries: Sequence[Mapping[str, object]]) -> list[str]:
    serialized: list[str] = []
    for entry in diff_entries:
        kind = entry.get("Kind")
        path = entry.get("Path", "")
        serialized.append(f"{_DIFF_KIND_MAP.get(kind, kind)}:{path}")  # type: ignore[call-overload]
    return serialized


def _decode_docker_output(output: object) -> str:
    if isinstance(output, bytes):
        return output.decode("utf-8", errors="replace")
    if isinstance(output, str):
        return output
    if isinstance(output, Sequence):
        combined = bytearray()
        for chunk in output:
            if isinstance(chunk, bytes):
                combined.extend(chunk)
        return combined.decode("utf-8", errors="replace")
    return ""


def _decode_exec_output(result: object) -> str:
    if hasattr(result, "output"):
        return _decode_docker_output(result.output)
    if isinstance(result, tuple) and len(result) == 2:
        return _decode_docker_output(result[1])
    return ""


def _parse_in_container_payload(
    raw_output: str,
    request_details: dict[str, object],
) -> dict[str, object]:
    try:
        payload = json.loads(raw_output)
    except json.JSONDecodeError:
        return {
            "status_code": 0,
            "headers": {},
            "body": "",
            "elapsed_ms": 0.0,
            "request": request_details,
            "error": f"invalid sandbox response: {raw_output.strip()!r}",
        }
    if not isinstance(payload, dict):
        return {
            "status_code": 0,
            "headers": {},
            "body": "",
            "elapsed_ms": 0.0,
            "request": request_details,
            "error": f"invalid sandbox response structure: {payload!r}",
        }
    payload.setdefault("status_code", 0)
    payload.setdefault("headers", {})
    payload.setdefault("body", "")
    payload.setdefault("elapsed_ms", 0.0)
    payload.setdefault("request", request_details)
    payload.setdefault("error", None)
    return payload


def _summarize_build_logs(build_logs: object, *, limit: int = 3) -> str:
    if not isinstance(build_logs, Sequence):
        return ""
    lines: list[str] = []
    for entry in build_logs:
        if not isinstance(entry, Mapping):
            continue
        text = entry.get("stream") or entry.get("status") or entry.get("error")
        if isinstance(text, str):
            stripped = text.strip()
            if stripped:
                lines.append(stripped)
    return " | ".join(lines[-limit:])


def _read_json(path: Path) -> dict[str, object]:
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise ValueError(f"expected JSON object in {path}")
    return data


def _docker_client() -> Any:
    if _docker is None:
        raise RuntimeError("docker is not installed")
    return _docker.from_env()


def _requests_module() -> Any:
    if _requests is None:
        raise RuntimeError("requests is not installed")
    return _requests


def _close_client(client: Any) -> None:
    close = getattr(client, "close", None)
    if callable(close):
        close()


__all__ = [
    "ExploitResult",
    "SandboxCapture",
    "SynthesizedPayload",
    "TargetLaunchProfile",
    "build_image",
    "capture_results",
    "fire_payload",
    "run_in_sandbox",
    "start_container",
    "wait_for_ready",
]
