from __future__ import annotations

from tests.test_threat._helpers import make_entry_point, make_finding

from piranesi.threat.dfd import extract_dfd, render_dfd


def test_dfd_elements_from_entry_points() -> None:
    entries = [make_entry_point(http_method="POST", route_pattern="/api/users")]
    dfd = extract_dfd(findings=[], entry_points=entries)
    types = {element.element_type for element in dfd.elements}
    assert "external_entity" in types
    assert "process" in types


def test_dfd_data_store_from_sql_sink() -> None:
    finding = make_finding(vuln_class="CWE-89", sink_type="sql_query")
    dfd = extract_dfd(findings=[finding], entry_points=[])
    types = {element.element_type for element in dfd.elements}
    assert "data_store" in types


def test_taint_overlay_marks_flows() -> None:
    finding = make_finding(vuln_class="CWE-89")
    entry = make_entry_point()
    dfd = extract_dfd(findings=[finding], entry_points=[entry])
    tainted_flows = [flow for flow in dfd.flows if flow.is_tainted]
    assert len(tainted_flows) > 0


def test_trust_boundary_detection() -> None:
    entry = make_entry_point(middleware=["authenticate", "validateInput"])
    dfd = extract_dfd(findings=[], entry_points=[entry])
    boundaries = [element for element in dfd.elements if element.element_type == "trust_boundary"]
    assert len(boundaries) > 0


def test_mermaid_dfd_output() -> None:
    finding = make_finding(vuln_class="CWE-89")
    entry = make_entry_point()
    dfd = extract_dfd(findings=[finding], entry_points=[entry])
    mermaid = render_dfd(dfd, format="mermaid")
    assert "subgraph" in mermaid
    assert "graph" in mermaid
