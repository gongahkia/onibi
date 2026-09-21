<h1 align="center"><code>Zenbu</code></h1>

<p align="center"><img src="./assets/logo/zenbu-logo-transparent.png" width="40%" height="40%" alt="zenbu mascot"></p>

<p align="center"><em>A Research Language for modelling & evaluating Singapore Criminal Law </em></p>

<p align="center">
  <a href="https://github.com/gongahkia/Zenbu/releases/tag/1.0.0"><img src="https://img.shields.io/badge/zenbu_1.0.0-passing-light_green"></a>
  <a href="https://github.com/gongahkia/Zenbu/actions/workflows/ci.yml"><img src="https://github.com/gongahkia/zenbu/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
</p>

## What is Zenbu?

`Zenbu`

# `Zenbu`

[![CI](https://github.com/gongahkia/zenbu/actions/workflows/ci.yml/badge.svg)](https://github.com/gongahkia/zenbu/actions/workflows/ci.yml)

A terminal-first text editor with programmable, model-neutral editing.

[Getting started](docs/GETTING_STARTED.md) · [Editing-model DSL](docs/EDITING_MODEL_DSL.md) · [Architecture](docs/ARCHITECTURE.md) · [Extension API](docs/generated/EXTENSION_API.md)

## What is Zenbu?

Zenbu is an OCaml text editor built around a simple boundary: no particular
editing grammar owns the document. Vim-like, selection-first, direct,
structural, trusted-Lua, and declarative `.zenmodel` models all turn logical
input into the same inspectable model effects. The ordinary runtime then
validates and applies those effects through semantic intents, transactions,
history, syntax updates, provenance, and replay.

That makes input grammars replaceable without creating a second mutation path.
An editing model cannot directly mutate a document, selection, history, or
syntax tree.

## Current capabilities

| Area | Capability |
| --- | --- |
| Editing models | Built-in Vim-like, selection-first, direct, and structural models; trusted Lua models; and declarative `.zenmodel` grammars |
| Semantic core | Immutable UTF-8 documents, ordered selections, validated transactions, branching undo/redo, deterministic replay, and provenance |
| Zenbu editing-model DSL | Statically checked finite state/input grammars with prefixes, text capture, actions, a small guard surface, and eligible selection commands |
| Terminal host | Multi-buffer split views, themes, presentation profiles, search/replace, command palette, layouts, project file/search tools, and generic model inspection |
| Language support | Version-bound OCaml/JSON syntax, diagnostics, hover, definition, completion, rename, formatting, and a private LSP adapter |
| Customisation | Trusted local Lua configuration and separately capability-constrained Wasm Component extensions |
| Tooling | Headless replay, model/session execution, grammar validation/description, configuration checks, inspection, and extension-contract generation |

Zenbu is currently **0.11.0-dev (M11)**. It is an actively developed source
release, not a finished editor distribution.

## The Zenbu editing-model DSL

`.zenmodel` is Zenbu's declarative surface for finite, deterministic,
inspectable editing grammars. It compiles to `Editing_model.S` and returns the
same existing `Model_effect` values as every other editing model; it is not a
second editor runtime or a Lua substitute.

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

Use `.zenmodel` when the behavior is naturally a finite input/state grammar.
Use trusted Lua or OCaml when it needs arbitrary programming, dynamic
algorithms, complex mutable state, counts, registers, macros, or host
automation. See the [language reference](docs/EDITING_MODEL_DSL.md) for the
complete syntax, validation, inspection, replay, and authority rules.

## Installation

Zenbu supports Linux x86_64 and Apple Silicon macOS. It needs `opam`, a C
toolchain, `curl`, `tar`, a SHA-256 tool, and a Lua 5.4 shared library. Fedora
users should install `lua-libs`; on macOS install Xcode Command Line Tools,
Homebrew, `opam`, and `lua@5.4`:

```sh
xcode-select --install
brew install opam lua@5.4
```

Then bootstrap a checkout:

```sh
git clone https://github.com/gongahkia/zenbu.git
cd zenbu
opam init --bare --yes # once on a new Opam installation
make bootstrap
make build
```

`make bootstrap` is idempotent. It creates only ignored local `_opam` and
`.zenbu/` directories, prepares OCaml 5.3.0 and test dependencies, fetches the
pinned Wasmtime C API, and runs the normal validation gate. For an already
prepared switch, use `make build`, `make test`, or `make check`.

## Usage

Activate the local switch before invoking Dune directly:

```sh
eval "$(opam env --switch="$PWD" --set-switch)"
eval "$(./scripts/zenbu-env.sh)"
```

Run the editor with a built-in, Lua, or declarative model:

```sh
dune exec bin/zenbu.exe -- README.md
dune exec bin/zenbu.exe -- --model selection README.md
dune exec bin/zenbu.exe -- --model direct README.md
dune exec bin/zenbu.exe -- --model structural README.md
dune exec bin/zenbu.exe -- --model script --config examples/script-modal-editor.lua README.md
dune exec bin/zenbu.exe -- --model-dsl examples/script-modal-editor.zenmodel README.md
```

Validate and inspect a grammar without entering terminal mode:

```sh
dune exec bin/zenbu_headless.exe -- model-check examples/modal-operator.zenmodel
dune exec bin/zenbu_headless.exe -- model-describe examples/modal-operator.zenmodel
```

`--model-dsl PATH` reads, validates, and compiles the grammar before terminal
raw mode begins. It is mutually exclusive with `--model`; there is no hot
reload, and layouts deliberately do not persist external grammar paths.

## Model surfaces and authority

| Surface | Best for | Authority model |
| --- | --- | --- |
| Built-in OCaml models | First-party, specialised behavior | Compiled against the public model API; return ordinary effects |
| `.zenmodel` | Finite, shareable, statically validated editing grammar | Inert grammar; only fixed effect forms and eligible no-argument selection commands |
| Trusted Lua | Local programmable models and automation | Trusted local code; **not** a sandbox |
| Wasm Components | Third-party executable extensions | Separate capability-constrained Extension API v1 runtime |

`.zenmodel` has no filesystem, process, terminal, renderer, plugin, Lua, or
Wasm authority. Trusted Lua remains trusted local code. Wasm Components remain
separately isolated under their existing capability contract.

## Headless tooling

Zenbu exposes deterministic tooling for CI and model authors:

```sh
dune exec bin/zenbu_headless.exe -- demo
dune exec bin/zenbu_headless.exe -- replay test/fixtures/unicode.replay
dune exec bin/zenbu_headless.exe -- session test/fixtures/sessions/vim-edit.session
dune exec bin/zenbu_headless.exe -- bindings direct
dune exec bin/zenbu_headless.exe -- syntax test/fixtures/syntax_sample.ml
dune exec bin/zenbu_headless.exe -- why test/fixtures/sessions/observability-vim.session
dune exec bin/zenbu_headless.exe -- model-check examples/script-modal-editor.zenmodel
dune exec bin/zenbu_headless.exe -- model-describe examples/script-modal-editor.zenmodel
dune exec bin/zenbu_headless.exe -- extension-api
```

## Documentation

- [Getting started](docs/GETTING_STARTED.md): guided workflow, controls, configuration, and plugins.
- [Editing-model DSL](docs/EDITING_MODEL_DSL.md): `.zenmodel` syntax, static checks, runtime behavior, compatibility, and authority boundary.
- [Architecture](docs/ARCHITECTURE.md): package, semantic-kernel, model, syntax, and extension boundaries.
- [Editing model API](docs/EDITING_MODEL_API.md) and [editing protocol](docs/EDITING_PROTOCOL.md): public model/effect contract.
- [Scripting](docs/SCRIPTING.md), [extensions](docs/EXTENSIONS.md), and [isolation](docs/ISOLATION.md): distinct Lua and Wasm authority models.
- [Terminal](docs/TERMINAL.md), [syntax](docs/SYNTAX.md), [presentation](docs/PRESENTATION.md), [themes](docs/THEMES.md), and [language services](docs/LANGUAGE_SERVICES.md): host behavior.
- [Observability](docs/OBSERVABILITY.md), [performance](docs/PERFORMANCE.md), [roadmap](docs/ROADMAP.md), and [release process](docs/RELEASE.md): engineering references.

## Development and release checks

```sh
make fmt
make build
dune runtest
make test
make check
git diff --check
```

`make check` runs formatting verification, the full build, Component SDK and
distribution checks, and the complete test suite. `make install` installs the
active Opam package plus its required Wasmtime library; `make release` builds
release-profile binaries, and `make release-archive` makes a local unsigned
platform archive.

## Deliberate limits

Zenbu does not claim full Vim, Emacs, Helix, Kakoune, or Micro compatibility.
It has no Ex/command-line language, plugin marketplace, general workspace
resource API, automatic external-file reload, code-action command execution,
or native grammar loading. The `.zenmodel` language deliberately excludes
general expressions, variables, counts, registers, macros, loops, callbacks,
command arguments, filesystem/process effects, and host UI control.

Those limits keep the semantic core model-neutral and the declarative language
finite, deterministic, inspectable, replay-friendly, and authority-constrained.

## Contributing

Read [Contributing](CONTRIBUTING.md) for the expected local workflow. The
[architecture](docs/ARCHITECTURE.md) and [invariants](docs/INVARIANTS.md) are
the best starting points before changing kernel or model behavior.
