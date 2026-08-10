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
11. M3 selectors resolve only to valid snapshot-local UTF-8 ranges. Word and
    line classification is deterministic and documented; unavailable targets
    reject rather than silently clipping. `collapse-to-start/end` only changes
    selections and never bypasses transaction validation.
12. Clipboard slots are immutable runtime state containing validated UTF-8 text
   and model-neutral characterwise/linewise shape. Copy does not mutate a
   document. Paste, undo, redo, and semantic repeat are declarative runtime
   effects; no model owns a private mutable history or buffer mutation path.
13. M4 display geometry is a host projection, never a kernel coordinate
    replacement. Documents and model contexts retain validated UTF-8 byte
    offsets. The view maps source grapheme clusters to terminal display columns
    without rewriting document text; terminal cursor coordinates are frame-local.
14. M4 dirty state uses a saved document version as a fast clean check and
    compares source contents when a later version is selected. Selection-only
    history transitions therefore do not mark a file dirty; undoing to saved
    contents naturally becomes clean. Saving an existing file writes an adjacent
    temporary file then renames it; save and quit policy are host commands, not
    model effects.
15. M5 syntax snapshots name exactly one immutable document id/version. A
    syntax node is usable only through its owning snapshot; conversion to a
    kernel range reuses `Document_snapshot.range`, so it cannot create an
    unvalidated or cross-version selection. `Editor_context` exposes syntax
    only when `Snapshot.matches_document` holds.
16. The syntax service owns parser/tree lifetime and only a bounded current
    cache. It copies a backend tree before incremental editing, keeps no global
    syntax state, and may fully reparse when no matching cached predecessor
    exists. Models receive no parser, raw node, query, or backend handle and
   must reach structural mutations through ordinary semantic intents.
17. Provenance contains only stable semantic ids and runtime-local execution or
    interaction ids. It contains no timing, terminal, parser, or backend value
    and remains optional so replay stays deterministic and compatible.
18. Trace and profile records are explicitly owned by a runtime/session, have
    positive bounded capacities, evict oldest records deterministically, and
    are local-only. Disabled services retain no records.
19. Generic inspection consumes public model descriptors/status/input rules,
    kernel history/provenance, and Zenbu syntax values. It must not branch on a
    model id or require Tree-sitter or Notty types.
20. Model input rules describe the actual current state without executing an
    input. Prefixes and catch-alls are first-class; a model need not pretend its
    grammar is a flat keymap.
21. M7 callbacks cross the Lua boundary only as copied data and declarative
    semantic results. They never receive a mutable document/history, terminal
    object, parser/node handle, transaction constructor, or raw Lua value in a
    public Zenbu API. A staged generation replaces an active overlay only after
    complete evaluation and validation; a failed stage retains the prior one.

The following rule is architectural rather than merely local:

> Core mutation APIs must not expose arbitrary `mutable Editor` access to
> extensions.

The M2 dogfood invariant is equally strict:

> Anything a first-party editing model can do must ultimately be possible for a
> third-party editing model using the public API.

M3 verifies that invariant with separate Vim-style and selection-first clients.
Their grammar states differ, but each enters edits through the same command,
selector, transformation, transaction, and history path.
