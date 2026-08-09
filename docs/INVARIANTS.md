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
7. Editing models only receive immutable `Input_event` values and an immutable
   `Editor_context` facade. The facade provides document text, byte length,
   selection offsets, document identity/version, and command descriptors; it
   intentionally omits mutable documents, history nodes, text-buffer storage,
   transaction commit, and arbitrary callbacks.
8. Model effects are inspectable data: execute a semantic intent, invoke a
   stable command id with typed arguments, or emit a message. They never carry
   an editor-mutation closure. Model status is derived from model-owned state.
9. The model runtime interprets an input event against a snapshot, then commits
   all resulting effects through normal history/transaction APIs. If any effect
   fails, it returns the detailed error and retains the prior runtime state,
   history, and input trace.
10. A command registry is an explicit immutable value. Command ids are stable,
    unique, deterministically enumerated identifiers; a command handler
    receives only an `Editor_context` and returns semantic intents.

The following rule is architectural rather than merely local:

> Core mutation APIs must not expose arbitrary `mutable Editor` access to
> extensions.

The M2 dogfood invariant is equally strict:

> Anything a first-party editing model can do must ultimately be possible for a
> third-party editing model using the public API.
