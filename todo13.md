# TODO 13: Add Stable Report API And Embeddable Library Mode

## Goal

Make Piranesi useful as a library and integration component, not only as a CLI.
Adoption grows when other tools can call Piranesi, ingest its reports, and rely on
stable schema contracts.

## Current State

Host assessment is exposed mostly through internal Python functions and CLI output.
JSON reports exist, but there is no formally versioned host report API, schema
export, or compatibility policy.

## Desired Behavior

Expose a stable public Python API:

```python
from piranesi.host.api import assess_host_bundle, collect_host_evidence

report = assess_host_bundle(
    "piranesi-evidence",
    analysis="deterministic",
    format="model",
)
```

Add schema export commands:

```bash
piranesi schema host-report --output host-report.schema.json
piranesi schema host-snapshot --output host-snapshot.schema.json
piranesi schema fleet-report --output fleet-report.schema.json
```

## API Design

Create:

```text
src/piranesi/host/api.py
src/piranesi/schema.py
```

Public functions should:

- Accept `str | Path`.
- Return Pydantic models or plain JSON dictionaries.
- Avoid CLI-only side effects.
- Avoid process exits.
- Use typed exceptions.
- Preserve deterministic defaults.

## Compatibility Policy

Document:

- Schema versioning rules.
- Additive field policy.
- Deprecation policy.
- Minimum supported Python version.
- Public vs internal module boundaries.

## Tests

Add tests for:

- Public API assessment from fixture bundles.
- JSON schema export for host snapshot/report.
- Backward-compatible parsing of older sample reports.
- No Typer dependency required for library usage.

## Documentation

Update:

- `docs/plugin-api.md`
- `docs/host-posture.md`
- `docs/capabilities.md`

Add a new:

```text
docs/api.md
```

## Acceptance Criteria

- Integrators can call host assessment without invoking the CLI.
- JSON schemas are exported and checked into release artifacts.
- Backward compatibility rules are explicit.
- Tests protect the public API surface.

## Out Of Scope

- Network service API.
- gRPC.
- Hosted multi-tenant API.

## Validation Commands

```bash
uv run pytest tests/test_host_api.py tests/test_host_posture.py
uv run piranesi schema host-report --output /tmp/host-report.schema.json
```

