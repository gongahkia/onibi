# M8/M9 extensions

M8 is Zenbu's first stable third-party extension contract. M9 adds its first
isolated runtime without adding an editor-mutation escape hatch.
The authoritative machine-readable contract is the public
`zenbu.extension.Contract` module; its committed reference is
[generated Extension API v1](generated/EXTENSION_API.md).

## Scope and trust boundary

An extension is a local package discovered from a configured directory. It is
not configuration: `$XDG_CONFIG_HOME/zenbu/init.lua` remains a trusted user
overlay, while plugins are separate package directories under
`$XDG_CONFIG_HOME/zenbu/plugins` by default. The interactive host accepts
`--plugin-dir PATH` to replace that search path and `--no-plugins` to disable
discovery. No project-local discovery, downloading, resolver, lockfile,
marketplace, dependency graph, or network activity exists.

V1 has two runtimes. `lua-trusted` uses PUC Lua standard libraries, so it must
be treated as trusted local code: its capabilities constrain *Zenbu API*
authority, not filesystem/process/network/memory/CPU/native-library authority.
`wasm-component` is M9's isolated Component Model runtime. It has no WASI,
filesystem, network, process, environment, clock, random, stdin, stdout, or
stderr service; it receives only data supplied through the normal extension
request. See [Component authoring](WASM_COMPONENTS.md) and the exact
[isolation/threat model](ISOLATION.md).

## Package format and discovery

Each immediate child directory of a configured plugin root is inspected in
lexical path order for `zenbu-plugin.toml`. The manifest must declare:

```toml
manifest_version = 1

[plugin]
id = "com.example.surround"
name = "Surround"
description = "Optional human-readable text"
version = "1.0.0"
api = 1
runtime = "lua-trusted"
entrypoint = "init.lua"
contributions = ["commands", "selectors", "transformations", "bindings", "events"]
capabilities = ["document.read", "document.edit", "selection.read", "selection.write", "ui.message", "event.subscribe"]

# Optional and used only by runtime = "wasm-component".
[wasm]
fuel = 5000000
memory_bytes = 16777216
```

`manifest_version` and `api` must both be supported v1 values. Plugin IDs have
at least two lowercase dot-separated segments; each segment can contain
lowercase letters, digits, and interior hyphens. Versions use supported SemVer
core `MAJOR.MINOR.PATCH` form. `entrypoint` must be a regular relative file
that resolves within its package, so absolute paths and `..` traversal fail
before runtime evaluation. Manifest contribution and capability arrays contain
nonempty strings with no duplicates; either array may be empty (for example, a
package may intentionally request no host authority). Unknown
capability/contribution names are structured validation errors.

Plugin-owned command, selector, transformation, and binding command IDs must
begin `plugin.id + "."`; for example `com.example.surround.wrap`. This avoids
semantic namespace capture without reserving global string prefixes.

## Contributions and capabilities

Contributions state what the package may register; capabilities state what an
already registered callback may read, return, or subscribe to. They are
deliberately independent. Declaring a capability does not silently create a
command or binding, and declaring a contribution does not grant authority.

V1 contribution classes are `commands`, `selectors`, `transformations`,
`bindings`, and `events`. Registrations outside the manifest declaration fail
the complete plugin stage. `events` additionally require `event.subscribe`.

The stable Component ABI v1 has no command-parameter registration field, so a
`wasm-component` command remains parameterless from the terminal palette in
this release. This is intentional compatibility preservation: adding typed
parameter metadata requires a new Component ABI rather than silently changing
the v1 registration record. Trusted Lua configuration can declare the current
typed parameter kinds; see [Scripting](SCRIPTING.md).

A binding's existing `input` string accepts one to sixteen logical input tokens
separated by one ASCII space, such as `Ctrl-X Ctrl-K`. Tokens support named
keys plus `Ctrl-`, `Shift-`, `Alt-`, and `Meta-` modifiers; `Space`, `Minus`,
`Plus`, `Comma`, `Period`, and `Slash` name text keys that would otherwise be
ambiguous in a sequence. The same grammar is used by trusted Lua and Wasm
Components, so the Component ABI remains unchanged. Bindings may be global,
model-scoped, or model-status-scoped. Identical or prefix-overlapping sequences
in the same scope are rejected during staging; host-reserved controls cannot
appear at any position in an extension sequence.

Trusted Lua configuration and `lua-trusted` plugins may additionally declare
data-only binding layers; see [Scripting](SCRIPTING.md#dynamic-binding-layers).
Layered bindings remain outside the stable Component ABI v1: a Component's
bindings are ordinary unlayered maps and it cannot enable or disable a layer.
Layer IDs are unique across configuration and plugins during staging. Different
Lua layers may safely contain overlapping bindings until activation, where
equal-priority, same-scope prefix overlap is rejected atomically for the
current buffer.

V1 capabilities are:

- `document.read` — copied document metadata and text; enables `zenbu.text`.
- `document.edit` — declarative insert/delete/replace effects subject to the
  normal transaction validator.
- `selection.read` and `selection.write` — copied selections and declarative
  selection replacement.
- `syntax.read` — copied syntax summaries/tree data; enables `zenbu.syntax`.
- `command.invoke` — declarative invocation of an existing command ID.
- `ui.message` — declarative informational messages.
- `event.subscribe` — document-changed and after-save hooks.

A missing required capability returns the stable `capability-denied` error,
including the provider, operation, required capability, and granted list. It
does not mutate the document.

## Runtime-neutral host boundary

`zenbu.model_api.Extension_host` is the generic boundary used by command and
semantic registries. A registry stores an opaque invocation token, provider,
granted capability names, and a data-only request/response decoder; it does
not store Lua values, Lua callbacks, documents, terminal values, Tree-sitter
objects, or closures from the adapter. The Lua adapter privately maps its token
to a PUC Lua callback and converts only `Extension_value` values at the edge.

The host supplies copied `document`, `selections`, `primary`, and `syntax`
data only when the corresponding read capability is granted. Lua callback
results remain declarative commands/effects, selection sets, or edit proposals.
The standard model runtime still resolves semantic operations, validates
anchors/UTF-8/edits, creates transactions, commits history, refreshes syntax,
and provides undo/redo. No extension has a special mutation route.

This is intentionally runtime-neutral: a future adapter implements the same
request/response protocol and keeps its own private callback handles. It must
not add a runtime object to the generic command or semantic behavior registry.

## M9 `wasm-component` runtime

The entrypoint is a compiled WebAssembly Component, not a core Wasm module or
WAT source. Its required ABI is the committed
[`zenbu:plugin@1.0.0` WIT world](wit/zenbu-plugin.wit): it exports
`zenbu:plugin/control@1.0.0` with `register()` and `invoke(invocation)`.
`register` returns typed contribution records; `invoke` receives a callback
token and the capability-projected, copied request envelope and returns typed
declarative values. It imports no Zenbu host interface. A Component that asks
for WASI or any other import fails staging because the linker has no such
implementation.

The WIT `value` representation is a flat typed pre-order tree rather than JSON
or a raw linear-memory convention: every node has a typed field/item path,
kind, and typed primitive payload. The host rejects duplicate, unreachable, or
ill-formed paths before an action/selector/transformation decoder sees them.
This preserves the existing `Extension_value` protocol without making a Wasm
object part of a command or semantic registry. See
[Component authoring](WASM_COMPONENTS.md) for a complete guest contract.

The default Component policy is 5,000,000 fuel units for each `register` or
`invoke` call and a 16 MiB Wasmtime store memory limit. A package can replace
both positive values with its optional `[wasm]` manifest table. The active
Plugins inspector shows the effective limits. Fuel is reset per callback, so a
successful callback cannot borrow budget from the next one. Fuel stops guest
instruction loops; memory growth is constrained by the store limiter. Calls
remain synchronous on the host thread: there is no hard wall-clock cancellation
or background scheduling yet.

The Component conversion path also bounds host response amplification: 4,096
WIT nodes, 64 path segments, 1 MiB decoded string data, 128 registrations, 256
actions, 1,024 selections, and 4,096 edits. An excess is the stable
`extension-response-limit` error and is rejected before semantic action
interpretation. See ADR 0028.

M9 maps Component ABI/linker mismatch, fuel exhaustion, memory exhaustion, and
traps to distinct stable extension errors. A guest `result<_, string>` error or
a malformed declarative response remains an ordinary `extension-runtime-error`.
All failures occur before a transaction commit and leave the current immutable
document/history value unchanged.

M10 additionally exposes runtime health in every plugin view. Fuel exhaustion,
memory exhaustion, and traps transition a Component to `unavailable`; a second
callback reports `extension-runtime-unavailable` without calling Wasmtime.
Reload builds a replacement generation and returns it to `healthy` only when
staging succeeds. Lua-trusted plugins remain `healthy` in this generic view
because they do not have a fatal Component store state.

## Lifecycle and atomicity

Discovery parses and validates manifests first, then stages each package in a
fresh runtime. A plugin's commands, semantic descriptors, bindings, and hooks
are activated as one immutable snapshot or none are. Duplicate IDs and binding
collisions, including same-scope prefix overlaps, are checked across all staged
plugins and the existing configuration overlay; two plugins that collide with
each other both fail activation. This includes a Lua binding-layer ID that is
already owned by configuration or another plugin. Ordinary bindings continue to
reject same-scope prefix overlap at staging; layered-map overlap is instead
checked when a user enables equal-priority layers. Failure of one unrelated
plugin does not disable independently valid plugins.

Reload repeats discovery/staging before replacing a package's active snapshot.
A successful candidate disposes the old private runtime only after its
replacement is ready. A parse, validation, evaluation, registration, or
collision failure retains the previous active plugin at its last known-good
version and reports the new error. Packages no longer discovered are disposed;
`Plugin_host.deactivate` also cleanly disposes one active provider. There are
no user-defined unload hooks in v1.

Session order is deterministic: configuration bindings/hooks are considered
before active plugins, and plugins are ordered by plugin ID. Within one plugin,
registration order is preserved. Save, quit, and reload remain host controls.

## Diagnostics, SDK, and examples

```sh
dune exec bin/zenbu_headless.exe -- plugins [DIRECTORY]
dune exec bin/zenbu_headless.exe -- plugin-check PLUGIN-DIRECTORY
dune exec bin/zenbu_headless.exe -- plugin-describe PLUGIN-DIRECTORY
dune exec bin/zenbu_headless.exe -- extension-api
dune exec bin/zenbu_headless.exe -- extension-sdk
dune exec bin/zenbu_headless.exe -- plugin-session examples/plugins test/fixtures/m8-surround.session
make extension-docs
./scripts/zenbu-component-package.sh build COMPONENT-DIRECTORY
./scripts/zenbu-component-package.sh check COMPONENT-DIRECTORY
```

`plugin-check` fully stages the package entrypoint without retaining it and
prints state, manifest path, declared capabilities/contributions, registered
IDs, effective Component limits, and structured errors. `plugins` lists all
discovered packages. The
interactive `Plugins` inspector presents the same status and latest retained
reload failure. [`examples/plugins/surround`](../examples/plugins/surround)
is an executable v1 package. `sdk/lua/zenbu.lua` is a generated Lua-language
stub; it is intentionally a small editor-assistance SDK, not a second runtime.
`plugin-check PATH` stages `PATH` itself, not sibling packages, so it validates
one package without introducing unrelated directory collisions.

The first-party [Component guest SDK](../sdk/wasm-component) supplies a pinned
WIT snapshot, Rust helper source, starter package, lockfile, and reproducible
build/check tool. Its `build` command runs `plugin-check` after emitting the
Component; its `check` command validates an already-built artifact. It has no
dependency resolver, registry, signing format, or additional authority.

`make extension-docs` deterministically regenerates the committed reference
and SDK sources from `zenbu.extension.Contract`. The M8 test suite checks the
generated outputs; the ordinary check also rejects stale Component SDK WIT or
helper snapshots.

## Observability and compatibility

Providers retain plugin ID, semantic version, runtime, and manifest source.
`why` reports extension lifecycle/callback events, Component
compile/instantiate/register/call telemetry with fuel consumption, and
capability denial;
bindings, commands, descriptors, history provenance, and the Plugins view use
the same provider data. A plugin command's committed history chain therefore
shows e.g. `com.example.surround@1.0.0 (lua-trusted)`. Bounded profiling adds
`extension.load`, `extension.reload`, `extension.command`,
`extension.selector`, `extension.transformation`, `extension.event`, and the
Component-specific `extension.wasm.compile`, `.instantiate`, `.register`, and
`.call` stages.

V1 compatibility means Zenbu retains required v1 manifest behavior,
contribution/capability names, service behavior, and stable error names. It may
add optional v1 fields/services. Removing or changing required v1 behavior
requires an API-version increment. Existing M7 configuration remains supported
as experimental trusted local configuration, but it is not a plugin package
and makes no stable-plugin compatibility claim.

M9 still defers dependency resolution, signatures, permissions UI, per-plugin
enablement persistence, cross-platform Wasmtime distribution, asynchronous
services, hard wall-clock cancellation, language-grammar packages, marketplace
distribution, and richer Component host imports. `lua-trusted` remains
intentionally unsandboxed. See [roadmap](ROADMAP.md),
[the M9 pressure test](M9_PRESSURE_TEST.md), and ADRs 0022-0028.
