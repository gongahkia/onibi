from __future__ import annotations

import json
import os
import re
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any, Literal, Protocol

from piranesi.report.redteam import build_red_team_report
from piranesi.workspace import WorkspaceState

SlackEventType = Literal["report-ready", "delivered", "retest-ready", "verification-failed"]
ALLOWED_EVENTS: tuple[SlackEventType, ...] = (
    "report-ready",
    "delivered",
    "retest-ready",
    "verification-failed",
)


class SlackNotificationError(ValueError):
    """Raised when a Slack notification cannot be prepared or sent."""


class SlackWebhookClient(Protocol):
    def post(self, payload: dict[str, Any]) -> None: ...


@dataclass(frozen=True, slots=True)
class SlackNotificationResult:
    event: SlackEventType
    dry_run: bool
    payload: dict[str, Any]
    sent: bool

    def as_payload(self) -> dict[str, Any]:
        return {
            "event": self.event,
            "dry_run": self.dry_run,
            "payload": self.payload,
            "sent": self.sent,
        }


class SlackIncomingWebhookClient:
    def __init__(self, webhook_url: str, *, timeout_seconds: int = 20) -> None:
        if not webhook_url.startswith("https://"):
            raise SlackNotificationError("Slack webhook URL must use https")
        self._webhook_url = webhook_url
        self._timeout_seconds = timeout_seconds

    def post(self, payload: dict[str, Any]) -> None:
        request = urllib.request.Request(  # noqa: S310
            self._webhook_url,
            data=json.dumps(payload).encode("utf-8"),
            headers={
                "Content-Type": "application/json",
                "User-Agent": "piranesi-slack-notify",
            },
            method="POST",
        )
        try:
            with urllib.request.urlopen(  # noqa: S310
                request,
                timeout=self._timeout_seconds,
            ) as response:
                if response.status < 200 or response.status >= 300:
                    raise SlackNotificationError(f"Slack webhook returned HTTP {response.status}")
        except urllib.error.HTTPError as exc:
            raise SlackNotificationError(f"Slack webhook returned HTTP {exc.code}") from exc
        except urllib.error.URLError as exc:
            raise SlackNotificationError(f"Slack webhook request failed: {exc.reason}") from exc


def send_slack_notification(
    state: WorkspaceState,
    *,
    event: SlackEventType,
    dry_run: bool = True,
    include_engagement: bool = False,
    webhook_url: str | None = None,
    client: SlackWebhookClient | None = None,
) -> SlackNotificationResult:
    if event not in ALLOWED_EVENTS:
        raise SlackNotificationError(f"unsupported Slack notification event: {event}")
    payload = slack_notification_payload(
        state,
        event=event,
        include_engagement=include_engagement,
    )
    if dry_run:
        return SlackNotificationResult(event=event, dry_run=True, payload=payload, sent=False)
    if client is None:
        url = webhook_url or slack_webhook_url_from_environment()
        if url is None:
            raise SlackNotificationError(
                "live Slack notification requires --webhook-url, "
                "PIRANESI_SLACK_WEBHOOK_URL, or SLACK_WEBHOOK_URL"
            )
        client = SlackIncomingWebhookClient(url)
    client.post(payload)
    return SlackNotificationResult(event=event, dry_run=False, payload=payload, sent=True)


def slack_notification_payload(
    state: WorkspaceState,
    *,
    event: SlackEventType,
    include_engagement: bool = False,
) -> dict[str, Any]:
    report = build_red_team_report(state, redact_sensitive_evidence=True)
    summary = report.executive_summary
    engagement = report.engagement
    project_label = _project_label(engagement, include_engagement=include_engagement)
    text = (
        f"Piranesi {event}: {project_label} | "
        f"{summary['finding_count']} findings, "
        f"{summary['evidence_count']} evidence records"
    )
    fields = [
        {"type": "mrkdwn", "text": f"*Event*\n{event}"},
        {"type": "mrkdwn", "text": f"*Findings*\n{summary['finding_count']}"},
        {"type": "mrkdwn", "text": f"*High/Critical*\n{_high_critical_count(report.findings)}"},
        {"type": "mrkdwn", "text": f"*Evidence*\n{summary['evidence_count']} records"},
        {"type": "mrkdwn", "text": f"*IOCs*\n{summary['ioc_count']}"},
        {"type": "mrkdwn", "text": "*Raw Evidence*\nomitted"},
    ]
    return {
        "text": text,
        "blocks": [
            {
                "type": "section",
                "text": {
                    "type": "mrkdwn",
                    "text": f"*Piranesi {event}*\n{project_label}",
                },
            },
            {"type": "section", "fields": fields},
            {
                "type": "context",
                "elements": [
                    {
                        "type": "mrkdwn",
                        "text": (
                            "Summary-only notification. Review the local workspace for evidence."
                        ),
                    }
                ],
            },
        ],
    }


def slack_webhook_url_from_environment() -> str | None:
    for name in ("PIRANESI_SLACK_WEBHOOK_URL", "SLACK_WEBHOOK_URL"):
        value = os.environ.get(name)
        if value:
            return value
    return None


def _project_label(engagement: dict[str, Any], *, include_engagement: bool) -> str:
    if not include_engagement:
        return "[redacted engagement]"
    client = _redact_tokenish_text(str(engagement.get("client") or "client"))
    project = _redact_tokenish_text(str(engagement.get("project") or "project"))
    return f"{client} / {project}"


def _high_critical_count(findings: list[dict[str, Any]]) -> int:
    return len([item for item in findings if item.get("severity") in {"high", "critical"}])


def _redact_tokenish_text(value: str) -> str:
    return re.sub(
        r"(?i)(token|secret|password|api[_-]?key|cookie)[=:]\S+",
        "[redacted]",
        value,
    )


__all__ = [
    "ALLOWED_EVENTS",
    "SlackEventType",
    "SlackIncomingWebhookClient",
    "SlackNotificationError",
    "SlackNotificationResult",
    "send_slack_notification",
    "slack_notification_payload",
    "slack_webhook_url_from_environment",
]
