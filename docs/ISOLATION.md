# M9 Component isolation and threat model

`wasm-component` is Zenbu's isolated Extension API v1 runtime. It protects the
semantic editor host from a Component that is buggy or malicious at the
extension-contract level. It is not a claim that a Wasm runtime or the host
process is invulnerable.

## Boundary

One active Component generation owns one private Wasmtime engine, store,
Component linker, compiled Component, and instance. Zenbu creates them during
staging and disposes the old generation only after a replacement stages. No
Wasmtime handle crosses the private `zenbu.extension` adapter boundary.

```text
Component callback
        ↓ typed WIT value
private Wasm adapter
        ↓ Extension_value
Extension_host capability/decode checks
        ↓ Model_effect / Semantic_behavior
Model_runtime
        ↓ validated Transaction
Document + History
```

The Component has no document pointer, mutable editor value, transaction,
history object, Tree-sitter value, terminal handle, OCaml closure, or direct
`Document.apply` path. A returned edit, selection, or action is untrusted data
until the ordinary semantic runtime validates and commits it atomically.

## Capabilities and imports

Extension API v1 is a host-push contract: Zenbu supplies copied context in an
invocation and the guest returns declarative actions. It has no guest-pull host
service interface. Therefore the M9 WIT world intentionally has **no imports**
rather than an omnipotent import guarded by a Boolean.

| v1 capability | Component-visible effect | Host enforcement |
| --- | --- | --- |
| `document.read` | copied id/version/length/text in request context | context projection |
| `selection.read` | copied selections/primary and transformation input | context projection and semantic behavior entry |
| `syntax.read` | copied Zenbu syntax summary | context projection |
| `document.edit` | insert/delete/replace declarative actions | action decoder |
| `selection.write` | declarative selection replacement | action and selector decoder |
| `command.invoke` | declarative command invocation | action decoder |
| `ui.message` | declarative informational message | action decoder |
| `event.subscribe` | document/save hook registration | staging registration check |

An attempt to import `zenbu:plugin/document-read@1.0.0` fails staging even if
the manifest requests another capability: the Component linker has no Zenbu
host implementation at all. This is tested by the committed
`m9_unauthorized_import_component` fixture. Future guest-pull v1-compatible
services, if added, must be separate capability-oriented WIT interfaces and
must be conditionally linked per grant; M9 does not pre-link a global service
surface.

## No ambient host authority

Zenbu registers no WASI implementation and inherits no terminal streams. A
normal Component receives no runtime path to filesystem files or preopened
directories, environment variables, clocks, random, stdin, stdout, stderr,
network sockets, HTTP, process creation, shell execution, native loading, or
terminal/Notty values. A Component importing WASI or any other unresolved
interface fails instantiation.

Tests use both a WASI-importing fixture and the explicit unauthorized Zenbu
import fixture. This is a host-runtime property; it does not protect against a
defect in Wasmtime, the C bridge, the kernel, or the OS.

## Resource policy

The default policy is one 16 MiB store memory limit and 5,000,000 fuel units
for each `register` and `invoke`. A `wasm-component` manifest may supply
positive `wasm.fuel` and `wasm.memory_bytes` values. Fuel is reset before every
callback. The private store limiter also limits table elements to 10,000,
instances to 16, tables to 64, and memories to 64.

Component output has independent host-side quotas:

- 4,096 WIT value nodes, 64 path segments, and 1 MiB total decoded string data;
- 64 fields per Component record and 1 MiB per Component string at the C edge;
- 128 registrations, 256 actions, 1,024 selections, and 4,096 edits per
  callback.

Exceeding a quota is `extension-response-limit`, before any action is
interpreted. Fuel exhaustion is `extension-fuel-exhausted`, memory growth is
`extension-memory-exhausted`, traps are `extension-trap`, and ABI/link errors
are `extension-abi-mismatch`. A guest-declared `result` error or malformed
value remains `extension-runtime-error`.

Fuel bounds guest instructions, including an infinite guest loop. Calls are
synchronous and M9 does not provide an epoch-based hard wall-clock deadline,
asynchronous worker, or cancellation API. Component compilation is also
synchronous. A resource failure rejects that invocation and never commits a
partial transaction. M10 records fatal fuel/memory/trap callback failure as
runtime health `unavailable`; later callbacks return
`extension-runtime-unavailable` before guest entry. Ordinary editor input
remains usable. Explicit reload stages a fresh generation and restores
`healthy` only after successful construction.

## Threat model

M9 defends against a Component that traps, loops, attempts memory growth,
returns malformed or oversized values, proposes invalid ranges or selections,
tries to use undeclared extension authority, or tries to import ambient host
services. It also keeps independently staged plugins active after another
package fails.

M9 does not defend against vulnerabilities in Wasmtime or Zenbu's native
dependencies, malicious native code already loaded into the process, kernel or
OS compromise, side channels, speculative-execution attacks, or denial of
service outside the bounded synchronous guest call. It does not claim formal
memory-leak freedom from lifecycle stress testing.

## Lua and Components

| property | `lua-trusted` | `wasm-component` |
| --- | --- | --- |
| Extension API v1 semantics | yes | yes |
| commands/selectors/transforms/bindings/events | yes | yes |
| manifest capability checks | yes | yes |
| process-memory isolation | no | Wasmtime Component boundary |
| ambient OS access | Lua standard libraries | no WASI by default |
| fuel and store limits | no | per callback/generation |
| intended code source | trusted local packages | third-party local packages |

Lua remains useful for trusted local automation. Its capability declarations
constrain Zenbu services but do not sandbox Lua's standard libraries. Components
are the M9 choice when a package must not receive ambient host authority.
