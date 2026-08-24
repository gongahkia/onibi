# ADR 0034: own each Component store on a worker and cancel through Wasmtime epochs

## Status

Accepted for the Linux x86_64 Component runtime.

## Context

Fuel bounds guest instructions and the store limiter bounds linear memory, but
neither gives the terminal a bounded elapsed-time cancellation path. A direct
call also holds the host thread until the guest returns. Wasmtime permits an
engine epoch to be incremented from another thread, but its store must not be
used concurrently.

The solution must preserve the existing Extension API: editing models must not
receive a thread, promise, store, or callback handle; a cancelled, stale, or
failed callback must not partially mutate a document or plugin snapshot.

## Decision

Each active Component generation creates one worker that exclusively loads,
registers, invokes, and disposes its Wasmtime store. It accepts at most 64
queued/running calls. A small watchdog compares a monotonic elapsed deadline to
the active call and increments that generation's engine epoch. The store has an
explicit epoch-deadline callback that returns a bounded interruption error.
Fuel is reset and the local epoch deadline is reset before every guest call.

Commands and event hooks become deferred data-only calls. The worker signals a
private nonblocking pipe when a call becomes terminal; `Session` adds that
descriptor to its ordinary poll set. The generic model effect contains only an
integer completion id. A runtime-local owner token prevents sessions that use
the same document id from draining one another's calls.

Only the session main thread decodes a completion and invokes the normal model
effect/transaction path. It requires the captured document id, version, and
contents to equal the current snapshot. Otherwise it drops the result. Reload,
unload, and session shutdown cancel queued/running calls, interrupt the active
store, join worker/watchdog threads, and discard closed-generation completions.

Selectors, transformations, and model callbacks use the same worker but wait at
the current synchronous semantic boundary. Native Component compilation is also
outside the deferred cancellation surface.

## Consequences

An elapsed callback yields the stable `extension-deadline-exhausted` error and
makes that generation unavailable, like fuel, memory, and trap failures. A
lifecycle cancellation is retained as `extension-cancelled` for the private
boundary but is discarded during teardown, so it cannot poison a healthy
replacement. Terminal input remains available while Component commands/events
run.

This design is implemented and verified on Linux x86_64 against the pinned
Wasmtime 47.0.3 C API. It is not a macOS portability claim; macOS needs its own
implementation/verification pass. It does not add Lua threading, WASI, host
imports, process isolation, or a general-purpose asynchronous model API.
