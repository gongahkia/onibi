# ADR 0027: isolate Components by denying imports and projecting extension requests

## Status

Accepted for M9.

## Context

Extension API v1 already has one runtime-neutral callback boundary:
`Extension_host` carries an opaque callback token, provider metadata, granted
capabilities, and serialisable request/response values. Adding a Component
runtime must not create a second API that can mutate a document, retain a
terminal/parser object, or bypass semantic transaction validation.

## Decision

The Component WIT world exports only the control interface. It imports no WASI
or Zenbu host interface. Zenbu creates one private Wasmtime engine/store/linker
and Component instance per staged plugin generation, links no WASI services,
and invokes only the typed control exports. Capability checks continue at the
existing `Extension_host` request construction and declarative action decoder;
the guest receives only the resulting copied data.

The store has a manifest-configurable memory cap and fuel is reset before every
registration/callback. The Component adapter converts WIT data to the existing
`Extension_value` protocol and records bounded runtime stage/fuel observations.
Failed staging or reload does not replace the active immutable plugin snapshot.

## Consequences

A Component cannot gain filesystem, network, process, environment, terminal,
document/history object, Tree-sitter pointer, or ambient host authority through
the Zenbu linker. Its effects follow the same command/selector/transformation
path as Lua and first-party clients. An attempted WASI/unlinked import fails
instantiation rather than producing a partial package.

This intentionally leaves richer imported Component host functions for later.
M9 is synchronous and fuel-limited, not an async worker or hard wall-clock
cancellation system. `lua-trusted` remains outside this isolation boundary.
