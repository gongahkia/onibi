# Roadmap

This repository implements M0-M10.

| milestone | goal | status |
| --- | --- | --- |
| M0 | kernel primitives | implemented |
| M1 | headless editing, transactions, history, replay | implemented |
| M2 | public editing-model API and input/state-machine abstraction | implemented |
| M3 | substantial Vim-style and selection-first models | implemented |
| M4 | terminal host and rendering | implemented |
| M5 | Tree-sitter syntax service and structural model | implemented |
| M6 | provenance, `why`, discovery, history, trace, syntax, profiling | implemented |
| M7 | trusted-local hot-reloadable Lua configuration | implemented; experimental API |
| M8 | local plugin contract, capabilities, SDK, diagnostics | implemented |
| M9 | isolated WebAssembly Component plugin runtime | implemented on Linux x86_64 |
| M10 | usability, adoption, and release hardening | implemented in this checkout |

M10 adds only host/presentation policy above the existing semantic boundary:
literal Unicode search, all-provider command discovery/palette, save-as,
metadata-derived help, live model switching, syntax colouring from public
snapshot spans, bracketed-paste aggregation, Component health/reload behavior,
bootstrap/install/release targets, CI, and adoption documentation. It does not
grant a host or renderer a private document mutation path.

## Deferred work

The following remain deliberately out of scope:

- cross-platform Wasmtime C API distribution and a first-party Component guest
  SDK/package build tool;
- signatures, dependency resolution, remote download, project discovery, or a
  plugin marketplace;
- hard wall-clock cancellation, asynchronous/background extension execution,
  richer Component imports, or a sandbox claim for trusted Lua;
- LSP, diagnostics transport, project search, file watching, panes/layouts,
  command-line/Ex compatibility, macros, or broad Vim/Helix/Kakoune emulation;
- public Tree-sitter query APIs, grammar downloads, embedded-language parsing,
  refactoring, or asynchronous syntax workers;
- external-modification conflict detection and parent-directory fsync after
  save (M10's save-as uses the established adjacent-temp-file atomic write).

## Next proposed milestone

The M10 daily-editor pass makes **M11: model-neutral language intelligence**
the strongest next step. It should introduce an asynchronous LSP service for
diagnostics, navigation, hover, completion, and rename as ordinary commands
over Zenbu selections, not model-specific protocol bindings. The service must
remain outside the kernel, must not expose protocol objects through
`zenbu.model_api`, and must leave single-buffer terminal hosting intact. It
should not bundle project search, panes, a marketplace, or a Component
distribution redesign.

Portable Component distribution and a first-party guest authoring tool remain
the next packaging concern after M11: they need a verified platform matrix and
must preserve WIT capability projection plus fatal-runtime health/reload
semantics without adding a resolver or marketplace.

See [the architecture](ARCHITECTURE.md), [Component authoring](WASM_COMPONENTS.md),
[isolation policy](ISOLATION.md), [M10 pressure report](PRESSURE_REPORT.md),
and [release gate](RELEASE.md).
