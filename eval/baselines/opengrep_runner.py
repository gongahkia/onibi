from __future__ import annotations

import argparse
import json
import logging
import shutil
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Any

try:
    from eval.baselines.opengrep_normalizer import normalize_opengrep_output
except ImportError:  # pragma: no cover - supports `python eval/baselines/opengrep_runner.py`
    from opengrep_normalizer import (  # type: ignore[import-not-found,no-redef]
        normalize_opengrep_output,
    )

DEFAULT_CONFIGS: tuple[str, ...] = ("p/typescript", "p/javascript")
DEFAULT_TIMEOUT_SECONDS = 600
LOGGER = logging.getLogger("eval.baselines.opengrep_runner")


@dataclass(frozen=True, slots=True)
class ScannerSelection:
    tool: str
    binary: str


@dataclass(frozen=True, slots=True)
class ScannerRunResult:
    tool: str
    command: tuple[str, ...]
    raw_output: dict[str, Any]
    stdout: str
    stderr: str


def resolve_scanner_binary() -> ScannerSelection:
    for tool in ("opengrep", "semgrep"):
        binary = shutil.which(tool)
        if binary is not None:
            return ScannerSelection(tool=tool, binary=binary)
    raise FileNotFoundError("neither opengrep nor semgrep is installed")


def build_scan_command(
    *,
    binary: str,
    project_dir: Path,
    configs: tuple[str, ...] = DEFAULT_CONFIGS,
    dataflow_traces: bool = True,
) -> tuple[str, ...]:
    command: list[str] = [binary]
    for config in configs:
        command.append(f"--config={config}")
    command.append("--json")
    if dataflow_traces:
        command.append("--dataflow-traces")
    command.append(str(project_dir))
    return tuple(command)


def _run_command(
    command: tuple[str, ...],
    timeout_seconds: int,
) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        list(command),
        capture_output=True,
        text=True,
        timeout=timeout_seconds,
        check=False,
    )


def _looks_like_dataflow_flag_error(stderr: str) -> bool:
    normalized = stderr.casefold()
    return "dataflow-traces" in normalized and (
        "unknown option" in normalized
        or "unrecognized option" in normalized
        or "unknown flag" in normalized
        or "unknown argument" in normalized
    )


def _execute_with_optional_dataflow(
    selection: ScannerSelection,
    *,
    project_dir: Path,
    configs: tuple[str, ...],
    timeout_seconds: int,
    dataflow_traces: bool,
) -> tuple[subprocess.CompletedProcess[str], tuple[str, ...]]:
    command = build_scan_command(
        binary=selection.binary,
        project_dir=project_dir,
        configs=configs,
        dataflow_traces=dataflow_traces,
    )
    completed = _run_command(command, timeout_seconds)
    no_retry = (
        completed.returncode == 0
        or not dataflow_traces
        or not _looks_like_dataflow_flag_error(completed.stderr)
    )
    if no_retry:
        return completed, command

    fallback_command = build_scan_command(
        binary=selection.binary,
        project_dir=project_dir,
        configs=configs,
        dataflow_traces=False,
    )
    LOGGER.warning(
        "scanner %s does not support --dataflow-traces; retrying without it",
        selection.tool,
    )
    return _run_command(fallback_command, timeout_seconds), fallback_command


def run_opengrep_scan(
    project_dir: Path,
    *,
    configs: tuple[str, ...] = DEFAULT_CONFIGS,
    timeout_seconds: int = DEFAULT_TIMEOUT_SECONDS,
    dataflow_traces: bool = True,
    output_path: Path | None = None,
    normalized_output_path: Path | None = None,
) -> ScannerRunResult:
    project_root = project_dir.resolve(strict=False)
    primary = resolve_scanner_binary()
    attempted_tools = [primary]
    if primary.tool == "opengrep":
        fallback_binary = shutil.which("semgrep")
        if fallback_binary is not None:
            attempted_tools.append(ScannerSelection(tool="semgrep", binary=fallback_binary))

    last_completed: subprocess.CompletedProcess[str] | None = None
    last_command: tuple[str, ...] = ()
    last_tool = primary.tool
    for selection in attempted_tools:
        completed, command = _execute_with_optional_dataflow(
            selection,
            project_dir=project_root,
            configs=configs,
            timeout_seconds=timeout_seconds,
            dataflow_traces=dataflow_traces,
        )
        last_completed = completed
        last_command = command
        last_tool = selection.tool
        if completed.returncode == 0:
            break
        if selection.tool == "opengrep" and len(attempted_tools) > 1:
            LOGGER.warning(
                "opengrep scan failed with exit code %s; retrying with semgrep fallback",
                completed.returncode,
            )
            continue
        raise RuntimeError(
            f"{selection.tool} exited with code {completed.returncode}: {completed.stderr.strip()}"
        )

    assert last_completed is not None
    stdout = last_completed.stdout.strip()
    if not stdout:
        raise RuntimeError(f"{last_tool} produced no JSON output")
    try:
        raw_output = json.loads(stdout)
    except json.JSONDecodeError as exc:  # pragma: no cover - defensive against tool corruption
        raise RuntimeError(f"{last_tool} produced invalid JSON output") from exc

    if output_path is not None:
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(json.dumps(raw_output, indent=2), encoding="utf-8")

    if normalized_output_path is not None:
        normalized_output = normalize_opengrep_output(
            raw_output,
            project_root=project_root,
            tool_name=last_tool,
        )
        normalized_output_path.parent.mkdir(parents=True, exist_ok=True)
        normalized_output_path.write_text(json.dumps(normalized_output, indent=2), encoding="utf-8")

    return ScannerRunResult(
        tool=last_tool,
        command=last_command,
        raw_output=raw_output,
        stdout=last_completed.stdout,
        stderr=last_completed.stderr,
    )


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run OpenGrep with Semgrep fallback against a project.",
    )
    parser.add_argument("project_dir", type=Path, help="Ground truth project directory to scan.")
    parser.add_argument("--output", type=Path, help="Path to raw JSON output.")
    parser.add_argument("--normalized-output", type=Path, help="Path to normalized JSON output.")
    parser.add_argument(
        "--config",
        dest="configs",
        action="append",
        help="Registry config to include. Defaults to p/typescript and p/javascript.",
    )
    parser.add_argument(
        "--timeout-seconds",
        type=int,
        default=DEFAULT_TIMEOUT_SECONDS,
        help="Subprocess timeout in seconds.",
    )
    parser.add_argument(
        "--no-dataflow-traces",
        action="store_true",
        help="Disable --dataflow-traces even when the scanner supports it.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    configs = tuple(args.configs) if args.configs else DEFAULT_CONFIGS
    result = run_opengrep_scan(
        args.project_dir,
        configs=configs,
        timeout_seconds=args.timeout_seconds,
        dataflow_traces=not args.no_dataflow_traces,
        output_path=args.output,
        normalized_output_path=args.normalized_output,
    )
    print(json.dumps({"tool": result.tool, "command": list(result.command)}, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
