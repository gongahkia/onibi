from __future__ import annotations

import argparse
import os
import sys
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
SRC_ROOT = REPO_ROOT / "src"
if str(SRC_ROOT) not in sys.path:
    sys.path.insert(0, str(SRC_ROOT))
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from piranesi.config import PiranesiConfig  # noqa: E402
from piranesi.host.eval import (  # noqa: E402
    build_host_benchmark_report,
    write_host_benchmark_outputs,
)
from piranesi.llm.cost import CostTracker  # noqa: E402
from piranesi.llm.provider import LLMProvider  # noqa: E402
from piranesi.llm.router import ModelRouter  # noqa: E402
from piranesi.llm.trace import TraceLogger  # noqa: E402
from piranesi.trace import TraceWriter  # noqa: E402

_LLM_ENV_NAMES = (
    "OPENAI_API_KEY",
    "ANTHROPIC_API_KEY",
    "AZURE_OPENAI_API_KEY",
)


def _llm_is_configured() -> bool:
    return any(os.getenv(name) for name in _LLM_ENV_NAMES)


def _llm_skip_note(include_llm: bool, provider: LLMProvider | None) -> str | None:
    if not include_llm:
        return None
    if provider is not None:
        return None
    return "LLM credentials were not detected; deterministic+LLM baseline will be skipped."


@contextmanager
def _benchmark_llm_provider(
    *,
    include_llm: bool,
    output_dir: Path,
) -> Iterator[LLMProvider | None]:
    if not include_llm or not _llm_is_configured():
        yield None
        return
    config = PiranesiConfig()
    config = config.model_copy(
        update={
            "trace": config.trace.model_copy(
                update={"file_path": str(output_dir / "host_benchmark_llm_trace.jsonl")}
            )
        }
    )
    cost_tracker = CostTracker()
    trace_writer = TraceWriter(config.trace, config.budget)
    trace_logger = TraceLogger(trace_writer, log_prompts=config.trace.log_prompts)
    provider = LLMProvider(trace_logger, cost_tracker, router=ModelRouter(config, cost_tracker))
    trace_writer.open()
    try:
        yield provider
    finally:
        trace_writer.close()


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run the Piranesi host benchmark harness.")
    parser.add_argument(
        "--fixtures",
        type=Path,
        default=Path("tests/fixtures/host"),
        help="Directory containing host fixture subdirectories with ground_truth.json.",
    )
    parser.add_argument(
        "--output",
        type=Path,
        default=Path("eval/reports/host-benchmark"),
        help=(
            "Output directory for host_benchmark.json, host_benchmark.md, "
            "and findings_matrix.csv."
        ),
    )
    parser.add_argument(
        "--include-llm",
        action="store_true",
        help=(
            "Include the deterministic+LLM baseline when a caller supplies an LLMProvider. "
            "The standalone script does not make live LLM calls."
        ),
    )
    parser.add_argument(
        "--treat-private-as-public",
        action="store_true",
        help="Treat private listeners as exposed for lab benchmark variants.",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    with _benchmark_llm_provider(
        include_llm=args.include_llm,
        output_dir=args.output,
    ) as provider:
        report = build_host_benchmark_report(
            args.fixtures,
            include_llm=args.include_llm,
            llm_provider=provider,
            treat_private_as_public=args.treat_private_as_public,
        )
    note = _llm_skip_note(args.include_llm, provider)
    if note is not None:
        report = report.model_copy(update={"notes": [*report.notes, note]})
    write_host_benchmark_outputs(report, args.output)
    primary = report.metrics
    print(f"fixtures: {report.fixture_count}")
    print(f"expected: {primary.expected_issue_count}")
    print(f"detected: {primary.detected_issue_count}")
    print(
        "precision/recall/f1: "
        f"{primary.precision:.3f}/{primary.recall:.3f}/{primary.f1:.3f}"
    )
    print(f"output: {args.output.resolve(strict=False)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
