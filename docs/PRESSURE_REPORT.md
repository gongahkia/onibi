# M10 pressure report

This report records the release-hardening pressure boundaries, the tests that
guard them, and the M10 local acceptance findings. It is deliberately candid:
passing the checks below does not turn Zenbu into a complete IDE.

It is the historical M10 report. M11 language-service evidence and its current
limits live in [M11 pressure test](M11_PRESSURE_TEST.md).

| Pressure | Guard |
| --- | --- |
| Fatal Component callback | M9 Component test asserts fuel/trap classification, an unavailable health view, non-mutating repeated invocation, normal host edits, and successful reload recovery. |
| Host interaction isolation | M10 host test drives Unicode/incremental/missing search, cancellation restoration, viewport follow, host/builtin/script palette dispatch, save-as, model switch, and paste decoding through `Session`, without model-private APIs. |
| Snapshot presentation | M10 host test checks OCaml/JSON/malformed/unknown/current snapshot behavior plus selection > search > syntax rendering precedence. |
| Large-buffer frame work | `make benchmark` uses generated 1 MiB OCaml. Session caching retains source-line and highlight projection by contents; the renderer filters spans to the viewport before grapheme styling. |
| Syntax lifecycle | M5 tests compare incremental and full parses and reject stale snapshot use. |
| Script/plugin lifecycle | M7/M8/M9 tests cover registration, reload replacement/failure retention, capabilities, provenance, and runtime limits. |
| Regression breadth | `make check` runs formatter validation, all builds, and every test executable; `make demo` exercises the public headless path. |

Known bounded risks remain: the Component runtime is native Linux x86_64 and
Apple Silicon macOS code;
bracketed paste is mediated by terminal markers that Notty documents as
best-effort; and the renderer is synchronous/full-frame rather than a terminal
capability-negotiating UI framework.

## M10 local evidence

The M10 worktree acceptance ran `dune build @fmt`, `dune build @all`,
`dune runtest --force`, `make check`, `make demo`, `make benchmark`, and
`make release-check`. The benchmark used a generated 1 MiB OCaml source; the
recorded numbers are in [Performance](PERFORMANCE.md).

A real Notty PTY pass opened OCaml, valid JSON, and malformed JSON; exercised
Vim insertion/deletion/undo/redo, selection-first and structural editing,
Unicode incremental search plus cancellation, palette/help, `why`, resize,
unnamed save-as, later ordinary save, bracketed multiline Unicode paste and
its one-step undo, clean exit, and dirty two-press exit. Separate sessions
loaded the example Lua config, Lua plugin, normal Component plugin, and an M9
Component fixture. The latter exhausted fuel, returned structured
`extension-runtime-unavailable` thereafter, permitted ordinary Vim editing,
and recovered its callback after reload.

## M10 usability / adoption pressure test

The host work landed without a second editing API: search changes selections
through the existing semantic runtime, palette entries come from descriptors,
and switching models preserves shared state while clearing private grammar.
That is the most compelling interactive proof of the project's thesis. The
palette's provider labels made a Lua command, a local plugin command, and a
Component command immediately distinguishable; metadata-derived help made the
small supported model subsets discoverable without duplicate binding tables.

The first confusing setup issue is a bare global `dune` invocation: it can see
neither the project-local Tree-sitter sublibraries nor `ocamlformat`. The
README now makes `make bootstrap` and the explicit local-switch activation
line the only direct-Dune path. Explicit broken config/plugin arguments now
also fail before terminal entry. Component authoring is viable for an example
package but still requires the WIT/Rust guide rather than a scaffold.

Zenbu remains short of a daily coding environment because it has no language
diagnostics, completion, navigation, or rename; it also lacks project search,
file watching, a system clipboard bridge, and a cross-platform Component
runtime. Normal save refuses externally changed, replaced, and missing targets;
synchronous first-frame highlighting of a large source is the measurable
presentation cost. These observations support
M11 language intelligence as the next focused milestone; portable Component
distribution remains a subsequent packaging milestone.
