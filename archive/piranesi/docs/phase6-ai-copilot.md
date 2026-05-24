# Phase 6 AI Co-Pilot Closeout

Date: 2026-05-20

Status: implemented as alpha, local-first, operator-controlled assistance.

Phase 6 is complete when AI remains a drafting and suggestion layer over existing
workspace facts, not an autonomous security engine. The implementation now covers:

- operator-control policy and allowed/prohibited use boundaries;
- redaction-before-prompt payload construction;
- privacy mode and cloud provider external-call kill switch;
- first-class local provider configuration;
- prompt/output trace logging linked to workspace chain of custody;
- privacy and hallucination evals for AI output;
- remediation draft mode with explicit accept/reject workflow;
- executive-summary draft mode with explicit accept/reject workflow;
- dedupe, severity-rationale, and retest-checklist suggestion mode with
  accept/reject/ignore disposition.

The core invariants are still mandatory:

- AI may not create findings, evidence, hosts, assets, source references, or
  retest state.
- AI may not run scanners, payloads, exploits, target interaction, or replay.
- External model calls require explicit BYOK provider configuration and are
  disabled in privacy mode.
- Report-changing AI text must start as a draft and must not affect output until
  a human accepts it.
- Suggestion acceptance records operator disposition only; it does not mutate
  workspace findings, evidence, or reports.
- Every AI provider call records a redacted prompt, redacted response, provider
  metadata, target field, digests, and approval state.

Issue #44 can close once #92 lands on `main` and the remaining child issues are
closed. Future AI work should open new issues that name the specific workflow and
prove it preserves these invariants.
