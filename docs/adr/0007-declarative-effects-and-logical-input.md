# ADR 0007: declarative effects and logical input

## Context

Input models need to request editing without depending on a terminal library or
receiving direct document mutation authority.

## Decision

Use logical `Input_event` values and inspectable model effects. Effects execute
a semantic intent, invoke a command id with typed arguments, or emit a message.
Committed text is distinct from a logical text key; physical-key metadata is
optional and non-semantic.

## Alternatives considered

- Raw terminal escape sequences: ties every model to one host backend.
- Automatically insert printable keys: prevents command grammars and IME-aware
  distinctions.
- Arbitrary effect closures: cannot be traced, inspected, isolated, or safely
  attributed.

## Consequences

The runtime can trace effects and atomically preserve state on failure.
Terminal decoding is deferred, and commands can remain separate from bindings.
