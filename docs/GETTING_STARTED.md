# Getting started

Zenbu is a terminal editor with four interchangeable editing models and a
local multi-buffer split-view workspace. Its kernel owns every text mutation;
models, Lua, and WebAssembly Components request ordinary semantic edits instead
of changing a buffer directly.

## Install from a clone

Linux x86_64 and Apple Silicon macOS are supported interactive platforms in
M11. Linux needs `opam`, a C toolchain, `curl`, `tar`, `sha256sum`, and a Lua
5.4 shared library (`lua-libs` on Fedora or `liblua5.4-0` on Debian/Ubuntu).
macOS needs Xcode Command Line Tools, Homebrew, and Lua 5.4:

```sh
xcode-select --install
brew install opam lua@5.4
```

Then run:

```sh
git clone https://github.com/gongahkia/zenbu.git
cd zenbu
make bootstrap
make build
eval "$(opam env --switch="$PWD" --set-switch)"
eval "$(./scripts/zenbu-env.sh)"
dune exec bin/zenbu.exe -- README.md
```

`make bootstrap` creates only an ignored local `_opam` switch and `.zenbu/`
runtime cache. It installs OCaml 5.3-compatible dependencies, the exact
`ocamlformat` required by `.ocamlformat`, Tree-sitter's OCaml/JSON libraries,
and the checksum-pinned Wasmtime C API. Make targets activate that switch
themselves; direct `dune` invocations need the `eval` line above.

To install into the active Opam prefix without `sudo`:

```sh
make install
zenbu README.md
```

The target installs `zenbu` under the active prefix's `bin/` directory and
copies its platform Wasmtime dynamic library to `PREFIX/lib/zenbu/`.

## Packaged releases

Tagged builds produce source-validated `tar.gz` archives for Linux x86_64 and
Apple Silicon macOS. Each archive bundles Wasmtime, Lua 5.4, and libffi; run
its `bin/zenbu` or `bin/zenbu-headless` launchers after extraction. macOS
command-line archives are unsigned and not notarized.

## First ten minutes

Open an OCaml or JSON file. Zenbu detects those extensions and colours the
current version-bound syntax snapshot. Unknown extensions remain plain text.
The initial model is Vim-style; choose an alternative at startup with
`--model selection`, `--model direct`, or `--model structural`, or change it
live with `Alt-M`.

The bundled Direct-model Micro adapter is a reproducible configuration example:

```sh
dune exec bin/zenbu.exe -- --model direct --config examples/micro-adapter.lua README.md
```

It maps `Ctrl-E` to Zenbu's command palette, `Ctrl-W` to focus the next split,
selected-text `Ctrl-X` to the shared kill history, and `Ctrl-C`/`Ctrl-V` to the
system clipboard, while keeping Direct text editing and host save semantics.
It does not claim to reproduce Micro's full UI or plugin surface.

The separate Emacs-oriented adapter maps `Ctrl-Y` to `editor.kill-ring.yank`;
Direct itself maps `Ctrl-W` to cutting a non-empty selection:

```sh
dune exec bin/zenbu.exe -- --model direct --config examples/emacs-adapter.lua README.md
```

The kill history is local to the Zenbu session and does not use the macOS
clipboard.

The selection-first Helix view-navigation adapter is another bounded
configuration example:

```sh
dune exec bin/zenbu.exe -- --model selection --config examples/helix-adapter.lua README.md
```

It maps `PageUp`, `PageDown`, `Ctrl-U`, `Ctrl-D`, and `z z` to checked
scroll/page/center requests for the focused view. These controls leave text,
selections, history, and replay unchanged; the host calculates page height from
the active pane and presentation profile. It is not a Helix compatibility
configuration or a script rendering API.

## System clipboard

`editor.clipboard.copy` and `editor.clipboard.paste` are fixed host commands
available through the palette and trusted adapter bindings. Copy requires at
least one non-empty selection, writes the system clipboard first, and then
updates Zenbu's ordinary unnamed slot. Paste reads one UTF-8 string and replaces
the active selections through the normal transaction path. The provider rejects
invalid UTF-8 and values larger than 16 MiB; it never runs a command supplied by
Lua or a plugin.

On macOS Zenbu detects `/usr/bin/pbcopy` and `/usr/bin/pbpaste`. On Linux it
uses `wl-copy`/`wl-paste`, then `xclip`, then `xsel`, when a pair is available
on `PATH`. If no supported provider is present, the host command reports an
error and leaves the document and ordinary clipboard unchanged. The default
Direct model keeps its internal `Ctrl-C`/`Ctrl-V` behavior; the Micro adapter
opts into the host bridge. OSC 52, X primary-selection support, rich clipboard
types, Windows, SSH-local clipboard forwarding, and automatic Emacs
kill/yank synchronization are not implemented.

| key | action |
| --- | --- |
| `Ctrl-S` | save; refuses an externally changed, replaced, or missing target, and opens save-as for an unnamed buffer |
| `Ctrl-Shift-S` | save-as; an existing destination is atomically replaced |
| `Ctrl-Q` | quit; press again to force-quit a dirty buffer |
| `Ctrl-F` | start literal Unicode search; `Ctrl-P` → `search.regexp` starts the UTF-8-safe regexp variant |
| `Ctrl-G` / `Ctrl-Shift-G` | next / previous match, with wraparound |
| `Ctrl-P` | command palette; declared command arguments are collected in order |
| `Alt-M` | switch Vim-style, selection-first, structural, and direct models |
| `Alt-H` | help from current model metadata |
| `Ctrl-O` | inspect the latest `why` explanation |
| `Ctrl-Space` | ask a ready language service for completion |
| `Alt-R` / `Ctrl-Alt-R` | reload the selected Lua config and local plugins |

These are the normal host controls. In the Direct model, `Ctrl-S` and
`Ctrl-X Ctrl-S` still save through the host, while `Ctrl-F`, `Ctrl-G`, and
`Ctrl-P` remain model-owned Emacs-style input; use `Alt-H` to inspect the
active grammar.

Literal search and `search.regexp` are incremental. `Escape` while either
prompt is open restores the pre-search selection; `Enter` keeps the selected
match. The regexp command uses the OCaml `Str` dialect, finds only non-empty
non-overlapping matches, and rejects a pattern whose byte match would split a
UTF-8 code point; it is not a Helix, Kakoune, Micro, or Emacs regexp-compatibility
claim. Search highlighting
is view-only and selection styling always wins over it.

`Ctrl-P` also exposes `search.replace.literal` and `search.replace.regexp`.
Each asks for a query and literal replacement text, recomputes the active
buffer's matches, and commits all accepted non-overlapping replacements as one
normal transaction. Literal replacement means `$1` remains `$1`; regexp
replacement does not implement captures, query confirmation, search/replace
history, or another editor's replacement language.

The command palette invokes parameterless commands immediately. For a command
with declared parameters it opens one prompt per parameter; `Escape` cancels
the whole invocation. The built-in `editor.apply` demonstrates the typed form:
enter a selector such as `document`, then a transformation such as
`delete`, `select`, or `replace:text`.

`workspace.buffers` lists stable buffer IDs, display names, current selection,
and dirty state. Use `workspace.buffer.rename` to give a scratch or generated
buffer a short UTF-8 label, and `workspace.buffer.switch` with a listed ID to
show a particular buffer in the focused pane. Names change terminal chrome
only: they do not rename a file or alter the document. The next/previous buffer
commands remain available for keyboard-oriented adapters. `workspace.buffer.close`
refuses to discard unsaved content; `workspace.buffer.force-close` is the
separate explicit discard operation. Closing the final clean buffer starts a
fresh unnamed buffer.

`Alt-H` shows the active model's `Input_rule` table. For deterministic,
scriptable inspection, use:

```sh
dune exec bin/zenbu_headless.exe -- commands
dune exec bin/zenbu_headless.exe -- describe command search.next
dune exec bin/zenbu_headless.exe -- bindings vim
dune exec bin/zenbu_headless.exe -- bindings direct
dune exec bin/zenbu_headless.exe -- demo
```

## Choose a terminal theme

The default palette preserves Zenbu's original ANSI styling. Start with a
built-in true-colour palette using `--theme dark` or `--theme light`, or load a
validated TOML file with `--theme path/to/theme.toml`:

```sh
dune exec bin/zenbu.exe -- --theme dark README.md
dune exec bin/zenbu.exe -- --theme path/to/theme.toml README.md
```

Themes affect only semantic terminal styles; they cannot change document state,
input handling, layout, or plugin authority. See [Themes](THEMES.md) for the
complete role list and TOML schema.

## Choose terminal chrome

`--presentation` controls only pure renderer chrome, independently of the
palette. Its built-ins are `default`, `numbered`, `relative`, `minimal`,
`bare`, and `buffered`; a TOML file may combine `line_numbers = "none" |
"absolute" | "relative"`, `status_line = "detailed" | "minimal" | "hidden"`,
and `buffer_line = "visible" | "hidden"`.

```sh
dune exec bin/zenbu.exe -- --theme dark --presentation relative README.md
dune exec bin/zenbu.exe -- --presentation path/to/presentation.toml README.md
```

The gutter and optional host-owned buffer line use display rows and shift the
terminal cursor without changing document coordinates. The buffer line is a
bounded noninteractive summary of local buffers, not an arbitrary tab-widget
surface. Switch built-ins or a validated TOML profile live through `Ctrl-P` →
`view.presentation.switch`. See [Terminal presentation
profiles](PRESENTATION.md) for the complete contract and its limits.

For a saved `.ml` or `.mli`, M11 starts `ocamllsp` when it is available. Use
the palette for `language.status`, hover, definition, completion, rename, and
diagnostic navigation; `Ctrl-Space` asks for completion. The service is
asynchronous but every accepted edit still becomes a normal transaction. See
[Language services](LANGUAGE_SERVICES.md) for server selection, status, and
the bounded cross-file workspace contract.

## Configure and extend

Zenbu works with no config. A user-owned trusted Lua config is loaded from
`$XDG_CONFIG_HOME/zenbu/init.lua`; start from
[`examples/m7-init.lua`](../examples/m7-init.lua), then reload with `Alt-R`.
Read [Scripting](SCRIPTING.md) for its trust model and API.

Local plugin packages are separate from config. Copy
[`examples/plugins/surround`](../examples/plugins/surround), edit its manifest
and `init.lua`, then validate it before launching Zenbu:

```sh
dune exec bin/zenbu_headless.exe -- plugin-check examples/plugins/surround
dune exec bin/zenbu.exe -- --plugin-dir examples/plugins README.md
```

An explicitly named bad `--config` or `--plugin-dir` fails before Zenbu enters
terminal mode and prints the structured configuration/package error. A failed
default user config remains visible in the session inspector so an ordinary
unnamed launch is still recoverable.

For isolated Components, use the WIT and Rust guest examples in
[Component authoring](WASM_COMPONENTS.md). The generated
[Extension API](generated/EXTENSION_API.md), [Lua SDK](../sdk/lua/zenbu.lua),
and [WIT contract](wit/zenbu-plugin.wit) are the stable v1 references.

## Boundaries and limits

M11 deliberately limits cross-file language edits to already-open saved
buffers. The host provides an explicit-root file picker and bounded literal
project-text search for local readable text files; it does not include grammar
downloads, a marketplace, automatic external reload, or a system clipboard
bridge. Its local file watcher reports external changes without modifying a
buffer. Normal save does refuse externally changed, replaced, and missing targets. See [the
roadmap](ROADMAP.md) before designing around a missing feature.
