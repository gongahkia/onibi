# ADR 0032: position conversion and incremental synchronization are Zenbu-owned

## Status

Accepted for M11.

## Context

Zenbu selections and transactions use UTF-8 byte offsets tied to immutable
snapshots. LSP uses lines and negotiated UTF-8/UTF-16/UTF-32 units. Ad-hoc
conversion at individual features would mishandle emoji, combining marks,
CRLF, and simultaneous edits inconsistently.

## Decision

`Language.Position` is the only LSP-coordinate converter and rejects invalid
boundaries. `Language.Sync` converts transaction source edits in descending
source order and checks that sequential changes reconstruct committed text. The
LSP adapter uses those values for synchronization, requests, diagnostics, and
returned edits; public models continue to see byte offsets only.

## Consequences

Coordinate policy is independently testable and reusable by a future adapter.
Incremental sync has a clear correctness oracle. Grapheme and terminal-cell
coordinates remain presentation concerns, not LSP or kernel coordinates.
