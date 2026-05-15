# Integration Exports

Piranesi can export host and fleet findings into common security workflows without
requiring LLM credentials. Exporters use deterministic host report fields and skip
suppressed findings by default so accepted risk does not become new active work.
See [privacy and data handling](privacy-data-handling.md) for the broader local
storage, redaction, and outbound-network model.

## File Exports

SARIF is useful for CI systems and security dashboards that understand SARIF 2.1.0:

```bash
piranesi export sarif piranesi-output/host-report.json \
  --output host-report.sarif.json
```

CSV works for both host and fleet reports. Fleet exports resolve per-host
`hosts/<name>/host-report.json` files when they are present beside
`fleet-report.json`.

```bash
piranesi export csv fleet-output/fleet-report.json \
  --output fleet-findings.csv
```

Both exporters write an `audit-log.jsonl` event next to the exported artifact.

## Webhooks And Slack

Webhook delivery is dry-run by default. The default payload contains a redacted
summary, active findings, evidence summaries, remediation text, risk scores, and
dedupe keys. Raw host snapshots are not included unless explicitly requested.

```bash
piranesi export webhook piranesi-output/host-report.json \
  --url https://example.invalid/hook
```

To send the payload:

```bash
piranesi export webhook piranesi-output/host-report.json \
  --url "$SLACK_WEBHOOK_URL" \
  --send \
  --yes
```

Use `--include-raw-snapshot` only for trusted internal receivers. Use
`--no-redact` only when the receiver is approved for host identifiers and
sensitive metadata.

## GitHub Issues

GitHub issue creation is also dry-run by default:

```bash
piranesi export github-issues piranesi-output/host-report.json \
  --repo owner/repo \
  --dry-run
```

Real creation requires `--create --yes`, a repository, and `GITHUB_TOKEN`:

```bash
GITHUB_TOKEN=ghp_... \
piranesi export github-issues piranesi-output/host-report.json \
  --repo owner/repo \
  --create \
  --yes
```

Each issue body includes a deterministic dedupe key derived from target and
finding ID. Piranesi does not perform two-way ticket synchronization.

## Jira

Jira ticket previews require only a project key:

```bash
piranesi export jira piranesi-output/host-report.json \
  --project SEC \
  --dry-run
```

Real creation uses Jira Cloud's REST API and requires a base URL plus credentials:

```bash
JIRA_BASE_URL=https://example.atlassian.net \
JIRA_EMAIL=security@example.com \
JIRA_API_TOKEN=... \
piranesi export jira piranesi-output/host-report.json \
  --project SEC \
  --create \
  --yes
```

## Security Defaults

- Externally visible integrations run in dry-run mode unless `--create` or
  `--send` is paired with `--yes`.
- Raw snapshots are excluded from outbound payloads by default.
- Sensitive host metadata is redacted by default.
- Suppressed findings are omitted from active work-item exporters.
- Every integration action writes an audit event file.
