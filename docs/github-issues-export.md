# GitHub Issues Export

Piranesi can export selected local findings to GitHub Issues as a one-way handoff
workflow. The exporter follows the threat model in
[`github-issues-export-threat-model.md`](github-issues-export-threat-model.md).

Preview an issue payload without calling GitHub:

```bash
piranesi integrations github-issues \
  --workspace ./workspace \
  --repo owner/repo \
  --finding-id finding:abc123 \
  --dry-run \
  --json
```

Create live issues only after reviewing dry-run output:

```bash
GITHUB_TOKEN=... piranesi integrations github-issues \
  --workspace ./workspace \
  --repo owner/repo \
  --finding-id finding:abc123 \
  --live
```

Defaults:

- Raw evidence is omitted from every GitHub issue body.
- Affected assets are redacted unless `--include-assets` is passed.
- Generated labels include `piranesi`, `severity:<level>`, and `status:<state>`.
- The command requires explicit `--finding-id` selection.
- Live export requires `GITHUB_TOKEN` or `GH_TOKEN`; tokens are not written into
  workspace artifacts or output.

This is not a bidirectional sync. Piranesi does not ingest GitHub comments, map
issue status back to finding status, or upload raw evidence attachments.
