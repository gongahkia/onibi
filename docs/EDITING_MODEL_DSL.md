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
