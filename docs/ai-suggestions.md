# AI Suggestions

AI suggestion mode produces operator-reviewable hints from existing workspace
facts only. It supports three suggestion kinds:

- `dedupe-candidate`: possible duplicate or related findings to compare.
- `severity-rationale`: draft wording for why an existing severity is justified.
- `retest-checklist`: follow-up checks for retest planning.

Providers must return a JSON object with a `suggestions` list. Each suggestion
must include a `kind`, `text`, optional existing `finding_ids`, and at least one
existing `evidence_ids` entry or allowed `report_fields` citation. Supported
report fields are listed in the prompt policy and include `findings`, `severity`,
`evidence`, and `retest_status`.

Suggestion mode is intentionally non-mutating. Creating, accepting, rejecting, or
ignoring a suggestion set does not change findings, evidence, report text, source
references, retest state, or chain-of-custody manifests. The accepted/rejected/
ignored state records operator disposition only.

Every suggestion set:

1. Uses the redaction-before-prompt contract.
2. Runs privacy and hallucination evals.
3. Rejects invented finding or evidence identifiers.
4. Stores the suggestion payload under `ai/suggestions/`.
5. Writes a redacted prompt/output trace to `ai/traces.jsonl`.
6. Records the operator disposition in the AI trace approval state.
