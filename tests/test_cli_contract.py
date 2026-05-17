from __future__ import annotations

import json
import re

from typer.testing import CliRunner

from piranesi.cli import app

runner = CliRunner()
_ANSI_RE = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")


def _plain_output(output: str) -> str:
    return _ANSI_RE.sub("", output)


def test_root_help_exposes_exactly_phase_1_verbs() -> None:
    result = runner.invoke(app, ["--help"])
    output = _plain_output(result.stdout)

    assert result.exit_code == 0
    for command in ["ingest", "report", "retest", "sign", "serve"]:
        assert command in output
    for old_command in [
        "quickstart",
        "demo",
        "collect",
        "doctor",
        "assess",
        "fleet",
        "container",
        "k8s",
        "run",
        "ui",
        "rules",
        "plugins",
        "support-bundle",
    ]:
        assert old_command not in output


def test_old_commands_are_not_accepted() -> None:
    result = runner.invoke(app, ["doctor"])

    assert result.exit_code != 0
    assert "No such command" in result.output


def test_not_yet_implemented_adapter_has_deliberate_exit_code(tmp_path) -> None:
    fixture = tmp_path / "burp.xml"
    fixture.write_text("<issues/>", encoding="utf-8")

    result = runner.invoke(
        app,
        [
            "ingest",
            "burp",
            "--input",
            str(fixture),
            "--workspace",
            str(tmp_path / "workspace"),
            "--json-errors",
        ],
    )

    assert result.exit_code == 64
    payload = json.loads(result.output)
    assert payload["exit_code"] == 64
    assert "issue #32" in payload["error"]
