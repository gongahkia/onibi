from __future__ import annotations

from pathlib import Path

from piranesi.detect.alias import extract_alias_findings
from piranesi.detect.flows import extract_candidate_findings
from piranesi.scan.specs import get_sink_specs, get_source_specs

ALIAS_APP_DIR = Path(__file__).resolve().parents[1] / "fixtures" / "typescript" / "alias_app"


class EmptyJoernServer:
    def query(self, _cpgql: str) -> dict[str, object]:
        return {"success": True, "stdout": 'val res0: String = "[]"'}


def _relevant_sink_specs():
    sinks = get_sink_specs(frameworks=("express",))
    wanted = {
        "raw_sql_query",
        "child_process_exec",
    }
    return tuple(spec for spec in sinks if spec.name in wanted)


def test_extract_alias_findings_detects_property_destructuring_and_spread_patterns() -> None:
    findings = extract_alias_findings(
        ALIAS_APP_DIR,
        sink_specs=_relevant_sink_specs(),
    )

    actual = {
        (
            finding.source.location.line,
            finding.source.parameter_name,
            finding.sink.api_name,
            finding.vuln_class,
        )
        for finding in findings
    }

    assert actual == {
        (4, "name", "db.query", "CWE-89"),
        (11, "email", "db.query", "CWE-89"),
        (16, "name", "db.query", "CWE-89"),
        (22, "sql", "db.query", "CWE-89"),
        (28, "command", "child.exec", "CWE-78"),
    }
    assert all(finding.metadata.get("detector") == "alias" for finding in findings)


def test_extract_candidate_findings_merges_alias_detector_results() -> None:
    body_source = next(
        spec
        for spec in get_source_specs(frameworks=("express",))
        if spec.name == "express_req_body"
    )

    findings = extract_candidate_findings(
        EmptyJoernServer(),  # type: ignore[arg-type]
        joern_project_root=ALIAS_APP_DIR,
        source_specs=(body_source,),
        sink_specs=_relevant_sink_specs(),
        sanitizer_specs=(),
    )

    actual = {
        (
            finding.source.location.line,
            finding.source.parameter_name,
            finding.sink.api_name,
            finding.vuln_class,
        )
        for finding in findings
    }

    assert actual == {
        (4, "name", "db.query", "CWE-89"),
        (11, "email", "db.query", "CWE-89"),
        (16, "name", "db.query", "CWE-89"),
        (22, "sql", "db.query", "CWE-89"),
        (28, "command", "child.exec", "CWE-78"),
    }
