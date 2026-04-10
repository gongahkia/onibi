from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
from typing import Any

try:
    from eval.scoring import normalize_cwe_id, normalize_file_path
except ImportError:  # pragma: no cover - supports `python eval/baselines/opengrep_normalizer.py`
    from scoring import (  # type: ignore[import-not-found,no-redef]
        normalize_cwe_id,
        normalize_file_path,
    )

_KEYWORD_TO_CWE: tuple[tuple[str, str], ...] = (
    ("server-side request forgery", "CWE-918"),
    ("ssrf", "CWE-918"),
    ("path traversal", "CWE-22"),
    ("directory traversal", "CWE-22"),
    ("path-traversal", "CWE-22"),
    ("command injection", "CWE-78"),
    ("shell injection", "CWE-78"),
    ("child_process", "CWE-78"),
    ("cross-site scripting", "CWE-79"),
    ("cross site scripting", "CWE-79"),
    ("xss", "CWE-79"),
    ("sql injection", "CWE-89"),
    ("nosql injection", "CWE-89"),
    ("tainted sql", "CWE-89"),
    ("unsafe sql", "CWE-89"),
    ("eval", "CWE-94"),
    ("code injection", "CWE-94"),
)


def _coerce_mapping(value: Any) -> dict[str, Any]:
    if isinstance(value, dict):
        return value
    return {}


def _coerce_list(value: Any) -> list[Any]:
    if isinstance(value, list):
        return value
    return []


def _coerce_int(value: Any) -> int | None:
    if isinstance(value, bool):
        return None
    if isinstance(value, int):
        return value if value > 0 else None
    if isinstance(value, float):
        parsed = int(value)
        return parsed if parsed > 0 else None
    if isinstance(value, str) and value.strip().isdigit():
        parsed = int(value.strip())
        return parsed if parsed > 0 else None
    return None


def _flatten_strings(value: Any) -> list[str]:
    if isinstance(value, str):
        return [value]
    if isinstance(value, dict):
        flattened: list[str] = []
        for nested in value.values():
            flattened.extend(_flatten_strings(nested))
        return flattened
    if isinstance(value, list):
        flattened = []
        for nested in value:
            flattened.extend(_flatten_strings(nested))
        return flattened
    return []


def _unwrap_payload(payload: Any) -> tuple[str, dict[str, Any]]:
    mapping = _coerce_mapping(payload)
    tool = mapping.get("tool")
    if isinstance(tool, str) and isinstance(mapping.get("raw_output"), dict):
        return tool, _coerce_mapping(mapping.get("raw_output"))
    if isinstance(tool, str) and isinstance(mapping.get("results"), list):
        return tool, mapping
    if isinstance(mapping.get("results"), list):
        return "opengrep", mapping
    return "opengrep", mapping


def _normalize_result_path(raw_path: str | None, project_root: Path | None) -> str:
    if raw_path is None:
        return ""
    candidate = Path(raw_path)
    if project_root is not None:
        root = project_root.resolve(strict=False)
        if candidate.is_absolute():
            resolved_candidate = candidate.resolve(strict=False)
        else:
            resolved_candidate = (root / candidate).resolve(strict=False)
        try:
            candidate = resolved_candidate.relative_to(root)
        except ValueError:
            candidate = Path(raw_path)
    return normalize_file_path(candidate.as_posix())


def _collect_line_numbers(result: dict[str, Any]) -> list[int]:
    start = _coerce_mapping(result.get("start"))
    end = _coerce_mapping(result.get("end"))
    lines: set[int] = set()
    for candidate in (start.get("line"), end.get("line")):
        line = _coerce_int(candidate)
        if line is not None:
            lines.add(line)
    return sorted(lines)


def _extract_cwe_id(result: dict[str, Any]) -> str:
    extra = _coerce_mapping(result.get("extra"))
    metadata = _coerce_mapping(extra.get("metadata"))
    for raw_candidate in _flatten_strings(metadata.get("cwe")):
        normalized = normalize_cwe_id(raw_candidate)
        if normalized != "UNKNOWN":
            return normalized

    haystack = " ".join(
        [
            str(result.get("check_id") or ""),
            str(extra.get("message") or ""),
            str(metadata.get("category") or ""),
            " ".join(_flatten_strings(metadata.get("owasp"))),
        ]
    ).casefold()
    for keyword, cwe_id in _KEYWORD_TO_CWE:
        if keyword in haystack:
            return normalize_cwe_id(cwe_id)
    return "UNKNOWN"


def _extract_snippet(payload: Any) -> str:
    if isinstance(payload, str):
        return payload.strip()
    mapping = _coerce_mapping(payload)
    for key in ("snippet", "code", "value", "content"):
        value = mapping.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    for nested_key in ("location", "trace", "value"):
        value = mapping.get(nested_key)
        snippet = _extract_snippet(value)
        if snippet:
            return snippet
    for nested in _coerce_list(payload):
        snippet = _extract_snippet(nested)
        if snippet:
            return snippet
    return ""


def _find_location_payload(payload: Any) -> dict[str, Any] | None:
    mapping = _coerce_mapping(payload)
    if mapping:
        location = _coerce_mapping(mapping.get("location"))
        if location:
            return location
        if any(key in mapping for key in ("path", "file", "start", "line")):
            return mapping
        for nested in mapping.values():
            nested_location = _find_location_payload(nested)
            if nested_location is not None:
                return nested_location
    for nested in _coerce_list(payload):
        nested_location = _find_location_payload(nested)
        if nested_location is not None:
            return nested_location
    return None


def _build_location(payload: Any, project_root: Path | None) -> dict[str, Any] | None:
    location_payload = _find_location_payload(payload)
    if location_payload is None:
        return None

    path_value = location_payload.get("path") or location_payload.get("file")
    if not isinstance(path_value, str):
        return None
    start = _coerce_mapping(location_payload.get("start"))
    end = _coerce_mapping(location_payload.get("end"))
    line = _coerce_int(location_payload.get("line")) or _coerce_int(start.get("line")) or 1
    column = _coerce_int(location_payload.get("column")) or _coerce_int(start.get("col")) or 0
    end_line = _coerce_int(end.get("line"))
    end_column = _coerce_int(end.get("col"))
    snippet = _extract_snippet(location_payload) or _extract_snippet(payload)

    location: dict[str, Any] = {
        "file": _normalize_result_path(path_value, project_root),
        "line": line,
        "column": column,
        "snippet": snippet,
    }
    if end_line is not None:
        location["end_line"] = end_line
    if end_column is not None:
        location["end_column"] = end_column
    return location


def _extract_trace_node(
    trace: dict[str, Any],
    *,
    keys: tuple[str, ...],
    project_root: Path | None,
) -> tuple[str, dict[str, Any] | None]:
    for key in keys:
        if key not in trace:
            continue
        payload = trace[key]
        location = _build_location(payload, project_root)
        snippet = _extract_snippet(payload)
        if location is None and not snippet:
            continue
        normalized_mapping: dict[str, Any] = {}
        if snippet:
            normalized_mapping["normalized"] = snippet
        if location is not None:
            normalized_mapping["location"] = location
        return snippet, normalized_mapping or None
    return "", None


def _extract_taint_path(trace: dict[str, Any], project_root: Path | None) -> list[dict[str, Any]]:
    steps: list[dict[str, Any]] = []
    seen: set[tuple[str, int, int, str]] = set()
    for key in ("taint_source", "intermediate_vars", "taint_sink"):
        payload = trace.get(key)
        if payload is None:
            continue
        values = payload if isinstance(payload, list) else [payload]
        for value in values:
            location = _build_location(value, project_root)
            if location is None:
                continue
            step_key = (
                location["file"],
                location["line"],
                location["column"],
                location["snippet"],
            )
            if step_key in seen:
                continue
            seen.add(step_key)
            steps.append({"location": location})
    return steps


def _finding_id(tool: str, rule_id: str, file_path: str, line_numbers: list[int]) -> str:
    parts = [tool, rule_id, file_path, ",".join(str(line) for line in line_numbers)]
    digest = hashlib.sha256("|".join(parts).encode("utf-8")).hexdigest()
    return digest


def normalize_opengrep_output(
    payload: Any,
    *,
    project_root: Path | None = None,
    tool_name: str | None = None,
) -> dict[str, Any]:
    detected_tool, raw_payload = _unwrap_payload(payload)
    tool = tool_name or detected_tool
    findings: list[dict[str, Any]] = []

    for result in _coerce_list(raw_payload.get("results")):
        result_mapping = _coerce_mapping(result)
        if not result_mapping:
            continue
        extra = _coerce_mapping(result_mapping.get("extra"))
        file_path = _normalize_result_path(
            result_mapping.get("path") if isinstance(result_mapping.get("path"), str) else None,
            project_root,
        )
        line_numbers = _collect_line_numbers(result_mapping)
        rule_id = str(result_mapping.get("check_id") or result_mapping.get("rule_id") or "")
        description = str(extra.get("message") or result_mapping.get("message") or "").strip()
        cwe_id = _extract_cwe_id(result_mapping)

        finding: dict[str, Any] = {
            "id": _finding_id(tool, rule_id, file_path, line_numbers),
            "tool": tool,
            "rule_id": rule_id or None,
            "cwe_id": cwe_id,
            "description": description,
            "severity": extra.get("severity") or result_mapping.get("severity"),
            "affected_files": [file_path] if file_path else [],
            "line_numbers": line_numbers,
        }

        trace = _coerce_mapping(extra.get("dataflow_trace"))
        if trace:
            source_text, source_mapping = _extract_trace_node(
                trace,
                keys=("taint_source", "source"),
                project_root=project_root,
            )
            sink_text, sink_mapping = _extract_trace_node(
                trace,
                keys=("taint_sink", "sink"),
                project_root=project_root,
            )
            if source_text:
                finding["taint_source"] = source_text
            if sink_text:
                finding["taint_sink"] = sink_text
            if source_mapping is not None:
                finding["source"] = source_mapping
            if sink_mapping is not None:
                finding["sink"] = sink_mapping
            taint_path = _extract_taint_path(trace, project_root)
            if taint_path:
                finding["taint_path"] = taint_path

        findings.append(finding)

    return {
        "tool": tool,
        "metadata": {
            "tool": tool,
            "version": raw_payload.get("version"),
            "errors": _coerce_list(raw_payload.get("errors")),
        },
        "pipeline_findings": len(findings),
        "total_cost_usd": 0.0,
        "findings": findings,
    }


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Normalize OpenGrep or Semgrep JSON output.")
    parser.add_argument(
        "--input",
        required=True,
        type=Path,
        help="Path to raw OpenGrep/Semgrep JSON output.",
    )
    parser.add_argument(
        "--output",
        required=True,
        type=Path,
        help="Path to normalized output JSON.",
    )
    parser.add_argument(
        "--project-root",
        type=Path,
        help="Project root used to relativize result paths.",
    )
    parser.add_argument(
        "--tool",
        help="Explicit tool name override, for example opengrep or semgrep.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    raw_payload = json.loads(args.input.read_text(encoding="utf-8"))
    normalized = normalize_opengrep_output(
        raw_payload,
        project_root=args.project_root,
        tool_name=args.tool,
    )
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(normalized, indent=2), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
