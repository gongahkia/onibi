# TODO 14: Add Integration Exporters For Security Workflows

## Goal

Let teams move Piranesi findings into the systems where they already work:
GitHub, GitLab, Jira, Linear, Slack, SARIF code scanning, and generic webhooks.

This should increase adoption by reducing the cost of acting on findings.

## Current State

Host reports are local JSON/Markdown. The legacy source-code pipeline has more
output formats, but host mode does not yet provide workflow integrations.

## Desired CLI

Add:

```bash
piranesi export sarif piranesi-output/host-report.json --output host-report.sarif.json
piranesi export github-issues piranesi-output/host-report.json --dry-run
piranesi export jira piranesi-output/host-report.json --project SEC --dry-run
piranesi export webhook piranesi-output/host-report.json --url https://example.invalid/hook
```

Also support fleet reports:

```bash
piranesi export csv fleet-output/fleet-report.json --output fleet-findings.csv
```

## Integration Principles

- Dry-run by default for externally visible actions.
- Never send raw snapshots unless explicitly requested.
- Include finding IDs, evidence summaries, remediation, severity, risk score, and
  report path.
- Preserve suppressions and avoid creating tickets for suppressed findings.
- Deduplicate by stable finding ID and target.

## Implementation Notes

Create:

```text
src/piranesi/exporters/
  __init__.py
  common.py
  sarif.py
  csv.py
  webhook.py
  github.py
  jira.py
```

Use adapter-style interfaces:

```python
class FindingExporter(Protocol):
    def export(self, report: HostPostureReport | FleetReport) -> ExportResult: ...
```

## Security Requirements

- Redact sensitive host metadata by default for outbound integrations.
- Show a preview before ticket creation unless `--yes` is supplied.
- Log integration actions in an audit event file.
- Do not embed LLM prompts or raw command output in exported tickets.

## Tests

Add tests for:

- SARIF export from host findings.
- CSV export from host and fleet reports.
- Webhook payload shape with redacted metadata.
- Ticket deduplication key generation.
- Dry-run creates no external side effects.

## Documentation

Add:

```text
docs/integrations.md
```

Include examples for:

- GitHub issue creation.
- Jira ticket creation.
- Slack/webhook summary.
- SARIF upload in CI.

## Acceptance Criteria

- Host findings can be exported to common workflow formats.
- External integrations are safe-by-default and auditable.
- Redaction is applied to outbound payloads.
- Suppressed findings are not exported as active work items.

## Out Of Scope

- Full two-way ticket sync.
- OAuth application hosting.
- Vendor-specific asset inventory sync.

## Validation Commands

```bash
uv run pytest tests/test_exporters.py tests/test_host_posture.py
uv run piranesi export sarif tests/fixtures/reports/host-report.json --output /tmp/host.sarif.json
```

