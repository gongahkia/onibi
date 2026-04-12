from __future__ import annotations

import json
from collections.abc import Mapping, Sequence
from dataclasses import asdict, dataclass
from typing import Literal, cast

from piranesi.models import AttackSurfaceNode, CandidateFinding, EntryPoint, ScannedFunction
from piranesi.report.cwe import cwe_title, extract_cwe_id
from piranesi.threat.attack_tree import AttackNode, generate_attack_tree, render_tree
from piranesi.threat.dfd import DfdDiagram, extract_dfd, render_dfd
from piranesi.threat.dread import DreadScore, score_all
from piranesi.threat.stride import (
    STRIDE_ORDER,
    StrideCategory,
    classify_all,
    stride_breakdown,
    stride_label,
    stride_members,
)


@dataclass(frozen=True)
class ThreatTreeResult:
    finding_id: str
    cwe: str
    title: str
    dread: float
    risk_level: str
    tree: AttackNode


@dataclass(frozen=True)
class ThreatModelResult:
    summary: dict[str, object]
    stride: dict[str, list[str]]
    dread: list[dict[str, object]]
    attack_trees: list[ThreatTreeResult]
    dfd: DfdDiagram
    risk_matrix: dict[str, dict[str, int]]


ThreatModelFormat = Literal["markdown", "json"]


def build_threat_model(
    findings: Sequence[CandidateFinding],
    *,
    entry_points: Sequence[EntryPoint] | None = None,
    attack_surface: Sequence[AttackSurfaceNode] | None = None,
    functions: Sequence[ScannedFunction] | None = None,
    call_graph: Mapping[str, Sequence[str]] | None = None,
    verification_results: dict[str, object] | None = None,
    top_n: int = 5,
) -> ThreatModelResult:
    stride_classifications = classify_all(findings)
    stride_groups = stride_breakdown(stride_classifications)
    dread_scores = score_all(
        findings,
        entry_points=entry_points,
        attack_surface=attack_surface,
        verification_results=verification_results,
    )
    ranked_findings = sorted(
        findings,
        key=lambda finding: (
            dread_scores[finding.id].normalized,
            dread_scores[finding.id].total,
            finding.id,
        ),
        reverse=True,
    )
    top_finding = ranked_findings[0] if ranked_findings else None
    top_attack_trees = _select_attack_tree_findings(ranked_findings, dread_scores, top_n=top_n)
    attack_surface_by_finding = _attack_surface_by_finding(findings, attack_surface or ())
    attack_trees = [
        ThreatTreeResult(
            finding_id=finding.id,
            cwe=extract_cwe_id(finding.vuln_class),
            title=cwe_title(extract_cwe_id(finding.vuln_class), fallback=finding.vuln_class),
            dread=dread_scores[finding.id].normalized,
            risk_level=dread_scores[finding.id].risk_level,
            tree=generate_attack_tree(
                finding,
                attack_surface=attack_surface_by_finding.get(finding.id),
            ),
        )
        for finding in top_attack_trees
    ]
    dfd = extract_dfd(
        findings=findings,
        entry_points=entry_points,
        attack_surface=attack_surface,
        call_graph=call_graph,
        functions=functions,
        taint_overlay=True,
    )
    summary = {
        "findings_analyzed": len(findings),
        "stride_breakdown": {
            stride_label(category): len(stride_groups[category]) for category in STRIDE_ORDER
        },
        "dread_critical": sum(
            1 for score in dread_scores.values() if score.risk_level == "critical"
        ),
        "dread_high": sum(1 for score in dread_scores.values() if score.risk_level == "high"),
        "top_threat": None
        if top_finding is None
        else {
            "finding_id": top_finding.id,
            "cwe": extract_cwe_id(top_finding.vuln_class),
            "title": cwe_title(
                extract_cwe_id(top_finding.vuln_class),
                fallback=top_finding.vuln_class,
            ),
            "dread": dread_scores[top_finding.id].normalized,
        },
    }
    dread_entries = [
        {
            "finding_id": finding.id,
            "cwe": extract_cwe_id(finding.vuln_class),
            "title": cwe_title(extract_cwe_id(finding.vuln_class), fallback=finding.vuln_class),
            "damage": dread_scores[finding.id].damage,
            "reproducibility": dread_scores[finding.id].reproducibility,
            "exploitability": dread_scores[finding.id].exploitability,
            "affected_users": dread_scores[finding.id].affected_users,
            "discoverability": dread_scores[finding.id].discoverability,
            "total": dread_scores[finding.id].total,
            "normalized": dread_scores[finding.id].normalized,
            "risk_level": dread_scores[finding.id].risk_level,
        }
        for finding in ranked_findings
    ]
    risk_matrix = _risk_matrix(findings, stride_classifications, dread_scores)
    return ThreatModelResult(
        summary=summary,
        stride={stride_label(category): stride_groups[category] for category in STRIDE_ORDER},
        dread=dread_entries,
        attack_trees=attack_trees,
        dfd=dfd,
        risk_matrix=risk_matrix,
    )


def generate_threat_model(
    findings: Sequence[CandidateFinding],
    *,
    entry_points: Sequence[EntryPoint] | None = None,
    attack_surface: Sequence[AttackSurfaceNode] | None = None,
    functions: Sequence[ScannedFunction] | None = None,
    call_graph: Mapping[str, Sequence[str]] | None = None,
    verification_results: dict[str, object] | None = None,
    top_n: int = 5,
    format: ThreatModelFormat = "markdown",
) -> str:
    model = build_threat_model(
        findings,
        entry_points=entry_points,
        attack_surface=attack_surface,
        functions=functions,
        call_graph=call_graph,
        verification_results=verification_results,
        top_n=top_n,
    )
    if format == "json":
        payload = asdict(model)
        payload["attack_trees"] = [
            {
                "finding_id": item.finding_id,
                "cwe": item.cwe,
                "title": item.title,
                "dread": item.dread,
                "risk_level": item.risk_level,
                "tree": asdict(item.tree),
            }
            for item in model.attack_trees
        ]
        payload["dfd_mermaid"] = render_dfd(model.dfd, format="mermaid")
        return json.dumps(payload, indent=2)
    return _render_markdown(model)


def _render_markdown(model: ThreatModelResult) -> str:
    lines = ["# Threat Model", "", "## Threat Model Summary", ""]
    stride_counts = cast(dict[str, int], model.summary["stride_breakdown"])
    lines.extend(
        [
            f"- Findings analyzed: {model.summary['findings_analyzed']}",
            "- STRIDE breakdown: "
            + "  ".join(
                f"{label[0]}:{count}"
                for label, count in ((label, int(count)) for label, count in stride_counts.items())
            ),
            f"- DREAD critical (>= 8.0): {model.summary['dread_critical']}",
            f"- DREAD high (>= 6.0): {model.summary['dread_high']}",
        ]
    )
    top_threat = model.summary.get("top_threat")
    if isinstance(top_threat, dict):
        lines.append(
            "- Top threat: "
            f"{top_threat['title']} ({top_threat['finding_id']}, DREAD {top_threat['dread']})"
        )

    lines.extend(
        [
            "",
            "## STRIDE Classification",
            "",
            "| Category | Count | Findings |",
            "| --- | ---: | --- |",
        ]
    )
    for label, finding_ids in model.stride.items():
        rendered_findings = ", ".join(finding_ids) if finding_ids else "—"
        lines.append(f"| {label} | {len(finding_ids)} | {rendered_findings} |")

    lines.extend(
        [
            "",
            "## DREAD Risk Priority",
            "",
            "| Rank | Finding | CWE | DREAD | Damage | Repro | Exploit | Users | Discover | Risk |",
            "| ---: | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |",
        ]
    )
    for index, item in enumerate(model.dread, start=1):
        lines.append(
            f"| {index} | {item['finding_id']} | {item['cwe']} | {item['normalized']:.1f} | "
            f"{item['damage']} | {item['reproducibility']} | {item['exploitability']} | "
            f"{item['affected_users']} | {item['discoverability']} | {item['risk_level']} |"
        )

    lines.extend(["", "## Top Attack Trees", ""])
    if not model.attack_trees:
        lines.append("_No attack trees generated._")
    for tree in model.attack_trees:
        lines.extend(
            [
                f"### {tree.title} ({tree.finding_id})",
                "",
                f"- CWE: {tree.cwe}",
                f"- DREAD: {tree.dread:.1f} ({tree.risk_level})",
                "",
                render_tree(tree.tree, format="markdown"),
                "",
            ]
        )

    lines.extend(
        [
            "## Data Flow Diagram",
            "",
            "```mermaid",
            render_dfd(model.dfd, format="mermaid"),
            "```",
            "",
            "## Risk Matrix",
            "",
            "| Category | critical | high | medium | low |",
            "| --- | ---: | ---: | ---: | ---: |",
        ]
    )
    for category_label, counts in model.risk_matrix.items():
        lines.append(
            f"| {category_label} | {counts['critical']} | {counts['high']} | "
            f"{counts['medium']} | {counts['low']} |"
        )
    return "\n".join(lines).strip() + "\n"


def _select_attack_tree_findings(
    ranked_findings: Sequence[CandidateFinding],
    dread_scores: Mapping[str, DreadScore],
    *,
    top_n: int,
) -> list[CandidateFinding]:
    selected: list[CandidateFinding] = []
    seen_ids: set[str] = set()
    for finding in ranked_findings:
        if dread_scores[finding.id].risk_level not in {"critical", "high"}:
            continue
        selected.append(finding)
        seen_ids.add(finding.id)
        if len(selected) == top_n:
            return selected
    for finding in ranked_findings:
        if finding.id in seen_ids:
            continue
        selected.append(finding)
        if len(selected) == top_n:
            break
    return selected


def _attack_surface_by_finding(
    findings: Sequence[CandidateFinding],
    attack_surface: Sequence[AttackSurfaceNode],
) -> dict[str, AttackSurfaceNode]:
    mapping: dict[str, AttackSurfaceNode] = {}
    for finding in findings:
        for node in attack_surface:
            if (
                node.location.file == finding.source.location.file
                and node.location.line == finding.source.location.line
                and node.source_type == finding.source.source_type
            ):
                mapping[finding.id] = node
                break
    return mapping


def _risk_matrix(
    findings: Sequence[CandidateFinding],
    stride_classifications: Mapping[str, StrideCategory],
    dread_scores: Mapping[str, DreadScore],
) -> dict[str, dict[str, int]]:
    matrix = {
        stride_label(category): {"critical": 0, "high": 0, "medium": 0, "low": 0}
        for category in STRIDE_ORDER
    }
    for finding in findings:
        classification = stride_classifications.get(finding.id)
        score = dread_scores.get(finding.id)
        if classification is None or score is None:
            continue
        for category in stride_members(classification):
            matrix[stride_label(category)][score.risk_level] += 1
    return matrix


__all__ = [
    "ThreatModelResult",
    "ThreatTreeResult",
    "build_threat_model",
    "generate_threat_model",
]
