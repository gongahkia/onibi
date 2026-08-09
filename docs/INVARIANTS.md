# Kernel invariants

The M0/M1 constructors and commit path enforce these invariants.

1. Documents, snapshots, anchors, and selections carry a document id and
   version. A transaction can only commit to the document/version it names.
2. Stored text and inserted/replaced text are valid UTF-8. Public offsets are
   UTF-8 byte offsets and must fall on code-point boundaries. Combining marks
   are individual code points; grapheme and display-cell handling are deferred.
3. Ranges are half-open `[start, stop)` and their endpoints share a snapshot.
4. A selection retains anchor and head direction. A selection set is non-empty,
   has one primary selection, is sorted by its normalized range, has no
   duplicate selections, and has no overlapping non-empty ranges. Touching
   ranges and empty carets at different offsets are allowed.
5. A transaction has a source document/version, at least one edit or selection
   change, and non-conflicting edits. Commit validates all buffer bounds and
   code-point boundaries before creating any new document value.
6. History nodes retain immutable before/after documents and transaction
   metadata. Undo/redo selects existing nodes rather than applying inverse
   mutations.

The following rule is architectural rather than merely local:

> Core mutation APIs must not expose arbitrary `mutable Editor` access to
> extensions.
