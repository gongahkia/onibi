# Prompt: Improve Finding Explanation Metadata and Confidence Math

You are working in Piranesi, an AppSec CLI that includes `piranesi explain <finding-id>` for detailed finding explanations. Current explanations are useful but should be more transparent about matched sources/sinks, sanitizers, propagation, cluster membership, and confidence scoring.

Goal: improve explanation metadata so users can understand exactly why a finding exists and why it has its current confidence/severity.

Relevant areas:

- `src/piranesi/cli.py`, especially `piranesi explain`
- `src/piranesi/report/renderer.py`
- Detection and triage models under `src/piranesi/models/`, `src/piranesi/detect/`, and `src/piranesi/triage/`

Implementation requirements:

- Add explanation fields for matched source spec, matched sink spec, sanitizers considered, sanitizers observed, propagation path summary, verification state, and confidence contributors.
- If confidence is currently a single opaque value, introduce a structured breakdown such as static reachability, source quality, sink quality, sanitizer absence/presence, triage result, verification result, and suppressions.
- Keep backward compatibility by adding fields rather than removing existing ones where possible.
- Update Markdown explanations and `--json` output.
- Add tests for explanation rendering and JSON output.
- Document the confidence model at a user-facing level.

Acceptance criteria:

- `piranesi explain` answers: what matched, what path was found, what sanitizers mattered, and how confidence was derived.
- JSON output is stable enough for downstream tooling.
- Tests cover at least one active candidate and one verified finding.
