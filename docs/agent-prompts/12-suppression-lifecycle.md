# Prompt: Improve Suppression Lifecycle

You are working in Piranesi, an AppSec CLI that supports suppressed findings. Suppressions need lifecycle management so they do not become permanent silent risk.

Goal: improve suppression handling with expiry, reason codes, ownership, validation, and stale suppression reporting.

Implementation requirements:

- Inspect suppression models, config, report rendering, and CLI commands.
- Add suppression metadata such as reason, owner, created date, expires date, ticket/reference, and scope.
- Detect stale suppressions that no longer match any finding.
- Detect expired suppressions and surface them as warnings or failures depending on config.
- Add or update CLI support for listing suppressions and validating suppression files.
- Ensure reports include suppression summaries without hiding risk completely.
- Add tests for active, expired, invalid, and stale suppressions.
- Update docs with suppression best practices.

Acceptance criteria:

- Suppressions are auditable and time-bound.
- CI can fail on expired or invalid suppressions if configured.
- Users can identify stale suppressions after code changes.
