# TODO 7: Add Host-Specific PII And Secret Redaction For LLM Prompts

## Goal

Add an explicit host data hygiene layer before any host evidence is sent to an LLM. The VM vulnerability proposal requires PII/MMPI sanitization before API submission. Current host LLM prompting is bounded but does not have a dedicated host redaction step.

## Current State

Relevant files:

- `src/piranesi/host/analyze.py`
- `src/piranesi/llm/sanitize.py`
- `src/piranesi/llm/trace.py`
- `src/piranesi/llm/router.py`
- `docs/known-limitations.json`
- `tests/test_host_posture.py`
- `tests/test_llm/*`

Known limitation:

- `docs/known-limitations.json` says LLM prompt redaction and token budgeting are heuristic.

Current host LLM prompt includes:

- Host identity
- OS
- Kernel
- Evidence inventory
- Available evidence keys
- Deterministic finding summaries

This is less risky than raw logs, but it can still include hostnames, user names, package names, IP addresses, and future auth evidence.

## Desired Behavior

Before calling `provider.complete()` from host analysis:

1. Build a structured host LLM input object.
2. Run it through a host redactor.
3. Record redaction metadata.
4. Use only the redacted payload in the prompt.

The redactor should support:

- Secret patterns.
- Token/key/password assignments.
- Private keys and API keys.
- Emails.
- Usernames.
- Hostnames.
- IP addresses, with configurable preservation.
- MAC addresses.
- File paths with home directories.
- Command lines with likely secrets.

## Redaction Policy

Add configuration options if the project config pattern supports it:

```toml
[host.redaction]
mode = "strict" # strict | balanced | off
preserve_private_ips = false
preserve_package_names = true
preserve_usernames = false
preserve_hostnames = false
```

If config integration is too much for this task, implement a default strict redactor and leave config as follow-up.

Recommended defaults:

- Preserve package names because they are needed for vulnerability analysis.
- Preserve service names and port numbers.
- Redact usernames except `root`.
- Redact hostnames.
- Redact private IPs by default, but preserve whether an address is public/private/loopback.
- Redact secrets always.

## Data Model

Add:

```python
class RedactionStatus(BaseModel):
    applied: bool
    redacted_value_count: int
    categories: dict[str, int] = Field(default_factory=dict)
    mode: str
```

Add to `HostPostureReport` or report metadata:

```python
llm_redaction: RedactionStatus | None = None
```

If only LLM analysis uses it, include it when `analysis_modes` contains `llm`.

## Implementation Notes

Create a host-oriented redaction module:

```text
src/piranesi/host/redaction.py
```

Possible API:

```python
def redact_host_llm_payload(payload: object, policy: HostRedactionPolicy) -> RedactedPayload:
    ...
```

Avoid mutating original snapshots. Return a redacted copy.

Use stable placeholders:

- `[HOSTNAME_1]`
- `[USER_1]`
- `[PRIVATE_IP_1]`
- `[PUBLIC_IP_1]`
- `[SECRET]`

Stable placeholders keep cross-field relationships visible without leaking actual values.

## Trace Hygiene

If prompt tracing is enabled, traces must contain the redacted prompt, not the raw prompt.

Check:

- `TraceLogger`
- `TraceWriter`
- any `log_prompts` behavior

Do not add raw payloads to exceptions or debug logs.

## Tests

Add tests for:

- Hostname redaction in LLM prompt.
- Username redaction in LLM prompt.
- Secret assignment redaction.
- IP redaction with classification retained.
- Package names preserved.
- Redaction metadata count/categories.
- Trace logging uses redacted content.
- Deterministic `assess` without LLM does not need redaction.

Use a fake `LLMProvider` or monkeypatch provider completion to capture prompt content.

## Documentation

Update:

- `README.md`
- `docs/host-posture.md`
- `docs/known-limitations.json`

Once implemented, update the known limitation from "open" to a more precise residual limitation if needed. Do not delete the limitation unless tests meaningfully cover the risk.

## Acceptance Criteria

- Host LLM prompts pass through a dedicated redaction function.
- Tests prove sensitive host fields are not sent raw.
- Redaction metadata is present in output or trace where useful.
- Prompt traces do not leak raw sensitive values.
- Existing deterministic behavior is unchanged.

## Out Of Scope

- Perfect PII detection.
- Organization-specific DLP integration.
- Encrypting local evidence bundles.
- Redacting local JSON/Markdown reports by default.

## Validation Commands

```bash
uv run pytest tests/test_host_posture.py tests/test_llm
uv run piranesi assess tests/fixtures/host/debian-vulnerable --analysis deterministic --output /tmp/piranesi-redaction-out
```

