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

The repository's strongest next step is **M11: portable Component distribution
and authoring**. It should first replace the Linux-only pinned-runtime
assumption with a verified platform matrix and package the guest build/tooling
story. It must keep WIT requests capability-projected, preserve fatal-runtime
health/reload semantics, and continue to route every contribution through the
existing extension host and semantic transaction pipeline. No resolver or
marketplace should be added in the same milestone.

See [the architecture](ARCHITECTURE.md), [Component authoring](WASM_COMPONENTS.md),
[isolation policy](ISOLATION.md), [M10 pressure report](PRESSURE_REPORT.md),
and [release gate](RELEASE.md).
