# Editing-model API

M2-M4 defines the public boundary through which an editing grammar turns
logical input into semantic editing. A model is not a terminal backend and it
is not a privileged part of the editor.

```text
terminal decoder
          ↓
     Input_event
          ↓
  Editing_model.handle_input
          ↓
   Model_effect values
          ↓
    Model_runtime
          ↓
      Intent / command
          ↓
 transaction + history commit
```

The runtime is synchronous and functional at its public boundary: given the
same model state, input event, editor context, command registry, and document
history, a deterministic model produces the same next state and semantic
effects. The runtime stores no mutable global editor object.

## Logical input

`Input_event` describes decoded, logical input. A `Key_press` contains a
logical key (`Logical_text` or a named key such as `Escape`/`Enter`), a
normalized modifier set, and optional physical-key metadata. `Text_input`
represents committed UTF-8 text, such as IME output or paste.

Physical key metadata is diagnostic only; editing models should interpret the
logical key. `Logical_text "d"` is a key the model may treat as a command;
`Text_input "d"` is committed text that an insertion state may choose to turn
into `insert-text`. Neither automatically changes the document. This avoids
US-QWERTY assumptions and keeps terminal escape decoding below the API.

M2 intentionally excludes mouse, resize, raw terminal bytes, release events,
Notty, Lambda-Term, and curses from editing grammar input.

## Context and lifecycle

An `Editing_model.S` has an opaque `state` plus `initialize`, `handle_input`,
`reset`, and `status` functions. The runtime is a functor over that module only
so it can retain model state without inspecting it. It does not know what a
model's states mean.

`Editor_context` is an immutable snapshot facade. It exposes active document
id/version, contents, byte length, selections as anchor/head byte offsets,
registered command descriptors, read-only clipboard-slot entries, and an
optional matching `zenbu.syntax` snapshot. The syntax value contains only
Zenbu's opaque snapshot/node abstraction, never a parser, tree, query, or
backend handle. It is absent for unsupported languages and is filtered out if
its document id/version does not match the context snapshot. The context
deliberately does not expose a mutable document, `Document.apply`, history
internals, text-buffer representation, transaction construction, terminal
state, or arbitrary callbacks.

`Model_status` is generic: stable id, human label, optional description,
optional pending input, small metadata, and an input disposition. The latter is
either `Key_commands` or `Text_entry`. It is not a mode name: the terminal
decoder uses it only to decide whether an unmodified printable terminal event
becomes `Key_press` or committed `Text_input`. This lets the host preserve
command grammars and insertion text without inspecting a Vim/selection state
or model id. A model with no modes fits equally well.

## Effects, selectors, and transformations

Model effects are values, never closures:

- `Execute_intent` requests a model-neutral semantic intent.
- `Invoke_command` names a registered command id and typed arguments.
- `Emit_message` reports an inspectable message.

`Model_intent` is the model-facing facade for M1 intents plus selector/
transformation composition. M3 adds word, line, document, vertical, and literal
all-occurrences selectors along with generic `collapse to start/end`
transformations. The runtime converts it to the kernel intent and lets existing
transaction/history validation perform the mutation.

M5 adds generic syntax commands (`syntax.focus`, `syntax.parent`,
`syntax.child`, `syntax.next-sibling`, `syntax.previous-sibling`,
`syntax.expand`, and `syntax.select-same-kind`). Their handlers use only the
optional abstract syntax snapshot and return ordinary `set-selections` intents.
They do not encode a structural-model keybinding, an OCaml node kind, or a
Tree-sitter query. Syntax-aware models can use the same public syntax selectors
directly when they need model-owned status or navigation history.

Selectors answer *which regions?*; transformations answer *what happens?*.
The M3 models share `next-word`, line, and delete selectors/transformations,
though one obtains them after an operator and the other makes them visible
first. Regex, LSP, grapheme, and display-width selectors remain excluded.

## Commands

`Command_descriptor` contains a stable `Command_id`, title, optional
description/category, a small named parameter list, and examples. The explicit
immutable `Command_registry` rejects duplicate ids, lists descriptors in id
order, and invokes commands semantically. Command handlers receive only an
`Editor_context` and return `Model_intent` values.

Bindings are not commands. A model may interpret a key grammar however it
wants, then invoke `editor.apply` or another command directly; it never needs
to synthesize keystrokes. M2 has no global keymap language.

## Runtime services and M3 effects

M3 retains declarative effects and adds model-neutral effects to copy a selector
to a clipboard slot, paste a slot at a documented placement, undo, redo, and
repeat the latest repeatable semantic edit. The runtime owns these immutable
services; a model cannot mutate a document, history, or clipboard directly.
Clipboard slots are generic UTF-8 entries with characterwise or linewise
shape. A grammar may call a slot a register, but the API does not.

## Runtime behavior and traces

`Model_runtime.Make(Model).handle_input` builds a fresh context, calls the
model, then interprets effects in order. Intents commit through `History` using
the existing transaction path. The returned step exposes input, effects,
intents, messages, committed change ids, resulting version, and status.

If an effect or command fails, the call returns its specific error. The
runtime's prior model state, history, and input trace remain unchanged.

The runtime's `input_trace` is intentionally separate from M1 semantic replay:

```text
input trace:       d, w
semantic replay:   apply(next-text-unit, delete)
```

Input traces help state-machine tests and debugging. Semantic replay remains
model-independent and is suitable for macros, bug reports, and automation.

## Headless sessions

`zenbu-headless session <file>` runs a small inspectable session format for
tests and debugging. A fixture has `model=vim`, `model=selection-first`, or
`model=structural`; structural fixtures set `language=ocaml`. One
escaped `text=` line, `input=` logical key lines, and `text-input=` committed
UTF-8 text lines. Named `Escape`, `Backspace`, `Enter`, and `Ctrl-r` inputs are
also supported. The runner prints model status transitions, declared effects,
semantic intents, resulting documents, selections, and history. It is not a
user configuration language or terminal-event format.

`zenbu-headless syntax <file>` prints stable language/document identity and the
named-node tree (kind, byte range, named/error/missing flags), never backend
pointers.

## M4 terminal input

M4's terminal adapter maps Unicode printable keys, Escape, Enter, Backspace,
Tab, arrows, Home/End, Delete, Ctrl/Alt/Meta/Shift modifiers, and resize into
host events. Only key events pass through `Input_decoder` into `Input_event`;
resize, save, quit, terminal lifecycle, and physical cursor presentation stay
above the model API. The adapter consults `Model_status.input_mode`, never a
model status id or private model state. Mouse and bracketed paste are disabled
for this milestone.

## M3 API Pressure Test

The M2 state-machine API handled pending grammars, counts, statuses, committed
Unicode input, command invocation, immutable contexts, and trace separation
unchanged. M3 exposed four concrete deficiencies:

1. Character selectors could not express reusable word, line, vertical, or
   literal-occurrence targets. M3 added generic selectors in the kernel rather
   than Vim motions or Helix selections.
2. Navigation needs to turn a target range into a caret without losing selector
   reuse. M3 added `collapse-to-start/end` transformations rather than a
   model-specific movement API.
3. Copied text must survive beyond one grammar state and be usable by both
   models. M3 added runtime clipboard slots with neutral text-shape metadata;
   no model gets a buffer escape hatch.
4. Undo/redo and limited semantic repeat require runtime/history ownership.
   M3 added declarative effects rather than private Vim histories or raw-key
   replay.

Counts, operator pending state, text-object prefixes, model statuses, desired
grammar semantics, and slot-prefix syntax remain model-owned. The Vim-style
model uses selector targets as operator operands; the selection-first model
makes selectors visible first and subsequently transforms the current set.
Both use `editor.apply`, so their cross-model equivalence is semantic rather
than keybinding-based.

## M5 Syntax/API Pressure Test

M0-M4 documents, snapshots, UTF-8 anchors/ranges, transactions, history,
selection sets, semantic intents, clipboard effects, repeat, status rendering,
terminal decoding, and the view survived unchanged. The sole model-context
addition is optional `Editor_context.syntax`; it is version-filtered and owns
no mutation authority.

Transactions supplied sufficient edit ranges and replacement text to derive
Tree-sitter byte/point edits without changing transaction semantics.
Snapshot-local anchors aligned directly with syntax ranges after revalidation
through `Document_snapshot.range`. Old syntax snapshots are never exposed in a
new context, and the runtime refreshes synchronously before model invocation.

The public API nearly leaked Tree-sitter node and query types; M5 instead keeps
opaque Zenbu nodes, grammar-textual `Syntax.Kind`, and a small structural
selector vocabulary. The structural model navigates AST relationships with
arrows and produces selections before shared transformations. Its `.` repeats
the last shared textual intent over the current selection; it deliberately does
not re-run a previous syntax selector. Plugin API stabilization should still
decide how language packages register grammars and how a future background
syntax worker delivers versioned results.

## M2 proof models

`zenbu.proof_models` contains intentionally incomplete examples, both linked
only to `zenbu.model_api`:

```text
operator-first:    d → pending delete; w → delete(next-text-unit)
selection-first:   w → select(next-text-unit); d → delete(current-selections)
```

Thus `d w` means different things under the two models, while operator-first
`d w` and selection-first `w d` converge on the same semantic deletion. This
is the M2 architectural proof, not a Vim/Helix/Kakoune compatibility claim.

## M6 observability/API pressure test

M2 command descriptors needed provider identity, but their ids, descriptions,
parameters, examples, and deterministic registry were otherwise sufficient.
M2 status described current labels and pending input but needed model-owned
`Input_rule` values to make multi-step grammars inspectable without flattening
them into a keymap. Rules support exact/named inputs, prefixes, compact ranges,
and committed-text catch-alls without executing an input.

M3 effects and M1 transactions had enough semantic identity once provenance
was added at the runtime boundary. M1 history needed a public read-only tree
view, not a structural exposure of its map. M5 syntax already exposed enough
Zenbu-owned node/status data once service strategy and current-node inspection
were added; neither Tree-sitter nor terminal types enter the inspector.

M7 now registers commands/selectors/transformations through the same
descriptors, providers, effects, semantic behavior registry, and provenance.
Its Lua adapter supplies data-only callback results and has no public mutable
editor object. A session installs a validated generation as a runtime overlay,
so the editing-model API does not gain an M7-only mutation route. Dynamic
semantic operations resolve to concrete transactions but are deliberately not
retained as repeatable model intents across reloads. This remains experimental
configuration, not a stable plugin SDK; see [scripting](SCRIPTING.md).

## M8/M9 runtime-neutral extension host

M8 adds `Extension_value` and `Extension_host` to `zenbu.model_api`, not a
second editor API. `Extension_value` is recursively data-only (`nil`, booleans,
integers/floats, text, lists, and string-keyed records). An `Extension_host`
request names an opaque invocation token, provider, contribution kind,
operation, capability grants, copied context, and data-only arguments; its
response is another `Extension_value`.

`Command` and `Semantic_behavior` can contain a local OCaml handler or an
extension-host invocation. The latter stores no runtime callback/value. The
runtime adapter owns the mapping from token to private callback and decodes the
same declarative effects/selection/edit results that the normal runtime already
validates. Capability checks shape copied context and action authority before
kernel mutation is considered. A failed invocation leaves the ordinary runtime
state unchanged and emits generic extension trace/profile information.

This keeps M8 plugins and the M9 Component adapter as clients of the same
semantic command/selector/transformation path as first-party models. The
package/compatibility policy is intentionally outside this library; see
[extensions](EXTENSIONS.md).
