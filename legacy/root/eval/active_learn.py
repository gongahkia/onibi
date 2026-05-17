#!/usr/bin/env python3
"""Interactive active-learning loop for the false-positive classifier."""

from __future__ import annotations

import argparse
import json
import logging
from pathlib import Path
from typing import Any

import yaml

from eval.ground_truth.schema import Complexity
from eval.train_classifier import train
from piranesi.models import CandidateFinding
from piranesi.triage.ml_classifier import default_model_path, predict

logger = logging.getLogger(__name__)
_DEFAULT_GT_DIR = Path(__file__).resolve().parent / "ground_truth"


def active_learn(
    findings_json: Path,
    *,
    model_path: Path | None = None,
    uncertainty_low: float = 0.4,
    uncertainty_high: float = 0.6,
    max_samples: int = 20,
    gt_dir: Path | None = None,
    retrain: bool = True,
) -> int:
    findings = _load_findings(findings_json)
    scored = predict(findings, model_path=model_path)
    uncertain = [
        prediction
        for prediction in scored
        if uncertainty_low <= prediction.true_positive_probability <= uncertainty_high
    ]
    uncertain.sort(key=lambda item: abs(item.true_positive_probability - 0.5))
    uncertain = uncertain[:max_samples]

    if not uncertain:
        print("No uncertain findings to label.")
        return 0

    target_gt_dir = (gt_dir or _DEFAULT_GT_DIR).expanduser()
    target_gt_dir.mkdir(parents=True, exist_ok=True)
    labeled_count = 0
    for index, prediction in enumerate(uncertain, start=1):
        finding = prediction.finding
        print(f"[{index}/{len(uncertain)}] {finding.vuln_class}")
        source_summary = (
            f"{finding.source.source_type} @ "
            f"{finding.source.location.file}:{finding.source.location.line}"
        )
        print(
            "  Source:",
            source_summary,
        )
        sink_summary = (
            f"{finding.sink.api_name} @ "
            f"{finding.sink.location.file}:{finding.sink.location.line}"
        )
        print(
            "  Sink:  ",
            sink_summary,
        )
        print(f"  P(TP):  {prediction.true_positive_probability:.3f}")
        snippet = ""
        if finding.sink.location.snippet:
            snippet = finding.sink.location.snippet.strip().splitlines()[0]
        if snippet:
            print(f"  Snippet: {snippet[:140]}")
        response = _prompt_for_label()
        if response == "quit":
            break
        if response == "skip":
            continue
        _write_gt_entry(
            finding,
            label=response,
            score=prediction.true_positive_probability,
            gt_dir=target_gt_dir,
        )
        labeled_count += 1

    if labeled_count > 0 and retrain:
        train(gt_dir=target_gt_dir, output_dir=(model_path or default_model_path()).parent)
    return labeled_count


def _load_findings(path: Path) -> list[CandidateFinding]:
    raw = json.loads(path.read_text(encoding="utf-8"))
    finding_payloads = _extract_findings_payloads(raw)
    return [CandidateFinding.model_validate(payload) for payload in finding_payloads]


def _extract_findings_payloads(payload: Any) -> list[dict[str, Any]]:
    if isinstance(payload, list):
        return [item for item in payload if isinstance(item, dict)]
    if not isinstance(payload, dict):
        return []
    if isinstance(payload.get("findings"), list):
        return [item for item in payload["findings"] if isinstance(item, dict)]
    artifact = payload.get("artifact")
    if isinstance(artifact, dict) and isinstance(artifact.get("findings"), list):
        return [item for item in artifact["findings"] if isinstance(item, dict)]
    partial = payload.get("triage") or payload.get("detect")
    if isinstance(partial, dict) and isinstance(partial.get("findings"), list):
        return [item for item in partial["findings"] if isinstance(item, dict)]
    return []


def _prompt_for_label() -> str:
    while True:
        answer = input(
            "  Label [y=true positive / n=false positive / s=skip / q=quit]: "
        ).strip().lower()
        if answer == "y":
            return "true_positive"
        if answer == "n":
            return "false_positive"
        if answer == "s":
            return "skip"
        if answer == "q":
            return "quit"


def _write_gt_entry(
    finding: CandidateFinding,
    *,
    label: str,
    score: float,
    gt_dir: Path,
) -> Path:
    next_index = _next_gt_index(gt_dir)
    prefix = "gt" if label == "true_positive" else "gt-fp"
    gt_id = f"{prefix}-{next_index:03d}"
    path = gt_dir / f"{gt_id}.yaml"
    entry = {
        "id": gt_id,
        "source_project": "active-learning",
        "commit_hash": "active-learning-no-commit",
        "cwe_id": _parse_cwe(finding.vuln_class),
        "cwe_name": finding.vuln_class,
        "label": label,
        "affected_files": [finding.source.location.file],
        "line_numbers": [finding.source.location.line, finding.sink.location.line],
        "taint_source": finding.source.source_type,
        "taint_sink": finding.sink.api_name,
        "taint_path": [step.operation for step in finding.taint_path],
        "complexity": _complexity_for_finding(finding).value,
        "exploitable": label == "true_positive",
        "reference_exploit": None,
        "reference_fix_commit": None,
        "notes": f"Added via active learning; model score={score:.3f}",
        "framework": finding.metadata.get("framework"),
    }
    path.write_text(yaml.safe_dump(entry, sort_keys=False), encoding="utf-8")
    logger.info("wrote active-learning GT entry to %s", path)
    return path


def _next_gt_index(gt_dir: Path) -> int:
    highest = 0
    for path in gt_dir.glob("*.yaml"):
        suffix = path.stem.rsplit("-", maxsplit=1)[-1]
        if suffix.isdigit():
            highest = max(highest, int(suffix))
    return highest + 1


def _parse_cwe(vuln_class: str) -> str:
    for token in vuln_class.split():
        if token.startswith("CWE-"):
            return token.rstrip(":")
    return vuln_class


def _complexity_for_finding(finding: CandidateFinding) -> Complexity:
    if len(finding.taint_path) >= 3:
        return Complexity.MULTI_STEP
    if len({step.through_function for step in finding.taint_path if step.through_function}) > 1:
        return Complexity.INTERPROCEDURAL
    return Complexity.SIMPLE


def _build_arg_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("findings_json", type=Path)
    parser.add_argument("--model", type=Path, default=default_model_path())
    parser.add_argument("--uncertainty-low", type=float, default=0.4)
    parser.add_argument("--uncertainty-high", type=float, default=0.6)
    parser.add_argument("--max-samples", type=int, default=20)
    parser.add_argument("--gt-dir", type=Path, default=_DEFAULT_GT_DIR)
    parser.add_argument("--no-retrain", action="store_true")
    return parser


def main() -> int:
    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(message)s")
    args = _build_arg_parser().parse_args()
    active_learn(
        args.findings_json,
        model_path=args.model,
        uncertainty_low=args.uncertainty_low,
        uncertainty_high=args.uncertainty_high,
        max_samples=args.max_samples,
        gt_dir=args.gt_dir,
        retrain=not args.no_retrain,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
