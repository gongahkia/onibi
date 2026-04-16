# Prompt: Add Advisory Database CLI Workflow

You are working in Piranesi, an AppSec CLI with advisory-related modules. Users need clear commands for managing advisory data used in scans and reports.

Goal: add a first-class advisory DB workflow to the CLI.

Implementation requirements:

- Inspect `src/piranesi/advisory/` and existing CLI commands.
- Add commands such as `piranesi advisory status`, `piranesi advisory update`, `piranesi advisory import`, and `piranesi advisory search` if they fit existing architecture.
- Record advisory DB version, source, last updated time, checksum, and freshness warnings.
- Keep network access optional and explicit. Offline scans should degrade gracefully with stale/missing advisory data warnings.
- Add tests for command behavior using local fixture data.
- Update docs with online and offline workflows.

Acceptance criteria:

- Users can inspect and update advisory data intentionally.
- Reports include advisory DB freshness metadata when relevant.
- Offline behavior is deterministic and documented.
