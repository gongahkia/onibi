# ADR 0004: deterministic transaction ordering

## Context

Multiple semantic edits must produce one result independent of incidental
implementation mutation order.

## Decision

Use half-open source ranges, reject overlapping non-empty ranges and insertions
strictly inside them, and apply a documented source-order assembly algorithm.
Same-point insertions retain declaration order; start-boundary insertions occur
before a replacement and stop-boundary insertions occur after it.

## Alternatives considered

- Sequentially mutate a buffer in caller order: later coordinates become
  ambiguous.
- Reject all touching edits: unnecessarily restricts valid semantic commands.

## Consequences

Transactions are easy to reason about and replay. The exact boundary policy is
part of the public protocol and must be preserved compatibly.

