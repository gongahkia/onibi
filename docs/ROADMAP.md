# Roadmap

This repository implements M0-M11.

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
| M11 | model-neutral asynchronous language intelligence | implemented in this checkout |

M10 added host/presentation policy above the existing semantic boundary:
literal Unicode search, all-provider command discovery/palette, save-as,
metadata-derived help, live model switching, syntax colouring from public
snapshot spans, bracketed-paste aggregation, Component health/reload behavior,
bootstrap/install/release targets, CI, and adoption documentation. It did not
grant a host or renderer a private document mutation path.

M11 adds optional `zenbu.language` data and a private `zenbu.lsp` adapter. The
default `.ml`/`.mli` path starts `ocamllsp`; diagnostics, hover, same-document
definition, explicit completion, rename, and bounded `workspace/applyEdit`
results arrive asynchronously, then use normal selection effects or
transactions. LSP/JSON-RPC values remain outside the kernel and editing-model
API. See [Language services](LANGUAGE_SERVICES.md).

## Deferred work

The following remain deliberately out of scope:

- cross-platform Wasmtime C API distribution and a first-party Component guest
  SDK/package build tool;
- signatures, dependency resolution, remote download, project discovery, or a
  plugin marketplace;
- hard wall-clock cancellation, asynchronous/background extension execution,
  richer Component imports, or a sandbox claim for trusted Lua;
- project search, file watching, panes/layouts, multi-buffer/cross-file edits,
  command-line/Ex compatibility, macros, or broad Vim/Helix/Kakoune emulation;
- LSP code actions, formatting, symbols, semantic tokens, user-authored server
  configuration, workspace folders, and a language-server trust/sandbox model;
- public Tree-sitter query APIs, grammar downloads, embedded-language parsing,
  refactoring, or asynchronous syntax workers;
- external-modification conflict detection and parent-directory fsync after
  save (M10's save-as uses the established adjacent-temp-file atomic write).

## Next proposed milestone

M11 makes **M12: workspace buffers and cross-file language results** the
strongest next step. It should extend the version-gated language inbox to a
document table, open same-workspace definition targets, and either apply a
fully validated workspace edit or reject it atomically. It should define
external-file-change and save coordination before adding file watching. It must
not make buffers, providers, or language protocols visible to the kernel or
model API, and should not bundle project search, panes, a marketplace, or a
Component distribution redesign.

Portable Component distribution and a first-party guest authoring tool remain
the next packaging concern after M11: they need a verified platform matrix and
must preserve WIT capability projection plus fatal-runtime health/reload
semantics without adding a resolver or marketplace.

See [the architecture](ARCHITECTURE.md), [Component authoring](WASM_COMPONENTS.md),
[isolation policy](ISOLATION.md), [M11 pressure report](M11_PRESSURE_TEST.md),
and [release gate](RELEASE.md).
