from __future__ import annotations

import io
import json
import logging
from contextlib import suppress
from types import SimpleNamespace

from rich.logging import RichHandler

from piranesi.observability import JsonLogFormatter, command_archive, run_subprocess, setup_logging
from piranesi.ui import console as ui_console


class _FakeStderr(io.StringIO):
    def __init__(self, *, isatty: bool) -> None:
        super().__init__()
        self._isatty = isatty

    def isatty(self) -> bool:
        return self._isatty


def test_setup_logging_tty_reuses_ui_console(monkeypatch) -> None:
    monkeypatch.setattr(
        "piranesi.observability.sys.stderr",
        SimpleNamespace(isatty=lambda: True),
    )

    setup_logging(json_logs=False)

    root_logger = logging.getLogger()
    rich_handler = next(
        handler for handler in root_logger.handlers if isinstance(handler, RichHandler)
    )
    assert rich_handler.console is ui_console


def test_setup_logging_json_logs_forces_stream_formatter(monkeypatch) -> None:
    fake_stderr = _FakeStderr(isatty=True)
    monkeypatch.setattr("piranesi.observability.sys.stderr", fake_stderr)

    setup_logging(json_logs=True)

    root_logger = logging.getLogger()
    assert len(root_logger.handlers) == 1
    handler = root_logger.handlers[0]
    assert not isinstance(handler, RichHandler)
    assert isinstance(handler.formatter, JsonLogFormatter)


def test_command_archive_records_subprocess_output(tmp_path) -> None:
    with command_archive(tmp_path / "debug"):
        result = run_subprocess(["python3", "-c", "print('hello')"])

    assert result.returncode == 0
    commands = (tmp_path / "debug" / "commands.ndjson").read_text(encoding="utf-8").splitlines()
    assert len(commands) == 1
    payload = json.loads(commands[0])
    assert payload["exit_code"] == 0
    assert "python3 -c" in payload["cmd"]
    assert payload["stdout_preview"].strip() == "hello"

    tool_logs = sorted((tmp_path / "debug" / "tools").glob("*.md"))
    assert len(tool_logs) == 1
    tool_log = tool_logs[0].read_text(encoding="utf-8")
    assert "## Input" in tool_log
    assert "hello" in tool_log


def test_command_archive_records_missing_command(tmp_path) -> None:
    with command_archive(tmp_path / "debug"), suppress(FileNotFoundError):
        run_subprocess(["definitely-not-a-piranesi-command"])

    commands = (tmp_path / "debug" / "commands.ndjson").read_text(encoding="utf-8").splitlines()
    payload = json.loads(commands[0])
    assert payload["exit_code"] is None
    assert "error" in payload
