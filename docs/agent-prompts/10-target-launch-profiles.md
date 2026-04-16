# Prompt: Add Reusable Target Launch Profiles for Verification

You are working in Piranesi, an AppSec CLI that can verify findings dynamically. To make verification easier, users should be able to define how to start a target app once and reuse that across scans.

Goal: add reusable target launch profiles that define app startup, readiness checks, environment variables, and teardown for verification.

Implementation requirements:

- Inspect configuration loading and verification execution code.
- Design a `target_profiles` or similar config section with fields such as name, command, cwd, env, startup timeout, readiness URL/command, base URL, teardown behavior, and logs path.
- Add CLI selection for a profile if appropriate.
- Ensure profiles are optional and verification still works with direct target URL configuration.
- Capture launch logs and startup failures in verification artifacts.
- Add tests for config parsing and profile selection. Avoid requiring real long-running services in unit tests.
- Update docs with a minimal Express/Node example and a Python example if feasible.

Acceptance criteria:

- Users can run verification against local apps with minimal repeated configuration.
- Failed startup/readiness is reported clearly.
- No profile is required for static scanning.
