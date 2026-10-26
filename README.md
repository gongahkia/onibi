[![](https://github.com/gongahkia/shack/actions/workflows/ci.yml/badge.svg)](https://github.com/gongahkia/shack/actions/workflows/ci.yml)

# `Shack`

Shack is an **atomweight terminal text editor IDE with sensible Day 1 defaults** for macOS and Linux.

<div align="center">
    <img src="./assets/logo/shack.png" width="20%" alt="A small wooden shack with a corrugated roof">
</div>

## Stack

* *Scripting*: [Go 1.27+](https://go.dev/dl/)
* *Terminal*: [tcell](https://github.com/gdamore/tcell)
* *Highlighting*: [Chroma](https://github.com/alecthomas/chroma)
* *Build*: Make
* *Test*: Go's built-in testing tools

## Features

* Nonmodal editing with familiar keyboard shortcuts and mouse selection
* Multiple buffers, split panes, a project file tree and an optional minimap
* Literal and regex search, multiple selections and reviewed multi-file replacements
* Bundled syntax highlighting, with optional language servers for completion, diagnostics, navigation and formatting
* Markdown and Org reading views, live previews and code folding
* Integrated terminals, build/test tasks, Git tools and optional source-level debugging
* Unsaved-buffer recovery, project sessions and [JSON configuration](./docs/USAGE.md#configuration)

## Usage

> [!IMPORTANT]
> Building from source requires Go 1.27 or newer and `make`. The current version is `0.2.0-dev`.

### Building `Shack` yourself

1. Clone the repository.

```console
$ git clone https://github.com/gongahkia/shack && cd shack
```

2. Build and open the project in your terminal.

```console
$ GOTOOLCHAIN=auto make build
$ ./build/shack .
```

[`GOTOOLCHAIN=auto`](https://go.dev/doc/toolchain) lets Go download the required toolchain if your installed version is older.

To open a specific file, or edit without language servers:

```console
$ ./build/shack docs/USAGE.md
$ ./build/shack --no-lsp .
```

Press **F1** for help, **F2** for commands, **Ctrl+S** to save and **Ctrl+Q** to quit.

No configuration file is needed. Language servers are optional external programs detected on your `PATH`; ordinary editing and highlighting work without them.

### Development checks

```console
$ GOTOOLCHAIN=auto make test
$ GOTOOLCHAIN=auto make check
```

See the [usage guide](./docs/USAGE.md) for all controls and configuration, and [language-server workflows](./docs/LANGUAGE_SERVERS.md) for optional language support.
