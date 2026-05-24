from __future__ import annotations

import subprocess
from datetime import UTC, datetime
from pathlib import Path

from piranesi.models import (
    CandidateFinding,
    ConfirmedFinding,
    PathCondition,
    SandboxResult,
    SourceLocation,
    TaintSink,
    TaintSource,
    TaintStep,
    TriagedFinding,
)
from piranesi.verify.constraints import extract_exploit_template
from piranesi.verify.reproducer import generate_reproducer_script, write_reproducer_script
from piranesi.verify.sandbox import SynthesizedPayload
from piranesi.verify.solver import solve_exploit_template


def test_candidate_finding_constraints_solve_to_a_valid_payload(tmp_path: Path) -> None:
    app_file = tmp_path / "app.js"
    app_file.write_text(
        "\n".join(
            [
                'const express = require("express");',
                "const app = express();",
                'app.get("/search", (req, res) => {',
                "  const query = req.query.q;",
                '  if (typeof query !== "string") {',
                '    return res.status(400).send("bad");',
                "  }",
                "  if (query.length < 8) {",
                '    return res.status(400).send("short");',
                "  }",
                "  return res.send(`<html>${query}</html>`);",
                "});",
            ]
        ),
        encoding="utf-8",
    )
    finding = _candidate_finding(
        app_file,
        vuln_class="CWE-79",
        source_line=4,
        source_snippet="const query = req.query.q;",
        source_type="req.query.q",
        parameter_name="q",
        sink_line=11,
        sink_snippet="return res.send(`<html>${query}</html>`);",
        sink_api="res.send",
        path_conditions=[
            _raw_condition(app_file, 'typeof query === "string"'),
            _raw_condition(app_file, "query.length >= 8"),
        ],
    )

    template = extract_exploit_template(finding)
    result = solve_exploit_template(template)

    assert result.status == "SAT"
    payload = result.solutions[0].payload
    assert payload.method == "GET"
    assert payload.url == "/search"
    assert payload.encoding == "query"
    assert payload.body == {"q": payload.payload_values["q"]}
    assert "<script>" in payload.payload_values["q"]
    assert len(payload.payload_values["q"]) >= 8


def test_generate_reproducer_script_renders_hardened_bash(tmp_path: Path) -> None:
    app_file = tmp_path / "app.js"
    app_file.write_text("app.post('/login', () => {});\n", encoding="utf-8")
    finding = _candidate_finding(
        app_file,
        vuln_class="CWE-89",
        source_line=1,
        source_snippet="const username = req.body.username;",
        source_type="req.body.username",
        parameter_name="username",
        sink_line=1,
        sink_snippet="db.query(sql);",
        sink_api="db.query",
        path_conditions=[],
    )
    payload = SynthesizedPayload(
        method="POST",
        url="/login",
        headers={"Content-Type": "application/json"},
        body={"username": "' OR 1=1--", "password": "anything"},
        payload_values={"username": "' OR 1=1--"},
        encoding="json",
    )

    script = generate_reproducer_script(
        finding,
        payload=payload,
        target_path=tmp_path,
        internal_port=3000,
        generated_at=datetime(2026, 4, 9, 12, 0, tzinfo=UTC),
    )
    script_path = write_reproducer_script(tmp_path / "repro.sh", script)

    assert script.startswith("#!/usr/bin/env bash\n")
    assert "WARNING: This script demonstrates a security vulnerability." in script
    assert "docker network create --internal" in script
    assert "--read-only" in script
    assert "--cap-drop ALL" in script
    assert "--security-opt no-new-privileges" in script
    assert "db.query at line 1" in script
    assert '--data \'{"username":"\'"\'"\' OR 1=1--","password":"anything"}\'' in script

    subprocess.run(["bash", "-n", str(script_path)], check=True)


def test_generate_reproducer_script_uses_confirmed_finding_request_when_payload_missing(
    tmp_path: Path,
) -> None:
    app_file = tmp_path / "app.js"
    app_file.write_text("app.get('/search', () => {});\n", encoding="utf-8")
    candidate = _candidate_finding(
        app_file,
        vuln_class="CWE-79",
        source_line=1,
        source_snippet="const query = req.query.q;",
        source_type="req.query.q",
        parameter_name="q",
        sink_line=1,
        sink_snippet="res.send(query);",
        sink_api="res.send",
        path_conditions=[],
    )
    confirmed = _confirmed_finding(candidate)

    script = generate_reproducer_script(
        confirmed,
        target_path=tmp_path,
        generated_at=datetime(2026, 4, 9, 12, 0, tzinfo=UTC),
    )
    script_path = write_reproducer_script(tmp_path / "confirmed-repro.sh", script)

    assert "--get" in script
    assert "--data-urlencode 'q=<script>alert(1)</script>'" in script
    assert "Reflected XSS payload was returned unescaped." in script
    subprocess.run(["bash", "-n", str(script_path)], check=True)


def _candidate_finding(
    file_path: Path,
    *,
    vuln_class: str,
    source_line: int,
    source_snippet: str,
    source_type: str,
    parameter_name: str,
    sink_line: int,
    sink_snippet: str,
    sink_api: str,
    path_conditions: list[PathCondition],
) -> CandidateFinding:
    source_location = SourceLocation(
        file=str(file_path),
        line=source_line,
        column=1,
        snippet=source_snippet,
    )
    sink_location = SourceLocation(
        file=str(file_path),
        line=sink_line,
        column=1,
        snippet=sink_snippet,
    )
    return CandidateFinding(
        id="finding-repro",
        vuln_class=vuln_class,
        source=TaintSource(
            location=source_location,
            source_type=source_type,
            data_categories=["identifier"],
            parameter_name=parameter_name,
        ),
        sink=TaintSink(
            location=sink_location,
            sink_type="sink",
            api_name=sink_api,
        ),
        taint_path=[
            TaintStep(
                location=sink_location,
                operation="call_arg",
                taint_state="tainted",
            )
        ],
        path_conditions=path_conditions,
        confidence=0.9,
        severity="high",
    )


def _raw_condition(file_path: Path, expression: str) -> PathCondition:
    return PathCondition(
        location=SourceLocation(
            file=str(file_path),
            line=1,
            column=1,
            snippet=expression,
        ),
        condition_type="branch",
        expression=expression,
        required_value=True,
        symbolic_constraint=None,
    )


def _confirmed_finding(candidate: CandidateFinding) -> ConfirmedFinding:
    triaged = TriagedFinding(
        finding=candidate,
        triage_verdict="confirmed",
        skeptic_analysis="Exploit verified in sandbox.",
        ensemble_score=0.99,
        escalated=False,
    )
    sandbox_result = SandboxResult(
        container_id="sandbox-1",
        request={
            "method": "GET",
            "url": "http://127.0.0.1:3000/search",
            "headers": {},
            "body": {"q": "<script>alert(1)</script>"},
            "encoding": "query",
            "payload_values": {"q": "<script>alert(1)</script>"},
        },
        response={"status": 200, "body": "<html><script>alert(1)</script></html>"},
        timing_ms=12,
        side_effects=[],
        container_diff=[],
        stdout="",
        stderr="",
        exit_code=0,
        network_isolated=True,
        confirmed=True,
    )
    return ConfirmedFinding(
        finding=triaged,
        exploit_payload="<script>alert(1)</script>",
        exploit_constraints=["query.length >= 8"],
        sandbox_result=sandbox_result,
        reproducer_script="",
        related_cves=[],
    )
