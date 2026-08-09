# ADR 0001: immutable snapshots and transactions

## Context

Zenbu must support several unrelated editing grammars without making one of
them fundamental. Selectors and later background services need safe, stable
read views.

## Decision

Use immutable documents and snapshots. Express every text/selection state
change as a transaction validated against a source document id/version.
Semantic intents resolve to transactions outside the mutation path.

## Alternatives considered

- A mutable editor object with command callbacks: convenient initially, but
  creates privileged mutation paths and unsafe read concurrency.
- Record key events and replay them: couples history to an editing grammar.

## Consequences

Updates are explicit values, failures are result values, and different editing
models can use the same kernel. Persistent data and version validation add some
allocation and API ceremony, which is appropriate at this milestone.

