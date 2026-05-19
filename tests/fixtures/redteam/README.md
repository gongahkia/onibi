# Red-Team Validation Fixtures

These fixtures are sanitized, authorized local-lab artifacts used by the red-team
workspace validation test. They do not contain client data, credentials, real C2
infrastructure, or third-party target output.

- `notes/operator-note.md`: operator note from a synthetic local-lab exercise.
- `transcripts/operator-terminal.txt`: sanitized terminal transcript.
- `c2/mock-c2-events.jsonl`: mock C2-style event log for handoff modeling only.
- `screenshots/portal-login.svg`: synthetic screenshot-style SVG.

Scanner fixtures are reused from `tests/fixtures/pentest/`; those are localhost
exports with separate provenance metadata.
