# AI Redaction Contract

AI prompt construction must use `piranesi.ai.build_redacted_prompt_payload`.
Provider implementations must consume `RedactedPromptPayload.provider_payload()`
instead of raw workspace objects.

The contract redacts or summarizes:

- client, project, owner, scope, hostnames, URLs, and IP addresses;
- token-like values, authorization headers, cookies, passwords, API keys, session
  IDs, and private keys;
- request, response, curl, payload, transcript, loot, session, and secret
  evidence bodies.

The prompt payload keeps finding IDs, evidence IDs, severity, confidence, status,
weakness IDs, references, redacted locators, and redaction events so operators can
inspect what would be sent before any model call.

