# ADR 0019: keep the PUC Lua adapter private and data-only

## Status

Accepted for M7.

## Context

Zenbu needs an executable trusted-local configuration language to pressure-test
the public command and semantic editing surfaces. Available OCaml Lua bindings
were either incompatible with the target PUC Lua 5.4 ABI or unsuitable for the
required lifetime/control boundary. Exposing a Lua state through the public
API would let later extensions couple to runtime values and bypass the intended
semantic contract.

## Decision

`scripting/lua_backend` uses Ctypes against the system `liblua-5.4.so` and is
private to `zenbu.scripting`. It owns state creation, standard library setup,
callback references, module search path, protected calls, disposal, and
best-effort PUC-Lua source/line diagnostics. The public scripting library
exposes registrations and callbacks only in terms of `Extension_value`,
`Editor_context`, `Model_effect`, and semantic behavior data. All Lua values
are converted at the adapter boundary; functions, userdata, threads, and
unsupported tables do not cross it.

## Consequences

The core, model API, syntax library, renderer, and terminal do not link to or
name Lua values. Lua 5.4 is a host dependency and configuration is intentionally
experimental/trusted local. Replacing the implementation later does not alter
the semantic extension surface, but this ADR does not promise a stable Lua ABI.
