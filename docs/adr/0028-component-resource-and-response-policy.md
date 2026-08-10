# ADR 0028: bound Component resources and decoded response amplification

## Status

Accepted for M9.

## Context

Fuel and linear-memory limits prevent an isolated Component from running
forever or growing memory without bound, but a guest can still return a large
valid Component value. Decoding an unbounded registration, action, selection,
or edit list would shift denial of service to OCaml before transaction
validation.

## Decision

Each Component generation has a default 16 MiB store cap and each
`register`/`invoke` call receives 5,000,000 fuel units. Positive manifest
overrides remain inspectable. The private C boundary caps Component list,
record, string, and nesting conversion; the WIT value decoder caps nodes,
paths, and aggregate text; the adapter caps registrations, actions, selections,
and edits. Exceeding any limit is the stable `extension-response-limit` error.

The quotas are deliberately conservative but not user-facing configuration in
M9. They bound host work while preserving normal multi-selection edits. A
future configuration surface must keep host-side validation authoritative.

## Consequences

Response amplification fails before any semantic action is interpreted, so a
valid action followed by an invalid or over-limit value cannot commit a partial
document/history change. Resource failures remain distinct from semantic errors
such as an invalid range. The 200-generation stress test is evidence against
stale callback/lifecycle regressions, not a formal leak proof.
