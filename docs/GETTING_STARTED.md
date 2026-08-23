# Getting started

Zenbu is a single-buffer terminal editor with three interchangeable editing
models. Its kernel owns every text mutation; models, Lua, and WebAssembly
Components request ordinary semantic edits instead of changing a buffer
directly.

## Install from a clone

Linux x86_64 and Apple Silicon macOS are supported interactive platforms in
M11. Linux needs `opam`, a C toolchain, `curl`, `tar`, and `sha256sum`.
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
Apple Silicon macOS. The macOS archive bundles Wasmtime, Lua 5.4, and libffi;
run its `bin/zenbu` or `bin/zenbu-headless` launchers after extraction. These
macOS command-line archives are unsigned and not notarized.

## First ten minutes

Open an OCaml or JSON file. Zenbu detects those extensions and colours the
current version-bound syntax snapshot. Unknown extensions remain plain text.
The initial model is Vim-style; choose an alternative at startup with
`--model selection` or `--model structural`, or change it live with `Alt-M`.

| key | action |
| --- | --- |
| `Ctrl-S` | save; opens save-as for an unnamed buffer |
| `Ctrl-Shift-S` | save-as; an existing destination is atomically replaced |
| `Ctrl-Q` | quit; press again to force-quit a dirty buffer |
| `Ctrl-F` | start literal Unicode search |
| `Ctrl-G` / `Ctrl-Shift-G` | next / previous match, with wraparound |
| `Ctrl-P` | command palette over Zenbu, model, Lua, and plugin commands |
| `Alt-M` | switch Vim-style, selection-first, and structural models |
| `Alt-H` | help from current model metadata |
| `Ctrl-O` | inspect the latest `why` explanation |
| `Ctrl-Space` | ask a ready language service for completion |
| `Alt-R` / `Ctrl-Alt-R` | reload the selected Lua config and local plugins |

Search is literal and incremental. `Escape` while its prompt is open restores
the pre-search selection; `Enter` keeps the selected match. Search highlighting
is view-only and selection styling always wins over it.

`Alt-H` shows the active model's `Input_rule` table. For deterministic,
scriptable inspection, use:

```sh
dune exec bin/zenbu_headless.exe -- commands
dune exec bin/zenbu_headless.exe -- describe command search.next
dune exec bin/zenbu_headless.exe -- bindings vim
dune exec bin/zenbu_headless.exe -- demo
```

For a saved `.ml` or `.mli`, M11 starts `ocamllsp` when it is available. Use
the palette for `language.status`, hover, definition, completion, rename, and
diagnostic navigation; `Ctrl-Space` asks for completion. The service is
asynchronous but every accepted edit still becomes a normal transaction. See
[Language services](LANGUAGE_SERVICES.md) for server selection, status, and
the intentional single-buffer/cross-file limits.

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

M11 deliberately does not include cross-file buffers/edits, panes,
project/file search, a file picker, grammar downloads, a marketplace,
external-file conflict detection, or a system clipboard bridge. See
[the roadmap](ROADMAP.md) before designing around a missing feature.
