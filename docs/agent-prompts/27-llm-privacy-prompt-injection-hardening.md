# Prompt: Add Privacy and Prompt-Injection Hardening Tests for LLM Stages

You are working in Piranesi, an AppSec CLI that can pass source snippets and findings to LLM stages. Because scanned repositories may contain secrets or malicious prompt-injection strings, LLM boundaries need explicit tests and safeguards.

Goal: add privacy and prompt-injection hardening for LLM triage/patch stages.

Implementation requirements:

- Inspect LLM prompt construction, context selection, triage, and patch modules.
- Add redaction of likely secrets before LLM calls, including API keys, tokens, private keys, passwords, cookies, and authorization headers.
- Add prompt-injection guardrails: repository text should be treated as untrusted data, not instructions.
- Ensure system/developer instructions are separated from code/context content where the LLM client supports roles.
- Add tests that inject malicious source comments such as instructions to ignore policies or exfiltrate secrets, and verify they remain quoted/untrusted context.
- Add tests that secrets are redacted before fake LLM calls receive prompts.
- Update security docs with the LLM threat model and limitations.

Acceptance criteria:

- Fake LLM tests prove secrets are not sent raw.
- Prompt-injection strings from scanned code do not become operative instructions in prompt structure.
- Users understand what data may be sent to LLM providers and how to disable LLM stages.
