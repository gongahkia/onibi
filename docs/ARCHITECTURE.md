# Architecture

Zenbu M0/M3 is a functional semantic editing kernel plus a public
editing-model protocol. The central kernel transition is
conceptually:

```text
old document + transaction -> new document
```

`Document.t` is immutable and owns a document id, version, text buffer, and
selection set. `Document.snapshot` creates an immutable
`Document_snapshot.t`, which is the only source of anchors and ranges. A
snapshot is identified by its document id and version and is safe to retain for
selectors, tests, and future background services.

`Text_buffer.t` is abstract. M0 uses a correct UTF-8 string implementation,
but no document-facing API reveals that choice. Future rope or piece-table
work belongs behind that abstraction.

Intent resolution is separate from mutation:

```text
semantic Intent + snapshot -> validated Transaction
Transaction + current matching document -> new document
```

Transactions, not callbacks or mutable editor state, are the only mutation
mechanism. The kernel validates the source document/version, edit ranges,
conflicts, UTF-8 text, and selections before constructing a new document. A
failed application returns an explicit error and leaves the old value intact.

`History.t` stores immutable nodes containing a transaction, semantic metadata,
and before/after documents. Nodes retain children, so committing after undo
creates a branch instead of discarding future history. `Replay` serializes
headless intents and transaction specifications, then reports the index of the
first failing action.

## M2/M3 model boundary

M2 adds this deliberately one-way dependency graph:

```text
zenbu.kernel
      ↑
zenbu.model_api
      ↑
 ┌────┴───────────────┐
zenbu.vim-style   zenbu.selection-first
```

`zenbu.kernel` owns documents, snapshots, selectors, transformations, intents,
transactions, history, and semantic replay. It has no input event, editing
model, command registry, normal mode, motion, or keybinding type.

`zenbu.model_api` supplies immutable logical `Input_event` values, a restricted
`Editor_context`, inspectable `Model_effect` values, commands and their
immutable registry, an immutable clipboard-slot service, and a synchronous
state-machine runtime. A model returns a new opaque state and effects; the
runtime is the only API component that interprets effects into existing kernel
intents, history navigation, or clipboard state.

The M2 proof models and M3 first-party models link only to `zenbu.model_api`.
Their source cannot call `Document.apply`, `History.commit`, or a storage
implementation; command handlers receive only `Editor_context` and return
semantic intents. Dune's separate library dependencies enforce this direct
dependency boundary. OCaml does not make public libraries a security sandbox,
so actual untrusted-plugin isolation remains an M8/M9 concern.

## Extension boundary

> First-party editing models and first-party plugins must eventually use only
> the same public editing APIs available to third parties. No editing model
> receives privileged access to editor mutation.

There is no `Editor` object with a public mutable escape hatch. M2 defines an
editing-model/state-machine API that resolves input into the M1 `Intent` and
`Transaction` interfaces. M0/M1 does not decide whether commands are
operator-motion, selection-action, structural, or something else.

M3 pressure-tested that API without moving an editing grammar into the kernel:
the Vim-style model owns Normal/Insert/OperatorPending/count state, while the
selection-first model owns its select-then-transform grammar. See [the
editing-model API](EDITING_MODEL_API.md) for the public protocol.
