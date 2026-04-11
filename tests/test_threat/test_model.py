from __future__ import annotations

import json

from tests.test_threat._helpers import make_entry_point, make_finding

from piranesi.threat.model import generate_threat_model


def test_full_threat_model_contains_all_sections() -> None:
    findings = [
        make_finding(finding_id="f1", vuln_class="CWE-89"),
        make_finding(
            finding_id="f2",
            vuln_class="CWE-79",
            sink_type="html_output",
            sink_api_name="res.send",
        ),
    ]
    report = generate_threat_model(findings, entry_points=[make_entry_point()], top_n=2)
    assert "Threat Model Summary" in report
    assert "STRIDE" in report
    assert "DREAD" in report
    assert "Attack Tree" in report or "GOAL" in report
    assert "Risk Matrix" in report


def test_json_format_parseable() -> None:
    findings = [make_finding(vuln_class="CWE-89")]
    report = generate_threat_model(findings, entry_points=[make_entry_point()], format="json")
    parsed = json.loads(report)
    assert "stride" in parsed
    assert "dread" in parsed
    assert "attack_trees" in parsed
    assert "dfd" in parsed
