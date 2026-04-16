# Prompt: Add Framework-Aware Receiver and Type Disambiguation

You are working in Piranesi, a Python AppSec CLI that scans code and detects flows into vulnerability sinks. Some sink specs can overmatch because the same method name means different things depending on receiver type or framework. A known example is SSRF detection accidentally matching Express route registration such as `app.get(...)`, even though the sink should be outbound HTTP calls such as `axios.get(...)`, `fetch(...)`, or Node HTTP client calls.

Goal: add framework-aware receiver/type disambiguation so sink matching considers receiver identity and framework context where possible.

Relevant areas to inspect:

- `src/piranesi/scan/specs.py`
- `src/piranesi/detect/flows.py`
- Tests under `tests/test_scan/` and `tests/test_detect/`
- Rule/spec files that define sink APIs

Implementation requirements:

- Identify how source/sink specs currently represent APIs and receiver names.
- Extend the representation to allow receiver constraints, excluded receivers, or framework-specific context.
- Ensure SSRF specs do not match Express route registration methods such as `app.get`, `app.post`, `router.get`, or `router.post`.
- Preserve legitimate outbound HTTP SSRF matches.
- Prefer simple static heuristics over brittle complexity: imported module names, known receiver declarations, and framework markers are enough for a first version.
- Add tests for both false-positive prevention and true-positive retention.
- Update docs or rule authoring guidance if the spec schema changes.

Acceptance criteria:

- Express routing methods are not treated as SSRF sinks.
- Common outbound HTTP calls remain detected.
- The spec format explains receiver constraints clearly.
- Tests demonstrate the intended behavior.
