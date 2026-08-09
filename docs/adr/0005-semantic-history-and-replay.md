# ADR 0005: semantic history and replay

## Context

Future models need macros, repeat, reproducible bug reports, and automation
without sharing a keyboard grammar.

## Decision

Store committed transactions with semantic metadata in a persistent history
tree. Replay records intents or explicit transaction specifications, never raw
key presses.

## Alternatives considered

- Key-event logs: model-specific and fragile under configuration changes.
- A permanent external protocol now: premature before M2 clients exist.

## Consequences

Replay is inspectable and deterministic today. Its v1 line format is deliberately
small and may be superseded behind a compatible import boundary later.

