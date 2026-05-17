# Stability Guarantees

Piranesi is stable-alpha. Public contracts are documented and tested, while
internal implementation modules may change without deprecation.

## Public Surfaces

Stable-alpha Python modules:

- `piranesi.host.api`
- `piranesi.host.models`
- `piranesi.schema`

Stable-alpha JSON schemas:

- `host-report`
- `host-snapshot`
- `fleet-report`

Stable-alpha community rule formats:

- `rules/community/host/*.toml`

CLI command/group shape is tracked by `tests/test_contract_snapshots.py` and the
snapshot under `tests/snapshots/contracts/`.

## Schema Compatibility

Before a future major-version break:

- Existing required fields keep their meaning.
- New fields are additive and optional or defaulted for Pydantic parsing.
- `schema_version` increments when payload shape changes materially.
- Saved host reports are normalized by `load_host_report` when safe defaults can
  represent older data.
- Deprecated fields remain readable for at least one minor release after a
  replacement is documented.

These rules apply to `HostSnapshot`, `HostPostureReport`, and `FleetReport`.
They do not freeze private helper models or intermediate pipeline artifacts.

## Module Boundaries

Import documented public modules for integrations. Other modules under
`piranesi.*` are implementation details unless a doc page explicitly promotes
them. Internal modules can still be imported by tests and CLI wiring, but they
are not compatibility promises.

## Community Host Rules

Community host rules are constrained TOML data, not executable plugins. The
format is stable-alpha:

- supported `rule.schema_version` values remain accepted for at least one minor
  release after deprecation is announced
- new match operators must be additive
- unsafe executable keys remain rejected
- fixture metadata and expected finding IDs are compatibility inputs for rule
  review

Deprecations should include a migration note in `docs/contributing-host-rules.md`
and a compatibility test where practical.
