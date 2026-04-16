# Prompt: Improve Rule Authoring UX

You are working in Piranesi, an AppSec CLI that supports custom source/sink/security rules. Rule authoring should be approachable and validated before users run full scans.

Goal: improve the rule authoring experience with validation, scaffolding, diagnostics, and examples.

Implementation requirements:

- Inspect rule loading, rule schemas, docs, and CLI commands.
- Add or improve `piranesi rules validate` or equivalent command if it does not exist.
- Provide actionable error messages for invalid rule fields, unknown categories, malformed patterns, duplicate IDs, and unsupported schema versions.
- Add scaffolding for a new rule pack if appropriate.
- Add tests for valid and invalid rule packs.
- Update docs with a short tutorial: create a rule, validate it, run it against a fixture, inspect results.

Acceptance criteria:

- Users can validate custom rules without running a full scan.
- Error messages point to the offending file/field.
- Rule examples and schema expectations are documented.
