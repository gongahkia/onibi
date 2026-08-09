# zenbu

Zenbu is a terminal-first programmable modal editor under development. This
repository contains M0-M4: a semantic kernel, public editing-model protocol,
two substantial first-party modal models, and the first interactive terminal
host. It deliberately contains no complete Vim/Helix/Kakoune implementation,
syntax service, or plugin runtime.

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

The checked environment uses OCaml 5.3.0 and Dune 3.20.2. The M4 host uses
`notty-community`, `uuseg`, and `uucp`; install project dependencies before
building a fresh checkout.

```sh
make check
make demo
dune exec bin/zenbu_headless.exe -- replay test/fixtures/unicode.replay
dune exec bin/zenbu_headless.exe -- session test/fixtures/sessions/vim-edit.session
dune exec bin/zenbu.exe -- --model vim README.md
```

`make check` runs Dune's formatting check, build, and the dependency-free unit,
property, and replay test executable. Install `ocamlformat` (0.28.1-compatible)
to run the formatter locally; if it is unavailable, Dune reports that rather
than silently skipping format validation.

On a machine without `ocamlformat`, bootstrap an ignored local opam switch once
before running `make check`:

```sh
opam switch create . ocaml-system --no-install
opam install . --deps-only
opam install ocamlformat.0.28.1
eval "$(opam env)"
make check
```

## Scope and layout

- `lib/` contains the public kernel modules. Text storage is hidden behind
  `Text_buffer`; all mutations are `Document.apply` transitions driven by
  validated `Transaction` values. It also contains the separate
  `zenbu.model_api` public library, which exposes logical input, constrained
  contexts, model effects, commands, and the runtime.
- `models/` contains the retained M2 proof models plus a Vim-style and a
  selection-first M3 model. Every model links only to `zenbu.model_api`, never
  directly to `zenbu.kernel`.
- `test/` contains deterministic unit/property tests and inspectable replay
  fixtures, including model-runtime and cross-model tests.
- `view/` projects immutable editor contexts into pure, terminal-independent
  frames. `terminal/` is the only layer that imports the terminal backend.
- `app/` owns a session's file path, saved version, dirty state, viewport, and
  host commands. `bin/zenbu.ml` is the interactive executable;
  `bin/zenbu_headless.ml` remains the deterministic session/replay runner.
- `docs/` records protocol semantics, invariants, architecture, roadmap, and
  durable architectural decisions.

See [architecture](docs/ARCHITECTURE.md), the [editing protocol](docs/EDITING_PROTOCOL.md),
and [invariants](docs/INVARIANTS.md) before extending the kernel.

## M4 terminal host

`zenbu [--model vim|selection] [FILE]` opens an existing UTF-8 file or an
unnamed empty buffer. `Ctrl-S` atomically saves an existing file. `Ctrl-Q`
exits a clean session; a dirty session requires a second `Ctrl-Q`. The
selection-first model is a different editing grammar over the same kernel, not
a compatibility mode.

The host restores terminal input, cursor visibility, and the normal screen on
normal exit and exceptions. It renders only the source lines in the viewport,
maps document byte offsets through grapheme clusters to display columns, and
keeps the primary selection visible. See [terminal host notes](docs/TERMINAL.md)
for lifecycle, persistence, coordinate, and terminal-width limits.

## Remaining limitations

Coordinates are UTF-8 byte offsets at Unicode code-point boundaries. They are
not grapheme-cluster, line/column, or terminal display-cell coordinates.
Anchors are snapshot-local: they intentionally do not survive arbitrary edits
unless carried forward by the transaction's documented selection transform.
Branching history is retained as an immutable tree, but no history UI or merge
policy exists yet.

The M3 Vim-style model implements a documented, intentionally incomplete
subset; it is not Vim compatible. The selection-first model is inspired by
Kakoune/Helix's select-then-transform principle, not a compatibility layer.
See [the Vim-style subset](docs/models/VIM.md) and the
[selection-first subset](docs/models/SELECTION_FIRST.md).

Terminal decoding and rendering are intentionally narrow: no mouse, bracketed
paste, terminal capability probing beyond the chosen backend, save-as prompt,
or model switching in a live session exists yet. Keymap configuration UI,
Tree-sitter, LSP, scripting, and plugin isolation remain deferred.

The recommended next goal is M5: add Tree-sitter integration and a structural
editing model through the same public model API.
