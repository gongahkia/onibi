from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from dataclasses import asdict, dataclass
from datetime import date
from pathlib import Path
from typing import Any

import yaml

try:
    from eval.ground_truth.schema import GroundTruthEntry, Label
except ImportError:  # pragma: no cover - supports `python eval/scoring.py`
    from ground_truth.schema import (  # type: ignore[import-not-found,no-redef]
        GroundTruthEntry,
        Label,
    )

_CWE_PATTERN = re.compile(r"cwe[-_ ]?(\d+)", re.IGNORECASE)
_WHITESPACE_PATTERN = re.compile(r"\s+")
_FLOAT_TOLERANCE = 1e-9
_NEARBY_LINE_DISTANCE = 2
_CWE_ALIAS_MAP = {
    "CWE-77": "CWE-78",
    "CWE-943": "CWE-89",
}
_DEFAULT_COMPARISON_CAPABILITIES: dict[str, dict[str, bool]] = {
    "piranesi": {
        "regulatory_mapping": True,
        "exploit_generation": True,
    },
    "opengrep": {
        "regulatory_mapping": False,
        "exploit_generation": False,
    },
    "semgrep": {
        "regulatory_mapping": False,
        "exploit_generation": False,
    },
    "llm_only": {
        "regulatory_mapping": False,
        "exploit_generation": False,
    },
}


@dataclass(frozen=True, slots=True)
class NormalizedFinding:
    id: str | None
    cwe_id: str
    affected_files: tuple[str, ...]
    taint_source: str
    taint_sink: str
    line_numbers: tuple[int, ...] = ()
    description: str = ""
    severity: str | None = None
    rule_id: str | None = None
    tool: str | None = None


@dataclass(frozen=True, slots=True)
class ScanObservation:
    file: str
    value: str


@dataclass(frozen=True, slots=True)
class MatchAssignment:
    finding_id: str | None
    ground_truth_id: str
    weight: float
    match_type: str


@dataclass(frozen=True, slots=True)
class MatchSummary:
    predictions: int
    ground_truth: int
    tp_weight: float
    fp_weight: float
    fn_weight: float
    exact_matches: int
    partial_matches: int
    assignments: tuple[MatchAssignment, ...]


@dataclass(frozen=True, slots=True)
class StageScore:
    available: bool
    precision: float | None = None
    recall: float | None = None
    f1: float | None = None
    tp_weight: float = 0.0
    fp_weight: float = 0.0
    fn_weight: float = 0.0
    predictions: int = 0
    ground_truth: int = 0
    exact_matches: int = 0
    partial_matches: int = 0


@dataclass(frozen=True, slots=True)
class ScanScore:
    available: bool
    source_hits: int = 0
    source_total: int = 0
    source_recall: float | None = None
    sink_hits: int = 0
    sink_total: int = 0
    sink_recall: float | None = None


@dataclass(frozen=True, slots=True)
class TriageScore:
    available: bool
    input_count: int = 0
    surviving_count: int = 0
    fp_candidates: int = 0
    fp_filtered: int = 0
    fp_leaked: int = 0
    fp_filter_rate: float | None = None
    tp_detect_weight: float = 0.0
    tp_retained_weight: float = 0.0
    tp_incorrectly_filtered_weight: float = 0.0
    tp_retention_rate: float | None = None


@dataclass(frozen=True, slots=True)
class VerifyScore:
    available: bool
    post_triage_count: int = 0
    confirmed_count: int = 0
    confirmation_rate: float | None = None


@dataclass(frozen=True, slots=True)
class PipelineResults:
    scan_sources: tuple[ScanObservation, ...]
    scan_sinks: tuple[ScanObservation, ...]
    detect_findings: tuple[NormalizedFinding, ...]
    triage_findings: tuple[NormalizedFinding, ...]
    verify_findings: tuple[NormalizedFinding, ...]
    final_findings: tuple[NormalizedFinding, ...]
    metadata: dict[str, Any]
    total_cost_usd: float | None


@dataclass(frozen=True, slots=True)
class EvaluationReport:
    date: str
    commit: str
    ground_truth_total: int
    ground_truth_true_positive: int
    ground_truth_false_positive: int
    pipeline_findings: int
    overall: StageScore
    per_cwe: dict[str, dict[str, Any]]
    false_positive_handling: dict[str, Any]
    scan: ScanScore
    detect: StageScore
    triage: TriageScore
    verify: VerifyScore
    total_cost_usd: float | None
    cost_per_finding_usd: float | None
    cost_per_true_positive_usd: float | None


def normalize_text(value: str | None) -> str:
    if value is None:
        return ""
    return _WHITESPACE_PATTERN.sub("", value).casefold()


def normalize_file_path(value: str | None) -> str:
    if value is None:
        return ""
    normalized = value.replace("\\", "/").strip()
    while normalized.startswith("./"):
        normalized = normalized[2:]
    return normalized.casefold()


def normalize_cwe_id(value: str | None) -> str:
    if value is None:
        return "UNKNOWN"
    match = _CWE_PATTERN.search(value)
    if match:
        canonical = f"CWE-{int(match.group(1))}"
        return _CWE_ALIAS_MAP.get(canonical, canonical)
    stripped = value.strip().upper()
    if not stripped:
        return "UNKNOWN"
    return _CWE_ALIAS_MAP.get(stripped, stripped)


def _format_count(value: float | int) -> str:
    numeric = float(value)
    if abs(numeric - round(numeric)) < _FLOAT_TOLERANCE:
        return str(round(numeric))
    return f"{numeric:.1f}"


def _format_metric(value: float | None, *, digits: int = 3) -> str:
    if value is None:
        return "n/a"
    return f"{value:.{digits}f}"


def _format_percent(value: float | None) -> str:
    if value is None:
        return "n/a"
    return f"{value * 100:.0f}%"


def _safe_divide(numerator: float, denominator: float) -> float | None:
    if abs(denominator) < _FLOAT_TOLERANCE:
        return None
    return numerator / denominator


def _compute_prf(
    tp_weight: float,
    fp_weight: float,
    fn_weight: float,
) -> tuple[float | None, float | None, float | None]:
    precision = _safe_divide(tp_weight, tp_weight + fp_weight)
    recall = _safe_divide(tp_weight, tp_weight + fn_weight)
    if precision is None or recall is None:
        f1 = None
    elif abs(precision + recall) < _FLOAT_TOLERANCE:
        f1 = 0.0
    else:
        f1 = 2 * precision * recall / (precision + recall)
    return precision, recall, f1


def _cwe_sort_key(cwe_id: str) -> tuple[int, str]:
    match = _CWE_PATTERN.search(cwe_id)
    if match:
        return int(match.group(1)), cwe_id
    return sys.maxsize, cwe_id


def load_ground_truth_entries(ground_truth_dir: Path) -> list[GroundTruthEntry]:
    seen_ids: set[str] = set()
    entries: list[GroundTruthEntry] = []
    for path in sorted(ground_truth_dir.glob("*.yaml")):
        with path.open("r", encoding="utf-8") as handle:
            payload = yaml.safe_load(handle)
        entry = GroundTruthEntry.model_validate(payload)
        if entry.id in seen_ids:
            raise ValueError(f"duplicate ground truth id: {entry.id}")
        seen_ids.add(entry.id)
        entries.append(entry)
    return entries


def _coerce_mapping(value: Any) -> dict[str, Any]:
    if isinstance(value, dict):
        return value
    return {}


def _coerce_list(value: Any) -> list[Any]:
    if isinstance(value, list):
        return value
    return []


def _extract_file_from_mapping(payload: dict[str, Any]) -> str | None:
    location = _coerce_mapping(payload.get("location"))
    file_value = payload.get("file") or location.get("file")
    if isinstance(file_value, str):
        return file_value
    return None


def _extract_source_text(payload: dict[str, Any]) -> str:
    if isinstance(payload.get("taint_source"), str):
        return str(payload["taint_source"])
    source = payload.get("source")
    if isinstance(source, str):
        return source
    source_mapping = _coerce_mapping(source)
    for key in ("normalized", "source_type", "parameter_name"):
        value = source_mapping.get(key)
        if isinstance(value, str) and value.strip():
            return value
    location = _coerce_mapping(source_mapping.get("location"))
    snippet = location.get("snippet")
    return snippet if isinstance(snippet, str) else ""


def _extract_sink_text(payload: dict[str, Any]) -> str:
    if isinstance(payload.get("taint_sink"), str):
        return str(payload["taint_sink"])
    sink = payload.get("sink")
    if isinstance(sink, str):
        return sink
    sink_mapping = _coerce_mapping(sink)
    for key in ("normalized", "api_name", "sink_type"):
        value = sink_mapping.get(key)
        if isinstance(value, str) and value.strip():
            return value
    location = _coerce_mapping(sink_mapping.get("location"))
    snippet = location.get("snippet")
    return snippet if isinstance(snippet, str) else ""


def _coerce_line_number(value: Any) -> int | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, int):
        return value if value > 0 else None
    if isinstance(value, float):
        integer_value = int(value)
        return integer_value if integer_value > 0 else None
    if isinstance(value, str):
        stripped = value.strip()
        if not stripped.isdigit():
            return None
        parsed = int(stripped)
        return parsed if parsed > 0 else None
    return None


def _extract_line_numbers(payload: dict[str, Any]) -> tuple[int, ...]:
    lines: set[int] = set()
    explicit_lines = _coerce_list(payload.get("line_numbers"))
    for item in explicit_lines:
        line = _coerce_line_number(item)
        if line is not None:
            lines.add(line)

    single_line = _coerce_line_number(payload.get("line_number"))
    if single_line is not None:
        lines.add(single_line)

    location = _coerce_mapping(payload.get("location"))
    start = _coerce_mapping(payload.get("start"))
    end = _coerce_mapping(payload.get("end"))
    for candidate in (
        location.get("line"),
        start.get("line"),
        end.get("line"),
    ):
        line = _coerce_line_number(candidate)
        if line is not None:
            lines.add(line)

    source = _coerce_mapping(payload.get("source"))
    sink = _coerce_mapping(payload.get("sink"))
    for mapping in (source, sink):
        nested_location = _coerce_mapping(mapping.get("location"))
        line = _coerce_line_number(nested_location.get("line"))
        if line is not None:
            lines.add(line)

    return tuple(sorted(lines))


def _extract_description(payload: dict[str, Any]) -> str:
    for key in ("description", "message"):
        value = payload.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return ""


def _extract_affected_files(payload: dict[str, Any]) -> tuple[str, ...]:
    files: set[str] = set()
    for raw_path in _coerce_list(payload.get("affected_files")):
        if isinstance(raw_path, str):
            files.add(normalize_file_path(raw_path))

    source_mapping = _coerce_mapping(payload.get("source"))
    sink_mapping = _coerce_mapping(payload.get("sink"))
    for mapping in (source_mapping, sink_mapping):
        location = _coerce_mapping(mapping.get("location"))
        if isinstance(location.get("file"), str):
            files.add(normalize_file_path(location["file"]))

    for step in _coerce_list(payload.get("taint_path")):
        step_mapping = _coerce_mapping(step)
        location = _coerce_mapping(step_mapping.get("location"))
        file_value = location.get("file")
        if isinstance(file_value, str):
            files.add(normalize_file_path(file_value))

    return tuple(sorted(file_path for file_path in files if file_path))


def _unwrap_finding(payload: Any) -> dict[str, Any]:
    current = _coerce_mapping(payload)
    if "confirmed" in current and isinstance(current["confirmed"], dict):
        current = _coerce_mapping(current["confirmed"])
    while "finding" in current and isinstance(current["finding"], dict):
        current = _coerce_mapping(current["finding"])
    return current


def _is_finding_like(payload: Any) -> bool:
    finding = _unwrap_finding(payload)
    finding_keys = (
        "source",
        "sink",
        "affected_files",
        "taint_source",
        "taint_sink",
        "cwe_id",
        "vuln_class",
    )
    return any(key in finding for key in finding_keys)


def normalize_finding(payload: Any) -> NormalizedFinding | None:
    finding = _unwrap_finding(payload)
    if not _is_finding_like(finding):
        return None
    cwe_id = normalize_cwe_id(
        str(finding.get("cwe_id") or finding.get("vuln_class") or finding.get("rule_id") or "")
    )
    affected_files = _extract_affected_files(finding)
    taint_source = _extract_source_text(finding)
    taint_sink = _extract_sink_text(finding)
    finding_id = finding.get("id")
    return NormalizedFinding(
        id=str(finding_id) if finding_id is not None else None,
        cwe_id=cwe_id,
        affected_files=affected_files,
        taint_source=taint_source,
        taint_sink=taint_sink,
        line_numbers=_extract_line_numbers(finding),
        description=_extract_description(finding),
        severity=finding.get("severity") if isinstance(finding.get("severity"), str) else None,
        rule_id=finding.get("rule_id") if isinstance(finding.get("rule_id"), str) else None,
        tool=finding.get("tool") if isinstance(finding.get("tool"), str) else None,
    )


def _normalize_findings(items: list[Any]) -> tuple[NormalizedFinding, ...]:
    findings: list[NormalizedFinding] = []
    for item in items:
        finding = normalize_finding(item)
        if finding is not None:
            findings.append(finding)
    return tuple(findings)


def _extract_list_container(payload: Any) -> list[Any] | None:
    if isinstance(payload, list):
        return payload
    mapping = _coerce_mapping(payload)
    for key in ("findings", "items", "results", "entries", "candidates"):
        nested = mapping.get(key)
        if isinstance(nested, list):
            return nested
    if _is_finding_like(mapping):
        return [mapping]
    return None


def _extract_findings_from_keys(
    payload: dict[str, Any],
    *keys: str,
) -> tuple[NormalizedFinding, ...]:
    for key in keys:
        value = payload.get(key)
        container = _extract_list_container(value)
        if container is not None:
            return _normalize_findings(container)
    return ()


def _triage_survives(payload: Any) -> bool:
    mapping = _coerce_mapping(payload)
    verdict = mapping.get("triage_verdict") or mapping.get("verdict")
    if isinstance(verdict, str):
        rejected = {"rejected", "false_positive", "false positive", "filtered"}
        return verdict.casefold() not in rejected
    return True


def _verify_confirmed(payload: Any) -> bool:
    mapping = _coerce_mapping(payload)
    if isinstance(mapping.get("confirmed"), bool):
        return bool(mapping["confirmed"])
    sandbox_result = _coerce_mapping(mapping.get("sandbox_result"))
    if isinstance(sandbox_result.get("confirmed"), bool):
        return bool(sandbox_result["confirmed"])
    if "confirmed" in mapping:
        return True
    return True


def _extract_observation_value(payload: dict[str, Any], *, kind: str) -> str:
    for key in ("value", kind, f"taint_{kind}", f"{kind}_type", "api_name"):
        value = payload.get(key)
        if isinstance(value, str) and value.strip():
            return value
    location = _coerce_mapping(payload.get("location"))
    snippet = location.get("snippet")
    return snippet if isinstance(snippet, str) else ""


def _parse_scan_observations(items: list[Any], *, kind: str) -> tuple[ScanObservation, ...]:
    observations: list[ScanObservation] = []
    for item in items:
        mapping = _coerce_mapping(item)
        file_value = _extract_file_from_mapping(mapping)
        if file_value is None:
            continue
        observations.append(
            ScanObservation(
                file=normalize_file_path(file_value),
                value=_extract_observation_value(mapping, kind=kind),
            )
        )
    return tuple(observations)


def _extract_total_cost(payload: dict[str, Any]) -> float | None:
    candidates: list[Any] = [
        payload.get("total_cost_usd"),
        _coerce_mapping(payload.get("cost")).get("total_usd"),
        _coerce_mapping(payload.get("cost")).get("total_cost_usd"),
        _coerce_mapping(payload.get("trace_summary")).get("total_cost_usd"),
        _coerce_mapping(payload.get("summary_stats")).get("total_cost_usd"),
    ]
    for candidate in candidates:
        if isinstance(candidate, int | float):
            return float(candidate)
    return None


def load_pipeline_results(pipeline_output_path: Path) -> PipelineResults:
    with pipeline_output_path.open("r", encoding="utf-8") as handle:
        payload = json.load(handle)

    if isinstance(payload, list):
        findings = _normalize_findings(payload)
        return PipelineResults(
            scan_sources=(),
            scan_sinks=(),
            detect_findings=(),
            triage_findings=(),
            verify_findings=(),
            final_findings=findings,
            metadata={},
            total_cost_usd=None,
        )

    if not isinstance(payload, dict):
        raise ValueError("pipeline output must be a JSON object or list")

    scan_payload = _coerce_mapping(payload.get("scan"))
    scan_sources_raw = (
        _extract_list_container(payload.get("scan_sources"))
        or _extract_list_container(scan_payload.get("scan_sources"))
        or _extract_list_container(scan_payload.get("sources"))
        or _extract_list_container(scan_payload.get("identified_sources"))
        or _extract_list_container(scan_payload.get("attack_surface"))
        or []
    )
    scan_sinks_raw = (
        _extract_list_container(payload.get("scan_sinks"))
        or _extract_list_container(scan_payload.get("scan_sinks"))
        or _extract_list_container(scan_payload.get("sinks"))
        or _extract_list_container(scan_payload.get("identified_sinks"))
        or []
    )

    detect_findings = _extract_findings_from_keys(
        payload,
        "detect",
        "detect_findings",
        "candidate_findings",
    )

    triage_raw = (
        _extract_list_container(payload.get("triage"))
        or _extract_list_container(payload.get("triaged_findings"))
        or []
    )
    triage_findings = _normalize_findings([item for item in triage_raw if _triage_survives(item)])

    verify_raw = (
        _extract_list_container(payload.get("verify"))
        or _extract_list_container(payload.get("confirmed_findings"))
        or []
    )
    verify_findings = _normalize_findings([item for item in verify_raw if _verify_confirmed(item)])

    final_findings: tuple[NormalizedFinding, ...] = ()
    report_payload = _coerce_mapping(payload.get("report")) or _coerce_mapping(
        payload.get("final_report")
    )
    report_findings_raw = _extract_list_container(report_payload.get("findings")) or []
    if report_findings_raw:
        final_findings = _normalize_findings(report_findings_raw)
    elif isinstance(payload.get("findings"), list):
        final_findings = _normalize_findings(_coerce_list(payload.get("findings")))
    elif verify_findings:
        final_findings = verify_findings
    elif triage_findings:
        final_findings = triage_findings
    else:
        final_findings = detect_findings

    metadata = {
        "commit": payload.get("commit") or _coerce_mapping(payload.get("metadata")).get("commit"),
        "pipeline_findings": payload.get("pipeline_findings"),
        "source_path": str(pipeline_output_path),
    }

    return PipelineResults(
        scan_sources=_parse_scan_observations(scan_sources_raw, kind="source"),
        scan_sinks=_parse_scan_observations(scan_sinks_raw, kind="sink"),
        detect_findings=detect_findings,
        triage_findings=triage_findings,
        verify_findings=verify_findings,
        final_findings=final_findings,
        metadata=metadata,
        total_cost_usd=_extract_total_cost(payload),
    )


def match_weight(finding: NormalizedFinding, entry: GroundTruthEntry) -> float:
    finding_files = set(finding.affected_files)
    entry_files = {normalize_file_path(file_path) for file_path in entry.affected_files}
    if not finding_files.intersection(entry_files):
        return 0.0
    if finding.cwe_id != normalize_cwe_id(entry.cwe_id):
        return 0.0

    finding_source = normalize_text(finding.taint_source)
    entry_source = normalize_text(entry.taint_source)
    finding_sink = normalize_text(finding.taint_sink)
    entry_sink = normalize_text(entry.taint_sink)

    if finding_source and entry_source and finding_source == entry_source:
        if finding_sink and finding_sink == entry_sink:
            return 1.0
        return 0.5

    if finding_sink and finding_sink == entry_sink:
        line_weight = _line_match_weight(finding.line_numbers, tuple(entry.line_numbers))
        if line_weight > 0:
            return line_weight
        return 0.5

    return _line_match_weight(finding.line_numbers, tuple(entry.line_numbers))


def _line_match_weight(
    finding_lines: tuple[int, ...],
    entry_lines: tuple[int, ...],
) -> float:
    if not finding_lines or not entry_lines:
        return 0.0
    finding_set = set(finding_lines)
    entry_set = set(entry_lines)
    if finding_set.intersection(entry_set):
        return 1.0
    nearest = min(
        abs(finding_line - entry_line) for finding_line in finding_set for entry_line in entry_set
    )
    if nearest <= _NEARBY_LINE_DISTANCE:
        return 0.5
    return 0.0


def _match_type_for_weight(weight: float) -> str:
    if abs(weight - 1.0) < _FLOAT_TOLERANCE:
        return "exact"
    if abs(weight - 0.5) < _FLOAT_TOLERANCE:
        return "partial"
    return "none"


def summarize_matches(
    predictions: list[NormalizedFinding] | tuple[NormalizedFinding, ...],
    ground_truth_entries: list[GroundTruthEntry] | tuple[GroundTruthEntry, ...],
) -> MatchSummary:
    prediction_list = list(predictions)
    entry_list = list(ground_truth_entries)
    if not prediction_list or not entry_list:
        return MatchSummary(
            predictions=len(prediction_list),
            ground_truth=len(entry_list),
            tp_weight=0.0,
            fp_weight=float(len(prediction_list)),
            fn_weight=float(len(entry_list)),
            exact_matches=0,
            partial_matches=0,
            assignments=(),
        )

    class _Edge:
        __slots__ = ("capacity", "cost", "reverse_index", "to")

        def __init__(self, to: int, reverse_index: int, capacity: int, cost: int) -> None:
            self.to = to
            self.reverse_index = reverse_index
            self.capacity = capacity
            self.cost = cost

    graph: list[list[_Edge]] = [[] for _ in range(2 + len(prediction_list) + len(entry_list))]
    source = 0
    sink = len(graph) - 1

    def add_edge(start: int, end: int, capacity: int, cost: int) -> None:
        graph[start].append(_Edge(end, len(graph[end]), capacity, cost))
        graph[end].append(_Edge(start, len(graph[start]) - 1, 0, -cost))

    for prediction_index in range(len(prediction_list)):
        add_edge(source, 1 + prediction_index, 1, 0)
    for entry_index in range(len(entry_list)):
        add_edge(1 + len(prediction_list) + entry_index, sink, 1, 0)

    for prediction_index, prediction in enumerate(prediction_list):
        for entry_index, entry in enumerate(entry_list):
            weight = match_weight(prediction, entry)
            if weight > 0:
                add_edge(
                    1 + prediction_index,
                    1 + len(prediction_list) + entry_index,
                    1,
                    -int(weight * 2),
                )

    total_cost = 0
    while True:
        distances = [sys.maxsize] * len(graph)
        previous_node = [-1] * len(graph)
        previous_edge = [-1] * len(graph)
        distances[source] = 0

        for _ in range(len(graph) - 1):
            updated = False
            for node, edges in enumerate(graph):
                if distances[node] == sys.maxsize:
                    continue
                for edge_index, edge in enumerate(edges):
                    if edge.capacity <= 0:
                        continue
                    candidate = distances[node] + edge.cost
                    if candidate < distances[edge.to]:
                        distances[edge.to] = candidate
                        previous_node[edge.to] = node
                        previous_edge[edge.to] = edge_index
                        updated = True
            if not updated:
                break

        if distances[sink] == sys.maxsize or distances[sink] >= 0:
            break

        node = sink
        while node != source:
            prev_node = previous_node[node]
            prev_edge_index = previous_edge[node]
            edge = graph[prev_node][prev_edge_index]
            edge.capacity -= 1
            reverse = graph[node][edge.reverse_index]
            reverse.capacity += 1
            node = prev_node
        total_cost += distances[sink]

    assignments: list[MatchAssignment] = []
    exact_matches = 0
    partial_matches = 0
    for prediction_index, prediction in enumerate(prediction_list):
        node_index = 1 + prediction_index
        for edge in graph[node_index]:
            entry_offset = edge.to - (1 + len(prediction_list))
            if not (0 <= entry_offset < len(entry_list)):
                continue
            if edge.cost >= 0 or edge.capacity != 0:
                continue
            weight = -edge.cost / 2.0
            match_type = _match_type_for_weight(weight)
            if match_type == "exact":
                exact_matches += 1
            elif match_type == "partial":
                partial_matches += 1
            assignments.append(
                MatchAssignment(
                    finding_id=prediction.id,
                    ground_truth_id=entry_list[entry_offset].id,
                    weight=weight,
                    match_type=match_type,
                )
            )

    tp_weight = -total_cost / 2.0
    fp_weight = float(len(prediction_list)) - tp_weight
    fn_weight = float(len(entry_list)) - tp_weight
    return MatchSummary(
        predictions=len(prediction_list),
        ground_truth=len(entry_list),
        tp_weight=tp_weight,
        fp_weight=fp_weight,
        fn_weight=fn_weight,
        exact_matches=exact_matches,
        partial_matches=partial_matches,
        assignments=tuple(sorted(assignments, key=lambda assignment: assignment.ground_truth_id)),
    )


def _build_stage_score(summary: MatchSummary) -> StageScore:
    precision, recall, f1 = _compute_prf(summary.tp_weight, summary.fp_weight, summary.fn_weight)
    return StageScore(
        available=True,
        precision=precision,
        recall=recall,
        f1=f1,
        tp_weight=summary.tp_weight,
        fp_weight=summary.fp_weight,
        fn_weight=summary.fn_weight,
        predictions=summary.predictions,
        ground_truth=summary.ground_truth,
        exact_matches=summary.exact_matches,
        partial_matches=summary.partial_matches,
    )


def _count_scan_hits(
    observations: tuple[ScanObservation, ...],
    entries: list[GroundTruthEntry],
    *,
    kind: str,
) -> int:
    hits = 0
    for entry in entries:
        target = entry.taint_source if kind == "source" else entry.taint_sink
        normalized_target = normalize_text(target)
        affected_files = {normalize_file_path(file_path) for file_path in entry.affected_files}
        matched = any(
            normalize_text(observation.value) == normalized_target
            and observation.file in affected_files
            for observation in observations
        )
        if matched:
            hits += 1
    return hits


def _git_commit_fallback() -> str:
    repo_root = Path(__file__).resolve().parents[1]
    try:
        result = subprocess.run(
            ["git", "rev-parse", "--short", "HEAD"],
            cwd=repo_root,
            check=True,
            capture_output=True,
            text=True,
        )
    except (FileNotFoundError, subprocess.CalledProcessError):
        return "unknown"
    return result.stdout.strip() or "unknown"


def build_report(
    pipeline_results: PipelineResults,
    ground_truth_entries: list[GroundTruthEntry],
) -> EvaluationReport:
    true_positive_entries = [
        entry for entry in ground_truth_entries if entry.label == Label.TRUE_POSITIVE
    ]
    false_positive_entries = [
        entry for entry in ground_truth_entries if entry.label == Label.FALSE_POSITIVE
    ]

    overall_summary = summarize_matches(pipeline_results.final_findings, true_positive_entries)
    overall = _build_stage_score(overall_summary)

    if pipeline_results.scan_sources or pipeline_results.scan_sinks:
        source_hits = _count_scan_hits(
            pipeline_results.scan_sources,
            ground_truth_entries,
            kind="source",
        )
        sink_hits = _count_scan_hits(pipeline_results.scan_sinks, ground_truth_entries, kind="sink")
        scan = ScanScore(
            available=True,
            source_hits=source_hits,
            source_total=len(ground_truth_entries),
            source_recall=_safe_divide(source_hits, len(ground_truth_entries)),
            sink_hits=sink_hits,
            sink_total=len(ground_truth_entries),
            sink_recall=_safe_divide(sink_hits, len(ground_truth_entries)),
        )
    else:
        scan = ScanScore(available=False)

    if pipeline_results.detect_findings:
        detect = _build_stage_score(
            summarize_matches(pipeline_results.detect_findings, true_positive_entries),
        )
    else:
        detect = StageScore(available=False)

    if pipeline_results.detect_findings and pipeline_results.triage_findings:
        detect_tp = summarize_matches(pipeline_results.detect_findings, true_positive_entries)
        triage_tp = summarize_matches(pipeline_results.triage_findings, true_positive_entries)
        detect_fp = summarize_matches(pipeline_results.detect_findings, false_positive_entries)
        triage_fp = summarize_matches(pipeline_results.triage_findings, false_positive_entries)

        detected_fp_ids = {assignment.ground_truth_id for assignment in detect_fp.assignments}
        surviving_fp_ids = {assignment.ground_truth_id for assignment in triage_fp.assignments}
        fp_filtered = len(detected_fp_ids - surviving_fp_ids)
        fp_leaked = len(surviving_fp_ids)
        tp_incorrectly_filtered_weight = max(detect_tp.tp_weight - triage_tp.tp_weight, 0.0)
        triage = TriageScore(
            available=True,
            input_count=len(pipeline_results.detect_findings),
            surviving_count=len(pipeline_results.triage_findings),
            fp_candidates=len(detected_fp_ids),
            fp_filtered=fp_filtered,
            fp_leaked=fp_leaked,
            fp_filter_rate=_safe_divide(fp_filtered, len(detected_fp_ids)),
            tp_detect_weight=detect_tp.tp_weight,
            tp_retained_weight=triage_tp.tp_weight,
            tp_incorrectly_filtered_weight=tp_incorrectly_filtered_weight,
            tp_retention_rate=_safe_divide(triage_tp.tp_weight, detect_tp.tp_weight),
        )
    else:
        triage = TriageScore(available=False)

    if pipeline_results.triage_findings or pipeline_results.verify_findings:
        post_triage_count = len(pipeline_results.triage_findings)
        confirmed_count = len(pipeline_results.verify_findings or pipeline_results.final_findings)
        verify = VerifyScore(
            available=True,
            post_triage_count=post_triage_count,
            confirmed_count=confirmed_count,
            confirmation_rate=_safe_divide(confirmed_count, post_triage_count),
        )
    else:
        verify = VerifyScore(available=False)

    per_cwe: dict[str, dict[str, Any]] = {}
    cwe_names: dict[str, str] = {}
    for entry in true_positive_entries:
        cwe_names.setdefault(normalize_cwe_id(entry.cwe_id), entry.cwe_name)
    predicted_cwes = {finding.cwe_id for finding in pipeline_results.final_findings}
    cwe_ids = sorted(set(cwe_names) | predicted_cwes, key=_cwe_sort_key)
    for cwe_id in cwe_ids:
        cwe_truth = [
            entry for entry in true_positive_entries if normalize_cwe_id(entry.cwe_id) == cwe_id
        ]
        cwe_predictions = [
            finding for finding in pipeline_results.final_findings if finding.cwe_id == cwe_id
        ]
        summary = summarize_matches(cwe_predictions, cwe_truth)
        precision, recall, f1 = _compute_prf(
            summary.tp_weight,
            summary.fp_weight,
            summary.fn_weight,
        )
        per_cwe[cwe_id] = {
            "cwe_name": cwe_names.get(cwe_id, ""),
            "precision": precision,
            "recall": recall,
            "f1": f1,
            "tp_weight": summary.tp_weight,
            "fp_weight": summary.fp_weight,
            "ground_truth": len(cwe_truth),
            "predictions": len(cwe_predictions),
            "exact_matches": summary.exact_matches,
            "partial_matches": summary.partial_matches,
        }

    fp_leak_summary = summarize_matches(pipeline_results.final_findings, false_positive_entries)
    leaked_fp_ids = sorted(
        {assignment.ground_truth_id for assignment in fp_leak_summary.assignments},
    )
    leaked_entries = [entry for entry in false_positive_entries if entry.id in leaked_fp_ids]
    false_positive_handling = {
        "known_fp_entries": len(false_positive_entries),
        "correctly_filtered": len(false_positive_entries) - len(leaked_entries),
        "leaked_through": len(leaked_entries),
        "leaked_entries": [
            {"id": entry.id, "notes": entry.notes, "cwe_id": normalize_cwe_id(entry.cwe_id)}
            for entry in leaked_entries
        ],
    }

    cost_per_finding = _safe_divide(
        pipeline_results.total_cost_usd or 0.0,
        len(pipeline_results.final_findings),
    )
    cost_per_true_positive = _safe_divide(pipeline_results.total_cost_usd or 0.0, overall.tp_weight)
    total_cost = pipeline_results.total_cost_usd
    if total_cost is None:
        cost_per_finding = None
        cost_per_true_positive = None

    commit = pipeline_results.metadata.get("commit")
    if not isinstance(commit, str) or not commit.strip():
        commit = _git_commit_fallback()

    pipeline_finding_count = pipeline_results.metadata.get("pipeline_findings")
    if not isinstance(pipeline_finding_count, int):
        pipeline_finding_count = len(pipeline_results.final_findings)

    return EvaluationReport(
        date=date.today().isoformat(),
        commit=commit,
        ground_truth_total=len(ground_truth_entries),
        ground_truth_true_positive=len(true_positive_entries),
        ground_truth_false_positive=len(false_positive_entries),
        pipeline_findings=pipeline_finding_count,
        overall=overall,
        per_cwe=per_cwe,
        false_positive_handling=false_positive_handling,
        scan=scan,
        detect=detect,
        triage=triage,
        verify=verify,
        total_cost_usd=total_cost,
        cost_per_finding_usd=cost_per_finding,
        cost_per_true_positive_usd=cost_per_true_positive,
    )


def render_report(report: EvaluationReport) -> str:
    lines = [
        "Piranesi Evaluation Report",
        "==========================",
        f"Date: {report.date}",
        f"Commit: {report.commit}",
        (
            "Ground truth entries: "
            f"{report.ground_truth_total} "
            f"({report.ground_truth_true_positive} TP, {report.ground_truth_false_positive} FP)"
        ),
        f"Pipeline findings: {report.pipeline_findings}",
        "",
        "Overall Metrics:",
        (
            f"  Precision:  {_format_metric(report.overall.precision)} "
            f"({_format_count(report.overall.tp_weight)}"
            f"/{_format_count(report.overall.predictions)} "
            "confirmed findings are real)"
        ),
        (
            f"  Recall:     {_format_metric(report.overall.recall)} "
            f"({_format_count(report.overall.tp_weight)}/{report.ground_truth_true_positive} "
            "real vulnerabilities found)"
        ),
        f"  F1:         {_format_metric(report.overall.f1)}",
        "",
        "Per-CWE Breakdown:",
    ]

    if report.per_cwe:
        for cwe_id in sorted(report.per_cwe, key=_cwe_sort_key):
            cwe_metrics = report.per_cwe[cwe_id]
            cwe_name = cwe_metrics["cwe_name"]
            label = f"{cwe_id} ({cwe_name})" if cwe_name else cwe_id
            lines.append(
                "  "
                f"{label}: "
                f"P={_format_metric(cwe_metrics['precision'], digits=2)}  "
                f"R={_format_metric(cwe_metrics['recall'], digits=2)}  "
                f"F1={_format_metric(cwe_metrics['f1'], digits=2)}  "
                f"({_format_count(cwe_metrics['tp_weight'])}/{cwe_metrics['ground_truth']} found, "
                f"{_format_count(cwe_metrics['fp_weight'])} FP)"
            )
    else:
        lines.append("  none")

    lines.extend(
        [
            "",
            "False Positive Handling:",
            f"  Known FP entries: {report.false_positive_handling['known_fp_entries']}",
            f"  Correctly filtered: {report.false_positive_handling['correctly_filtered']}",
        ]
    )
    leaked_entries = report.false_positive_handling["leaked_entries"]
    if leaked_entries:
        details = ", ".join(entry["id"] for entry in leaked_entries)
        lines.append(
            f"  Leaked through: {report.false_positive_handling['leaked_through']} ({details})"
        )
    else:
        lines.append("  Leaked through: 0")

    lines.extend(["", "Per-Stage Breakdown:"])
    if report.scan.available:
        lines.append(
            "  "
            f"Scan:    {report.scan.source_hits}/{report.scan.source_total} sources identified "
            f"({_format_percent(report.scan.source_recall)}), "
            f"{report.scan.sink_hits}/{report.scan.sink_total} sinks identified "
            f"({_format_percent(report.scan.sink_recall)})"
        )
    else:
        lines.append("  Scan:    n/a")

    if report.detect.available:
        lines.append(
            "  "
            f"Detect:  {report.detect.predictions} candidate findings generated "
            f"(P={_format_metric(report.detect.precision, digits=2)}, "
            f"R={_format_metric(report.detect.recall, digits=2)})"
        )
    else:
        lines.append("  Detect:  n/a")

    if report.triage.available:
        lines.append(
            "  "
            f"Triage:  {report.triage.fp_filtered} FPs filtered, "
            f"{_format_count(report.triage.tp_incorrectly_filtered_weight)} "
            "TPs incorrectly filtered"
        )
    else:
        lines.append("  Triage:  n/a")

    if report.verify.available:
        lines.append(
            "  "
            f"Verify:  {report.verify.confirmed_count}/{report.verify.post_triage_count} "
            "post-triage findings confirmed exploitable"
        )
    else:
        lines.append("  Verify:  n/a")

    if report.total_cost_usd is None:
        lines.append("")
        lines.append("Cost: n/a")
    else:
        lines.append("")
        lines.append(
            "Cost: "
            f"${report.total_cost_usd:.2f} total, "
            f"${report.cost_per_finding_usd:.2f}/finding, "
            f"${report.cost_per_true_positive_usd:.2f}/TP"
        )

    return "\n".join(lines)


def build_comparison_report(
    reports: dict[str, EvaluationReport],
    *,
    capabilities: dict[str, dict[str, bool]] | None = None,
) -> dict[str, Any]:
    merged_capabilities = {
        **_DEFAULT_COMPARISON_CAPABILITIES,
        **(capabilities or {}),
    }
    tools = [
        {
            "id": tool_id,
            "label": _tool_label(tool_id),
        }
        for tool_id in reports
    ]

    rows = [
        {
            "label": "Precision",
            "values": [reports[tool_id].overall.precision for tool_id in reports],
        },
        {
            "label": "Recall",
            "values": [reports[tool_id].overall.recall for tool_id in reports],
        },
        {
            "label": "F1",
            "values": [reports[tool_id].overall.f1 for tool_id in reports],
        },
        {
            "label": "Cost",
            "values": [reports[tool_id].total_cost_usd for tool_id in reports],
        },
        {
            "label": "Cost/TP",
            "values": [reports[tool_id].cost_per_true_positive_usd for tool_id in reports],
        },
        {
            "label": "FP count",
            "values": [reports[tool_id].overall.fp_weight for tool_id in reports],
        },
        {
            "label": "Regulatory map",
            "values": [
                merged_capabilities.get(tool_id, {}).get("regulatory_mapping", False)
                for tool_id in reports
            ],
        },
        {
            "label": "Exploit gen",
            "values": [
                merged_capabilities.get(tool_id, {}).get("exploit_generation", False)
                for tool_id in reports
            ],
        },
    ]

    return {
        "tools": tools,
        "rows": rows,
    }


def render_comparison_report(comparison: dict[str, Any]) -> str:
    tools = comparison.get("tools", [])
    rows = comparison.get("rows", [])
    tool_labels = [
        tool["label"]
        for tool in tools
        if isinstance(tool, dict) and isinstance(tool.get("label"), str)
    ]
    row_label_widths = [
        len(row["label"])
        for row in rows
        if isinstance(row, dict) and isinstance(row.get("label"), str)
    ]
    first_column_width = max(
        [len("Metric"), *row_label_widths],
        default=18,
    )
    tool_column_width = max(
        [10, *(len(label) + 2 for label in tool_labels)],
        default=10,
    )

    header_cols = "  ".join(f"{label:>{tool_column_width}}" for label in tool_labels)
    lines = [
        "Baseline Comparison",
        "===================",
        f"{'':<{first_column_width}}  " + header_cols,
    ]

    for row in rows:
        if not isinstance(row, dict):
            continue
        label = row.get("label")
        values = row.get("values")
        if not isinstance(label, str) or not isinstance(values, list):
            continue
        rendered_values = [
            f"{_render_comparison_value(label, value):>{tool_column_width}}" for value in values
        ]
        lines.append(f"{label:<{first_column_width}}  " + "  ".join(rendered_values))

    return "\n".join(lines)


def _tool_label(tool_id: str) -> str:
    labels = {
        "piranesi": "Piranesi",
        "opengrep": "OpenGrep",
        "semgrep": "Semgrep",
        "llm_only": "LLM-Only",
    }
    return labels.get(tool_id, tool_id.replace("_", " ").title())


def _render_comparison_value(row_label: str, value: Any) -> str:
    if row_label in {"Precision", "Recall", "F1"}:
        return _format_metric(value if isinstance(value, float | int) else None)
    if row_label in {"Cost", "Cost/TP"}:
        if not isinstance(value, float | int):
            return "n/a"
        return f"${float(value):.2f}"
    if row_label == "FP count":
        if not isinstance(value, float | int):
            return "n/a"
        return _format_count(value)
    if isinstance(value, bool):
        return "Yes" if value else "No"
    return str(value)


def _report_to_json(report: EvaluationReport) -> dict[str, Any]:
    payload = asdict(report)
    return payload


def score_pipeline_output(pipeline_output_path: Path, ground_truth_dir: Path) -> EvaluationReport:
    ground_truth_entries = load_ground_truth_entries(ground_truth_dir)
    pipeline_results = load_pipeline_results(pipeline_output_path)
    return build_report(pipeline_results, ground_truth_entries)


def score_multiple_outputs(
    pipeline_outputs: dict[str, Path],
    ground_truth_dir: Path,
) -> dict[str, EvaluationReport]:
    return {
        tool_id: score_pipeline_output(output_path, ground_truth_dir)
        for tool_id, output_path in pipeline_outputs.items()
    }


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Score Piranesi eval output against ground truth.")
    parser.add_argument("--pipeline-output", required=True, type=Path, help="Path to results.json.")
    parser.add_argument(
        "--ground-truth",
        required=True,
        type=Path,
        help="Directory containing YAML ground truth entries.",
    )
    parser.add_argument(
        "--output",
        type=Path,
        default=Path("eval/scores/latest.json"),
        help="Path for machine-readable score output.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    report = score_pipeline_output(args.pipeline_output, args.ground_truth)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(_report_to_json(report), indent=2), encoding="utf-8")
    print(render_report(report))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
