# ADR 0023: discover local manifests and activate complete plugin snapshots

## Status

Accepted for M8.

## Context

Stable extensions need identity, compatibility checks, deterministic discovery,
and safe replacement without turning M8 into a resolver or marketplace.

## Decision

M8 discovers immediate package children in configured XDG plugin roots. Each
package supplies a schema-validated `zenbu-plugin.toml` with ID, SemVer core
version, API/runtime, entrypoint, contributions, and capabilities. Entrypoints
must resolve within the package. The host stages every plugin in a fresh runtime
and publishes a complete immutable snapshot only after registration/collision
validation. Reload failure retains the package's last known-good snapshot;
removed/deactivated packages dispose their private runtime.

## Consequences

Plugin package failure is actionable and cannot leave half a provider active.
M8 intentionally has no dependency solver, signatures, remote fetch,
project-local auto-discovery, enablement database, or unload callback API.
