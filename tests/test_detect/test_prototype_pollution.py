from __future__ import annotations

from pathlib import Path

from piranesi.detect.flows import extract_candidate_findings
from piranesi.detect.prototype_pollution import extract_prototype_pollution_findings
from piranesi.scan.specs import get_sink_specs, get_source_specs

PROTOTYPE_APP_DIR = (
    Path(__file__).resolve().parents[1] / "fixtures" / "typescript" / "prototype_pollution_app"
)


class EmptyJoernServer:
    def query(self, _cpgql: str) -> dict[str, object]:
        return {"success": True, "stdout": 'val res0: String = "[]"'}


def _prototype_sink_specs():
    return tuple(
        spec for spec in get_sink_specs(frameworks=("express",)) if spec.cwe_id == "CWE-1321"
    )


def test_extract_prototype_pollution_findings_detects_merge_sinks_and_magic_paths() -> None:
    findings = extract_prototype_pollution_findings(
        PROTOTYPE_APP_DIR,
        sink_specs=_prototype_sink_specs(),
    )

    actual = {
        (
            finding.source.location.line,
            finding.sink.api_name,
            finding.metadata.get("magic_property"),
        )
        for finding in findings
    }

    assert actual == {
        (17, "Object.assign", None),
        (24, "_.merge", None),
        (31, "lodash.defaultsDeep", None),
        (36, "merge", None),
        (43, "_.merge", "constructor.prototype"),
        (50, "Object.assign", "__proto__"),
    }
    assert all(finding.vuln_class == "CWE-1321" for finding in findings)


def test_extract_candidate_findings_merges_prototype_pollution_results() -> None:
    body_source = next(
        spec
        for spec in get_source_specs(frameworks=("express",))
        if spec.name == "express_req_body"
    )

    findings = extract_candidate_findings(
        EmptyJoernServer(),  # type: ignore[arg-type]
        joern_project_root=PROTOTYPE_APP_DIR,
        source_specs=(body_source,),
        sink_specs=_prototype_sink_specs(),
        sanitizer_specs=(),
    )

    actual = {
        (
            finding.source.location.line,
            finding.sink.api_name,
            finding.metadata.get("magic_property"),
        )
        for finding in findings
    }

    assert actual == {
        (17, "Object.assign", None),
        (24, "_.merge", None),
        (31, "lodash.defaultsDeep", None),
        (36, "merge", None),
        (43, "_.merge", "constructor.prototype"),
        (50, "Object.assign", "__proto__"),
    }
