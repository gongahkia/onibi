# Zenbu

[![CI](https://github.com/gongahkia/zenbu/actions/workflows/ci.yml/badge.svg)](https://github.com/gongahkia/zenbu/actions/workflows/ci.yml)

Zenbu is a terminal-first programmable text editor built around one constraint:
no editing model is fundamental. Vim-style, selection-first, structural, and
future models act through the same public semantic editing API; they do not
mutate text, selections, history, or syntax state directly.

This checkout is **0.11.0-dev (M11)**. It is a development release, not a
complete editor distribution.

## Quick start

On Linux x86_64, install `opam`, a C toolchain, `curl`, `tar`, and `sha256sum`,
then follow this complete clone-to-editor path:

```sh
git clone https://github.com/gongahkia/zenbu.git
cd zenbu
make bootstrap
make build
eval "$(opam env --switch="$PWD" --set-switch)"
dune exec bin/zenbu.exe -- README.md
```

`make bootstrap` is idempotent and affects only ignored local `_opam` and
`.zenbu/` directories. The concise guided workflow, controls, configuration,
and plugin examples live in [Getting Started](docs/GETTING_STARTED.md).

## What is working

- The kernel owns immutable documents, UTF-8 validation, selections,
  transactions, branching undo/redo history, deterministic replay, clipboard
  slots, provenance, trace events, and profiler aggregates.
- `zenbu.model_api` is the public boundary for editing models. The supplied
  Vim-style, selection-first, and structural models all use it.
- `zenbu.syntax` provides version-bound OCaml and JSON snapshots through a
  private Tree-sitter backend. Its public `Syntax.Highlight` projection feeds
  terminal presentation without exposing parser pointers or queries.
- The terminal host has literal Unicode search, a provider-neutral command
  palette, save-as, a live model picker, metadata-derived help, and bracketed
  paste aggregation. These are host interactions, not additions to a model
  grammar.
- `zenbu.language` exposes model-neutral diagnostics, hover, definition,
  completion, rename, position conversion, and sync data. A private async LSP
  adapter starts `ocamllsp` by default for saved OCaml files; results become
  ordinary selections or validated transactions, never protocol-driven edits.
- Lua configuration and local plugins contribute commands, selectors,
  transformations, bindings, and events through host validation. Wasmtime
  Components have bounded fuel/memory and become explicitly unavailable after
  a fatal callback until reload; the document and ordinary editing remain
  available.
- `zenbu-headless` provides replay, model/session execution, syntax,
  inspection, configuration, plugin, Component-contract, and generated-API
  tooling for deterministic CI use.

## Bootstrap on Linux x86_64

M9's pinned Wasmtime C API currently supports Linux x86_64 only. Fedora users
need `opam`, a C toolchain, `curl`, `tar`, and `sha256sum`; install those with
DNF before bootstrapping.

```sh
make bootstrap
```

The target creates an ignored local opam switch with the system OCaml when needed
(Zenbu requires OCaml 5.3.0 or newer),
installs the package's test dependencies (including `ocamlformat`,
`ocaml-lsp-server`, and the Tree-sitter OCaml/JSON sublibraries), checksum-fetches the pinned Wasmtime C
API, and runs the full check. This resolves the common failure where a global
Dune finds `tree-sitter` but not `tree-sitter.json`, or where `ocamlformat` is
not on `PATH`. Its transient opam extraction directory is under ignored
`.zenbu/tmp`, avoiding a small system `/tmp` quota.

To use an already prepared switch:

```sh
make wasm-runtime
make check
make demo
```

`make check` runs format checking, build, and all tests. `make release-check`
verifies that the checked-in extension contract artifacts match the generator;
run `make extension-docs` to update them deliberately.

For a local development install after validation:

```sh
make install
```

This installs `zenbu` into the active Opam prefix and adds the dynamically
required Wasmtime library under `PREFIX/lib/zenbu/`; no `sudo` is involved.
`make release` builds checked release-profile binaries in
`.zenbu/release-build/default/bin/` after the release gate. They are dynamically linked and
become portable only with `lib/zenbu/libwasmtime.so`, as packaged by the
tag-triggered Linux release-candidate workflow.

## Run the editor

The Make targets activate the local switch themselves. Before using `dune`
directly, activate it in the current shell:

```sh
eval "$(opam env --switch="$PWD" --set-switch)"
```

```sh
dune exec bin/zenbu.exe -- test/fixtures/syntax_sample.ml
dune exec bin/zenbu.exe -- --model selection test/fixtures/syntax_sample.ml
dune exec bin/zenbu.exe -- --model structural test/fixtures/syntax_sample.ml
dune exec bin/zenbu.exe -- --trace --profile --plugin-dir examples/plugins FILE
```

The initial model is Vim-style. Syntax is detected from `.ml`, `.mli`, and
`.json`, or selected with `--language ocaml|json`; unknown paths deliberately
receive no syntax service and render as plain text.

Host keys have priority over model/configuration bindings:

| Key | Host action |
| --- | --- |
| `Ctrl-S` / `Ctrl-Shift-S` | save / prompt for save-as |
| `Ctrl-Q` | quit; press again after a dirty warning to force quit |
| `Alt-R` or `Ctrl-Alt-R` | reload Lua configuration and plugins transactionally |
| `Ctrl-F` | start literal Unicode search; `Ctrl-G` / `Ctrl-Shift-G` move next / previous |
| `Ctrl-P` | filter and invoke active builtin, script, and plugin commands |
| `Alt-M` | switch Vim-style, selection-first, or structural model while preserving shared semantic state |
| `Alt-H` | metadata-derived getting-started help |
| `Ctrl-O` | toggle the local `why` inspector |
| `Ctrl-Space` | explicit language completion for a ready language service |

Search, palette, save-as, and rename prompts accept ordinary text-entry input.
Bracketed terminal paste is collected as one committed text input only while a
model or host prompt declares text entry; it is intentionally ignored in a
command grammar. Selection styling wins over search styling, which wins over
syntax styling.

## Headless tooling

```sh
dune exec bin/zenbu_headless.exe -- demo
dune exec bin/zenbu_headless.exe -- replay test/fixtures/unicode.replay
dune exec bin/zenbu_headless.exe -- session test/fixtures/sessions/vim-edit.session
dune exec bin/zenbu_headless.exe -- syntax test/fixtures/syntax_sample.ml
dune exec bin/zenbu_headless.exe -- language-status test/fixtures/syntax_sample.ml
dune exec bin/zenbu_headless.exe -- lsp-position utf-16 0 test/fixtures/syntax_sample.ml
dune exec bin/zenbu_headless.exe -- why test/fixtures/sessions/observability-vim.session
dune exec bin/zenbu_headless.exe -- search-session path/to/session
dune exec bin/zenbu_headless.exe -- extension-api
dune exec bin/zenbu_headless.exe -- benchmark
```

Session fixtures can use `Ctrl-Shift-X` and `Alt-X` input values in addition to
the original logical keys and `text-input` lines, so host interactions are
testable without a TTY.

## Package boundaries

```text
zenbu.kernel                 documents, transactions, history, replay
        ↑
zenbu.model_api              public model/command/inspection vocabulary
   ↗         ↖
zenbu.proof_models     zenbu.structural_model ── zenbu.syntax (private Tree-sitter)
        ↑                         ↑
zenbu.language ── zenbu.lsp (private LSP/JSON-RPC process adapter)
        ↑
zenbu.app ── zenbu.view ── zenbu.terminal
        ↑
zenbu.scripting / zenbu.extension
```

The kernel does not depend on models, terminal I/O, Lua, Wasmtime, Tree-sitter,
or rendering. The view accepts display ranges/classes and terminal applies
colours; neither can mutate the document.

Start with [Getting Started](docs/GETTING_STARTED.md), then read
[the architecture](docs/ARCHITECTURE.md), [editing protocol](docs/EDITING_PROTOCOL.md),
[terminal guide](docs/TERMINAL.md), [syntax guide](docs/SYNTAX.md),
[language-service guide](docs/LANGUAGE_SERVICES.md),
[observability guide](docs/OBSERVABILITY.md), [scripting](docs/SCRIPTING.md),
[extension guide](docs/EXTENSIONS.md), [isolation policy](docs/ISOLATION.md),
the [generated Extension API](docs/generated/EXTENSION_API.md),
[Lua SDK](sdk/lua/zenbu.lua), [WIT contract](docs/wit/zenbu-plugin.wit), and
[roadmap](docs/ROADMAP.md). [Contributing](CONTRIBUTING.md) describes the
expected local workflow, [performance baseline](docs/PERFORMANCE.md) records
the M11 sanity numbers, and [release notes](docs/RELEASE.md) describe the gate.

## Deliberate limits

Zenbu has no multi-buffer/cross-file LSP workflow, project search,
external-file watcher, pane/layout system, command-line/Ex language, plugin
marketplace, asynchronous extension execution, public Tree-sitter query API,
grammar downloads, refactoring engine, or system clipboard bridge. Component
runtime support is Linux x86_64-specific because of the pinned native C API.
See the deferred work in
[the roadmap](docs/ROADMAP.md).
