# Prompt: Improve Compliance Evidence Bundles

You are working in Piranesi, an AppSec CLI that can generate reports useful for compliance review. Compliance reviewers often need evidence bundles that are structured, reproducible, and easy to attach to audit workflows.

Goal: add or improve compliance evidence bundle generation.

Implementation requirements:

- Inspect reporting and legal/compliance modules.
- Define an evidence bundle format, likely a directory or archive containing report JSON, Markdown/PDF if supported, finding evidence, verification evidence, config snapshot, tool version, scan timestamp, rule/spec versions, and compliance mapping metadata.
- Redact secrets from bundles.
- Add a CLI option or command to generate the bundle.
- Include a manifest file with checksums for bundle contents.
- Add tests for manifest generation, required files, and redaction behavior.
- Update docs with how to use the bundle in audit workflows.

Acceptance criteria:

- A user can produce a self-contained compliance evidence bundle from scan output.
- The bundle is reproducible and includes version metadata.
- Sensitive values are redacted by default.
