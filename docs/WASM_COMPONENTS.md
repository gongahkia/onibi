# M9 WebAssembly Component extensions

M9 is Zenbu's first isolated extension runtime. It is deliberately an adapter
to Extension API v1, not a second editing kernel: Component callbacks receive
data-only requests and return declarative values; Zenbu alone resolves intents,
validates transactions, commits history, refreshes syntax, and records
provenance.

## Runtime and installation

The host embeds the official Wasmtime 47.0.3 C API behind a private C shim.
Linux x86_64 uses the matching Linux archive; Apple Silicon macOS uses the
matching aarch64 macOS archive. Run this once in a fresh checkout:

```sh
make wasm-runtime
```

The script downloads the exact official archive into ignored `.zenbu/`, verifies
its platform-specific SHA-256, and refuses to replace an incomplete existing
destination. `make build`,
`make test`, `make check`, and `make demo` only verify that this prerequisite is
present; they never download an archive implicitly. Any Wasmtime upgrade or new
platform port must repeat the ADR 0026 vertical spike and M9 conformance suite.

The embedded linker registers no WASI implementation and this v1 Component
world has no host imports. Consequently a component importing `wasi:*`, an
undeclared Zenbu interface, or any arbitrary import fails instantiation. It has
no filesystem, network, process, environment, clock, random, stdin, stdout,
or stderr capability through Zenbu. This does not make `lua-trusted` packages
safe; only `wasm-component` receives this isolation policy.

## Package and resource policy

Use the normal Extension API v1 manifest with `runtime = "wasm-component"` and
a Component binary entrypoint. The optional `[wasm]` table sets positive limits:

```toml
[plugin]
id = "com.example.hello"
api = 1
runtime = "wasm-component"
entrypoint = "plugin.wasm"
contributions = ["commands", "bindings"]
capabilities = ["document.edit"]

[wasm]
fuel = 5000000
memory_bytes = 16777216
```

Absent `[wasm]`, these two values are the defaults: 5,000,000 fuel units per
`register`/`invoke` callback and a 16 MiB per-generation store memory limit.
Fuel resets before every callback. The host classifies a fuel trap as
`extension-fuel-exhausted`, memory exhaustion as
`extension-memory-exhausted`, an ABI/linker mismatch as
`extension-abi-mismatch`, and an execution trap as `extension-trap`.
Guest `result<_, string>` failures and invalid returned data use
`extension-runtime-error`.

The adapter also bounds response amplification independently of linear memory:
4,096 WIT nodes, 64 path segments, 1 MiB decoded string data, 128
registrations, 256 actions, 1,024 selections, and 4,096 edits. An over-limit
response is `extension-response-limit` before semantic action interpretation.
See [the isolation policy](ISOLATION.md) and ADR 0028 for the full store and
conversion limits.

Fuel bounds guest instructions but callbacks are synchronous. M9 does not yet
offer a separate worker, an epoch/wall-clock deadline, cancellation of native
compilation, or a promise that a malicious native process cannot starve the
host outside this runtime. The resource policy is a Component guest boundary,
not a universal liveness guarantee. After fuel exhaustion, memory failure, or
a trap, M10 marks the active Component runtime `unavailable`; later callbacks
return `extension-runtime-unavailable` without guest entry. Ordinary editor
input remains usable and explicit plugin reload stages a fresh, healthy
Component generation.

## WIT ABI

The authoritative Extension API metadata is `zenbu.extension.Contract`.
`Contract.wit ()` renders the checked WIT representation committed at
[`wit/zenbu-plugin.wit`](wit/zenbu-plugin.wit); `make extension-docs` refreshes
it and the contract test rejects drift. The world is:

```wit
package zenbu:plugin@1.0.0;
world extension { export control; }
```

The exported `zenbu:plugin/control@1.0.0` interface provides:

- `register: func() -> result<list<registration>, string>`;
- `invoke: func(invocation: invocation) -> result<value, string>`.

`registration` describes one existing v1 contribution. Its `callback` is an
opaque Component-local token. `invoke` receives that token plus the existing
Extension_host request record: kind, operation, a capability-projected copied
context, and data-only arguments. The Component cannot obtain a document,
history, terminal, parser pointer, callback closure, or runtime handle.

WIT does not allow a recursive `value` alias, so `value` is a list of typed
nodes. Each node has a path of `field(name)`/`item(index)` segments and a kind
(`nil`, `boolean`, `integer`, `floating`, `text`, `items`, or `fields`). It is
a canonical pre-order tree encoding. The host rejects duplicate paths, missing
roots, noncontiguous list paths, record item paths, and unreachable nodes. This
is structured Component ABI data—not JSON and not a raw pointer/linear-memory
convention.

The host validates export names and Component function types when it stages and
calls the required exports. Cargo Component output includes the expected
Component custom sections; Zenbu relies on Wasmtime's Component validator and
typed calls, rather than treating an untrusted custom section as authority.

## First-party Rust guest SDK and package tool

[`sdk/wasm-component`](../sdk/wasm-component) is the supported source SDK for
Component guests. Version 1.0.0 pins `zenbu:plugin@1.0.0`,
`wit-bindgen = 0.41.0`, `cargo-component = 0.21.1`, Rust 1.97.1, and the
`wasm32-unknown-unknown`/`wasm32-wasip1` targets. Every package carries an exact `Cargo.lock`,
a local WIT snapshot, and a copied `zenbu_sdk.rs` helper. The helper builds the
typed WIT value tree and registrations, while each guest continues to generate
its own final Component exports with `wit-bindgen`.

Install the exact packager once:

```sh
cargo install cargo-component --version 0.21.1 --locked
```

Then create, build, and stage-check a package from a Zenbu checkout:

```sh
./scripts/zenbu-component-package.sh new /path/to/my-component
./scripts/zenbu-component-package.sh build /path/to/my-component
./scripts/zenbu-component-package.sh check /path/to/my-component
```

`build` uses a temporary target directory, atomically replaces `plugin.wasm`,
and runs the ordinary `plugin-check` staging path. It rejects a missing lock,
stale SDK/WIT source, unpinned binding version, wrong Component world, or a
different Cargo Component version before the binary can become active.
`plugin-check` then validates the manifest, declared capabilities, Component
exports and function types, imports, and returned registrations against the
actual host. `check` repeats that validation for an existing artifact without
rebuilding it.

[`examples/wasm-component-hello`](../examples/wasm-component-hello) is the
small guest. [`wasm-component-conformance`](../examples/wasm-component-conformance)
uses the SDK and exercises every permitted v1 capability; its rebuilt binary is
the fixture executed by the normal M9 conformance suite. That suite also stages
the fixture with authority withheld and proves host-side capability denial. The
separate [`wasm-component-unauthorized-import`](../examples/wasm-component-unauthorized-import)
fixture proves that an added WIT import cannot manufacture host authority.

Rust/Cargo Component remains optional for ordinary local Zenbu builds because
the checked-in M9 fixture is already runnable. Linux CI installs the pinned
toolchain, rebuilds both first-party guests, confirms the conformance fixture
checksum, and runs that normal suite. `make extension-docs` refreshes the
generated WIT source and every SDK/example snapshot; `make check` rejects
snapshot drift without requiring Rust.

## Semantic behavior and diagnostics

Component commands, selectors, transformations, bindings, and hooks are
registered through the same `Extension_host`, command registry, semantic
behavior registry, and session resolver as Lua packages. A Component result
cannot apply text directly: inserts/replacements/selections/semantic operations
become ordinary model effects and are subject to all existing transaction,
selection, UTF-8, history, syntax, undo/redo, and provenance rules.

`plugin-check`, `plugin-describe`, `plugins`, and the interactive `Plugins`
view show the runtime and effective limits. `why` and `Trace` add structured
`Extension_runtime` events for successful Component compile, instantiate,
register, and call stages; call events include fuel consumed and failed calls
include the runtime error text. Bounded profiling records matching
`extension.wasm.*` CPU-time samples. Provider ID/version/runtime continue to
appear in bindings, descriptors, history provenance, and generic extension
callback events.

The conformance suite runs the same semantic assertions against paired Lua and
Component packages, then adds Component-only trap, loop, memory, malformed
binary/WASI/import, output-limit, reload, mixed-runtime, replay-after-unload,
and 200-generation stress coverage. See the
[M9 pressure test](M9_PRESSURE_TEST.md). It does not assert an OS-process
sandbox, as there is no guest process.
