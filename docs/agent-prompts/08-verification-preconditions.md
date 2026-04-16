# Prompt: Make Verification Preconditions Explicit

You are working in Piranesi, an AppSec CLI that can verify findings dynamically. Users need clear reasons when verification is skipped or inconclusive. Today, missing target URL, auth, route mapping, service startup, or safe proof configuration may not be explicit enough.

Goal: model and report verification preconditions as first-class artifacts.

Implementation requirements:

- Inspect `src/piranesi/verify/` and report rendering code.
- Define a precondition model that can capture required target URL, route, HTTP method, auth/cookies, request body, runtime service, callback server, proof mode, or other prerequisites.
- For each finding selected for verification, record which preconditions are satisfied, missing, inferred, or user-provided.
- Update `report.json`, `report.md`, and `piranesi explain` to show skipped/inconclusive reasons clearly.
- Ensure deterministic/no-execute mode reports verification as skipped with an explicit reason.
- Add tests for missing target URL, missing route mapping, and no-execute behavior.

Acceptance criteria:

- A skipped verification attempt always has a concrete, user-actionable reason.
- Users can see what to configure to make verification possible.
- Precondition output is machine-readable.
