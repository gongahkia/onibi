# AI Remediation Drafts

AI remediation mode drafts wording for an existing finding only. The workflow:

1. Builds a redacted prompt payload from workspace facts.
2. Calls a configured provider with the redacted payload.
3. Runs privacy and hallucination evals.
4. Records a prompt/output trace.
5. Stores an AI-generated remediation draft under `ai/drafts/`.
6. Applies the remediation text to the finding only after explicit acceptance.

Drafts can be rejected without changing findings. Accepted findings store the
AI trace ID and draft ID in provenance so report text remains traceable to the
prompt/output record.

This mode must not create findings, add evidence, or alter source references.
