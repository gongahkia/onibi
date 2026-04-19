from __future__ import annotations

import io
import logging
from types import SimpleNamespace

from rich.logging import RichHandler

from piranesi.observability import JsonLogFormatter, setup_logging
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
    rich_handler = next(handler for handler in root_logger.handlers if isinstance(handler, RichHandler))
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
