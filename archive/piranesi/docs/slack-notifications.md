# Slack Notifications

Piranesi can preview or send summary-only Slack webhook notifications for local
workflow events. The implementation follows
[`slack-notification-threat-model.md`](slack-notification-threat-model.md).

Preview a payload without network access:

```bash
piranesi integrations slack-notify \
  --workspace ./workspace \
  --event report-ready \
  --dry-run \
  --json
```

Send a live webhook notification:

```bash
PIRANESI_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/... \
  piranesi integrations slack-notify \
  --workspace ./workspace \
  --event delivered \
  --live
```

Defaults:

- Dry-run mode never performs network I/O.
- Client/project labels are redacted unless `--include-engagement` is passed.
- Raw evidence, screenshots, transcripts, payloads, request/response snippets, and
  webhook URLs are never included in payload output.
- Supported events are `report-ready`, `delivered`, `retest-ready`, and
  `verification-failed`.
- Notification failure does not mutate workspace delivery or report state.
