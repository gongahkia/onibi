from __future__ import annotations

import csv
import io
from pathlib import Path
from typing import TYPE_CHECKING

from piranesi.report.cwe import cwe_title

if TYPE_CHECKING:
    from piranesi.report.renderer import CombinedFinding, PiranesiReport, SuppressedFinding


_COLUMNS = (
    "id",
    "cwe_id",
    "cwe_name",
    "severity",
    "source_file",
    "source_line",
    "sink_file",
    "sink_line",
    "taint_source",
    "taint_sink",
    "exploit_payload",
    "regulatory_frameworks",
    "suppressed",
    "suppression_reason",
)


def generate_csv(report: PiranesiReport) -> str:
    target_root = Path(report.target).resolve(strict=False)
    buffer = io.StringIO(newline="")
    writer = csv.DictWriter(buffer, fieldnames=list(_COLUMNS))
    writer.writeheader()
    for finding in report.findings:
        writer.writerow(_row_for_finding(finding, target_root=target_root))
    for suppressed in report.suppressed_findings:
        writer.writerow(_row_for_suppressed(suppressed, target_root=target_root))
    return buffer.getvalue()


def _row_for_finding(finding: CombinedFinding, *, target_root: Path) -> dict[str, object]:
    frameworks = list(
        dict.fromkeys(obligation.framework for obligation in finding.regulatory_obligations)
    )
    return {
        "id": finding.finding_id,
        "cwe_id": finding.cwe,
        "cwe_name": cwe_title(finding.cwe, fallback=finding.title),
        "severity": finding.severity.upper(),
        "source_file": _display_path(finding.source_location.file, target_root=target_root),
        "source_line": finding.source_location.line,
        "sink_file": _display_path(finding.sink_location.file, target_root=target_root),
        "sink_line": finding.sink_location.line,
        "taint_source": finding.taint_source,
        "taint_sink": finding.taint_sink,
        "exploit_payload": finding.exploit_payload or "",
        "regulatory_frameworks": "|".join(frameworks),
        "suppressed": "false",
        "suppression_reason": "",
    }


def _row_for_suppressed(
    finding: SuppressedFinding,
    *,
    target_root: Path,
) -> dict[str, object]:
    return {
        "id": finding.finding_id,
        "cwe_id": finding.cwe,
        "cwe_name": cwe_title(finding.cwe, fallback=finding.title),
        "severity": finding.severity.upper(),
        "source_file": _display_path(finding.source_location.file, target_root=target_root),
        "source_line": finding.source_location.line,
        "sink_file": _display_path(finding.sink_location.file, target_root=target_root),
        "sink_line": finding.sink_location.line,
        "taint_source": finding.taint_source,
        "taint_sink": finding.taint_sink,
        "exploit_payload": "",
        "regulatory_frameworks": "",
        "suppressed": "true",
        "suppression_reason": finding.suppression_reason or "",
    }


def _display_path(path_str: str, *, target_root: Path) -> str:
    path = Path(path_str)
    if not path.is_absolute():
        return path.as_posix()
    try:
        return path.resolve(strict=False).relative_to(target_root).as_posix()
    except ValueError:
        return path.resolve(strict=False).as_posix()


__all__ = ["generate_csv"]
