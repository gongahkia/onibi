# Prompt: Make Baselines More PR-Friendly

You are working in Piranesi, an AppSec CLI that can be used in CI. Security scan baselines should help teams focus on new risk in pull requests while preserving visibility into existing risk.

Goal: improve baseline workflows for PR review.

Implementation requirements:

- Inspect baseline, report, and CI integration code/docs.
- Add support for comparing current findings against a baseline artifact.
- Classify findings as new, existing, fixed, or changed.
- Use stable finding fingerprints that are resilient to line-number drift where possible.
- Add CLI flags or config for baseline input/output and failure policy, such as fail only on new high/critical findings.
- Render a concise PR-friendly summary in Markdown and JSON.
- Add tests for new/existing/fixed classification and fingerprint stability.
- Update CI docs with an example workflow.

Acceptance criteria:

- CI can fail based on new findings instead of all findings.
- Reports clearly identify what changed since the baseline.
- Baseline comparison is deterministic.
