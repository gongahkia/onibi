# TODO 19: Add Local Web UI And Review Workbench

## Goal

Make Piranesi easier to review, explain, and share without requiring users to
inspect raw JSON or Markdown. A local web UI can drive adoption while preserving
the local-first trust boundary.

## Current State

Host mode writes JSON and Markdown only. `todo1.md` proposes a static dashboard,
but a broader review workbench would support multi-report comparison, suppression
review, remediation planning, and fleet triage.

## Desired CLI

Add:

```bash
piranesi ui piranesi-output
piranesi ui fleet-output
piranesi ui --watch piranesi-output
```

Behavior:

- Start a local-only server bound to `127.0.0.1`.
- Open a browser optionally with `--open`.
- Never upload report data.
- Support static export where possible.

## UI Capabilities

Initial workbench views:

- Host report overview.
- Findings table with filters.
- Finding detail drawer.
- Evidence timeline/inventory.
- Collection health view.
- Top actions view.
- Suppression review.
- Before/after diff view if `todo16.md` has landed.
- Fleet summary if `todo8.md` has landed.

## Implementation Notes

Keep the server small and local:

```text
src/piranesi/ui_server/
```

Options:

- Static frontend plus Python local server.
- No external CDN dependencies.
- Read reports from disk.
- Avoid writing user state unless explicitly requested.

## Security Requirements

- Bind to localhost by default.
- Require explicit `--host 0.0.0.0` for network exposure.
- Show a warning when exposed beyond localhost.
- Do not serve arbitrary filesystem paths.
- Avoid embedding raw secrets from snapshots in UI state.

## Tests

Add tests for:

- Server loads host report.
- API returns redacted report summary by default.
- Findings filters work.
- Localhost binding is default.
- Invalid report path fails safely.

Add Playwright smoke tests for the built UI if a frontend stack is introduced.

## Documentation

Add:

```text
docs/local-ui.md
```

Update README with screenshots or generated images once the UI exists.

## Acceptance Criteria

- Users can inspect reports in a local browser.
- UI works for host reports and is ready for fleet reports.
- No data leaves the machine.
- Security defaults are documented and tested.

## Out Of Scope

- Multi-user authentication.
- Hosted SaaS dashboard.
- Long-running database.

## Validation Commands

```bash
uv run pytest tests/test_ui_server.py
uv run piranesi ui tests/fixtures/reports/host-report --no-open
```

