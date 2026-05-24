from __future__ import annotations

import json
from dataclasses import dataclass
from email.message import EmailMessage
from pathlib import Path
from typing import Any

from piranesi.report.redteam import build_red_team_report
from piranesi.workspace import WorkspaceState, file_sha256, utc_now, workspace_path


class EmailHandoffError(ValueError):
    """Raised when an email handoff draft cannot be created safely."""


@dataclass(frozen=True, slots=True)
class EmailHandoffDraft:
    path: Path
    manifest_path: Path
    subject: str
    recipients: list[str]
    artifact_references: list[dict[str, str]]

    def as_payload(self) -> dict[str, Any]:
        return {
            "path": str(self.path),
            "manifest_path": str(self.manifest_path),
            "subject": self.subject,
            "recipients": self.recipients,
            "artifact_references": self.artifact_references,
            "sent": False,
        }


def write_email_handoff_draft(
    state: WorkspaceState,
    *,
    recipients: list[str],
    cc: list[str] | None = None,
    subject: str | None = None,
    artifact_paths: list[Path] | None = None,
    output_path: Path | None = None,
) -> EmailHandoffDraft:
    if not recipients:
        raise EmailHandoffError("at least one --to recipient is required")
    report = build_red_team_report(state, redact_sensitive_evidence=True)
    references = _artifact_references(
        state.root,
        artifact_paths or _default_artifact_paths(state.root),
    )
    email_subject = subject or _default_subject(report.engagement)
    message = EmailMessage()
    message["To"] = ", ".join(recipients)
    if cc:
        message["Cc"] = ", ".join(cc)
    message["Subject"] = email_subject
    message["X-Piranesi-Handoff"] = "local-draft"
    message.set_content(_email_body(report.model_dump(mode="json"), references))
    output = output_path or workspace_path(
        state.root,
        Path("reports") / "email-handoff-draft.eml",
        allowed_roots=("reports",),
    )
    if not output.is_absolute():
        output = workspace_path(state.root, output, allowed_roots=("reports",))
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_bytes(bytes(message))
    manifest_path = _write_handoff_manifest(
        output.parent / "email-handoff-manifest.json",
        subject=email_subject,
        recipients=recipients,
        cc=cc or [],
        draft_path=output,
        workspace_root=state.root,
        references=references,
    )
    return EmailHandoffDraft(
        path=output,
        manifest_path=manifest_path,
        subject=email_subject,
        recipients=recipients,
        artifact_references=references,
    )


def _default_subject(engagement: dict[str, Any]) -> str:
    client = engagement.get("client") or "Piranesi"
    project = engagement.get("project") or "engagement"
    return f"{client} {project} handoff package"


def _email_body(report: dict[str, Any], references: list[dict[str, str]]) -> str:
    summary = report["executive_summary"]
    artifacts = (
        "\n".join(f"- {item['path']} (sha256: {item['sha256']})" for item in references)
        or "- No local report artifacts found yet."
    )
    return "\n".join(
        [
            "Hello,",
            "",
            "A local Piranesi handoff draft is ready for operator review.",
            "",
            "Summary:",
            f"- Findings: {summary['finding_count']}",
            f"- Evidence records: {summary['evidence_count']}",
            f"- Timeline events: {summary['timeline_event_count']}",
            f"- Objectives: {summary['objective_count']}",
            f"- Procedures: {summary['procedure_count']}",
            f"- IOCs: {summary['ioc_count']}",
            "",
            "Local artifacts referenced:",
            artifacts,
            "",
            "Sensitive evidence content and raw artifacts are not embedded in this email draft.",
            "Review the local workspace and attachments before sending through an email client.",
            "",
            "This draft was generated locally and was not sent automatically.",
            "",
        ]
    )


def _default_artifact_paths(workspace_root: Path) -> list[Path]:
    reports_root = workspace_root / "reports"
    if not reports_root.is_dir():
        return []
    return sorted(path for path in reports_root.iterdir() if path.is_file())


def _artifact_references(
    workspace_root: Path,
    artifact_paths: list[Path],
) -> list[dict[str, str]]:
    references: list[dict[str, str]] = []
    for artifact in artifact_paths:
        path = artifact
        if not path.is_absolute():
            path = workspace_path(workspace_root, path, allowed_roots=("reports",))
        try:
            relative = path.resolve(strict=True).relative_to(workspace_root)
        except (OSError, ValueError) as exc:
            raise EmailHandoffError(f"artifact must be inside workspace: {artifact}") from exc
        if not path.is_file():
            raise EmailHandoffError(f"artifact is not a file: {artifact}")
        references.append(
            {
                "path": relative.as_posix(),
                "sha256": file_sha256(path),
            }
        )
    return references


def _write_handoff_manifest(
    path: Path,
    *,
    subject: str,
    recipients: list[str],
    cc: list[str],
    draft_path: Path,
    workspace_root: Path,
    references: list[dict[str, str]],
) -> Path:
    relative_draft = draft_path.resolve(strict=True).relative_to(workspace_root)
    payload = {
        "schema_version": "piranesi.handoff-manifest.v1",
        "generated_at": utc_now(),
        "delivery_channel": "email-draft",
        "sent": False,
        "subject": subject,
        "recipients": recipients,
        "cc": cc,
        "draft": {
            "path": relative_draft.as_posix(),
            "sha256": file_sha256(draft_path),
        },
        "artifacts": references,
        "sensitive_content_embedded": False,
    }
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return path


__all__ = [
    "EmailHandoffDraft",
    "EmailHandoffError",
    "write_email_handoff_draft",
]
