# Editing-model DSL (`.zenmodel`)

A `.zenmodel` file defines an editing grammar. It does not implement a second
document/editing runtime.

The v1 DSL is for finite, inspectable input grammar: named states, logical
input bindings, deterministic multi-event prefixes, and a very small set of
semantic effects. It is intentionally not a replacement for OCaml or trusted
Lua when a model needs variables, general expressions, registers, asynchronous
work, external services, or complex selectors and transformations.

## Boundary and authority

`zenbu.model_dsl` depends on `zenbu.model_api`; neither `zenbu.kernel` nor the
semantic kernel depends on the DSL. Parsing and compilation receive a source
name and source string from their host. The DSL library does not read files.

At runtime the compiled grammar implements `Editing_model.S`. For each logical
input it returns a new immutable model state and existing `Model_effect`
values. `Model_runtime` remains the only component that resolves those effects
into semantic intents and the existing validated transaction/history path.
The DSL interpreter never mutates documents, selections, syntax state, history,
the terminal, or the renderer.

`.zenmodel` has no filesystem, process, shell, terminal, renderer, plugin,
Lua, Wasm, timer, or asynchronous authority. Trusted Lua remains trusted local
code. Wasm Components remain separately capability-constrained through the
Extension API. Semantic replay remains model-neutral: it records resolved
semantic operations rather than `.zenmodel` source or key grammar.

## Source format

Every file starts with the required compatibility header:

```text
zenbu-model 1
```

Version 1 accepts one model declaration. For example:

```text
zenbu-model 1

model "zenbu.example.modal" {
  title "Minimal modal"
  initial normal

  state normal {
    status {
      label "NORMAL"
      input keys
    }

    on "i" -> insert
    on "d w" -> normal {
      apply selector "current-word" transform "delete"
    }
  }

  state insert {
    status {
      label "INSERT"
      input text
    }

    on "Escape" -> normal
    on "<text>" as text -> insert {
      insert $text
    }
  }
}
```

Model IDs and titles are strings. State names are identifiers. Each state has
exactly one `status` block with a visible `label` and either `input keys` or
`input text`. States are flat; the model has exactly one declared `initial`
state.

Input strings use the existing `Input_event` binding spelling, including named
keys and modifiers. A sequence uses one ASCII space between tokens and retains
Zenbu's maximum sequence length of sixteen events. The DSL does not define a
separate key-name grammar.

## Inputs and prefixes

`on "d w"` is compiled into a deterministic per-state immutable trie. After
`d`, the model emits no effect, keeps `normal` as its stable source state, and
reports `pending_input = "d"` through `Model_status`. Generic inspection sees
the first event as an `Input_rule.Prefix`.

If the next input does not continue a pending prefix, the mismatching event is
consumed, the pending prefix is cleared, the model returns to its stable source
state, and no effect runs. The event is not reconsidered as a fresh binding.
There are no prefix timeouts.

To avoid precedence or timing rules, a complete sequence cannot also prefix a
longer complete sequence in one state. For example, `on "g"` together with
`on "g g"` is rejected; write an explicit intermediate state instead.
Duplicate and unresolved overlapping patterns are rejected as well.

`<text>` means one committed UTF-8 `Text_input` event. In v1 it must be the
entire transition pattern and may appear only in a state with `input text`.
It may be captured using `as NAME`. `insert $NAME` requires that capture and
preserves the committed text exactly. Forms such as `"a <text>"` and
`"<text> Escape"` are rejected intentionally, so terminal and IME behavior
remains deterministic. A singleton text rule is exposed as an
`Input_rule.Catch_all`.

## Effects

Only two effect forms are available in v1:

```text
apply selector "current-word" transform "delete"
insert $text
```

Selector IDs resolve via `Model_intent.selector_of_string`; transformation IDs
resolve via `Model_intent.transformation_of_string`. A successful `apply`
becomes the existing `Model_effect.execute (Model_intent.apply ...)` value,
with selector/transformation metadata retained for inspection. Captured text
becomes `Model_effect.execute (Model_intent.insert_text text)` at transition
time. Unknown IDs, missing captures, and unsupported syntax are diagnostics;
there is no direct mutation form.

## Validation and diagnostics

The compiler validates source UTF-8 using the kernel text-buffer validator and
retains byte offsets. Diagnostics include the source name, byte-derived line
and column, severity, and a focused message. Validation covers the language
version, model title/initial state, duplicate states, state targets, input
syntax and length, ambiguity, text capture restrictions, and selector and
transformation names.

The grammar is fully parsed and validated before it is configured as a model.
The v1 compiler has no warnings today, but its structured diagnostic result
keeps room for non-fatal checks such as unreachable-state warnings.

## Headless tools

The headless host owns filesystem access:

```sh
dune exec bin/zenbu_headless.exe -- model-check examples/script-modal-editor.zenmodel
dune exec bin/zenbu_headless.exe -- model-describe examples/script-modal-editor.zenmodel
```

`model-check` prints structured diagnostics and returns nonzero for an invalid
grammar. `model-describe` prints a deterministic view of the language version,
model metadata, states and status modes, transitions, direct effects, generated
prefix nodes, transition source locations, and a non-security MD5 source
fingerprint.

## Interactive use

Pass a grammar path explicitly when starting the terminal host:

```sh
dune exec bin/zenbu.exe -- \
  --model-dsl examples/script-modal-editor.zenmodel \
  path/to/file.txt
```

`--model-dsl PATH` and `--model` are mutually exclusive. The CLI reads the
grammar, validates UTF-8, parses, validates, and compiles it before terminal
initialization. Errors are rendered with their source position and abort
startup; Zenbu never enters the editor with a partially valid grammar.

The initialized DSL state owns the immutable compiled grammar. Input handling
does not reread or reparse the source file, and it does not consult a grammar
registry. The active model is a normal `Model_runtime.Make` instance, so
interactive edits still follow the ordinary semantic validation,
transaction/history, syntax, replay, trace, and inspection paths. The generic
status and bindings inspector exposes state labels, pending prefixes, and
`Input_rule` values; `model-describe` remains the richer static grammar view.

Because `Editing_model.S` has a static `initialize` signature, the adapter has
a narrow process-local configuration slot used only around the synchronous
configure/initialize pair. Adapter initialization consumes and clears that slot,
and Zenbu also clears it with protected cleanup around the pair. It is never
read during input handling; every active state, including states in inactive
buffers, retains its own immutable grammar.

There is no hot reload. Supply `--model-dsl PATH` on each startup. DSL model
selection is deliberately not stored in saved layouts, so raw grammar paths
are not serialized into workspace state; a DSL-backed layout save is rejected
before a layout file is written. Run `model-check` before interactive use in
authoring and CI workflows.

## Explicit v1 omissions

V1 deliberately omits guards, variables, counts, registers, arbitrary
expressions, actions, fragments, imports, command calls/arguments, general
semantic-operation calls, hierarchy, entry/exit actions, timers, async work,
callbacks, Lua/Wasm interop, and hot reload. Complex, programmable models
remain an OCaml or trusted-Lua concern; capability-limited extension behavior
remains the Wasm Component concern.
