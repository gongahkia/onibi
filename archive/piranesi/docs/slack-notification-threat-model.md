# Slack Notification Threat Model

Status: accepted implementation gate for one-way Slack delivery notifications.

## Scope

Piranesi may send one-way Slack webhook notifications for local workflow events:
report-ready, delivered, retest-ready, and verification-failed. Notifications are
summary-only by default and are never used to control scanners, target interaction,
workspace mutation, or report approval.

## Assets At Risk

- Slack webhook URLs and signing metadata.
- Client names, project names, target hostnames, URLs, IP addresses, and paths.
- Finding titles, severities, status counts, report artifact names, and custody
  verification status.
- Local report paths, evidence paths, and chain-of-custody hashes.

## Trust Boundaries

- Slack is external to the local-first workspace boundary.
- Webhook URLs are bearer secrets and must not be stored in workspace artifacts,
  reports, audit logs, test fixtures, or generated payload summaries.
- Slack notifications are hints for humans. The local workspace remains the source
  of truth for report and custody state.

## Allowed Event Types

- `report-ready`: report artifacts are available for review.
- `delivered`: an operator marked the handoff delivered.
- `retest-ready`: a retest package or comparison is ready for review.
- `verification-failed`: local signing or custody verification failed.

## Payload Rules

Default Slack payloads may include:

- Event type.
- Client/project labels when asset redaction is disabled.
- Count summaries: findings, critical/high counts, report artifacts, IOCs, and
  objective status counts.
- Manifest status and local-only reminder text.

Default Slack payloads must not include:

- Raw evidence snippets, screenshots, transcripts, payloads, or request/response
  bodies.
- Secret values, tokens, cookies, session identifiers, or webhook URLs.
- Raw local filesystem paths outside stable relative artifact references.
- Full hostnames/IPs when redaction is enabled.

## Webhook Handling

- The webhook URL is supplied per command or via environment configuration.
- Piranesi does not persist webhook URLs.
- Dry-run mode must produce the same redacted payload shape without sending.
- Tests must cover webhook redaction, summary-only payloads, dry-run behavior, and
  non-2xx failure handling.

## Failure Behavior

- Dry-run never performs network I/O.
- 4xx Slack responses are non-retryable and should fail with a clear message.
- 429 and 5xx responses may be retried by a future bounded-backoff client; the
  initial implementation may fail closed.
- Notification failure does not change report, delivery, retest, or signing state.
- Partial delivery semantics are not needed because each command sends one event.

## Non-Goals

- Slack bot commands.
- Bidirectional Slack workflow approvals.
- Uploading report or evidence files to Slack.
- Multi-channel routing policy.
- Automatic delivery status mutation after a successful Slack post.
