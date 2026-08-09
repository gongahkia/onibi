# zenbu

Zenbu is a terminal-first programmable modal editor under development. This
repository contains only M0/M1: a headless, model-neutral semantic editing
kernel. It deliberately contains no terminal UI, keybinding grammar, Vim
semantics, selection-first semantics, syntax service, or plugin runtime.

The project thesis is that no editing model is fundamental. A future Vim-like
model, selection-first model, structural model, and third-party model must all
be ordinary clients of the same public editing API.

> First-party editing models and first-party plugins must eventually use only
> the same public editing APIs available to third parties. No editing model
> receives privileged access to editor mutation.

> Input does not directly mutate text. Editing models eventually produce
> semantic intents. Semantic intents resolve into transactions. The kernel
> validates and atomically commits transactions.

The core mutation API exposes immutable documents and explicit transitions; it
does not expose arbitrary `mutable Editor` access to extensions.

## Quick start

The checked environment uses OCaml 5.3.0 and Dune 3.20.2. The kernel has no
third-party runtime or test dependencies.

```sh
make check
make demo
dune exec bin/zenbu_headless.exe -- replay test/fixtures/unicode.replay
```

`make check` runs Dune's formatting check, build, and the dependency-free unit,
property, and replay test executable. Install `ocamlformat` (0.28.1-compatible)
to run the formatter locally; if it is unavailable, Dune reports that rather
than silently skipping format validation.

On a machine without `ocamlformat`, bootstrap an ignored local opam switch once
before running `make check`:

```sh
opam switch create . ocaml-system --no-install
opam install ocamlformat.0.28.1
eval "$(opam env)"
make check
```

## Scope and layout

- `lib/` contains the public kernel modules. Text storage is hidden behind
  `Text_buffer`; all mutations are `Document.apply` transitions driven by
  validated `Transaction` values.
- `test/` contains deterministic unit/property tests and inspectable replay
  fixtures.
- `bin/` is a small headless demonstration, not a terminal editor.
- `docs/` records protocol semantics, invariants, architecture, roadmap, and
  durable architectural decisions.

See [architecture](docs/ARCHITECTURE.md), the [editing protocol](docs/EDITING_PROTOCOL.md),
and [invariants](docs/INVARIANTS.md) before extending the kernel.

## M1 limitations

Coordinates are UTF-8 byte offsets at Unicode code-point boundaries. They are
not grapheme-cluster, line/column, or terminal display-cell coordinates.
Anchors are snapshot-local: they intentionally do not survive arbitrary edits
unless carried forward by the transaction's documented selection transform.
Branching history is retained as an immutable tree, but no history UI or merge
policy exists yet.

The recommended next goal is M2: define the public editing-model/state-machine
API on top of this kernel.
