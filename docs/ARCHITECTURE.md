# Architecture

Zenbu M0-M5 is a functional semantic editing kernel plus public editing-model
and syntax protocols and a narrow terminal host. The central kernel transition is
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

## M5 syntax boundary

M5 adds a second optional source of selections without teaching the kernel what
a syntax tree is:

```text
tree-sitter binding
      ↑
private Tree_sitter_backend
      ↑
      zenbu.syntax
      ↑
zenbu.model_api.Editor_context.syntax
      ↑
zenbu.structural
```

`zenbu.syntax` owns language identities, a small registry, synchronous
services, version-bound syntax snapshots, opaque nodes, and generic structural
selectors. The Tree-sitter parser, tree, node, query, and FFI lifetime rules
are private implementation details. A snapshot holds an immutable kernel
document snapshot and cannot match a different document id/version. The service
has no global state; one service owns one parser and a bounded current snapshot
cache. It copies the old backend tree before applying transaction-derived edits
for incremental parsing, so a caller retaining an old Zenbu snapshot cannot
observe a tree mutation.

The runtime refreshes syntax before constructing a model context and updates a
cached syntax snapshot after each committed transaction. Undo/redo can safely
fall back to a full parse because syntax history is intentionally not retained.
The optional context field is absent for unknown languages or backend failure;
models must not infer syntax from byte heuristics.

The structural model is an ordinary client of `zenbu.model_api` and
`zenbu.syntax`. It owns keybindings and small expand/shrink offset history, but
derives visible selections through `Model_intent.set_selections` and edits
through existing intents/effects. It does not link to Tree-sitter, another
model, the terminal, or kernel mutation APIs. See [syntax](SYNTAX.md) and
[structural model](models/STRUCTURAL.md).

## M4 host and view boundary

M4 adds a second one-way dependency path without changing the editing-model
contract:

```text
notty-community
      ↑
zenbu.terminal (opaque backend adapter)
      ↑
    bin/zenbu
      ↑
 zenbu.app ─────── zenbu.view
      ↑                 ↑
zenbu.model_api ─── immutable Editor_context
      ↑
 first-party models
```

`zenbu.terminal.Backend` is the sole module that imports Notty and owns raw
input, alternate-screen, cursor, resize, and event conversion. Its public
surface exposes only Zenbu terminal events and pure `Frame` values. The kernel
and model libraries neither link to nor name a terminal backend.

`zenbu.app.Session` is a coherent immutable session value: active model
runtime, file path, saved document version, viewport, terminal dimensions,
message, and quit confirmation. It handles host-only save and quit policy;
models still receive only logical input and return semantic effects. The pure
view layer converts immutable context selections to styled cells, while the
backend alone places the physical cursor. See [terminal host notes](TERMINAL.md)
and ADRs 0011-0013.

## M6 observability boundary

M6 observes the existing semantic path rather than creating a second editing
path:

```text
logical input → model transition → model effect → intent/command
              → transaction → history → syntax refresh
```

`zenbu.kernel.Provenance` is deterministic transaction metadata: an ordered
chain may name a model/provider, interaction, effect, command/provider,
selector, transformation, or semantic repeat. It never contains timing,
terminal, parser, or backend values. `zenbu.model_api.Trace` and `Profiler` are
explicit runtime-owned bounded local services; their records are observations,
not replay input or transaction equality.

`Editing_model.S.input_rules` supplies model-owned descriptions of the current
grammar state. The generic inspector consumes descriptors, statuses, rules,
history views, syntax snapshots, traces, and profiles. It does not import
Vim, selection-first, structural, Tree-sitter, or Notty internals. The app
selects a registered runtime only to obtain generic values; the renderer sees
ordinary inspector text lines and frame values.
