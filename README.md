<h1 align="center"><code>Zenbu</code></h1>

<p align="center"><img src="./asset/logo/zenbu-logo-transparent.png" width="40%" height="40%" alt="zenbu mascot"></p>

<p align="center"><em>A programmable framework for building text editors in the terminal</em></p>

<p align="center">
  <a href="https://github.com/gongahkia/Zenbu/releases/tag/1.0.0"><img src="https://img.shields.io/badge/zenbu_1.0.0-passing-light_green"></a>
  <a href="https://github.com/gongahkia/Zenbu/actions/workflows/ci.yml"><img src="https://github.com/gongahkia/zenbu/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
</p>

## What is Zenbu?

`Zenbu` ships in 2 parts.

1. A declarative [DSL](https://en.wikipedia.org/wiki/Domain-specific_language) comprised of `.zenmodel` files that [define the editor's input model](#the-zenbu-dsl).
2. An [OCaml](https://ocaml.org/) text editor written in `.zenmodel` as a proof of `Zenbu`'s capabilities.

## Current features

| Area | Capability |
| --- | --- |
| Editing models | Built-in Vim-like, selection-first, direct, and structural models; trusted Lua models; and declarative `.zenmodel` grammars |
| Semantic core | Immutable UTF-8 documents, ordered selections, validated transactions, branching undo/redo, deterministic replay, and provenance |
| Zenbu editing-model DSL | Statically checked finite state/input grammars with prefixes, text capture, actions, a small guard surface, and eligible selection commands |
| Terminal host | Multi-buffer split views, themes, presentation profiles, search/replace, command palette, layouts, project file/search tools, and generic model inspection |
| Language support | Version-bound OCaml/JSON syntax, diagnostics, hover, definition, completion, rename, formatting, and a private LSP adapter |
| Customisation | Trusted local Lua configuration and separately capability-constrained Wasm Component extensions |
| Tooling | Headless replay, model/session execution, grammar validation/description, configuration checks, inspection, and extension-contract generation |

## The Zenbu DSL

> [!NOTE]  
> For more details, see the [language reference](docs/EDITING_MODEL_DSL.md) for `.zenmodel`.

`.zenmodel` is Zenbu's declarative surface for specifying a finite, deterministic,
inspectable editing grammars. It compiles to the intermediary format `Editing_model.S` and returns existing `Model_effect` values *(the same as every other editing model)*.

Below is a snippet of `.zenmodel` in action.

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

## Usage

> [!IMPORTANT]  
> Zenbu requires `opam` *(a C toolchain)*, `curl`, `tar` *(a SHA-256 tool)* and a Lua 5.4 shared library to run.
> 
> 
> * Linux users should therefore install `lua-libs`
> * MacOS users should install Xcode Command Line Tools, Homebrew, `opam`, and `lua@5.4`

The below instructions are for getting started with `Zenbu` on your local machine.

1. First run the below to get `Zenbu` installed locally.

```console
$ git clone https://github.com/gongahkia/zenbu.git && cd zenbu
$ opam init --bare --yes # once on a new Opam installation
$ make bootstrap
$ make build
```

2. Next, execute the below to activate a local switch before invoking Dune directly.

```console
$ eval "$(opam env --switch="$PWD" --set-switch)"
$ eval "$(./scripts/zenbu-env.sh)"
```

3. Finally, run the editor with a built-in, Lua, or declarative model.

```console
$ dune exec bin/zenbu.exe -- README.md
$ dune exec bin/zenbu.exe -- --model selection README.md
$ dune exec bin/zenbu.exe -- --model direct README.md
$ dune exec bin/zenbu.exe -- --model structural README.md
$ dune exec bin/zenbu.exe -- --model script --config examples/script-modal-editor.lua README.md
$ dune exec bin/zenbu.exe -- --model-dsl examples/script-modal-editor.zenmodel README.md
```

4. Optionally run any of the other below commands to interact with `Zenbu`'s functionality.

```console
$ dune exec bin/zenbu_headless.exe -- model-check examples/modal-operator.zenmodel
$ dune exec bin/zenbu_headless.exe -- model-describe examples/modal-operator.zenmodel
```

5. `Zenbu` also exposes some deterministic headless tooling for CI and model authors.

```console
$ dune exec bin/zenbu_headless.exe -- demo
$ dune exec bin/zenbu_headless.exe -- replay test/fixtures/unicode.replay
$ dune exec bin/zenbu_headless.exe -- session test/fixtures/sessions/vim-edit.session
$ dune exec bin/zenbu_headless.exe -- bindings direct
$ dune exec bin/zenbu_headless.exe -- syntax test/fixtures/syntax_sample.ml
$ dune exec bin/zenbu_headless.exe -- why test/fixtures/sessions/observability-vim.session
$ dune exec bin/zenbu_headless.exe -- model-check examples/script-modal-editor.zenmodel
$ dune exec bin/zenbu_headless.exe -- model-describe examples/script-modal-editor.zenmodel
$ dune exec bin/zenbu_headless.exe -- extension-api
```

## Other documents

* [Getting started](docs/GETTING_STARTED.md): guided workflow, controls, configuration, and plugins.
* [Editing-model DSL](docs/EDITING_MODEL_DSL.md): `.zenmodel` syntax, static checks, runtime behavior, compatibility, and authority boundary.
* [Architecture](docs/ARCHITECTURE.md): package, semantic*kernel, model, syntax, and extension boundaries.
* [Editing model API](docs/EDITING_MODEL_API.md) and [editing protocol](docs/EDITING_PROTOCOL.md): public model/effect contract.
* [Scripting](docs/SCRIPTING.md), [extensions](docs/EXTENSIONS.md), and [isolation](docs/ISOLATION.md): distinct Lua and Wasm authority models.
* [Terminal](docs/TERMINAL.md), [syntax](docs/SYNTAX.md), [presentation](docs/PRESENTATION.md), [themes](docs/THEMES.md), and [language services](docs/LANGUAGE_SERVICES.md): host behavior.
* [Observability](docs/OBSERVABILITY.md), [performance](docs/PERFORMANCE.md), [roadmap](docs/ROADMAP.md), and [release process](docs/RELEASE.md): engineering references.
