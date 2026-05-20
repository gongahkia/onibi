# AI Provider Configuration

Cloud model calls require explicit BYOK configuration. Piranesi stores the
environment variable name, provider identity, model identity, privacy-mode state,
and external-call state; it does not store API key values in workspace artifacts.

Provider code must call `require_cloud_provider_ready()` before any external model
request. That guard fails closed when:

- privacy mode is enabled;
- external calls were not explicitly enabled;
- the configured BYOK environment variable is absent.

Trace records include provider name, model name, base URL, privacy-mode state, and
whether the request was external. They include the API key environment variable
name for auditability, never the key value.
