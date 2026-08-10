# M9 WebAssembly Component extensions

M9 is Zenbu's first isolated extension runtime. It is deliberately an adapter
to Extension API v1, not a second editing kernel: Component callbacks receive
data-only requests and return declarative values; Zenbu alone resolves intents,
validates transactions, commits history, refreshes syntax, and records
provenance.

## Runtime and installation

The host embeds the official Wasmtime 47.0.3 Linux x86_64 C API behind a private
C shim. Run this once in a fresh checkout:

```sh
make wasm-runtime
```

The script downloads the exact official archive into ignored `.zenbu/`, verifies
SHA-256 `aaa3621f2a3d8393696702897f8f78a1cc504437d500701496d560125aefd732`,
and refuses to replace an incomplete existing destination. `make build`,
`make test`, `make check`, and `make demo` depend on it. The current pin is
Linux x86_64 only; a Wasmtime upgrade or a new platform port must repeat the
ADR 0026 vertical spike and M9 conformance suite.

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

Fuel bounds guest instructions but callbacks are synchronous. M9 does not yet
offer a separate worker, an epoch/wall-clock deadline, cancellation of native
compilation, or a promise that a malicious native process cannot starve the
host outside this runtime. The resource policy is a Component guest boundary,
not a universal liveness guarantee.

## WIT ABI

The authoritative ABI is [`wit/zenbu-plugin.wit`](wit/zenbu-plugin.wit):

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

## Minimal Rust guest

[`examples/wasm-component-hello`](../examples/wasm-component-hello) is a
compilable Rust guest using `wit-bindgen` 0.41.0. It registers a command and a
Ctrl-K binding and returns a typed declarative insert action. With Rust, the
`wasm32-unknown-unknown` target, and `cargo-component` installed:

```sh
cd examples/wasm-component-hello
make component
make check
```

`make component` produces `plugin.wasm` beside the manifest. The repository
does not require Rust or Cargo Component for ordinary builds/tests: the M9 test
suite commits actual Component fixture artifacts, including a WASI-importing
fixture that must be rejected.

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

The conformance suite exercises a real Component command, selector, and
transformation; malformed returned trees/actions; guest traps; a fuel loop; a
memory-limit request; absent edit capability; denied WASI imports; malformed
binary rejection; atomic failed reload retention; limits inspection; and normal
provenance/trace/profile behavior. It does not assert an OS-process sandbox,
as there is no guest process.
