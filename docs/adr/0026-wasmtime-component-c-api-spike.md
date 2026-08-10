# ADR 0026: use a pinned Wasmtime Component Model C API behind a narrow adapter

## Status

Accepted for M9 after a local feasibility spike on 2026-08-10.

## Context

M8 deliberately exposes a runtime-neutral, data-only extension invocation
boundary but has only the trusted PUC Lua adapter. M9 needs a language-neutral
runtime that can execute a real WebAssembly Component without giving it an
alternate document, terminal, filesystem, process, or history-mutation path.
The old opam `wasmtime` package is version 0.0.3 and documents testing only
against Wasmtime 0.21/0.22; it does not provide a credible Component Model
embedding surface. The available opam `wasmer` package is likewise not an
appropriate Component Model embedding dependency.

Before choosing an integration, this repository tested the official
`wasmtime-v47.0.3-x86_64-linux-c-api` release. A small C program:

- enabled the Component Model and fuel accounting on an engine;
- compiled a Component Model binary from Component WAT;
- created one store with a 1 MiB memory limiter and 10,000 units of fuel;
- linked exactly one typed `host.ping(u32) -> u32` import, with no WASI
  registration;
- instantiated the component and invoked its typed `run(u32) -> u32` export;
- observed the host receiving `41` and the component returning `42`.

The test uses `wasmtime_component_linker_*`,
`wasmtime_component_func_call`, and typed component values, rather than a
raw linear-memory ABI. It establishes that the current C API can provide the
required narrow embedding route.

The spike also found a C API implementation detail that the v47.0.3 header
does not accurately describe: although
`wasmtime_component_func_call` says result values are overwritten, its
implementation drops the existing result slots first. Passing uninitialised C
storage therefore faults. The adapter must initialise every result slot to a
valid empty Component value before every call, and must own/destroy all
Component values according to the pinned headers.

## Decision

M9 uses a small, private C shim against the official Wasmtime 47.0.3 C API.
OCaml reaches that shim through a narrow, data-only interface; it does not bind
the full Wasmtime header set or expose engine/store/component pointers in any
Zenbu public library.

The shim owns the engine, one store/runtime per active plugin generation, a
Component linker, typed Component values, resource cleanup, compilation,
instantiation, export lookup, traps, fuel, and store resource limits. It does
not register WASI, filesystem, network, process, clock, random, environment,
stdin, stdout, or stderr imports. It defines only capability-projected Zenbu
interfaces. Missing imports remain unresolved, so an undeclared capability
fails linking/instantiation rather than becoming ambient authority.

The integration pins the exact Wasmtime C API release and verifies its
availability during setup. A future Wasmtime upgrade requires rerunning the
vertical spike and the M9 conformance, resource-limit, malformed-component,
and failure-isolation tests before changing the pin.

## Consequences

The resulting extension adapter stays inside the existing
`Extension_host` request/response boundary and every guest result still
flows through normal semantic decoding, transaction validation, history,
syntax refresh, provenance, trace, and profiling paths. Wasmtime details and
C callback ownership remain private implementation concerns.

This introduces a native runtime prerequisite and a pinned ABI. It is
intentionally not replaced with the obsolete OCaml bindings, and it is not a
Rust helper process because the direct C API spike proved a sufficiently narrow
embedding is viable. The result-slot initialisation rule is covered by the
adapter's own call wrapper and tests.
