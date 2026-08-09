# Roadmap

This repository implements M0/M2 only.

| milestone | goal | status |
| --- | --- | --- |
| M0 | kernel primitives | implemented here |
| M1 | headless editing, transactions, history, replay | implemented here |
| M2 | public editing-model API and input/state-machine abstraction | implemented here |
| M3 | small Vim model and small selection-first model | future |
| M4 | terminal host and rendering | future |
| M5 | Tree-sitter integration and structural editing model | future |
| M6 | introspection: `:describe`, `:why`, `:bindings`, `:history`, `:trace` | future |
| M7 | hot-reloadable scripting/configuration | future |
| M8 | stable plugin contract, capabilities, SDK, generated docs | future |
| M9 | isolated language-neutral plugin runtime, possibly WASM/components if appropriate for OCaml | future |

M7-M9 are goals and constraints, not current API commitments. Any future
first-party model or plugin must use the same public editing APIs as a third
party; the core must not acquire a privileged builtin mutation path.

M2 includes only two small proof models to validate the API. M3 remains the
first milestone for actual editing-model behavior, not compatibility claims.
