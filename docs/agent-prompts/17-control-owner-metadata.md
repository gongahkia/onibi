# Prompt: Add Control Owner, Team, and System Metadata

You are working in Piranesi, an AppSec CLI whose reports may be consumed by engineering, security, and compliance teams. Findings should be attributable to systems, services, teams, and control owners where users provide that metadata.

Goal: add optional ownership metadata to findings and reports.

Implementation requirements:

- Inspect config loading, report models, and any package/service detection logic.
- Add config fields or auto-detected metadata for service name, system name, team, owner, repository, environment, and control owner.
- Allow mapping paths/packages to owners where feasible.
- Include ownership metadata in `report.json`, Markdown summaries, compliance bundles, and `piranesi explain` where relevant.
- Avoid requiring ownership metadata for basic scans.
- Add tests for config parsing and report rendering.
- Update docs with an example ownership mapping.

Acceptance criteria:

- Reports can answer who owns a finding or control when metadata is configured.
- Missing metadata is represented clearly, not as a crash.
- Ownership metadata is machine-readable.
