# Prompt: Persist Rich Verification Evidence

You are working in Piranesi, an AppSec CLI that verifies security findings. Verification output should contain enough evidence for humans and automation to audit what happened without exposing secrets or oversized payloads.

Goal: persist rich, sanitized verification evidence for every attempted finding.

Implementation requirements:

- Inspect verification models and report renderer.
- Add structured fields for attempted URL/route, method, payload class, template ID, status code, response diff summary, timing summary, error signature, headers subset, body excerpt hash/preview, screenshots if already supported, and redaction status.
- Redact secrets, authorization headers, cookies, and configured sensitive values.
- Add artifact paths where full evidence is written, if the project already uses artifact directories.
- Update `report.json`, `report.md`, and `piranesi explain` to reference evidence concisely.
- Add tests for evidence serialization and redaction.

Acceptance criteria:

- Verified and inconclusive attempts include useful evidence.
- Sensitive values are not leaked into reports.
- Evidence is machine-readable and stable enough for CI review.
