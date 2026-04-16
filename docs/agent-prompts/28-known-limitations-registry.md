# Prompt: Add Machine-Readable Known Limitations Registry

You are working in Piranesi, an AppSec CLI with multiple advanced features. Users and maintainers need a machine-readable registry of known limitations so docs, reports, tests, and release hygiene stay aligned.

Goal: add a known limitations registry and surface it in docs/reporting where useful.

Implementation requirements:

- Inspect docs, report rendering, and release hygiene script.
- Create a machine-readable file such as `docs/known-limitations.yml` or `data/known-limitations.json` with entries containing ID, title, affected feature, severity/impact, workaround, status, introduced version, and last reviewed date.
- Add a lightweight validation script or extend release hygiene checks to ensure required fields are present.
- Reference relevant limitations from docs and optionally include applicable limitations in report metadata.
- Add tests for registry validation.
- Avoid letting the registry become an excuse for vague docs: each limitation should be concrete and actionable.

Acceptance criteria:

- Known limitations are tracked in a structured file.
- CI or tests validate the registry shape.
- User-facing docs can be generated or checked against the registry.
