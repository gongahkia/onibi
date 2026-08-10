# M6 observability, provenance, and self-documentation

Zenbu's editing grammars are intentionally extensible. M6 establishes the
corresponding explanation surface before scripting or plugins exist. It is
local, structured, model-neutral, bounded where recording can grow, and
separate from deterministic editing semantics.

## Semantic provenance

Committed runtime transactions may retain `Provenance.t`. A chain begins with a
runtime-local execution id, model/provider, and logical input; as available it
adds pending-input interaction id, effect, command/provider, selector,
transformation, and semantic repeat identity. The chain uses stable textual
ids. It does not contain timestamps, a document snapshot, terminal objects, or
Tree-sitter values.

This answers where an edit came from without replaying model logic. Old direct
kernel callers and M1 replay continue to work with absent provenance.

## Trace and `why`

`Trace.enabled ~capacity` owns a FIFO of typed `Trace_event` values. A disabled
trace builds no event values; an enabled trace evicts oldest events in insertion
order. The runtime emits actual semantic-boundary events: input receipt,
model-before/transition, model effect, command invocation, selector/
transformation, transaction creation/commit/rejection, history navigation,
syntax refresh, and expected errors.

Each logical input has a monotonic runtime-local execution id. If a model status
declares pending input, the runtime groups subsequent executions into an
interaction without inspecting a model id. A completed `d w` interaction can
therefore show both transitions, both inputs, the resulting shared selector and
transformation, and transaction provenance.

`Inspector.why` consumes trace events only. It can explain mutations, pending
state, ignored input, and expected failures. It does not contain model-specific
branches. `why-input` is intentionally deferred: M6 offers static current
bindings rather than cloning or mutating opaque model state to speculate.

## Descriptors, discovery, and bindings

Commands retain stable ids and now name a provider. Kernel and syntax selectors
plus transformations use `Semantic_descriptor`; `Semantic_registry` rejects
duplicate ids. `Inspector` turns commands, models, selectors, and
transformations into one description shape with stable id, title, provider,
summary, and fields.

`Input_rule` is the model API's state-specific grammar descriptor. It supports
exact and named input, compact ranges, committed text, bindings, prefixes, and
catch-alls. First-party Vim-style, selection-first, and structural models
dogfood it. A rule may say syntax is required without leaking a syntax backend.

Use the deterministic headless reference surface:

```sh
dune exec bin/zenbu_headless.exe -- commands
dune exec bin/zenbu_headless.exe -- api
dune exec bin/zenbu_headless.exe -- describe selector next-word
dune exec bin/zenbu_headless.exe -- bindings vim
dune exec bin/zenbu_headless.exe -- bindings-session SESSION
```

## History, selections, and syntax

`History.nodes` is a public read-only tree view. `Inspector.history` reports
parent/children/current/saved markers, versions, provenance, edit count, and
deterministically truncated inserted/removed previews. `Inspector.selections`
reports primary status, anchor/head, range, and direction-preserving offsets.

Syntax inspection uses only `zenbu.syntax`: language, version, parse-error
state, current node kind/range, parent kind, child count, and service cache
strategy. Tree-sitter types and pointers remain private.

## Profiling

`Profiler.enabled ~capacity` records bounded CPU-time samples using `Sys.time`.
It measures existing model-handle, transaction-commit, and syntax-update
boundaries; no wall-clock timestamp is retained. Aggregates expose count,
total, mean, and max. Disabled profiling takes no clock reading. This is
lightweight local diagnosis, not telemetry or a metrics platform.

`zenbu-headless profile SESSION` enables a temporary bounded profiler for that
session. The terminal uses `--profile`; its profile API is available through
the same inspector values. No observation data leaves the process or machine.

## Interactive inspector

Run `zenbu --trace FILE`, edit, then press `Ctrl-O` to open the generic Why
overlay. It is an application-level, read-only frame presentation shared by all
models; `Escape` dismisses it and resize follows ordinary rendering. It is not
a command palette, Ex implementation, or model-specific mode.

## Determinism and limits

There are three distinct records:

1. Input trace: logical input grouping used for explanation.
2. Execution trace: typed local observations of processing boundaries.
3. Semantic replay: deterministic intents/transactions from M1.

Only the third reproduces edits. Provenance is deterministic semantic metadata;
trace ordering is deterministic, but profiling duration values are explicitly
non-deterministic observations. Trace and profile storage are bounded. There is
no JSON output in M6 because typed OCaml inspector values are authoritative and
adding a serialization dependency/schema would be disproportionate; headless
text reports are deterministic apart from profile timings.

## M6 API pressure test

M2 command descriptors needed provider identity, while their existing ids,
parameters, examples, and deterministic registry remained useful. M2 status
was insufficient to describe multi-step grammar, so model-owned input rules
were added. M3 effects and M1 history required provenance and a read-only tree
view, not a mutable log. M5 syntax abstraction was sufficient once it exposed
cache strategy; no backend value was needed. Terminal presentation stayed
model-neutral through generic inspector lines.

M7 can use these same descriptor, provider, provenance, and input-rule APIs,
but must still design script lifecycle, hot reload, isolation, and registration
failure behavior. M6 deliberately does not implement scripting or plugins.
