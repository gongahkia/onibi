# ADR 0020: replace scripting as staged whole generations

## Status

Accepted for M7.

## Context

A configuration contributes commands, semantic descriptors/behaviors, bindings,
and hooks that may refer to one another. Incremental mutation during reload
would expose a half-valid configuration and complicate old Lua callback
lifetime. Reload failure must leave the editor usable.

## Decision

Treat one evaluated Lua configuration as one generation. The session keeps
immutable builtin command/semantic bases and an optional active generation.
Reload creates a fresh state, evaluates it, checks all ids/collisions/bindings,
and creates replacement overlays before installation. On success the runtime
receives the new complete overlay and the prior generation is disposed. On any
failure the session retains its previous active runtime/generation and records
the failure for `Scripts`, trace, and the status message.

## Consequences

Registration removal is naturally expressed by deleting it and reloading.
Generation-level disposal keeps Lua callback references bounded across
successful reloads. There are no per-registration unload hooks or partial
reloads. Dynamic operations resolve to concrete history transactions; their
callback identity is not preserved as a cross-generation repeat intent.
