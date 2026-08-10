# ADR 0031: deliver language service output through a version-gated inbox

## Status

Accepted for M11.

## Context

Replies may arrive after a model changed text, selection, or model. Letting a
reader thread mutate session state would race history, model-private grammar,
rendering, and terminal ownership. Displaying an old diagnostic or edit would
be worse than omitting it.

## Decision

Client reader/stderr threads decode into a mutex-protected event queue and
write a nonblocking wakeup pipe. `Session.poll_language` on the main thread
drains and validates events. Requests retain document version/caret offset;
versioned diagnostics map LSP version back to a Zenbu snapshot; stale results
are dropped. Unversioned diagnostics are used only before the first edit.
Server-originated edits are accepted only when every target is the active URI
and an ordinary transaction validates them.

## Consequences

The terminal blocks on stdin plus wakeup without busy polling. Cancellation is
best-effort, but stale-result safety does not depend on server cooperation.
M11 gives up unversioned post-edit diagnostics and cross-file partial edits; a
future workspace buffer table can extend the same version-gated policy.
