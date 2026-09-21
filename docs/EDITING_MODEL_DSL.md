# Editing Model DSL

`.zenmodel` is Zenbu's declarative editing-model DSL. It defines finite,
inspectable input/state grammar; it does not implement a second editing runtime.

Use `.zenmodel` for finite deterministic grammars. Use OCaml or trusted Lua
when a model needs arbitrary computation, complex mutable state, counts,
registers, macros, dynamic algorithms, or host automation.

## Quick start

Save this as `example.zenmodel`:

```text
zenbu-model 1

model "example.modal" {
  title "Example modal"
  initial normal

  action delete_word {
    apply selector "current-word" transform "delete"
  }

  state normal {
    status { label "NORMAL" input keys }
    on "i" -> insert
    on "d w" -> normal { do delete_word }
  }

  state insert {
    status { label "INSERT" input text }
    on "Escape" -> normal
    on "<text>" as text -> insert { insert $text }
  }
}
```

Validate, inspect, and run it:

```sh
zenbu-headless model-check example.zenmodel
zenbu-headless model-describe example.zenmodel
zenbu --model-dsl example.zenmodel file.txt
```

From a checkout, prefix these with `dune exec bin/zenbu_headless.exe --` or
`dune exec bin/zenbu.exe --` as appropriate.

## Language version and compatibility

Every file starts with exactly this major compatibility header:

```text
zenbu-model 1
```

`zenbu-model 1` is the source-language major version. Zenbu may add optional
syntax within major version 1 only when existing valid version-1 files retain
their exact meaning. Incompatible syntax or semantic changes require a future
`zenbu-model 2`; unsupported major versions fail before activation. References
to “v1.1” describe implementation/release history, not another source header.

`#` starts a line comment. Source is validated as UTF-8 before lexing, and
diagnostics retain byte offsets plus source line and column.

## Model structure

A file declares one model with a non-empty string ID, one non-empty `title`,
one `initial` state, optional model-level actions, and flat named states:

```text
model "example.id" {
  title "Visible title"
  initial normal
  action name { ... }
  state normal { ... }
}
```

State and action names are identifiers. State names must be unique; initial
names and transition targets must name a declared state.

## States and status

Every state has exactly one status block:

```text
state normal {
  status {
    label "NORMAL"
    input keys
  }
}
```

`label` is generic model-status text. `input keys` keeps ordinary printable
terminal input as logical key presses; `input text` makes committed printable
input `Text_input`. Named and modified keys, such as `Escape` and `Ctrl-x`,
remain available in either disposition. States are flat and finite; there are
no nested/parallel states, timers, entry/exit actions, or variables.

## Input patterns and prefixes

Transition syntax is:

```text
on "INPUT SEQUENCE" [as capture] [when selection.any_nonempty | else] -> target {
  ACTIONS
}
```

The body is optional for a pure state change. Input spelling is Zenbu's
existing logical `Input_event` spelling, including named keys and modifiers.
The DSL defines no second key-name grammar, and sequences have at most sixteen
events.

`on "d w"` compiles to an immutable per-state trie. After `d`, Zenbu reports
`pending_input = "d"` through `Model_status` and generic inspection reports a
prefix. No effect runs until the sequence completes. Duplicate and unresolved
overlap patterns are errors. A completed sequence may not prefix another one:
`"g"` with `"g g"` is rejected; use an explicit intermediate state instead.

### Prefix mismatch

There are no sequence timeouts. A valid prefix followed by an invalid
continuation consumes that continuation, clears the prefix, restores the stable
source state, emits no effect, and does not reprocess the continuation.

## Text input and captures

`<text>` matches exactly one committed UTF-8 `Text_input` event:

```text
on "<text>" as text -> insert { insert $text }
```

It must be the whole pattern and may appear only in an `input text` state. A
capture is required by the matching `insert $NAME` effect and preserves the
exact committed byte sequence. `"a <text>"` and `"<text> Escape"` are
rejected. A singleton text binding is an `Input_rule.Catch_all` in generic
inspection.

## Actions

Actions are compile-time reuse of ordinary DSL effects:

```text
action delete_word {
  apply selector "current-word" transform "delete"
}

on "d w" -> normal { do delete_word }
```

Actions have no parameters, closures, state, dynamic dispatch, or runtime call
stack. `do NAME` expands before activation, so runtime IR contains only
ordinary immutable effect templates. An action may contain `apply`, eligible
`command`, and `do`; captures such as `$text` are forbidden in actions.
Unknown, duplicate, empty, and directly or indirectly recursive actions are
static errors.

## Guards

The only version-1 predicate is `selection.any_nonempty`: a synchronous,
deterministic, read-only `Editor_context` check that is true when at least one
selection has different anchor and head offsets.

```text
on "Backspace" when selection.any_nonempty -> direct {
  apply selector "current-selections" transform "delete"
}
on "Backspace" else -> direct {
  apply selector "previous-text-unit" transform "delete"
}
```

Identical complete patterns may form one guarded group: one
`when selection.any_nonempty` arm and an optional final `else`. Duplicate
predicates, duplicate `else`, an early `else`, or mixing an unguarded arm with
guarded arms are errors. Input matching happens before guard evaluation, so a
guard is not evaluated while a prefix is pending. A completed match with no
successful guard and no `else` is consumed as a stable-state no-op; it is not
reprocessed. There are no boolean operators, negation, comparisons,
user-defined predicates, callbacks, or expression language.

## Effects

Effects are requested values. The DSL interpreter does not execute changes.

### `apply`

```text
apply selector "current-word" transform "delete"
```

Selector IDs resolve through `Model_intent.selector_of_string`; transformation
IDs resolve through `Model_intent.transformation_of_string`. This returns the
existing `Model_effect.execute (Model_intent.apply ...)` value with inspection
metadata.

### `insert`

```text
insert $text
```

This is valid only for the capture on the same singleton `<text>` transition.
It returns the existing `Model_effect.execute (Model_intent.insert_text text)`
value.

### `command`

```text
command "editor.selection.merge-consecutive"
```

This returns the existing `Model_effect.Invoke_command` value. The ordinary
`Model_runtime` registry remains the only command executor; the DSL evaluator
does not call command handlers.

## Registered-command eligibility

The host supplies a read-only registry while compiling or activating a grammar.
A command must have a valid ID, exist in that registry, and have no required
parameters. The DSL supplies no command arguments or implicit defaults.

Eligibility is an existing-metadata rule, not a vague “safe command” label:
the descriptor category must be `selection` and the provider kind must be
`Editing_model`. This admits model-neutral selection algebra such as merge,
primary rotation, flip, and ensure-forward. It excludes host/UI, filesystem,
process, terminal, syntax, script, plugin, and extension commands. Unknown,
ineligible, and argument-requiring commands fail before activation.

## Static validation and diagnostics

Before activation, Zenbu validates UTF-8, header/version, model metadata,
state/action uniqueness, initial and target names, input spelling and length,
ambiguity, text captures, action expansion, guarded arms, selectors,
transformations, and command eligibility. Diagnostics are source-positioned:

```text
path/to/model.zenmodel:14:8: error: unknown state `insertz`
```

Normal user errors do not start an editor, cause a backtrace, or create a
partially valid grammar.

## Runtime semantics

The compiled grammar is immutable. Initialized model state owns its grammar and
pending-prefix position; input handling does not reread the source, reparse it,
consult a grammar registry, perform I/O, use time/randomness, or retain a
mutable document/context reference.

```text
DSL state + logical input + immutable Editor_context
  -> next DSL state + existing Model_effect values
  -> Model_runtime
  -> existing semantic intent / transaction / history / syntax path
```

Effect failure uses ordinary `Model_runtime` error and atomicity semantics. The
DSL adds no rollback or mutation path.

## Inspection, replay, and provenance

`model-describe` is static DSL inspection. It deterministically reports model
metadata, states, status/input disposition, actions, bindings, guarded arms,
commands/effects, source locations, and understandable prefixes.

Generic runtime inspection remains model-neutral: `Model_status`,
`pending_input`, `Input_rule`, bindings, trace, and `why` work as for built-in
models. Generic input rules may not explain every guarded arm; use
`model-describe` for source-level details.

Semantic replay remains model-neutral. It records resolved semantic behavior,
not a `.zenmodel` source/path/span. Existing model/input trace and command
provenance remain ordinary runtime data; the kernel provenance schema is
unchanged.

## Headless and interactive use

The host, not `zenbu.model_dsl`, reads files:

```sh
zenbu-headless model-check MODEL.zenmodel
zenbu-headless model-describe MODEL.zenmodel
zenbu --model-dsl MODEL.zenmodel FILE
```

`model-check` prints `model-check: ok: PATH` on success and exits nonzero with
diagnostics on failure. `model-describe` requires a valid model. `--model-dsl`
loads, validates, and compiles before terminal raw mode; it is mutually
exclusive with `--model`.

There is no hot reload. DSL-backed layouts do not persist grammar paths; save
is rejected before a layout file is written. Restart with `--model-dsl PATH`.

## Authority and security

`.zenmodel` is inert declarative grammar, not a general-purpose programming
environment or security sandbox. It has no direct document/history/selection/
syntax mutation API and no filesystem, shell, process, terminal, renderer,
plugin, Lua, Wasm, timer, async, or arbitrary-host-object authority. Its only
authority is its fixed effect forms and the command eligibility rule above.

Trusted Lua is different: it is trusted local general-purpose code with Lua
standard-library authority and is explicitly not sandboxed. Wasm Components
are different again: executable extensions isolated and capability-constrained
by the existing Component/Extension API. Neither boundary is weakened by DSL.

`zenbu.kernel` does not depend on `zenbu.model_dsl`; the DSL library depends
downward on `zenbu.model_api` only.

## Supported and deferred capabilities

| Capability | Status |
| --- | --- |
| Flat states, status labels, logical sequences, prefixes | Supported |
| Singleton committed-text capture | Supported |
| Compile-time actions | Supported |
| `selection.any_nonempty` with final `else` | Supported |
| Eligible no-argument selection commands | Supported |
| Fragments and imports | Not currently supported |
| Action parameters/captures; command arguments; extra guards | Not currently supported |
| Variables, counts, registers, macros, arbitrary computation | Use Lua or OCaml |
| Filesystem, process, terminal, renderer authority | Not exposed |
| Wasm extension implementation | Separate capability-constrained mechanism |

Shipped examples: `script-modal-editor.zenmodel` (minimal),
`modal-operator.zenmodel` (prefixes/actions), `selection-first.zenmodel`
(eligible commands), and `direct.zenmodel` (guarded direct editing).
