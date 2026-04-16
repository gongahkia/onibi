# Prompt: Add First-Run Golden Path End-to-End Fixture

You are working in Piranesi, an AppSec CLI whose value proposition depends on working with minimal configuration. The project needs a golden path test that represents a first-time user scanning a small vulnerable app.

Goal: add an end-to-end first-run fixture that validates the easiest successful user journey.

Implementation requirements:

- Inspect examples, CLI tests, and existing vulnerable fixtures.
- Create or reuse a small vulnerable app fixture that can be scanned quickly without external services.
- Test a first-run workflow such as `piranesi init`, `piranesi doctor`, and a safe deterministic scan that produces report artifacts.
- Verify expected outputs exist, such as `report.json`, `report.md`, and any explainable finding ID.
- Avoid requiring optional dependencies that are unavailable in the base environment unless the test is clearly marked/integration-only.
- Add docs that align with the tested golden path.

Acceptance criteria:

- The documented quickstart is backed by a test.
- The test catches regressions in out-of-the-box usability.
- The fixture is small and deterministic.
