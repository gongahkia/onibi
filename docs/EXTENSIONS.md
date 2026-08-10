# M8 extensions

M8 is Zenbu's first stable third-party extension contract. It builds on M7's
data-only callback experiment without adding an editor-mutation escape hatch.
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

The sole v1 runtime is `lua-trusted`. It uses PUC Lua standard libraries, so a
plugin must be treated as trusted local code. M8 constrains *Zenbu API*
authority; it is not a sandbox for filesystem, process, network, memory, CPU,
or native-library access. Runtime isolation is explicitly M9 work.

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
```

`manifest_version` and `api` must both be supported v1 values. Plugin IDs have
at least two lowercase dot-separated segments; each segment can contain
lowercase letters, digits, and interior hyphens. Versions use supported SemVer
core `MAJOR.MINOR.PATCH` form. `entrypoint` must be a regular relative file
that resolves within its package, so absolute paths and `..` traversal fail
before runtime evaluation. Manifest contribution and capability arrays are
nonempty-string arrays with no duplicates. Unknown capability/contribution
names are structured validation errors.

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

## Lifecycle and atomicity

Discovery parses and validates manifests first, then stages each package in a
fresh runtime. A plugin's commands, semantic descriptors, bindings, and hooks
are activated as one immutable snapshot or none are. Duplicate IDs and binding
collisions are checked across all staged plugins and the existing configuration
overlay; two plugins that collide with each other both fail activation. Failure
of one unrelated plugin does not disable independently valid plugins.

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
```

`plugin-check` fully stages the package entrypoint without retaining it and
prints state, manifest path, declared capabilities/contributions, registered
IDs, and structured errors. `plugins` lists all discovered packages. The
interactive `Plugins` inspector presents the same status and latest retained
reload failure. [`examples/plugins/surround`](../examples/plugins/surround)
is an executable v1 package. `sdk/lua/zenbu.lua` is a generated Lua-language
stub; it is intentionally a small editor-assistance SDK, not a second runtime.

`make extension-docs` deterministically regenerates the committed reference
and SDK from `zenbu.extension.Contract`. The M8 test suite checks that the
committed outputs still equal the contract.

## Observability and compatibility

Providers retain plugin ID, semantic version, runtime, and manifest source.
`why` reports extension lifecycle/callback events and capability denial;
bindings, commands, descriptors, history provenance, and the Plugins view use
the same provider data. A plugin command's committed history chain therefore
shows e.g. `com.example.surround@1.0.0 (lua-trusted)`. Bounded profiling adds
`extension.load`, `extension.reload`, `extension.command`,
`extension.selector`, `extension.transformation`, and `extension.event`.

V1 compatibility means Zenbu retains required v1 manifest behavior,
contribution/capability names, service behavior, and stable error names. It may
add optional v1 fields/services. Removing or changing required v1 behavior
requires an API-version increment. Existing M7 configuration remains supported
as experimental trusted local configuration, but it is not a plugin package
and makes no stable-plugin compatibility claim.

M8 explicitly defers dependency resolution, signatures, permissions UI,
per-plugin enablement persistence, resource limits, sandboxing, asynchronous
services, language-grammar packages, marketplace distribution, and a second
runtime. See [roadmap](ROADMAP.md) and ADRs 0022-0025.
