# AI Local Provider Interface

Local providers use the same `RedactedPromptPayload` input as cloud providers.
They may be configured as either:

- an operator-managed local endpoint, such as a loopback OpenAI-compatible API;
- an operator-managed local process command.

The interface does not assume a specific local model, runtime, or protocol. Local
provider trace metadata records provider name, optional model name, endpoint URL or
process command, privacy-mode state, and `external_call=false`.

Local providers do not weaken redaction or approval gates. Draft workflows must
still build a redacted prompt, write an AI trace record, and require human approval
before any report artifact changes.
