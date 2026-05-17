from __future__ import annotations

import io
import xml.etree.ElementTree as ET
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from piranesi.models import TaintStep
    from piranesi.report.renderer import CombinedFinding, PiranesiReport, SuppressedFinding


def generate_junit_xml(report: PiranesiReport) -> str:
    target_root = Path(report.target).resolve(strict=False)
    testsuite = ET.Element(
        "testsuite",
        attrib={
            "name": "piranesi",
            "tests": str(len(report.findings) + len(report.suppressed_findings)),
            "failures": str(len(report.findings)),
            "errors": "0",
            "skipped": str(len(report.suppressed_findings)),
            "time": f"{report.executive_summary.duration_s:.3f}",
            "timestamp": report.generated_at,
        },
    )

    for finding in report.findings:
        testsuite.append(_testcase_for_finding(finding, target_root=target_root))
    for suppressed in report.suppressed_findings:
        testsuite.append(_testcase_for_suppressed(suppressed, target_root=target_root))

    tree = ET.ElementTree(testsuite)
    ET.indent(tree, space="  ")
    buffer = io.BytesIO()
    tree.write(buffer, encoding="utf-8", xml_declaration=True)
    return buffer.getvalue().decode("utf-8")


def _testcase_for_finding(finding: CombinedFinding, *, target_root: Path) -> ET.Element:
    testcase_name = _display_location(
        finding.source_location.file,
        finding.source_location.line,
        target_root=target_root,
    )
    testcase = ET.Element(
        "testcase",
        attrib={
            "classname": finding.cwe,
            "name": f"{finding.title} in {testcase_name}",
            "time": "0",
        },
    )
    failure = ET.SubElement(
        testcase,
        "failure",
        attrib={
            "message": _failure_summary(finding),
            "type": finding.cwe,
        },
    )
    failure.text = _failure_details(finding)
    return testcase


def _testcase_for_suppressed(
    finding: SuppressedFinding,
    *,
    target_root: Path,
) -> ET.Element:
    testcase_name = _display_location(
        finding.source_location.file,
        finding.source_location.line,
        target_root=target_root,
    )
    testcase = ET.Element(
        "testcase",
        attrib={
            "classname": finding.cwe,
            "name": f"{finding.title} suppressed in {testcase_name}",
            "time": "0",
        },
    )
    ET.SubElement(
        testcase,
        "skipped",
        attrib={
            "message": f"Suppressed: {finding.suppression_reason or 'n/a'}",
        },
    )
    return testcase


def _failure_summary(finding: CombinedFinding) -> str:
    return (
        f"{finding.title}: {finding.taint_source} reaches {finding.taint_sink} "
        "without safe handling"
    )


def _failure_details(finding: CombinedFinding) -> str:
    return "\n".join(
        [
            f"Taint path: {_taint_path_text(finding)}",
            f"Severity: {finding.severity.upper()}",
            f"Exploit: {finding.exploit_payload or 'n/a'}",
        ]
    )


def _taint_path_text(finding: CombinedFinding) -> str:
    parts = [finding.taint_source]
    parts.extend(_step_label(step) for step in finding.taint_path)
    parts.append(finding.taint_sink)
    return " -> ".join(part for part in parts if part)


def _step_label(step: TaintStep) -> str:
    if step.through_function:
        return f"{step.through_function}()"
    snippet = " ".join(step.location.snippet.split())
    if snippet:
        return snippet
    return f"{step.operation}:{step.taint_state}"


def _display_location(path_str: str, line: int, *, target_root: Path) -> str:
    return f"{_display_path(path_str, target_root=target_root)}:{line}"


def _display_path(path_str: str, *, target_root: Path) -> str:
    path = Path(path_str)
    if not path.is_absolute():
        return path.as_posix()
    try:
        return path.resolve(strict=False).relative_to(target_root).as_posix()
    except ValueError:
        return path.resolve(strict=False).as_posix()


__all__ = ["generate_junit_xml"]
