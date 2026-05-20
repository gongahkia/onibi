# AI Trace Log

AI draft and suggestion workflows write redacted traces to `ai/traces.jsonl`.
Each trace record includes:

- redacted prompt payload;
- provider and model metadata;
- redacted response text and response metadata;
- target field or finding identifier;
- approval state;
- prompt and response digests.

Trace records are workspace artifacts. Approval changes update the trace approval
state and append audit events. `piranesi sign` includes `ai/traces.jsonl` in the
chain-of-custody manifest with role `ai-trace`, and verification detects trace
tampering the same way it detects report or evidence tampering.

Trace records must not contain unredacted client identifiers, hostnames, request
or response evidence, tokens, cookies, passwords, API keys, or private keys.
