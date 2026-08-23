# Zenbu

[![CI](https://github.com/gongahkia/zenbu/actions/workflows/ci.yml/badge.svg)](https://github.com/gongahkia/zenbu/actions/workflows/ci.yml)

Zenbu is a terminal-first programmable text editor built around one constraint:
no editing model is fundamental. Vim-style, selection-first, structural, and
future models act through the same public semantic editing API; they do not
mutate text, selections, history, or syntax state directly.

This checkout is **0.11.0-dev (M11)**. It is a development release, not a
complete editor distribution.

## Quick start

On Linux x86_64, install `opam`, a C toolchain, `curl`, `tar`, and a SHA-256
tool (`sha256sum` or `shasum`). On Apple Silicon macOS, install Xcode Command
Line Tools, Homebrew, `opam`, and `lua@5.4`:

```sh
xcode-select --install
brew install opam lua@5.4
```

Then follow this complete clone-to-editor path on either supported platform:

```sh
git clone https://github.com/gongahkia/zenbu.git
cd zenbu
make bootstrap
make build
eval "$(opam env --switch="$PWD" --set-switch)"
eval "$(./scripts/zenbu-env.sh)"
dune exec bin/zenbu.exe -- README.md
```

`make bootstrap` is idempotent and affects only ignored local `_opam` and
`.zenbu/` directories. The concise guided workflow, controls, configuration,
and plugin examples live in [Getting Started](docs/GETTING_STARTED.md).

## What is working

- The kernel owns immutable documents, UTF-8 validation, selections,
  transactions, branching undo/redo history, deterministic replay, clipboard
  slots, provenance, trace events, and profiler aggregates.
- `zenbu.model_api` is the public boundary for editing models. It includes
  model-neutral selection-set algebra—regex selection/splitting/filtering,
  merge, primary/content rotation, and orientation operations—and the supplied
  Vim-style, selection-first, and structural models all use it.
- `zenbu.syntax` provides version-bound OCaml and JSON snapshots through a
  private Tree-sitter backend. Its public `Syntax.Highlight` projection feeds
  terminal presentation without exposing parser pointers or queries.
- The terminal host has literal Unicode search, a provider-neutral searchable
  command palette with typed argument prompts and a moving result window,
  save-as, a live model picker,
  metadata-derived help, and bracketed paste aggregation. These remain host interactions; editing models can make
  declarative requests for reusable interactions such as literal search without
  receiving terminal-state access. Its semantic styles can be mapped to
  built-in or validated TOML terminal themes without affecting editing state.
  Its editor canvas also supports pane focus/caret placement, `Shift`-click
  extension, grapheme-safe primary dragging, and wheel scrolling through typed
  host pointer events. It also has a bounded, session-wide named keyboard-macro
  store and replayer that can be attached to adapter keymaps through Lua.
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

## Bootstrap on Linux x86_64 and Apple Silicon macOS

M9 pins official Wasmtime 47.0.3 C API archives for Linux x86_64 and Apple
Silicon macOS. Fedora users need `opam`, a C toolchain, `curl`, `tar`, and
`sha256sum`; macOS users need Xcode Command Line Tools and `brew install opam
lua@5.4`.

```sh
make bootstrap
```

The target creates an ignored local OCaml 5.3.0 switch when needed,
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

This installs `zenbu` into the active Opam prefix and adds the platform
Wasmtime library under `PREFIX/lib/zenbu/`; no `sudo` is involved.
`make release` builds checked release-profile binaries in
`.zenbu/release-build/default/bin/` after the release gate. They are dynamically linked and
become portable only with their bundled runtime libraries. The tag-triggered
release-candidate workflow produces archives for Linux x86_64 and Apple
Silicon macOS. On macOS, the archive includes Wasmtime, Lua 5.4, and libffi;
its `bin/` launchers select the bundled Lua library.

To produce the archive for the current supported host locally:

```sh
make release-archive
```

The resulting `dist/zenbu-<version>-<platform>.tar.gz` is unsigned. Apple
code signing and notarization are deliberately outside this source-build
release workflow.

## Run the editor

The Make targets activate the local switch themselves. Before using `dune`
directly, activate it in the current shell:

```sh
eval "$(opam env --switch="$PWD" --set-switch)"
eval "$(./scripts/zenbu-env.sh)"
```

```sh
dune exec bin/zenbu.exe -- test/fixtures/syntax_sample.ml
dune exec bin/zenbu.exe -- --model selection test/fixtures/syntax_sample.ml
dune exec bin/zenbu.exe -- --model structural test/fixtures/syntax_sample.ml
dune exec bin/zenbu.exe -- --theme dark test/fixtures/syntax_sample.ml
dune exec bin/zenbu.exe -- --trace --profile --plugin-dir examples/plugins FILE
```

The initial model is Vim-style. Syntax is detected from `.ml`, `.mli`, and
`.json`, or selected with `--language ocaml|json`; unknown paths deliberately
receive no syntax service and render as plain text.

The Vim model is a compatibility stress test for Zenbu's model API. It supports
normal/insert/replace states, operator motions, find, characterwise/linewise
visual selection, and `/ ? n N` search requests; it is not a full Vim clone.
Keyboard macros are generic host commands rather than built-in Vim `q`/`Q`
compatibility bindings; an adapter can declare those bindings in Lua.
The default register is `@`; named registers are available through the command
palette's optional `register` prompt. They are a reusable framework primitive,
not a claim of native Vim register grammar.
See [the compatibility baseline](docs/models/VIM.md) and [modal model
evaluation](docs/MODAL_MODEL_EVALUATION.md).

Host keys have priority over model/configuration bindings:

| Key | Host action |
| --- | --- |
| `Ctrl-S` / `Ctrl-Shift-S` | save / prompt for save-as |
| `Ctrl-Q` | quit; press again after a dirty warning to force quit |
| `Alt-R` or `Ctrl-Alt-R` | reload Lua configuration and plugins transactionally |
| `Ctrl-F` | start literal Unicode search; `Ctrl-G` / `Ctrl-Shift-G` move next / previous |
| `Ctrl-P` | filter commands; collect declared typed arguments before invocation |
| `Alt-M` | switch Vim-style, selection-first, or structural model while preserving shared semantic state |
| `Alt-H` | metadata-derived getting-started help |
| `Ctrl-O` | toggle the local `why` inspector |
| `Ctrl-Space` | explicit language completion for a ready language service |

Lua configuration and plugins can bind any non-host logical input sequence of
one to sixteen events—for example, `Ctrl-X Ctrl-K`—at global, model, or
model-status scope. Prefix state is host-owned and inspectable through the
generic bindings/trace views; it is not an editor-model implementation detail.

Search, save-as, rename, and command-argument prompts accept ordinary text-entry
input. Selecting a command with declared parameters from `Ctrl-P`, or reaching
one through a custom binding, collects each parameter in descriptor order;
`Escape` cancels without invoking it. This invokes the normal typed command
effect rather than introducing an Ex parser or a second mutation path.
Bracketed terminal paste is collected as one committed text input only while a
model or host prompt declares text entry; it is intentionally ignored in a
command grammar. Selection styling wins over search styling, which wins over
syntax styling.

The palette also exposes `workspace.split.vertical`,
`workspace.split.horizontal`, `workspace.pane.next`,
`workspace.pane.close`, `workspace.pane.only`, `workspace.buffer.new`,
`workspace.buffer.open`, `workspace.buffer.next`, and
`workspace.buffer.previous`. A pane has an independent viewport and can show
any open buffer. Definitions can open local targets, and rename or server
`workspace/applyEdit` can update already-open saved buffers together; unopened
targets and file resource operations are rejected. Project discovery remains
outside the workspace host.

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
[theme guide](docs/THEMES.md),
[language-service guide](docs/LANGUAGE_SERVICES.md),
[observability guide](docs/OBSERVABILITY.md), [scripting](docs/SCRIPTING.md),
[extension guide](docs/EXTENSIONS.md), [isolation policy](docs/ISOLATION.md),
the [generated Extension API](docs/generated/EXTENSION_API.md),
[Lua SDK](sdk/lua/zenbu.lua), [WIT contract](docs/wit/zenbu-plugin.wit), and
[roadmap](docs/ROADMAP.md). [Contributing](CONTRIBUTING.md) describes the
expected local workflow, [performance baseline](docs/PERFORMANCE.md) records
the M11 sanity numbers, and [release notes](docs/RELEASE.md) describe the gate.

## Deliberate limits

Zenbu has a local multi-buffer split-view workspace and bounded cross-file LSP
edits for already-open saved buffers, but no project search, external-file
watcher, general workspace resource operations, command-line/Ex language,
plugin marketplace, asynchronous extension execution, public Tree-sitter query
API, grammar downloads, refactoring engine, or system clipboard bridge.
Component runtime support is limited to Linux x86_64 and Apple Silicon macOS
because of the pinned native C API.
See the deferred work in
[the roadmap](docs/ROADMAP.md) and the [editor workload evaluation](docs/EDITOR_WORKLOAD_EVALUATION.md).
