# Getting started

Zenbu is a single-buffer terminal editor with three interchangeable editing
models. Its kernel owns every text mutation; models, Lua, and WebAssembly
Components request ordinary semantic edits instead of changing a buffer
directly.

## Install from a clone

Linux x86_64 is the supported interactive platform in M10 because the pinned
Wasmtime C API archive is Linux x86_64-only. Install `opam`, a C toolchain,
`curl`, `tar`, and `sha256sum`, then run:

```sh
git clone https://github.com/gongahkia/zenbu.git
cd zenbu
make bootstrap
make build
eval "$(opam env --switch="$PWD" --set-switch)"
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
copies its required `libwasmtime.so` to `PREFIX/lib/zenbu/`. It is a Linux
x86_64 dynamic install, not a static binary.

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

For isolated Components, use the WIT and Rust guest examples in
[Component authoring](WASM_COMPONENTS.md). The generated
[Extension API](generated/EXTENSION_API.md), [Lua SDK](../sdk/lua/zenbu.lua),
and [WIT contract](wit/zenbu-plugin.wit) are the stable v1 references.

## Boundaries and limits

M10 deliberately does not include LSP, panes, project/file search, a file
picker, grammar downloads, a marketplace, external-file conflict detection,
or a system clipboard bridge. See [the roadmap](ROADMAP.md) before designing
around a missing feature.
