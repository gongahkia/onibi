# Prompt: Add First-Party CI Provider Templates or Clarify Provider Boundary

You are working in Piranesi, an AppSec CLI intended for CI use. The repository should either provide first-party GitHub/GitLab templates or clearly document a provider-agnostic integration boundary.

Goal: improve CI onboarding by adding provider templates or explicitly documenting the boundary.

Implementation requirements:

- Inspect `.github/workflows/`, `docs/ci-integration.md`, and CLI options relevant to CI.
- If adding templates, include GitHub Actions and GitLab CI examples that run deterministic scans, upload artifacts, and fail according to configured severity/baseline policy.
- If not adding templates, improve provider-agnostic docs with exact commands, expected artifacts, exit codes, and environment variables.
- Ensure examples avoid requiring LLM credentials by default.
- Add tests or hygiene checks if templates are generated or referenced.

Acceptance criteria:

- A user can copy a CI template or command block and get a useful first scan.
- CI behavior around exit codes, outputs, baselines, and artifacts is explicit.
- Documentation does not imply unsupported provider behavior.
