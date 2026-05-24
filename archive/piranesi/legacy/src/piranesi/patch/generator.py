"""Patch generation: build LLM-driven fix diffs for confirmed findings."""

from __future__ import annotations

import difflib
import logging
import re
from pathlib import Path

from pydantic import BaseModel, ConfigDict, ValidationError

from piranesi.llm.prompts import patcher_fix
from piranesi.llm.provider import LLMProvider
from piranesi.llm.router import TokenBudgetExceededError
from piranesi.models import CandidateFinding, ConfirmedFinding, PatchResult

_CWE_PATTERN = re.compile(r"(CWE-\d+)", re.IGNORECASE)
_CWE_TITLES = {
    "CWE-22": "Path Traversal",
    "CWE-78": "Command Injection",
    "CWE-79": "Cross-Site Scripting",
    "CWE-89": "SQL Injection",
    "CWE-918": "Server-Side Request Forgery",
}
_logger = logging.getLogger(__name__)


class PatchPayload(BaseModel):
    model_config = ConfigDict(extra="forbid")
    patched_code: str
    explanation: str
    mitigation_type: str


def generate_patches(
    findings: list[ConfirmedFinding],
    provider: LLMProvider,
    target_dir: Path,
) -> list[PatchResult]:
    """Iterate over confirmed findings, call LLM for each, return patch results."""
    patches: list[PatchResult] = []
    for index, finding in enumerate(findings):
        source_path = Path(finding.finding.finding.sink.location.file)
        original_code = _patch_source_text(source_path, finding)
        try:
            response = provider.complete(
                stage="patcher",
                messages=patcher_fix.render(
                    vuln_description=_finding_summary(finding.finding.finding),
                    cwe_id=_extract_cwe_id(finding.finding.finding.vuln_class),
                    vulnerable_code=original_code,
                    language=_language_for_path(source_path),
                ),
                max_tokens=1024,
            )
        except TokenBudgetExceededError as exc:
            remaining = len(findings) - index
            _logger.warning(
                "patch: token budget exhausted after %d generated patch(es); "
                "skipping %d remaining confirmed finding(s)",
                len(patches),
                remaining,
                extra={
                    "event": "patch_token_budget_exhausted",
                    "generated_patches": len(patches),
                    "remaining_findings": remaining,
                    "error": str(exc),
                },
            )
            break
        payload = _parse_patch_payload(response.content)
        patch_diff = _unified_diff(
            original_code=original_code,
            patched_code=payload.patched_code,
            file_path=source_path,
            target_dir=target_dir,
        )
        patches.append(
            PatchResult(
                finding=finding,
                patch_diff=patch_diff,
                patch_verified=False,
                patch_explanation=payload.explanation,
            )
        )
    return patches


def _patch_source_text(source_path: Path, finding: ConfirmedFinding) -> str:
    if source_path.exists():
        return source_path.read_text(encoding="utf-8")
    snippets: list[str] = []
    candidate = finding.finding.finding
    snippets.append(candidate.source.location.snippet)
    snippets.extend(step.location.snippet for step in candidate.taint_path)
    snippets.append(candidate.sink.location.snippet)
    return "\n".join(snippets)


def _parse_patch_payload(content: str) -> PatchPayload:
    try:
        return PatchPayload.model_validate_json(content)
    except (ValidationError, ValueError):
        return PatchPayload(
            patched_code=content,
            explanation="Patch model returned an unstructured response.",
            mitigation_type="other",
        )


def _unified_diff(
    *,
    original_code: str,
    patched_code: str,
    file_path: Path,
    target_dir: Path,
) -> str:
    rendered_path = _relative_render_path(file_path, target_dir)
    original_lines = original_code.splitlines(keepends=True)
    patched_lines = patched_code.splitlines(keepends=True)
    diff = difflib.unified_diff(
        original_lines,
        patched_lines,
        fromfile=f"a/{rendered_path}",
        tofile=f"b/{rendered_path}",
    )
    rendered = "".join(diff)
    return rendered or f"--- a/{rendered_path}\n+++ b/{rendered_path}\n"


def _relative_render_path(file_path: Path, target_dir: Path) -> str:
    try:
        return (
            file_path.resolve(strict=False).relative_to(target_dir.resolve(strict=False)).as_posix()
        )
    except ValueError:
        return file_path.as_posix()


def _finding_summary(finding: CandidateFinding) -> str:
    title = _CWE_TITLES.get(_extract_cwe_id(finding.vuln_class), finding.vuln_class)
    return f"{title} from {finding.source.source_type} to {finding.sink.api_name}"


def _extract_cwe_id(vuln_class: str) -> str:
    match = _CWE_PATTERN.search(vuln_class)
    if match is None:
        return vuln_class
    return match.group(1).upper()


def _language_for_path(path: Path) -> str:
    suffix = path.suffix.lower()
    if suffix == ".tsx":
        return "tsx"
    if suffix == ".ts":
        return "typescript"
    if suffix == ".jsx":
        return "jsx"
    if suffix == ".js":
        return "javascript"
    return "text"
