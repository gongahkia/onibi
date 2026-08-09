# ADR 0015: syntax snapshots are version-bound and caches are bounded

## Context

Syntax ranges become document selections. Reusing a parse from document version
N against version N+1 could generate a valid-looking but wrong mutation. A
Tree-sitter tree is mutable during incremental `edit`, while Zenbu snapshots
are intentionally immutable values.

## Decision

`Syntax.Snapshot` records the exact kernel document snapshot, language, and
private tree. `Snapshot.matches_document` checks document id/version and
`Editor_context` drops nonmatching syntax. The runtime refreshes before each
model call and attempts incremental update after every committed transaction.

Each service retains only its parser and one current snapshot. Before an
incremental update it copies the Tree-sitter tree, applies transaction-derived
edits to the copy, and parses with the copy as `~old`. Old Zenbu snapshots are
therefore not modified. A missing matching predecessor, including undo/redo,
uses a full parse rather than retaining all history trees.

## Consequences

Syntax selectors cannot silently commit old-version ranges. Cached syntax is
bounded and parser lifetime is owned by OCaml GC. Undo/redo trades some parse
reuse for a smaller cache and simpler immutable ownership. A future async
service can retain this snapshot/version protocol while changing delivery.
