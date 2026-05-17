from __future__ import annotations

import json

from tests.test_threat._helpers import make_finding

from piranesi.threat.attack_tree import AttackNode, generate_attack_tree, render_tree


def test_sqli_tree_structure() -> None:
    finding = make_finding(vuln_class="CWE-89")
    tree = generate_attack_tree(finding)
    assert tree.node_type == "goal"
    assert any(child.node_type == "or" for child in tree.children)
    leaf_labels = _collect_leaves(tree)
    assert any("UNION" in label for label in leaf_labels)
    assert any("blind" in label.lower() for label in leaf_labels)


def test_xss_tree_structure() -> None:
    finding = make_finding(vuln_class="CWE-79", sink_type="html_output", sink_api_name="res.send")
    tree = generate_attack_tree(finding)
    leaf_labels = _collect_leaves(tree)
    assert any("Reflected" in label for label in leaf_labels)
    assert any("Stored" in label for label in leaf_labels)


def test_mermaid_output_valid() -> None:
    finding = make_finding(vuln_class="CWE-89")
    tree = generate_attack_tree(finding)
    mermaid = render_tree(tree, format="mermaid")
    assert mermaid.startswith("graph TD")
    assert "-->" in mermaid


def test_json_output_roundtrip() -> None:
    finding = make_finding(
        vuln_class="CWE-78",
        sink_type="command_execution",
        sink_api_name="child_process.exec",
    )
    tree = generate_attack_tree(finding)
    parsed = json.loads(render_tree(tree, format="json"))
    assert parsed["node_type"] == "goal"
    assert len(parsed["children"]) > 0


def _collect_leaves(node: AttackNode) -> list[str]:
    if not node.children:
        return [node.label]
    labels: list[str] = []
    for child in node.children:
        labels.extend(_collect_leaves(child))
    return labels
