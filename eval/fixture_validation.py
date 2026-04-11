from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import tempfile
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml  # type: ignore[import-untyped]

REPO_ROOT = Path(__file__).resolve().parents[1]
SRC_ROOT = REPO_ROOT / "src"
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))
if str(SRC_ROOT) not in sys.path:
    sys.path.insert(0, str(SRC_ROOT))

from eval.ground_truth.schema import GroundTruthEntry, Label  # noqa: E402
from eval.scoring import NormalizedFinding, match_weight, normalize_finding  # noqa: E402
from piranesi import __version__ as PIRANESI_VERSION  # noqa: E402


DEFAULT_OUTPUT_DIR_NAME = "fixture-validation"
_DEFAULT_INCLUDE_PATTERNS: dict[str, list[str]] = {
    "typescript": ["**/*.ts", "**/*.tsx", "**/*.js", "**/*.jsx"],
    "javascript": ["**/*.ts", "**/*.tsx", "**/*.js", "**/*.jsx"],
    "python": ["**/*.py"],
    "go": ["**/*.go"],
    "java": ["**/*.java"],
}
_DEFAULT_EXCLUDE_PATTERNS = [
    "**/node_modules/**",
    "**/dist/**",
    "**/__pycache__/**",
    "**/.venv/**",
    "**/venv/**",
]


@dataclass(frozen=True, slots=True)
class ValidationResult:
    entry_id: str
    cwe_id: str
    label: str
    passed: bool
    expected_detected: bool
    matched: bool
    match_weight: float
    message: str
    fixture_root: Path
    output_dir: Path
    expected_line: int | None
    detected_line: int | None
    detected_file: str | None
    matched_finding_id: str | None
    cve_id: str | None
    framework: str | None
    complexity: str
    error: str | None = None

    def as_dict(self) -> dict[str, Any]:
        return {
            "id": self.entry_id,
            "cwe": self.cwe_id,
            "label": self.label,
            "passed": self.passed,
            "expected_detected": self.expected_detected,
            "matched": self.matched,
            "match_weight": round(self.match_weight, 3),
            "message": self.message,
            "fixture_root": str(self.fixture_root),
            "output_dir": str(self.output_dir),
            "expected_line": self.expected_line,
            "detected_line": self.detected_line,
            "detected_file": self.detected_file,
            "matched_finding_id": self.matched_finding_id,
            "cve_id": self.cve_id,
            "framework": self.framework,
            "complexity": self.complexity,
            "error": self.error,
        }


class DetectionExecutionError(RuntimeError):
    def __init__(self, message: str, *, fixture_root: Path, output_dir: Path) -> None:
        super().__init__(message)
        self.fixture_root = fixture_root
        self.output_dir = output_dir


def load_ground_truth_entry(path: Path) -> GroundTruthEntry:
    payload = yaml.safe_load(path.read_text(encoding="utf-8"))
    return GroundTruthEntry.model_validate(payload)


def resolve_affected_file_path(path: str | Path, *, fixtures_dir: Path | None = None) -> Path:
    candidate = Path(path)
    if candidate.is_absolute():
        return candidate

    repo_candidate = REPO_ROOT / candidate
    if repo_candidate.exists():
        return repo_candidate

    if fixtures_dir is not None:
        rooted = fixtures_dir / candidate
        if rooted.exists():
            return rooted
        if candidate.parts and candidate.parts[0] == fixtures_dir.name:
            stripped = Path(*candidate.parts[1:])
            stripped_candidate = fixtures_dir / stripped
            if stripped_candidate.exists():
                return stripped_candidate

    return repo_candidate


def resolve_fixture_root(entry: GroundTruthEntry, *, fixtures_dir: Path | None = None) -> Path:
    files = [
        resolve_affected_file_path(file_path, fixtures_dir=fixtures_dir)
        for file_path in entry.affected_files
    ]
    if not files:
        raise ValueError(f"{entry.id} has no affected files")
    common = Path(os.path.commonpath([str(path.parent) for path in files]))
    return common


def _infer_language_from_paths(paths: list[Path]) -> str:
    for path in paths:
        suffix = path.suffix.lower()
        if suffix in {".ts", ".tsx", ".js", ".jsx"}:
            return "typescript"
        if suffix == ".py":
            return "python"
        if suffix == ".go":
            return "go"
        if suffix == ".java":
            return "java"
    return "typescript"


def _config_text(
    *,
    output_dir: Path,
    framework: str | None,
    language: str,
) -> str:
    include_patterns = _DEFAULT_INCLUDE_PATTERNS.get(
        language,
        _DEFAULT_INCLUDE_PATTERNS["typescript"],
    )
    frameworks = [framework] if framework else ["auto"]
    escaped_output = json.dumps(str(output_dir))
    escaped_trace = json.dumps(str(output_dir / ".trace.jsonl"))
    framework_items = ", ".join(json.dumps(item) for item in frameworks)
    include_items = ", ".join(json.dumps(item) for item in include_patterns)
    exclude_items = ", ".join(json.dumps(item) for item in _DEFAULT_EXCLUDE_PATTERNS)
    return "\n".join(
        [
            "[output]",
            'format = "json"',
            f"output_dir = {escaped_output}",
            "",
            "[trace]",
            "enabled = false",
            f"file_path = {escaped_trace}",
            "log_prompts = false",
            "",
            "[scan]",
            f"frameworks = [{framework_items}]",
            f"include_patterns = [{include_items}]",
            f"exclude_patterns = [{exclude_items}]",
            "",
        ]
    )


def _piranesi_command() -> list[str]:
    return [sys.executable, "-c", "from piranesi.cli import app; app()"]


def _subprocess_env() -> dict[str, str]:
    env = os.environ.copy()
    pythonpath_parts = [str(SRC_ROOT), str(REPO_ROOT)]
    if env.get("PYTHONPATH"):
        pythonpath_parts.append(env["PYTHONPATH"])
    env["PYTHONPATH"] = os.pathsep.join(pythonpath_parts)
    return env


def _run_cli(command: list[str], *, cwd: Path, verbose: bool) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        command,
        cwd=str(cwd),
        env=_subprocess_env(),
        text=True,
        capture_output=not verbose,
        check=False,
    )


def _load_detect_findings(output_dir: Path) -> list[NormalizedFinding]:
    detect_path = output_dir / "detect.json"
    payload = json.loads(detect_path.read_text(encoding="utf-8"))
    findings = payload.get("findings")
    if not isinstance(findings, list):
        return []
    normalized: list[NormalizedFinding] = []
    for finding in findings:
        item = normalize_finding(finding)
        if item is not None:
            normalized.append(item)
    return normalized


def _summarize_cli_failure(text: str) -> str:
    lines = [line.strip() for line in text.splitlines() if line.strip()]
    for line in reversed(lines):
        if not line.startswith("{"):
            continue
        try:
            payload = json.loads(line)
        except json.JSONDecodeError:
            continue
        if not isinstance(payload, dict):
            continue
        message = payload.get("why") or payload.get("msg") or payload.get("event")
        if isinstance(message, str) and message.strip():
            return message.strip().splitlines()[0]
    if lines:
        return lines[-1]
    return "unknown error"


def run_detection(
    entry: GroundTruthEntry,
    *,
    fixture_root: Path | None = None,
    fixtures_dir: Path | None = None,
    output_dir: Path | None = None,
    verbose: bool = False,
    keep_output: bool = False,
) -> tuple[list[NormalizedFinding], Path]:
    root = fixture_root or resolve_fixture_root(entry, fixtures_dir=fixtures_dir)
    if output_dir is None:
        base_dir = Path(tempfile.mkdtemp(prefix=f"{entry.id}-", dir=None))
        managed_output = True
    else:
        base_dir = output_dir
        base_dir.mkdir(parents=True, exist_ok=True)
        managed_output = False
    config_path = base_dir / "piranesi.toml"
    language = entry.language or _infer_language_from_paths(
        [
            resolve_affected_file_path(file_path, fixtures_dir=fixtures_dir)
            for file_path in entry.affected_files
        ]
    )
    config_path.write_text(
        _config_text(output_dir=base_dir, framework=entry.framework, language=language),
        encoding="utf-8",
    )

    base_command = _piranesi_command()
    common_args = [
        "--config",
        str(config_path),
        "--output",
        str(base_dir),
        "--authorized",
        "--yes",
    ]
    scan = _run_cli([*base_command, "scan", str(root), *common_args], cwd=REPO_ROOT, verbose=verbose)
    if scan.returncode != 0:
        stderr = scan.stderr.strip() if scan.stderr else scan.stdout.strip()
        raise DetectionExecutionError(
            f"piranesi scan failed for {entry.id}: {_summarize_cli_failure(stderr)}",
            fixture_root=root,
            output_dir=base_dir,
        )

    detect = _run_cli(
        [*base_command, "detect", str(root), *common_args],
        cwd=REPO_ROOT,
        verbose=verbose,
    )
    if detect.returncode != 0:
        stderr = detect.stderr.strip() if detect.stderr else detect.stdout.strip()
        raise DetectionExecutionError(
            f"piranesi detect failed for {entry.id}: {_summarize_cli_failure(stderr)}",
            fixture_root=root,
            output_dir=base_dir,
        )

    findings = _load_detect_findings(base_dir)
    if managed_output and not keep_output:
        # The caller still needs the output path when debugging. Keep it only on request.
        pass
    return findings, base_dir


def best_match_for_entry(
    entry: GroundTruthEntry,
    findings: list[NormalizedFinding],
) -> tuple[NormalizedFinding | None, float]:
    best_finding: NormalizedFinding | None = None
    best_weight = 0.0
    for finding in findings:
        weight = match_weight(finding, entry)
        if weight > best_weight:
            best_finding = finding
            best_weight = weight
    return best_finding, best_weight


def validate_entry(
    entry: GroundTruthEntry,
    *,
    fixture_root: Path | None = None,
    fixtures_dir: Path | None = None,
    output_dir: Path | None = None,
    verbose: bool = False,
    keep_output: bool = False,
) -> ValidationResult:
    resolved_fixture_root = fixture_root or resolve_fixture_root(entry, fixtures_dir=fixtures_dir)
    try:
        findings, resolved_output_dir = run_detection(
            entry,
            fixture_root=resolved_fixture_root,
            fixtures_dir=fixtures_dir,
            output_dir=output_dir,
            verbose=verbose,
            keep_output=keep_output,
        )
    except DetectionExecutionError as exc:
        return validation_error_result(
            entry,
            error=str(exc),
            fixture_root=exc.fixture_root,
            output_dir=exc.output_dir,
        )
    return validation_result_from_findings(
        entry,
        findings=findings,
        fixture_root=resolved_fixture_root,
        output_dir=resolved_output_dir,
    )


def validation_result_from_findings(
    entry: GroundTruthEntry,
    *,
    findings: list[NormalizedFinding],
    fixture_root: Path,
    output_dir: Path,
) -> ValidationResult:
    finding, weight = best_match_for_entry(entry, findings)
    expected_detected = entry.label == Label.TRUE_POSITIVE
    matched = weight > 0.0
    passed = matched if expected_detected else not matched
    expected_line = entry.line_numbers[0] if entry.line_numbers else None
    detected_line = finding.line_numbers[0] if finding and finding.line_numbers else None
    detected_file = finding.affected_files[0] if finding and finding.affected_files else None

    if expected_detected:
        if matched:
            match_kind = "detected"
            if abs(weight - 0.5) < 1e-9:
                match_kind = "partially detected"
            detail = f"{match_kind} at line {detected_line}, expected line {expected_line}"
            message = f"{entry.id} | {entry.cwe_id} | {entry.cve_id or 'synthetic'} | PASS ({detail})"
        else:
            message = (
                f"{entry.id} | {entry.cwe_id} | {entry.cve_id or 'synthetic'} | "
                f"FAIL (not detected -- expected {entry.cwe_id} at line {expected_line})"
            )
    else:
        if matched:
            message = (
                f"{entry.id} | {entry.cwe_id} | {entry.cve_id or 'synthetic'} | "
                f"FAIL (unexpected detection at line {detected_line}, expected suppression)"
            )
        else:
            message = (
                f"{entry.id} | {entry.cwe_id} | {entry.cve_id or 'synthetic'} | "
                "PASS (no matching detection)"
            )

    return ValidationResult(
        entry_id=entry.id,
        cwe_id=entry.cwe_id,
        label=entry.label.value,
        passed=passed,
        expected_detected=expected_detected,
        matched=matched,
        match_weight=weight,
        message=message,
        fixture_root=fixture_root,
        output_dir=output_dir,
        expected_line=expected_line,
        detected_line=detected_line,
        detected_file=detected_file,
        matched_finding_id=None if finding is None else finding.id,
        cve_id=entry.cve_id,
        framework=entry.framework,
        complexity=entry.complexity.value,
    )


def validation_error_result(
    entry: GroundTruthEntry,
    *,
    error: str,
    fixture_root: Path,
    output_dir: Path,
) -> ValidationResult:
    return ValidationResult(
        entry_id=entry.id,
        cwe_id=entry.cwe_id,
        label=entry.label.value,
        passed=False,
        expected_detected=entry.label == Label.TRUE_POSITIVE,
        matched=False,
        match_weight=0.0,
        message=f"{entry.id} | {entry.cwe_id} | {entry.cve_id or 'synthetic'} | ERROR ({error})",
        fixture_root=fixture_root,
        output_dir=output_dir,
        expected_line=entry.line_numbers[0] if entry.line_numbers else None,
        detected_line=None,
        detected_file=None,
        matched_finding_id=None,
        cve_id=entry.cve_id,
        framework=entry.framework,
        complexity=entry.complexity.value,
        error=error,
    )


def build_filter_predicate(expressions: list[str]) -> Callable[[GroundTruthEntry], bool]:
    if not expressions:
        return lambda _entry: True

    pairs: list[tuple[str, str]] = []
    for expression in expressions:
        if "=" not in expression:
            raise ValueError(f"invalid filter expression: {expression}")
        key, value = expression.split("=", 1)
        pairs.append((key.strip(), value.strip()))

    def predicate(entry: GroundTruthEntry) -> bool:
        payload = entry.model_dump(mode="json")
        for key, value in pairs:
            current = payload.get(key)
            if current is None or str(current) != value:
                return False
        return True

    return predicate


def cleanup_output_dir(path: Path) -> None:
    shutil.rmtree(path, ignore_errors=True)


__all__ = [
    "DEFAULT_OUTPUT_DIR_NAME",
    "DetectionExecutionError",
    "PIRANESI_VERSION",
    "REPO_ROOT",
    "ValidationResult",
    "best_match_for_entry",
    "build_filter_predicate",
    "cleanup_output_dir",
    "load_ground_truth_entry",
    "resolve_fixture_root",
    "run_detection",
    "validate_entry",
    "validation_error_result",
    "validation_result_from_findings",
]
