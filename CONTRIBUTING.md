# Contributing to Zenbu

Zenbu is intentionally boundary-driven. Before proposing a feature, identify
whether it belongs in the semantic kernel, the public model API, a model, the
syntax library, the host, or the terminal adapter. Do not let a model mutate
kernel state directly or let Tree-sitter/Lua/Wasmtime types cross their private
boundaries.

## Local setup

On Linux x86_64, install `opam`, an OCaml-capable C toolchain, `curl`, `tar`,
and `sha256sum`, then run:

```sh
make bootstrap
```

The command owns an ignored `_opam` switch (using the system OCaml, which must
be 5.3.0 or newer) and `.zenbu/` runtime cache. Do not
commit either. The pinned Component runtime is currently Linux x86_64 only;
macOS development can cover kernel/model/syntax code only after explicitly
disabling or replacing that native runtime boundary.

## Before sending a change

```sh
make fmt
make check
make demo
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

Do not introduce LSP, project search, background execution, a marketplace, or
grammar downloads as incidental work. Those are explicit roadmap decisions.
