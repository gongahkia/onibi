# Roadmap

This repository implements M0-M7.

| milestone | goal | status |
| --- | --- | --- |
| M0 | kernel primitives | implemented here |
| M1 | headless editing, transactions, history, replay | implemented here |
| M2 | public editing-model API and input/state-machine abstraction | implemented here |
| M3 | substantial Vim-style and selection-first models | implemented here |
| M4 | terminal host and rendering | implemented here |
| M5 | Tree-sitter-backed syntax service and structural editing model | implemented here |
| M6 | generic observability: `describe`, `why`, bindings, discovery, history, trace, syntax inspection, profiling | implemented here |
| M7 | trusted-local hot-reloadable Lua scripting/configuration | implemented here (experimental API) |
| M8 | stable plugin contract, capabilities, SDK, generated docs | future |
| M9 | isolated language-neutral plugin runtime, possibly WASM/components if appropriate for OCaml | future |

M8-M9 are goals and constraints, not current API commitments. Any future
first-party model or plugin must use the same public editing APIs as a third
party; the core must not acquire a privileged builtin mutation path.

M2 includes two retained proof models. M3 adds substantial first-party models
as ordinary clients of the same public API; they remain documented subsets, not
Vim/Helix/Kakoune compatibility claims. M4 adds no editing grammar to the
kernel: it hosts both models, owns file/session policy above them, and keeps
terminal-library types inside its backend adapter. M5 adds an optional,
version-bound syntax service behind a private Tree-sitter backend and a third
structural model that uses it only to derive ordinary selections. M5 does not
add highlighting, LSP, refactoring, grammar downloads, or plugin scripting.

M7 adds an intentionally small trusted-local configuration layer. It can
register descriptors, commands, selectors, transformations, scoped bindings,
and document/save hooks; callbacks return data-only semantic results. Lua
values, document handles, history mutation, terminal types, and Tree-sitter
pointers remain private. Session reload evaluates and validates a new Lua state
before replacing the prior generation, and failed reloads preserve the working
generation. The M7 API is experimental: it is neither a sandbox nor a stable
plugin ABI. See [scripting](SCRIPTING.md).

M8 should turn the proven semantic descriptor and lifecycle surfaces into a
documented stable plugin contract with explicit capabilities, an SDK, generated
reference material, compatibility policy, and migration support. It must not
expand extension authority beyond the M7 data-only boundary merely for
convenience. M9 can then evaluate isolation (for example WASM/components) only
against that stable contract and its demonstrated threat model.
