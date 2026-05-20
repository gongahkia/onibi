# AI Executive Summary Drafts

AI executive-summary mode drafts text from existing workspace facts only:

- normalized finding counts;
- severities and statuses;
- scope after redaction;
- existing finding titles, descriptions, remediation, references, and weakness IDs.

The prompt omits evidence bodies. A draft must pass privacy and hallucination evals
and is stored under `ai/drafts/`. Report output does not change until an operator
accepts the draft. Acceptance writes `reports/ai-executive-summary.json`, and
pentest reports include the accepted AI draft text plus draft and trace IDs.

Rejected drafts do not alter report artifacts or findings.
