# Prompt: Enforce Token Budgets for LLM Stages

You are working in Piranesi, an AppSec CLI that can use LLMs for triage/patching. Configuration includes or should include token budget limits, but those limits need actual enforcement.

Goal: enforce configured token budgets for all LLM stages.

Implementation requirements:

- Inspect LLM client code, triage, patch, and configuration handling.
- Locate `budget.max_tokens` or equivalent settings.
- Ensure prompts, context construction, retries, and multi-finding batching respect the configured budget.
- Add graceful degradation: summarize, truncate, batch smaller, or skip optional context rather than crashing where possible.
- Emit clear warnings when context is omitted due to budget.
- Add tests using fake token counters or fake LLM clients. Do not require real API calls.
- Update docs to explain budget behavior and defaults.

Acceptance criteria:

- LLM calls cannot exceed configured token budgets by construction as far as local estimation allows.
- Users can see when budget constraints affected output.
- No real network/API dependency is introduced in tests.
