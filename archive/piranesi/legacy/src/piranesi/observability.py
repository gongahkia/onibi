from __future__ import annotations

import json
import logging
import shlex
import subprocess
import sys
import time
from collections.abc import Iterator, Mapping, Sequence
from contextlib import contextmanager
from contextvars import ContextVar
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from rich.logging import RichHandler

from piranesi.ui import console as ui_console

_ACTIVE_COMMAND_ARCHIVE: ContextVar[CommandArchive | None] = ContextVar(
    "piranesi_command_archive",
    default=None,
)

_RESERVED_LOG_KEYS = {
    "args",
    "asctime",
    "created",
    "exc_info",
    "exc_text",
    "filename",
    "funcName",
    "levelname",
    "levelno",
    "lineno",
    "module",
    "msecs",
    "message",
    "msg",
    "name",
    "pathname",
    "process",
    "processName",
    "relativeCreated",
    "stack_info",
    "thread",
    "threadName",
}


class JsonLogFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        payload: dict[str, Any] = {
            "ts": datetime.fromtimestamp(record.created, UTC).isoformat().replace("+00:00", "Z"),
            "level": record.levelname,
            "logger": record.name,
            "event": getattr(record, "event", record.name.split(".")[-1]),
            "msg": record.getMessage(),
        }
        for key, value in record.__dict__.items():
            if key not in _RESERVED_LOG_KEYS and not key.startswith("_"):
                payload[key] = value
        if record.exc_info is not None:
            payload["exception"] = self.formatException(record.exc_info)
        return json.dumps(payload, default=str)


def setup_logging(
    *,
    verbose: bool = False,
    quiet: bool = False,
    debug: bool = False,
    json_logs: bool = False,
) -> None:
    if debug or verbose:
        level = logging.DEBUG
    elif quiet:
        level = logging.WARNING
    else:
        level = logging.INFO

    is_tty = sys.stderr.isatty() and not json_logs
    if is_tty:
        handler: logging.Handler = RichHandler(
            console=ui_console,
            rich_tracebacks=True,
            tracebacks_show_locals=debug,
            show_path=debug,
            show_time=verbose or debug,
        )
        handler.setFormatter(logging.Formatter("%(message)s"))
    else:
        handler = logging.StreamHandler(sys.stderr)
        handler.setFormatter(JsonLogFormatter())

    logging.basicConfig(level=level, handlers=[handler], force=True)


def log_error_context(
    logger: logging.Logger,
    *,
    event: str,
    what: str,
    on_what: str,
    why: str,
    next_step: str,
    debug: str,
    **extra: object,
) -> None:
    logger.error(
        "%s failed | on=%s | why=%s | next=%s | debug=%s",
        what,
        on_what,
        why,
        next_step,
        debug,
        extra={
            "event": event,
            "what": what,
            "on_what": on_what,
            "why": why,
            "next": next_step,
            "debug": debug,
            **extra,
        },
    )


class CommandArchive:
    def __init__(self, debug_dir: Path) -> None:
        self.debug_dir = debug_dir
        self.tools_dir = debug_dir / "tools"
        self.commands_path = debug_dir / "commands.ndjson"
        self._counter = 0

    def open(self) -> None:
        self.tools_dir.mkdir(parents=True, exist_ok=True)
        self.commands_path.parent.mkdir(parents=True, exist_ok=True)
        self.commands_path.touch(exist_ok=True)

    def record(
        self,
        *,
        cmd: str,
        cwd: str | None,
        duration_ms: int,
        exit_code: int | None,
        stdout: str,
        stderr: str,
        error: str | None = None,
    ) -> None:
        self.open()
        self._counter += 1
        tool_name = _archive_tool_name(cmd)
        tool_path = self.tools_dir / f"{self._counter:03d}_{tool_name}.md"
        timestamp = datetime.now(UTC).isoformat().replace("+00:00", "Z")
        payload: dict[str, object] = {
            "timestamp": timestamp,
            "sequence": self._counter,
            "cmd": cmd,
            "cwd": cwd,
            "duration_ms": duration_ms,
            "exit_code": exit_code,
            "stdout_preview": _truncate_output(stdout, 1000),
            "stderr_preview": _truncate_output(stderr, 1000),
            "tool_log": str(tool_path),
        }
        if error is not None:
            payload["error"] = error
        with self.commands_path.open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(payload, default=str))
            handle.write("\n")
        tool_path.write_text(
            _render_tool_log(
                cmd=cmd,
                cwd=cwd,
                timestamp=timestamp,
                duration_ms=duration_ms,
                exit_code=exit_code,
                stdout=stdout,
                stderr=stderr,
                error=error,
            ),
            encoding="utf-8",
        )


@contextmanager
def command_archive(debug_dir: Path | None) -> Iterator[CommandArchive | None]:
    if debug_dir is None:
        yield None
        return
    archive = CommandArchive(debug_dir)
    archive.open()
    token = _ACTIVE_COMMAND_ARCHIVE.set(archive)
    try:
        yield archive
    finally:
        _ACTIVE_COMMAND_ARCHIVE.reset(token)


def run_subprocess(
    cmd: Sequence[str],
    *,
    cwd: str | Path | None = None,
    timeout: int = 60,
    env: Mapping[str, str] | None = None,
    logger: logging.Logger | None = None,
) -> subprocess.CompletedProcess[str]:
    log = logger or logging.getLogger("piranesi.subprocess")
    command = shlex.join(cmd)
    working_directory = str(cwd) if cwd is not None else None
    started_at = time.perf_counter()
    log.debug(
        "running subprocess %s",
        command,
        extra={"event": "subprocess_start", "cmd": command, "cwd": working_directory},
    )
    try:
        result = subprocess.run(
            list(cmd),
            cwd=working_directory,
            env=dict(env) if env is not None else None,
            capture_output=True,
            text=True,
            timeout=timeout,
            check=False,
        )
    except FileNotFoundError as exc:
        duration_ms = int((time.perf_counter() - started_at) * 1000)
        _record_active_command_archive(
            cmd=command,
            cwd=working_directory,
            duration_ms=duration_ms,
            exit_code=None,
            stdout="",
            stderr="",
            error=str(exc),
        )
        raise
    except subprocess.TimeoutExpired as exc:
        duration_ms = int((time.perf_counter() - started_at) * 1000)
        _record_active_command_archive(
            cmd=command,
            cwd=working_directory,
            duration_ms=duration_ms,
            exit_code=None,
            stdout=str(exc.stdout or ""),
            stderr=str(exc.stderr or ""),
            error=f"timeout after {timeout}s",
        )
        log_error_context(
            log,
            event="subprocess_timeout",
            what="subprocess_exec",
            on_what=command,
            why=f"timeout after {timeout}s",
            next_step="raising timeout",
            debug=(
                f"cwd={working_directory}; partial_stdout={exc.stdout!r}; "
                f"partial_stderr={exc.stderr!r}"
            ),
            duration_ms=duration_ms,
            cmd=command,
        )
        raise

    duration_ms = int((time.perf_counter() - started_at) * 1000)
    _record_active_command_archive(
        cmd=command,
        cwd=working_directory,
        duration_ms=duration_ms,
        exit_code=result.returncode,
        stdout=result.stdout,
        stderr=result.stderr,
    )
    stdout_preview = _truncate_output(result.stdout, 500)
    stderr_preview = _truncate_output(result.stderr, 500)
    if result.returncode == 0:
        log.debug(
            "subprocess completed exit=%d duration_ms=%d",
            result.returncode,
            duration_ms,
            extra={
                "event": "subprocess_complete",
                "cmd": command,
                "cwd": working_directory,
                "duration_ms": duration_ms,
                "exit_code": result.returncode,
                "stdout": stdout_preview,
                "stderr": stderr_preview,
            },
        )
        return result

    log_error_context(
        log,
        event="subprocess_failed",
        what="subprocess_exec",
        on_what=command,
        why=f"exit_code={result.returncode}",
        next_step="propagating result to caller",
        debug=f"cwd={working_directory}; stdout={result.stdout}; stderr={result.stderr}",
        duration_ms=duration_ms,
        cmd=command,
        exit_code=result.returncode,
    )
    return result


def _truncate_output(output: str, limit: int) -> str:
    if len(output) <= limit:
        return output
    return f"{output[:limit]}...<truncated>"


def _record_active_command_archive(
    *,
    cmd: str,
    cwd: str | None,
    duration_ms: int,
    exit_code: int | None,
    stdout: str,
    stderr: str,
    error: str | None = None,
) -> None:
    archive = _ACTIVE_COMMAND_ARCHIVE.get()
    if archive is None:
        return
    archive.record(
        cmd=cmd,
        cwd=cwd,
        duration_ms=duration_ms,
        exit_code=exit_code,
        stdout=stdout,
        stderr=stderr,
        error=error,
    )


def _archive_tool_name(cmd: str) -> str:
    try:
        first = shlex.split(cmd)[0]
    except (IndexError, ValueError):
        first = "command"
    name = Path(first).name or "command"
    return "".join(ch.lower() if ch.isalnum() else "-" for ch in name).strip("-") or "command"


def _render_tool_log(
    *,
    cmd: str,
    cwd: str | None,
    timestamp: str,
    duration_ms: int,
    exit_code: int | None,
    stdout: str,
    stderr: str,
    error: str | None,
) -> str:
    parts = [
        f"# {_archive_tool_name(cmd)}",
        f"Timestamp: {timestamp}",
        f"Working directory: {cwd or ''}",
        f"Duration: {duration_ms} ms",
        f"Exit code: {'' if exit_code is None else exit_code}",
        "",
        "## Input",
        "```bash",
        cmd,
        "```",
        "",
        "## Stdout",
        "```text",
        _truncate_lines(stdout),
        "```",
        "",
        "## Stderr",
        "```text",
        _truncate_lines(stderr),
        "```",
    ]
    if error is not None:
        parts.extend(["", "## Error", "```text", error, "```"])
    return "\n".join(parts) + "\n"


def _truncate_lines(output: str, max_lines: int = 200) -> str:
    lines = output.splitlines()
    if len(lines) <= max_lines:
        return output
    kept = "\n".join(lines[:max_lines])
    return f"{kept}\n[truncated: {len(lines)} lines total]"
