# ADR 0008: selector/transformation intents and explicit commands

## Context

Different editing models need to compose targeting and transformation semantics
without duplicating mutation logic or replaying key sequences.

## Decision

Extend M1 `Intent` with model-neutral selector/transformation composition and
retain the original intent forms. Use an immutable explicit command registry
whose handlers return semantic intents from a constrained context.

## Alternatives considered

- Add motions/operators to the kernel: encodes Vim vocabulary into the core.
- Make each model directly build edits: duplicates semantic validation and
  obscures intent.
- Use a global command table: prevents deterministic, testable registration.

## Consequences

`Apply(next-text-unit, delete)` is replayable independently of how it was
requested. The primitive selector set is intentionally small; syntax, regex,
and richer text semantics can extend the same boundary later.
