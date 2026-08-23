# Architecture

Zenbu M0-M11 is a functional semantic editing kernel plus public editing-model,
syntax, trusted-local configuration, stable local extension protocols, an
isolated Component runtime, and a narrow terminal host. The central kernel
transition is
conceptually:

```text
old document + transaction -> new document
```

M10 adds a host interaction/presentation layer, not a second editing kernel.
Search, command discovery, save-as, help, model switching, and syntax styling
live in `zenbu.app`/`zenbu.view`; their selection and command operations still
enter the public model runtime as semantic effects. The terminal only turns
physical events into public input and paints frame styles.

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

M11 adds optional language intelligence above that boundary. `zenbu.language`
owns editor-facing configuration, coordinates, diagnostics, hover, locations,
completion, and text-edit values. `zenbu.lsp` is a private adapter using LSP
and JSON-RPC packages plus a local child process. Neither the kernel nor
`zenbu.model_api` imports it or exposes protocol values. Reader threads enqueue
owned events; only `zenbu.app.Session` drains them and turns accepted results
into selections or semantic transactions.

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
dependency boundary. OCaml does not make public libraries a security sandbox;
M9 therefore places isolated third-party Components behind a private runtime
rather than treating library visibility as a security boundary.

## Extension boundary

> First-party editing models and first-party plugins must eventually use only
> the same public editing APIs available to third parties. No editing model
> receives privileged access to editor mutation.

There is no `Editor` object with a public mutable escape hatch. M2 defines an
editing-model/state-machine API that resolves input into the M1 `Intent` and
`Transaction` interfaces. M0/M1 does not decide whether commands are
operator-motion, selection-action, structural, or something else.

M3 pressure-tested that API without moving an editing grammar into the kernel.
The Vim compatibility model owns Normal/Insert/Replace/Visual, operator, count,
find, and register state; the selection-first model owns its
select-then-transform grammar. Both request only public semantic effects. See
[the editing-model API](EDITING_MODEL_API.md) and [modal model
evaluation](MODAL_MODEL_EVALUATION.md) for the public protocol and workload
criteria.

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
runtime, file path, saved document version, a focused view and per-view
viewports, terminal dimensions, message, and quit confirmation. The pure
`zenbu.view.Layout` composes same-buffer view frames in a binary vertical or
horizontal tree; it sees only immutable frames and has no document mutation
path. It handles host-only save, quit, and view-layout policy; models still
receive only logical input and return semantic effects. The pure view layer
converts immutable context selections to styled cells, while the backend alone
places the physical cursor. This is not yet a buffer-to-pane workspace. See
[terminal host notes](TERMINAL.md), [editor workload evaluation](EDITOR_WORKLOAD_EVALUATION.md),
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

## M7 scripting boundary

M7 inserts a generation-scoped data adapter above the public APIs, never a new
kernel mutation route:

```text
PUC Lua 5.4 / standard libraries
      ↑ private Ctypes adapter
zenbu.scripting registration + data conversion
      ↑ commands / semantic behavior registry / bindings / hooks
zenbu.app.Session staged generation overlay
      ↑ ordinary zenbu.model_api runtime
intent → transaction → history → syntax refresh
```

The Lua adapter owns states, callback references, source diagnostics, and all
Lua conversions. `zenbu.scripting` exposes only immutable registrations and
data-returning callbacks. A script command returns `Model_effect` values;
selectors return selection data; transformations return edit proposals. The
runtime remains responsible for converting data to `Model_intent`, validating a
transaction, committing history, and collecting provenance. Script behavior
therefore has the same undo, syntax-refresh, inspection, and error boundaries
as builtin behavior.

`Session` owns an optional active generation alongside immutable base command
and semantic registries. Reload stages a fresh full generation before calling
`Model_runtime.with_extensions`; only a successful stage replaces the overlay,
then disposes the prior Lua state. Binding precedence is model+status, model,
then global; hooks are session policy over committed document changes and saves.
The terminal keeps save/quit/reload controls outside this binding resolver.

Lua receives data-only contexts and optional data-only syntax node summaries.
No Lua state, terminal value, mutable history/document handle, or Tree-sitter
value enters `zenbu.kernel` or `zenbu.model_api`. Standard Lua libraries make
this trusted local execution rather than a security boundary. See
[scripting](SCRIPTING.md) and ADRs 0019-0021.

## M8/M9 extension boundary

M8 makes the callback path stable without exposing a Lua implementation value
to generic registries:

```text
zenbu-plugin.toml → zenbu.extension.Manifest → Plugin_host discovery/staging
                                                ↓
PUC Lua adapter (private callback token map) ← Extension_host request/response
                                                ↓
Command / Semantic_behavior / bindings / hooks
                                                ↓
Session snapshot composition → zenbu.model_api runtime → transactions/history
```

`zenbu.extension` is the public contract library for plugin identifiers,
versions, capabilities, contributions, manifest parsing, lifecycle host, and
generated-contract metadata. The kernel and editing models do not depend on it.
`Extension_host` belongs to `zenbu.model_api` because generic commands and
semantic behaviors must invoke every runtime through the same interface. Their
entries contain a host, opaque token, provider metadata, granted capabilities,
and data-only encoder/decoder only; the Lua adapter alone maps a token to a
private callback. No Lua value, document/history object, terminal value, or
Tree-sitter pointer is present in a generic registry.

`Session` composes builtin registries, the optional M7 configuration generation,
then active plugins sorted by plugin ID. Plugin staging validates a complete
candidate before replacement; failures retain that package's last known-good
snapshot. Configuration bindings/hooks precede plugin bindings/hooks, while
the ordinary model runtime still owns semantic resolution, transactions,
history, syntax refresh, and provenance.

M8's trusted local Lua runtime remains available. Its capabilities constrain
Zenbu host services, not Lua standard-library authority. Discovery only
enumerates local XDG directories; it has no resolver, download, or
project-search behavior.

M9 adds a second, isolated `wasm-component` adapter without changing the
generic registry shape:

```text
compiled WebAssembly Component / typed WIT values
      ↑ private Wasmtime 47 C shim (engine/store/linker per generation)
zenbu.extension.Wasm_plugin / opaque Extension_host token
      ↑ commands / semantic behavior registry / bindings / hooks
Session snapshot composition → zenbu.model_api runtime → transactions/history
```

The Component world has control exports only. Zenbu links no WASI or host
imports, so filesystem/network/process/environment/terminal/parser objects are
absent by construction. `Extension_host` continues to project capability-checked
copies into every request, and the adapter converts only serialisable WIT values
at its private edge. The result still enters normal effect decoding, semantic
resolution, transaction validation, syntax refresh, history, provenance, and
generic trace/profile services. A Wasm Component therefore has no privileged
editing route.

Each Component generation owns a private Wasmtime store with a manifest-visible
memory cap and a fuel budget reset before `register`/`invoke`. Plugin staging,
collision checks, reload retention, disposal, and provider ordering remain
shared `Plugin_host` behavior. M9's synchronous fuel-limited calls do not yet
provide async/background scheduling or hard wall-clock cancellation. See
[extensions](EXTENSIONS.md), [Component authoring](WASM_COMPONENTS.md), the
[isolation policy](ISOLATION.md), [M9 pressure test](M9_PRESSURE_TEST.md), and
ADRs 0022-0028.

## M11 language-service boundary

```text
Language.Registry → zenbu.lsp private stdio client → local language server
                            ↓ bounded event inbox / wakeup fd
                     zenbu.app.Session (main thread)
                         ↓                   ↓
           diagnostic ranges/status     semantic selection/edit effects
                         ↓                   ↓
                    zenbu.view         model runtime → transactions/history
```

The process adapter owns initialize/shutdown, capability negotiation,
position/sync conversion, request ids, cancellation, framing limits, stderr,
and child cleanup. It exposes only `zenbu.language` values and a wakeup
descriptor to the host. Session accepts a versioned diagnostic only for the
current snapshot and drops stale feature results; it rejects any returned
workspace edit that includes a non-active URI. The terminal waits on stdin and
the wakeup descriptor, then renders diagnostics and overlays as view data.
Models neither parse LSP messages nor own an async loop. The public semantic
`language.apply-edits` transformation is the sole document-mutation route for
completion, rename, and accepted server edits. See
[Language services](LANGUAGE_SERVICES.md) and ADRs 0030-0032.
