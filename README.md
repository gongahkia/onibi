# Zenbu

[![CI](https://github.com/gongahkia/zenbu/actions/workflows/ci.yml/badge.svg)](https://github.com/gongahkia/zenbu/actions/workflows/ci.yml)

Zenbu is a terminal-first programmable text editor built around one constraint:
no editing model is fundamental. Vim-style, selection-first, direct,
structural, and a runtime-selected trusted Lua model act through the same
public semantic editing API; they do not mutate text, selections, history, or
syntax state directly.

This checkout is **0.11.0-dev (M11)**. It is a development release, not a
complete editor distribution.

## Quick start

On Linux x86_64, install `opam`, a C toolchain, `curl`, `tar`, a SHA-256 tool
(`sha256sum` or `shasum`), and the Lua 5.4 shared library (`lua-libs` on
Fedora). On Apple Silicon macOS, install Xcode Command Line Tools, Homebrew,
`opam`, and `lua@5.4`:

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
  slots, a bounded session kill history, and a bounded host bridge to the
  system clipboard, plus provenance, trace events, and profiler aggregates.
- `zenbu.model_api` is the public boundary for editing models. It includes
  model-neutral selection-set algebra—regex selection/splitting/filtering,
  merge, primary/content rotation, and orientation operations—and the supplied
  Vim-style, selection-first, direct, structural, and a checked script-owned
  model all use it.
- `zenbu.syntax` provides version-bound OCaml and JSON snapshots through a
  private Tree-sitter backend. Its public `Syntax.Highlight` projection feeds
  terminal presentation without exposing parser pointers or queries.
- The terminal host has literal Unicode search plus an opt-in UTF-8-safe `Str`
  regexp search and palette-only literal/regexp replace-all, a provider-neutral
  searchable command palette with typed argument prompts, a bounded declarative
  command-line adapter prompt, and a moving result window,
  save-as, bounded explicit-root project-text search, a live model picker,
  metadata-derived help, and bracketed paste aggregation. These remain host interactions; editing models can make
  declarative requests for reusable interactions such as literal search without
  receiving terminal-state access. Its semantic styles can be mapped to
  built-in or validated TOML terminal themes and pure line-number/status-row/
  buffer-line presentation profiles without affecting editing state.
  Its editor canvas also supports pane focus/caret placement, `Shift`-click
  extension, grapheme-safe primary dragging, and wheel scrolling through typed
  host pointer events. Pane-local, version-bound folding projects source lines
  without changing source-byte editing state. It also has a bounded,
  session-wide named keyboard-macro
  store and replayer that can be attached to adapter keymaps through Lua.
  Embedding hosts can additionally contribute version-bound, bounded trailing
  annotations and virtual rows through a data-only decoration protocol; these
  cannot take terminal or editing authority.
- `zenbu.language` exposes model-neutral diagnostics, hover, definition,
  completion, code actions, rename, position conversion, and sync data. A
  private async LSP adapter starts `ocamllsp` by default for saved OCaml files;
  results become ordinary selections or validated transactions, never
  protocol-driven edits. It also supports checked document/range formatting
  with fixed two-space options. Code-action server commands are always denied.
- Lua configuration and local plugins contribute commands, selectors,
  transformations, bindings, and events through host validation. One trusted
  Lua generation may also provide a serialisable editing model with optional
  explicit reload-state migration,
  declare buffer-local data-only binding layers with deterministic priorities,
  request bounded external selection filters, and start bounded inspectable
  streaming background programs through absolute executable paths and argument
  vectors; it never receives a shell parser or process handle. Wasmtime
  Components have bounded fuel/memory and become explicitly unavailable after
  a fatal callback until reload; the document and ordinary editing remain
  available.
- `zenbu-headless` provides replay, model/session execution, syntax,
  inspection, configuration, plugin, Component-contract, and generated-API
  tooling for deterministic CI use.

## Bootstrap on Linux x86_64 and Apple Silicon macOS

M9 pins official Wasmtime 47.0.3 C API archives for Linux x86_64 and Apple
Silicon macOS. Fedora users need `opam`, a C toolchain, `curl`, `tar`,
`sha256sum`, and `lua-libs`; macOS users need Xcode Command Line Tools and
`brew install opam lua@5.4`.

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
Silicon macOS. Each archive includes Wasmtime, Lua 5.4, and libffi; its `bin/`
launchers select the bundled Lua library.

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
dune exec bin/zenbu.exe -- --model direct test/fixtures/syntax_sample.ml
dune exec bin/zenbu.exe -- --model structural test/fixtures/syntax_sample.ml
dune exec bin/zenbu.exe -- --model script --config examples/script-modal-editor.lua README.md
dune exec bin/zenbu.exe -- --theme dark test/fixtures/syntax_sample.ml
dune exec bin/zenbu.exe -- --theme dark --presentation relative test/fixtures/syntax_sample.ml
dune exec bin/zenbu.exe -- --trace --profile --plugin-dir examples/plugins FILE
```

The initial model is Vim-style. Syntax is detected from `.ml`, `.mli`, and
`.json`, or selected with `--language ocaml|json`; unknown paths deliberately
receive no syntax service and render as plain text.

The Vim model is a compatibility stress test for Zenbu's model API. It supports
normal/insert/replace states, operator motions, find, characterwise/linewise
visual selection, `/ ? n N` search requests, and a bounded `q{register}` /
bare-`q` / `@{register}` macro subset; it is not a full Vim clone. The same
generic macro service remains available to Lua adapters through host commands.
The default register is `@`; the command palette accepts a named register
prompt. This is a reusable framework primitive, not a claim of full Vim
register grammar.
See [the compatibility baseline](docs/models/VIM.md) and [modal model
evaluation](docs/MODAL_MODEL_EVALUATION.md).

The Direct model is a non-modal product-adapter baseline: committed text
inserts immediately, arrow/Emacs movement commands retain a caret or extend a
selection, `Ctrl-W` cuts a selected region to a bounded shared kill history,
and its `Ctrl-S` / `Ctrl-X Ctrl-S` requests the same host save path. It supports
evaluation of basic Micro and Emacs-style editing without making a Micro or
Emacs parity claim; see [its exact grammar and limits](docs/models/DIRECT.md).

The optional adapter-level system clipboard commands are host operations, not
general script process access. On macOS the default provider uses `pbcopy`/`pbpaste`;
Linux detects `wl-clipboard`, `xclip`, or `xsel`. See
[Getting Started](docs/GETTING_STARTED.md#system-clipboard) for limits and
fallback behavior.

Trusted Lua may separately request an external selection filter with an
absolute executable and argument vector. The host runs it per selection with
UTF-8, 16 MiB, and five-second limits, and only then commits the replacement
transaction. See the [scripting guide](docs/SCRIPTING.md#external-selection-filters)
for the exact contract and a headless fixture.

The terminal chrome is independently selectable with
`--presentation default|numbered|relative|minimal|bare|buffered|PATH` and can be
switched live through `Ctrl-P` → `view.presentation.switch`; themes can
likewise switch through `view.theme.switch`. These controls cover only a
line-number gutter, status-row density, a bounded host-owned buffer line, and
semantic-style colours; they are not an arbitrary widget or product-theme API. See [presentation
profiles](docs/PRESENTATION.md) and [themes](docs/THEMES.md).

Host keys normally have priority over model/configuration bindings. The Direct
adapter intentionally owns `Ctrl-S`, `Ctrl-F`, `Ctrl-G`, and `Ctrl-P`: save is still a
typed host request, while the latter two preserve its Emacs-style bindings.

| Key | Host action |
| --- | --- |
| `Ctrl-S` / `Ctrl-Shift-S` | save / prompt for save-as |
| `Ctrl-Q` | quit; press again after a dirty warning to force quit |
| `Alt-R` or `Ctrl-Alt-R` | reload Lua configuration and plugins transactionally |
| `Ctrl-F` | start literal Unicode search; `Ctrl-G` / `Ctrl-Shift-G` move next / previous; `Ctrl-P` → `search.regexp` starts the UTF-8-safe regexp variant |
| `Ctrl-P` | filter commands; collect declared typed arguments before invocation |
| `Alt-M` | switch Vim-style, selection-first, structural, direct, or the configured script model while preserving shared semantic state |
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
`search.replace.literal` and `search.replace.regexp` each collect a query and
literal replacement text through that prompt. They recompute matches in the
active buffer and commit every accepted non-overlapping match as one checked
transaction; they do not reuse an active search cursor, expose capture
templates, or implement another editor's replacement dialect.
`search.query-replace.literal` and `search.query-replace.regexp` instead hold
a version-bound host review plan: `s` skips, `r` replaces one match, `a`
replaces the remaining planned matches, and `q`/`Escape` quits. Each accepted
decision remains a checked transaction; a changed document cancels remaining
decisions before they can apply.
Bracketed terminal paste is collected as one committed text input only while a
model or host prompt declares text entry; it is intentionally ignored in a
command grammar. Selection styling wins over search styling, which wins over
syntax styling.

The palette also exposes `workspace.split.vertical`,
`workspace.split.horizontal`, `workspace.pane.next`,
`workspace.pane.close`, `workspace.pane.only`,
`workspace.pane.grow-width`, `workspace.pane.shrink-width`,
`workspace.pane.grow-height`, `workspace.pane.shrink-height`, and
`workspace.panes.balance`, followed by `workspace.buffer.new`,
`workspace.buffer.open`, `workspace.buffer.next`, and
`workspace.buffer.previous`, `workspace.layout.save`, and
`workspace.layout.restore`, `workspace.project.root.set`, and
`workspace.file-picker`, and `workspace.project.search`, plus `view.scroll.up`, `view.scroll.down`,
`view.page.up`, `view.page.down`, `view.center`, `view.fold.selection`,
`view.fold.syntax`, and `view.fold.clear`. Resize commands move the
focused pane's nearest matching divider by one terminal cell; balance restores
equal proportions. A pane has an independent viewport and can show
any open buffer. Each `(pane, buffer)` pairing also retains an ordered
selection set: panes showing the same buffer render and restore distinct
carets/selections, and inactive positions rebase through that buffer's current
history lineage after edits. Definitions can open local targets, and rename or server
`workspace/applyEdit` can update already-open saved buffers together; unopened
targets and file resource operations are rejected. Project discovery remains
outside the workspace host beyond this explicit-root picker.

The project-root picker is a narrow host-owned exception: select a readable
directory with `workspace.project.root.set`, then use
`workspace.file-picker` to filter deterministic readable text files beneath it.
It skips hidden, binary, unreadable, and symlink entries and revalidates the
selected relative path before the existing buffer opener loads or reuses it.
`workspace.project.search` takes a literal query and presents bounded results
from the same root. Selecting one revalidates its relative path and UTF-8 byte
location before the ordinary buffer opener selects it; `Escape` closes the
result view without changing a document. The host caps a search at 512 files,
256 results, 1 MiB per file, and 32 MiB total, and exposes the scan and limit
state through the inspector. Dot-prefixed names are the only ignored-path
policy; `.gitignore` is not read. It does not grant filesystem authority to models, scripts, or
plugins, and it does not provide shell search.

Layout commands accept a JSON path through the palette. They save or restore
only clean file-backed local buffers plus host-owned split/view state; unsaved
text, editing-model internals, terminal/LSP/plugin handles, and project or
cross-machine state are deliberately excluded. Restore validates every
referenced file and selection before replacing the current session.

The checked viewport commands never alter a document or a selection. Fold
commands are host-only, per-`(pane, buffer)` projections and invalidate after
content edits; [Folding](docs/FOLDING.md) specifies their source, pointer,
cursor, search, diagnostics, and viewport behavior. Lua
adapters can bind them to non-reserved inputs; for example,
[`examples/helix-adapter.lua`](examples/helix-adapter.lua) maps `PageUp`,
`PageDown`, `Ctrl-U`, `Ctrl-D`, and `z z` in selection mode. Page size is the
focused pane's current visible projected-row height, so a hidden-status
presentation uses one more projected row than a status-bearing presentation.
The separate [decoration protocol](docs/DECORATIONS.md) gives embedding hosts
bounded snapshot-bound virtual rows and trailing annotations without a
renderer callback or provider-owned input path.
Pane-local [display options](docs/VIEW_OPTIONS.md) add a bounded vertical
scroll margin and built-in presentation override with dynamic inheritance from
the session presentation.

`editor.location.set` and `editor.location.jump` are also palette commands.
They save a named ordered selection set in its current local buffer, rebase it
through that buffer's active transaction history, and restore it through a
normal selection transaction. A location whose source version is not on the
current history branch is reported as stale instead of being guessed at.
`editor.jump.push`, `editor.jump.backward`, and `editor.jump.forward` expose
the same rebased positions as a bounded session jump history; the Vim model
also maps `Ctrl-O` and `Ctrl-I` (or `Tab`) to backward/forward traversal.

## Headless tooling

```sh
dune exec bin/zenbu_headless.exe -- demo
dune exec bin/zenbu_headless.exe -- replay test/fixtures/unicode.replay
dune exec bin/zenbu_headless.exe -- session test/fixtures/sessions/vim-edit.session
dune exec bin/zenbu_headless.exe -- bindings direct
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

Zenbu has a local multi-buffer split-view workspace, bounded explicit-root
project-text search, a report-only local file watcher, and bounded cross-file
LSP edits for already-open saved buffers, but no automatic external reload,
general workspace resource operations, code-action command execution,
command-line/Ex language, plugin
marketplace, asynchronous extension execution, public Tree-sitter query API,
grammar downloads, refactoring engine, or system clipboard bridge.
Component runtime support is limited to Linux x86_64 and Apple Silicon macOS
because of the pinned native C API.
See the deferred work in
[the roadmap](docs/ROADMAP.md) and the [editor workload evaluation](docs/EDITOR_WORKLOAD_EVALUATION.md).
