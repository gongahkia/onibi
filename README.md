# zenbu

Zenbu is a terminal-first programmable modal editor under development. This
repository contains M0-M7: a semantic kernel, public editing-model and syntax
protocols, three first-party editing models, local observability, a trusted-local
Lua configuration runtime, and an interactive terminal host.
It deliberately contains no complete Vim/Helix/Kakoune implementation, syntax
highlighting, LSP, or plugin runtime.

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

The checked environment uses OCaml 5.3.0 and Dune 3.24.2. The host uses
`notty-community`, `uuseg`, `uucp`, and the OCaml Tree-sitter binding; install
project dependencies and the system PUC Lua 5.4 shared library before building
a fresh checkout. On Fedora, the latter is supplied by `lua-libs`.

```sh
make check
make demo
dune exec bin/zenbu_headless.exe -- replay test/fixtures/unicode.replay
dune exec bin/zenbu_headless.exe -- session test/fixtures/sessions/vim-edit.session
dune exec bin/zenbu_headless.exe -- syntax test/fixtures/syntax_sample.ml
dune exec bin/zenbu_headless.exe -- why test/fixtures/sessions/observability-vim.session
dune exec bin/zenbu_headless.exe -- bindings structural
dune exec bin/zenbu_headless.exe -- config-check examples/m7-init.lua
dune exec bin/zenbu_headless.exe -- script-session examples/m7-init.lua test/fixtures/m7-wrap.session
dune exec bin/zenbu.exe -- --trace --profile --model structural test/fixtures/syntax_sample.ml
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
- `syntax/` is the public `zenbu.syntax` library. Its snapshots and nodes are
  Zenbu-owned, version-bound abstractions; its Tree-sitter backend is private.
- `models/` contains the retained M2 proof models plus a Vim-style and a
  selection-first M3 model and the M5 structural model. The structural model
  links only to `zenbu.model_api` and `zenbu.syntax`, never to Tree-sitter.
- `test/` contains deterministic unit/property tests and inspectable replay
  fixtures, including model-runtime and cross-model tests.
- `view/` projects immutable editor contexts into pure, terminal-independent
  frames. `terminal/` is the only layer that imports the terminal backend.
- `app/` owns a session's file path, saved version, dirty state, viewport, and
  host commands. `bin/zenbu.ml` is the interactive executable;
  `bin/zenbu_headless.ml` remains the deterministic session/replay runner.
- `docs/` records protocol semantics, invariants, architecture, roadmap, and
  durable architectural decisions.
- `scripting/` is the experimental `zenbu.scripting` library. It adapts PUC
  Lua 5.4 through a private Ctypes boundary and translates registrations and
  callbacks to public semantic APIs only.

See [architecture](docs/ARCHITECTURE.md), the [editing protocol](docs/EDITING_PROTOCOL.md),
the [observability model](docs/OBSERVABILITY.md), and [invariants](docs/INVARIANTS.md)
before extending the kernel.

## M5 syntax and structural editing

Tree-sitter is a private M5 backend. `zenbu.syntax` currently registers OCaml
(`.ml`, `.mli`) and JSON (`.json`) and exposes only language metadata,
version-bound syntax snapshots, opaque nodes, editing-useful traversal, and
generic structural selectors. Unknown extensions simply produce no syntax
snapshot; ordinary text editing continues.

`zenbu [--model vim|selection|structural] [--language ID] [FILE]` detects a
language from the file extension unless the optional override is supplied. The
structural model uses `f` to focus the smallest named node; arrows select its
parent, first child, next sibling, or previous sibling; `e`/`r` expand/shrink;
`m` selects same-kind siblings; `x`, `c`, `y`, `p`, `u`, and `Ctrl-R` reuse the
shared transformation, clipboard, and history services. See
[syntax](docs/SYNTAX.md) and [the structural model](docs/models/STRUCTURAL.md).

## Terminal host

`zenbu [--model vim|selection|structural] [--config PATH|--no-config] [FILE]` opens an existing UTF-8 file or an
unnamed empty buffer. `Ctrl-S` atomically saves an existing file. `Ctrl-Q`
exits a clean session; a dirty session requires a second `Ctrl-Q`. The
selection-first model is a different editing grammar over the same kernel, not
a compatibility mode.

Without either configuration flag, Zenbu attempts `$XDG_CONFIG_HOME/zenbu/init.lua`
(or `$HOME/.config/zenbu/init.lua`); a missing default is a no-op. `Ctrl-Alt-R`
stages and atomically activates a new generation; `Alt-R`/`Meta-R` is accepted
as a terminal-portable fallback where Ctrl-Alt printable keys cannot be reported.
A bad reload leaves the prior
generation active. Configuration is deliberately trusted local code: it runs
with Lua's standard libraries and must not be loaded from untrusted projects.
See [scripting](docs/SCRIPTING.md).

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
Branching history is retained as an immutable tree and is available through the
read-only inspector; no graphical branch manager or merge policy exists yet.

The M3 Vim-style model implements a documented, intentionally incomplete
subset; it is not Vim compatible. The selection-first model is inspired by
Kakoune/Helix's select-then-transform principle, not a compatibility layer.
The structural model is intentionally a small generic AST grammar rather than
an OCaml refactoring engine. See [the Vim-style subset](docs/models/VIM.md),
[selection-first subset](docs/models/SELECTION_FIRST.md), and
[structural model](docs/models/STRUCTURAL.md).

Terminal decoding and rendering are intentionally narrow: no mouse, bracketed
paste, terminal capability probing beyond the chosen backend, save-as prompt,
or model switching in a live session exists yet. Keymap configuration UI,
syntax highlighting, LSP, stable plugin isolation, and a plugin marketplace
remain deferred. M7 configuration is not a sandbox, package manager, or stable
third-party plugin contract.

## M6/M7 observability and scripting

M6 adds local structured inspection instead of ad-hoc logging. Transactions
retain optional deterministic provenance; bounded traces and CPU-time profiles
are explicit runtime services. The generic inspector describes models,
commands, selectors, transformations, current bindings, selections, history,
syntax, and profile aggregates without importing model or backend internals.

`zenbu-headless commands`, `api`, `describe`, `bindings`, `why`, `history`,
`selection`, `syntax-session`, and `profile` expose those same typed views.
Interactive `--trace` and `--profile` opt in to bounded recording; with trace
enabled, `Ctrl-O` toggles a read-only generic explanation overlay and `Escape`
dismisses it. This is Zenbu's host-level inspector, not Vim Ex.

M7 now does this with data-only Lua callbacks. Scripts may register commands,
selectors, transformations, scoped bindings, and `document-changed`/`after-save`
hooks. They return declarative effects, selections, or edit proposals; the
normal model runtime validates transactions, records history/provenance, and
keeps Tree-sitter and terminal values private. `why`, history, bindings,
`Scripts`, `config-check`, `config-describe`, and `script-session` expose the
active generation and its effects. The recommended next milestone is M8: make a
stable capability-constrained plugin contract from this pressure-tested surface.
