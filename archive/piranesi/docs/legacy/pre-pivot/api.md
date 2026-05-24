> Legacy note: This document describes pre-pivot or roadmap behavior. It is retained for historical context and is not current Phase 1 guidance. Current guidance is in README.md, docs/ARCHITECTURE.md, and the pentest workspace/report/retest/sign/serve docs.

# Public API And Schemas

Piranesi exposes a stable-alpha Python API for host posture integrations. The
public API is intended for tools that want to assess local evidence bundles,
parse saved reports, or export JSON schemas without invoking the CLI.

## Public Modules

Stable-alpha host API:

```python
from piranesi.host.api import assess_host_bundle, load_host_report

report = assess_host_bundle("piranesi-evidence")
payload = assess_host_bundle("piranesi-evidence", format="dict")
saved = load_host_report("piranesi-output/host-report.json")
```

Schema API:

```python
from piranesi.schema import build_schema, write_schema

schema = build_schema("host-report")
write_schema("host-snapshot", "host-snapshot.schema.json")
```

Supported schema names:

- `host-report`
- `host-snapshot`
- `fleet-report`

The matching CLI commands are:

```bash
piranesi schema host-report --output host-report.schema.json
piranesi schema host-snapshot --output host-snapshot.schema.json
piranesi schema fleet-report --output fleet-report.schema.json
```

## Return Types

Public API functions accept `str | pathlib.Path` inputs. `assess_host_bundle`
defaults to deterministic analysis and returns a `HostPostureReport` Pydantic
model:

```python
from pathlib import Path
from piranesi.host.api import assess_host_bundle

report = assess_host_bundle(Path("piranesi-evidence"))
print(report.posture_score)
```

Pass `format="dict"` when an integration needs plain JSON-compatible Python
dictionaries:

```python
payload = assess_host_bundle("piranesi-evidence", format="dict")
```

## Exceptions

Public API functions raise typed exceptions instead of exiting the process:

- `PiranesiHostApiError`
- `HostAssessmentError`
- `HostReportParseError`
- `HostApiCollectionError`
- `SchemaExportError`

CLI commands still translate errors into process exit codes.

## Compatibility Policy

The canonical compatibility statement is [Stability Guarantees](stability.md).
The host report, host snapshot, and fleet report schemas are stable-alpha
contracts. Piranesi follows these rules before a future major-version break:

- Existing required fields keep their current meaning.
- New fields are additive and optional or defaulted for Pydantic parsing.
- `schema_version` increments when payload shape changes materially.
- Older saved host reports are normalized by `load_host_report` where the missing
  information can be represented safely with defaults.
- Deprecated fields remain readable for at least one minor release after a
  replacement is documented.

The minimum supported Python version for the public API is the package minimum in
`pyproject.toml`: Python 3.12 or newer.

## Internal Boundaries

Use `piranesi.host.api`, `piranesi.host.models`, and `piranesi.schema` for
integrations. Other `piranesi.host.*` modules are importable for Piranesi's own
tests and CLI wiring, but they are not compatibility promises unless documented
here.

The public host API has no Typer dependency. Typer is only required for CLI entry
points under `piranesi.cli`.
