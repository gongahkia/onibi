# Prompt: Add Composite Risk Scoring

You are working in Piranesi, an AppSec CLI that reports security findings. Severity alone is not enough; users need a composite risk score that incorporates reachability, exploitability, business context, exposure, verification, and suppressions.

Goal: add a transparent composite risk score for findings and summaries.

Implementation requirements:

- Inspect severity/confidence models in detection, triage, verification, and report rendering.
- Define a scoring model with documented inputs such as severity, confidence, source exposure, sink criticality, package/service ownership, verification status, exploitability evidence, public advisories, reachable path, and suppression state.
- Keep the model simple and explainable. Avoid opaque magic numbers without rationale.
- Include both numeric score and categorical band, such as low/medium/high/critical risk.
- Add a breakdown to `report.json` and `piranesi explain`.
- Update Markdown reports and executive summary to sort or summarize by composite risk where appropriate.
- Add tests for scoring behavior and sorting.
- Update docs with the scoring formula and caveats.

Acceptance criteria:

- Users can understand why one finding ranks above another.
- Risk score changes predictably when verification or exposure changes.
- The score is additive and backward-friendly with existing severity fields.
