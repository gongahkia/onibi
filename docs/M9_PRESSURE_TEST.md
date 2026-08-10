# M9 Isolation / Extension Contract Pressure Test

M9 asks whether Extension API v1 was genuinely runtime-neutral. The answer is
**yes**, with one explicit Component Model adaptation that does not change v1
semantics.

## Result

The same `assert_runtime_neutral_conformance` test routine runs a trusted Lua
package and a Wasmtime Component package with identical assertions. Both stage
through `Plugin_host`, resolve bindings, run a command, compose a selector and
transformation, deliver an event, create ordinary history/provenance, appear in
trace/inspection, produce generic extension profile samples, and support
undo/redo. Their resulting document is `done`; only provider runtime identity
differs.

The WIT Component contract remains Extension API **v1**. No command, selector,
transformation, binding, event, transaction, history, provenance, capability,
inspection, replay, or kernel API was redesigned for Wasm. The kernel and all
editing models have no Wasmtime or WIT dependency.

## WIT mapping

`Extension_value` is recursively data-only, while WIT does not permit a
recursive alias. M9 represents it as a typed pre-order list of nodes with
field/item paths. This is the only awkward value shape found. It remains typed
Component data—not JSON strings or a raw linear-memory protocol.

`zenbu.extension.Contract` remains the source of truth. Its `Contract.wit ()`
renderer generates `docs/wit/zenbu-plugin.wit`; the contract test compares the
committed WIT output with that renderer. The generated API reference and Lua
editor stub remain derived from the same contract module. This prevents v1
metadata and the Component representation from silently diverging.

The Component exports `register()` and `invoke(invocation)`. V1's existing
model is host-push: copied context enters an invocation and the guest returns a
declarative response. Accordingly M9 exposes no Component host imports. That
is stronger than pre-linking every service and checking a flag later; a
Component attempting a `document-read` import is rejected at staging. The
complete capability mapping is in [ISOLATION.md](ISOLATION.md).

## What held

- `Extension_host` needed no Wasmtime or guest-language value. The private
  adapter owns callback tokens and converts only `Extension_value`.
- Provider identity already carried plugin ID, version, runtime, and source,
  so history, bindings, descriptors, `why`, and traces remained generic.
- Staged immutable plugin snapshots already supplied activation, collision,
  unload, successful reload, and failed-reload rollback semantics.
- Semantic replay replays a committed Component transaction after its plugin
  store has been disposed; it does not need the Component binary or runtime.
- A future engine can implement the same adapter behavior without changing
  registries or the kernel. Wasmtime ABI details stay private.

## Isolation evidence

The M9 suite exercises a valid Component alongside the paired Lua package;
malformed values; a valid-then-invalid action list; invalid selections;
response-amplification quotas; guest traps; a fuel loop; memory growth; a
WASI-importing Component; a malformed binary; a Component importing
`zenbu:plugin/document-read@1.0.0`; 200 load/invoke/reload/GC cycles;
successful and failed reload; mixed Lua/Component snapshots; a cross-runtime
command-ID collision; runtime-stage trace/profile observations; and transaction
replay after unload.

The direct C bridge is deliberately small: engine/store/linker/component
construction, typed Component calls, value conversion, fuel/store limits,
metrics, and deterministic teardown. OCaml never receives a naked Wasmtime
pointer. The production world imports no host functions, so Wasmtime never
calls OCaml in M9; no callback roots or runtime-lock transitions are present.
The feasibility spike separately proved typed Component host-import invocation
is available in Wasmtime 47 if a future approved capability service needs it.

## Limits and next design work

M9 is Linux x86_64-only because it pins the official Wasmtime 47.0.3 C API
archive. Guest compilation is separate from ordinary Zenbu builds; committed
base64 fixtures have Rust/Cargo Component source, regeneration targets, and
checksum checks. Calls are synchronous and fuel-bounded but lack hard
wall-clock interruption. V1 intentionally omits filesystem, network, process,
async execution, persistent guest state, marketplace/resolution, signing, and
rich guest-pull host interfaces.

The next small architectural goal should be cross-platform Wasmtime bootstrap
and a supported guest-fixture/toolchain workflow, followed by a separately
designed asynchronous execution boundary. Neither should add a direct editor
mutation path or expand Extension API v1 casually.
