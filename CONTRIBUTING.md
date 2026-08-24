# Contributing to Zenbu

Zenbu is intentionally boundary-driven. Before proposing a feature, identify
whether it belongs in the semantic kernel, the public model API, a model, the
syntax library, the host, or the terminal adapter. Do not let a model mutate
kernel state directly or let Tree-sitter/Lua/Wasmtime types cross their private
boundaries.

## Local setup

On Linux x86_64, install `opam`, an OCaml-capable C toolchain, `curl`, `tar`,
`sha256sum`, and a Lua 5.4 shared library (`lua-libs` on Fedora), then run:

```sh
make bootstrap
```

The command owns an ignored `_opam` switch (using the system OCaml, which must
be 5.3.0 or newer) and `.zenbu/` runtime cache. Do not commit either. The
pinned Component runtime supports Linux x86_64 and Apple Silicon macOS; macOS
also requires Homebrew Lua 5.4.

## Before sending a change

```sh
make fmt
make check
make demo
make benchmark
make release-check
```

Run the smallest targeted test while iterating, then the complete check before
handoff. Add tests for public behavior and boundary invariants. If a change
alters the extension contract, run `make extension-docs` and include the
generated WIT, Lua SDK, and API documentation changes.

For terminal work, manually check a real TTY at a small size and with Unicode
text. For Component work, exercise both a successful callback and a fatal
callback followed by a reload; a fatal Component must report unavailable while
the rest of the host still edits normally.

## Scope and compatibility

Keep the kernel model-neutral. New host interactions must work across all
editing models and use semantic effects for selection/document changes. New
syntax presentation consumes version-bound snapshots and must not expose
Tree-sitter objects. New script/plugin capabilities remain host authority,
validated at registration and invocation.

Do not add cross-file language edits, project search, background extension
execution, a marketplace, or grammar downloads as incidental work. Language
servers remain private host processes: preserve their version-gated inbox and
do not expose LSP/JSON-RPC values through the model API.

## Architecture and ADRs

The two non-negotiable rules are: no editing model is fundamental, and every
text/selection mutation passes through a semantic intent and atomic transaction.
Put model grammar in a model; terminal prompts and paths in `zenbu.app`;
snapshot-derived presentation in `zenbu.view`; parser details in `zenbu.syntax`;
and runtime-specific values behind scripting/extension adapters. Add or update
an ADR under `docs/adr/` when changing one of those boundaries or a durable
policy, not for a local implementation detail.

Run `make benchmark` after a rendering, syntax, search, Lua, or Component
change and compare it with [the recorded baseline](docs/PERFORMANCE.md). For a
release-candidate build use `make release`; it produces dynamic Linux x86_64
artifacts, while `make install` places the matching Wasmtime library in the
active Opam prefix. Release archives on both supported platforms bundle
Wasmtime, Lua 5.4, and libffi.
