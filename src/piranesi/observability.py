from __future__ import annotations

import json
import logging
import shlex
import subprocess
import sys
import time
from collections.abc import Mapping, Sequence
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from rich.logging import RichHandler

from piranesi.ui import console as ui_console

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
    except subprocess.TimeoutExpired as exc:
        duration_ms = int((time.perf_counter() - started_at) * 1000)
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
