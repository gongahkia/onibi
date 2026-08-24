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
| M9 | isolated WebAssembly Component plugin runtime | implemented on Linux x86_64 and Apple Silicon macOS |
| M10 | usability, adoption, and release hardening | implemented in this checkout |
| M11 | model-neutral asynchronous language intelligence | implemented in this checkout |

M10 added host/presentation policy above the existing semantic boundary:
literal Unicode plus UTF-8-safe `Str` regexp search, one-buffer atomic
literal/regexp replace-all and reviewed query-replace, all-provider command discovery/palette, save-as,
metadata-derived help, live model switching, syntax colouring from public
snapshot spans, bracketed-paste aggregation, Component health/reload behavior,
basic terminal pointer selection/scrolling, scoped logical input sequences,
checked line/page/center viewport requests and PageUp/PageDown decoding,
bootstrap/install/release targets, CI, and adoption documentation. It did not
grant a host or renderer a private document mutation path.

The split-view host additionally stores bounded per-split proportions. It can
move the focused pane's nearest matching divider by one cell, drag an exact
visible divider, reset the tree to equal proportions, and save/restore a
versioned local JSON layout through palette commands. Layout files contain only
validated clean file-backed buffer references and host-owned split/view state;
restore validates all referenced files and selections before it replaces the
current session. These operations do not expose pane identifiers or terminal
geometry to a model. They intentionally omit product-specific minimum-size
policy, numeric resize arguments, cross-machine sync, and product/process
state serialization.

M11 adds optional `zenbu.language` data and a private `zenbu.lsp` adapter. The
default `.ml`/`.mli` path starts `ocamllsp`; diagnostics, hover, same- and
cross-file definition, explicit completion, checked code actions, checked
document/range formatting, bounded document/workspace symbols, rename, and
bounded
`workspace/applyEdit` results arrive asynchronously, then use normal selection
effects or transactions. LSP/JSON-RPC values remain outside the kernel and
editing-model API. See [Language services](LANGUAGE_SERVICES.md).

## Deferred work

The following remain deliberately out of scope:

- a first-party Component guest SDK/package build tool;
- signatures, dependency resolution, remote download, or a plugin marketplace;
- hard wall-clock cancellation, asynchronous/background extension execution,
  richer Component imports, or a sandbox claim for trusted Lua;
- automatic external reload/merge, general cross-file workspace resource
  operations, command-line/Ex compatibility, or broad Vim/Helix/Kakoune
  emulation; the explicit-root picker, bounded literal project search, and
  report-only local watcher are host-owned surfaces, not a general project
  workspace;
- LSP semantic tokens, user-authored server configuration, workspace folders,
  code-action command execution, and a language-server trust/sandbox model;
- public Tree-sitter query APIs, grammar downloads, embedded-language parsing,
  refactoring, or asynchronous syntax workers;
- parent-directory fsync after save (normal save detects externally changed,
  replaced, and missing targets; save-as intentionally replaces its target).

## Next proposed milestone

M12 has same-buffer split-view composition, a local buffer table, bounded
cross-file language results, versioned local layout persistence, an
explicit-root file-picker boundary, and bounded literal project search. Views have stable buffer ids and ordered
selection snapshots per `(view, buffer)` pairing; focused input restores that
view's selection and inactive positions rebase through forward local history.
Layout restore remains deliberately local and host-owned: it rebuilds only
clean file-backed buffers after preflight rather than storing text,
model/plugin/LSP state, or project metadata. The picker and search expose only
bounded validated relative text-file candidates/results to the host; result
activation reuses normal buffer loading and selection. Buffers retain their own model runtime/history, and all open saved
buffers participate in language wakeup polling. A definition target
opens/reuses a local buffer. Rename and `workspace/applyEdit` stage and publish
edits across already-open saved targets all-or-none, while retaining per-buffer
history. File watching or target auto-open must build on normal-save conflict
detection without making buffers, providers, or language protocols visible to
the kernel or model API, and should not bundle a marketplace or a Component
distribution redesign.

Portable Component distribution and a first-party guest authoring tool remain
the next packaging concern after M11: they need a verified platform matrix and
must preserve WIT capability projection plus fatal-runtime health/reload
semantics without adding a resolver or marketplace.

See [the architecture](ARCHITECTURE.md), [editor workload evaluation](EDITOR_WORKLOAD_EVALUATION.md), [Component authoring](WASM_COMPONENTS.md),
[isolation policy](ISOLATION.md), [M11 pressure report](M11_PRESSURE_TEST.md),
and [release gate](RELEASE.md).
