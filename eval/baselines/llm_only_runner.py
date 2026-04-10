from __future__ import annotations

import argparse
import fnmatch
import hashlib
import json
from pathlib import Path
from typing import Any

from pydantic import BaseModel, ConfigDict, Field

from piranesi.config import PiranesiConfig, load_config
from piranesi.llm.cost import CostTracker
from piranesi.llm.router import ModelRouter
from piranesi.llm.trace import TraceLogger
from piranesi.trace import TraceWriter


class LLMBaselineFinding(BaseModel):
    model_config = ConfigDict(extra="ignore")

    file: str | None = None
    line_numbers: list[int] = Field(default_factory=list)
    cwe_id: str = "UNKNOWN"
    description: str = ""
    severity: str = "unknown"
    taint_source: str | None = None
    taint_sink: str | None = None


class LLMBaselineResponse(BaseModel):
    model_config = ConfigDict(extra="ignore")

    findings: list[LLMBaselineFinding] = Field(default_factory=list)


def _matches_any(path: str, patterns: list[str]) -> bool:
    return any(fnmatch.fnmatch(path, pattern) for pattern in patterns)


def collect_project_files(project_dir: Path, config: PiranesiConfig) -> list[Path]:
    root = project_dir.resolve(strict=False)
    included: set[Path] = set()
    for pattern in config.scan.include_patterns:
        for candidate in root.glob(pattern):
            if candidate.is_file():
                included.add(candidate.resolve(strict=False))

    selected: list[Path] = []
    for candidate in sorted(included):
        relative_path = candidate.relative_to(root).as_posix()
        if _matches_any(relative_path, config.scan.exclude_patterns):
            continue
        if candidate.stat().st_size > config.scan.max_file_size:
            continue
        selected.append(candidate)
    return selected


def _build_messages(relative_path: str, source_code: str) -> list[dict[str, str]]:
    schema = {
        "findings": [
            {
                "file": relative_path,
                "line_numbers": [17],
                "cwe_id": "CWE-89",
                "description": "Unsanitized request input reaches a SQL query sink.",
                "severity": "high",
                "taint_source": "req.query.id",
                "taint_sink": "db.query()",
            }
        ]
    }
    return [
        {
            "role": "system",
            "content": (
                "You are auditing a single TypeScript/JavaScript source file "
                "for concrete security vulnerabilities. "
                "Return strict JSON only. "
                "Report only likely security vulnerabilities in this file. "
                "Use line_numbers as a JSON array. "
                'If no vulnerabilities are present, return {"findings":[]}.'
            ),
        },
        {
            "role": "user",
            "content": (
                "Identify all security vulnerabilities in the following "
                "TypeScript/JavaScript code. "
                "For each vulnerability, specify: file, line number(s), "
                "CWE ID, description, and severity. "
                "Include taint_source and taint_sink when you can infer them. "
                "Use this JSON shape exactly: "
                f"{json.dumps(schema, ensure_ascii=True)}\n\n"
                f"File: {relative_path}\n\n"
                f"```ts\n{source_code}\n```"
            ),
        },
    ]


def _extract_json_object(content: str) -> dict[str, Any]:
    stripped = content.strip()
    if stripped.startswith("```"):
        lines = stripped.splitlines()
        if len(lines) >= 3:
            stripped = "\n".join(lines[1:-1]).strip()

    for start_char, end_char in (("{", "}"), ("[", "]")):
        start = stripped.find(start_char)
        end = stripped.rfind(end_char)
        if start == -1 or end == -1 or end <= start:
            continue
        candidate = stripped[start : end + 1]
        try:
            loaded = json.loads(candidate)
        except json.JSONDecodeError:
            continue
        if isinstance(loaded, dict):
            return loaded
        if isinstance(loaded, list):
            return {"findings": loaded}
    raise ValueError("LLM-only baseline response did not contain valid JSON")


def _parse_baseline_response(content: str) -> LLMBaselineResponse:
    return LLMBaselineResponse.model_validate(_extract_json_object(content))


def _normalize_line_numbers(line_numbers: list[int]) -> list[int]:
    normalized = sorted({int(line) for line in line_numbers if int(line) > 0})
    return normalized


def _finding_id(file_path: str, cwe_id: str, line_numbers: list[int], description: str) -> str:
    parts = [file_path, cwe_id, ",".join(str(line) for line in line_numbers), description]
    digest = hashlib.sha256("|".join(parts).encode("utf-8")).hexdigest()
    return digest


def _normalize_llm_finding(
    finding: LLMBaselineFinding,
    *,
    project_root: Path,
    fallback_file: str,
) -> dict[str, Any]:
    file_value = finding.file or fallback_file
    file_path = Path(file_value)
    try:
        root = project_root.resolve(strict=False)
        if file_path.is_absolute():
            resolved_file = file_path.resolve(strict=False)
        else:
            resolved_file = (root / file_path).resolve(strict=False)
        relative_file = resolved_file.relative_to(root).as_posix()
    except ValueError:
        relative_file = file_path.as_posix()

    line_numbers = _normalize_line_numbers(finding.line_numbers)
    normalized: dict[str, Any] = {
        "id": _finding_id(relative_file, finding.cwe_id, line_numbers, finding.description),
        "tool": "llm_only",
        "cwe_id": finding.cwe_id,
        "description": finding.description,
        "severity": finding.severity,
        "affected_files": [relative_file],
        "line_numbers": line_numbers,
        "taint_source": finding.taint_source or "",
        "taint_sink": finding.taint_sink or "",
    }

    if line_numbers:
        first_line = line_numbers[0]
        if finding.taint_source:
            normalized["source"] = {
                "normalized": finding.taint_source,
                "location": {
                    "file": relative_file,
                    "line": first_line,
                    "column": 0,
                    "snippet": finding.taint_source,
                },
            }
        if finding.taint_sink:
            normalized["sink"] = {
                "normalized": finding.taint_sink,
                "location": {
                    "file": relative_file,
                    "line": line_numbers[-1],
                    "column": 0,
                    "snippet": finding.taint_sink,
                },
            }
    return normalized


def run_llm_only_baseline(
    project_dir: Path,
    *,
    config: PiranesiConfig,
) -> dict[str, Any]:
    from piranesi.llm.provider import LLMProvider

    root = project_dir.resolve(strict=False)
    files = collect_project_files(root, config)

    trace_writer = TraceWriter(config.trace, config.budget)
    trace_writer.open()
    try:
        cost_tracker = CostTracker()
        router = ModelRouter(config=config, cost_tracker=cost_tracker)
        tracer = TraceLogger(trace_writer, log_prompts=config.trace.log_prompts)
        provider = LLMProvider(tracer, cost_tracker, router=router)

        findings: list[dict[str, Any]] = []
        per_file_results: list[dict[str, Any]] = []
        detector_model = router.resolve("detector")
        for file_path in files:
            relative_path = file_path.relative_to(root).as_posix()
            source_code = file_path.read_text(encoding="utf-8")
            response = provider.complete(
                stage="detector",
                messages=_build_messages(relative_path, source_code),
                response_format={"type": "json_object"},
            )
            parsed = _parse_baseline_response(response.content)
            normalized_findings = [
                _normalize_llm_finding(finding, project_root=root, fallback_file=relative_path)
                for finding in parsed.findings
            ]
            findings.extend(normalized_findings)
            per_file_results.append(
                {
                    "file": relative_path,
                    "model": response.model,
                    "prompt_tokens": response.prompt_tokens,
                    "response_tokens": response.response_tokens,
                    "cost_usd": response.cost_usd,
                    "duration_ms": response.duration_ms,
                    "raw_response": response.content,
                    "findings": normalized_findings,
                }
            )

        trace_summary = trace_writer.summary().model_dump(mode="json")
        return {
            "tool": "llm_only",
            "metadata": {
                "tool": "llm_only",
                "model": detector_model,
                "project_root": str(root),
                "files_audited": len(files),
            },
            "files_audited": [file_path.relative_to(root).as_posix() for file_path in files],
            "per_file_results": per_file_results,
            "pipeline_findings": len(findings),
            "findings": findings,
            "trace_summary": trace_summary,
            "total_cost_usd": cost_tracker.total_usd,
        }
    finally:
        trace_writer.close()


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run the single-model LLM-only evaluation baseline.",
    )
    parser.add_argument("project_dir", type=Path, help="Ground truth project directory to audit.")
    parser.add_argument(
        "--config",
        type=Path,
        default=Path("piranesi.toml"),
        help="Path to piranesi.toml used to resolve the detector model.",
    )
    parser.add_argument(
        "--output",
        type=Path,
        default=Path("eval/results/llm-only.json"),
        help="Path to write normalized baseline output.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    config = load_config(args.config)
    payload = run_llm_only_baseline(args.project_dir, config=config)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    print(json.dumps({"tool": "llm_only", "model": payload["metadata"]["model"]}, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
