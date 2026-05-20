# C2 Log Import

`piranesi ingest c2` imports a neutral local JSONL event log as evidence and timeline
records:

```bash
piranesi ingest c2 --input mock-c2-events.jsonl --workspace ./workspace
```

This adapter is deliberately local and passive. It does not connect to C2
infrastructure, call vendor APIs, control sessions, execute commands, generate
payloads, or expand target scope.

## Neutral JSONL Shape

Each line is a JSON object. The initial neutral fields are:

- `timestamp`: required event time.
- `event` or `summary`: required event type or explicit summary.
- `operator` or `actor`: optional source actor.
- `target` or `host`: optional host/session target.
- `session` or `session_id`: optional session identifier.
- `source`: optional lab/tool/source label.
- `command_summary`: optional safe command summary.
- `output_locator`: optional pointer to preserved output evidence.
- `confidence`: optional `low`, `medium`, `high`, or `confirmed`.

The original JSONL is preserved as `c2-log` evidence under `raw/c2-log/`. Parsed
events are appended to `timeline/events.jsonl` and linked back to that evidence
record.

## Safety Boundary

The adapter records safe summaries and locators only. Raw command output should stay
in the preserved evidence file or a separate evidence artifact with sensitivity
metadata. Vendor-specific adapters should build on this boundary and must not add
live C2 access.
