# ADR 0006: editing models are opaque state machines

## Context

Zenbu must host simple direct commands and multi-key modal grammars without
making normal/insert/operator-pending state kernel concepts.

## Decision

Define `Editing_model.S` with an abstract state, initialize/handle/reset/status
operations, an immutable input/context transition, and a model-runtime functor
that retains but does not inspect the state.

## Alternatives considered

- Put modes, operators, or motions in the kernel: makes one grammar
  foundational.
- Use a mutable editor callback object: permits privileged and non-repeatable
  mutation.
- Erase state to strings: prevents type-safe model-owned state.

## Consequences

Models can represent arbitrary pending grammar without kernel knowledge. The
functor is a small, concrete use of OCaml abstraction needed to preserve opaque
state; actual hot model switching can be added later with an existential host.
