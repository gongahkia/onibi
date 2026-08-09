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
id/version, contents, byte length, selections as anchor/head byte offsets, and
registered command descriptors plus read-only clipboard-slot entries. It deliberately does not expose a mutable
document, `Document.apply`, history internals, text-buffer representation,
transaction construction, terminal state, or arbitrary callbacks.

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

Selectors answer *which regions?*; transformations answer *what happens?*.
The M3 models share `next-word`, line, and delete selectors/transformations,
though one obtains them after an operator and the other makes them visible
first. M3 still deliberately excludes regex, syntax, LSP, grapheme, and
display-width selectors.

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

## Headless M3 sessions

`zenbu-headless session <file>` runs a small inspectable session format for
tests and debugging. A fixture has `model=vim` or `model=selection-first`, one
escaped `text=` line, `input=` logical key lines, and `text-input=` committed
UTF-8 text lines. Named `Escape`, `Backspace`, `Enter`, and `Ctrl-r` inputs are
also supported. The runner prints model status transitions, declared effects,
semantic intents, resulting documents, selections, and history. It is not a
user configuration language or terminal-event format.

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
