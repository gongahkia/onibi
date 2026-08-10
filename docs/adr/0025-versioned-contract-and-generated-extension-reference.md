# ADR 0025: make Extension API v1 explicit and generate its reference material

## Status

Accepted for M8.

## Context

Documentation copied from runtime behavior drifts easily, while extension
authors need a small stable compatibility target and editor assistance.

## Decision

`zenbu.extension.Contract` is the authoritative v1 vocabulary for runtime
identifiers, contribution/capability names, services, and stable errors. It
generates the committed Markdown reference and Lua stub through
`make extension-docs`. V1 may add optional fields/services but must retain
required v1 manifest and service behavior; a breaking change requires a new API
version. M8 tests compare committed generated outputs with the contract.

## Consequences

The public `zenbu.extension` library is the supported stable extension surface.
M7 configuration stays supported but has no plugin-ABI compatibility promise.
