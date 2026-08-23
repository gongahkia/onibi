# ADR 0029: host interactions remain above model grammars

## Status

Accepted for M10.

## Context

Search, command discovery, save-as, help, paste handling, and live model
switching are necessary terminal-editor workflows. Encoding each one in every
editing model would duplicate grammar policy and make a future model less
capable by default. Putting them in the kernel would give the kernel terminal
and filename concerns that do not belong to semantic editing.

## Decision

`zenbu.app.Session` owns these interactions. Literal search stores host data,
including the prompt's pre-search selection, and moves through normal
`set-selections` effects; prompt cancellation restores that selection. The
palette joins static `zenbu.app` command descriptors with the ordinary active
command registry and invokes the normal command effect where applicable.
Save-as uses the established atomic file writer. Help projects `Model_status` and
`Input_rule` metadata. Terminal paste is decoded into the existing public
committed-text event only when generic text-entry status permits it.

Live model switching transfers only `Model_runtime.shared_state`: history,
clipboard, extensions/semantic registries, optional syntax service,
observability handles, and repeatable semantic intents. The target model is
initialized from current public context; its private parser/grammar state is
discarded. Syntax highlighting likewise consumes public snapshot spans and
passes view classes, never a parser object, into presentation.

## Consequences

All first-party models receive the same host capabilities without a new
mutation path. Search/palette actions retain normal provenance and history
semantics. Host shortcuts intentionally outrank model/config bindings. M10 did
not add a regex language, command argument UI, project search, external-file
conflict policy, or a terminal backend API to `zenbu.model_api`.

## Amendment: typed command arguments

The host now reads `Command_descriptor` parameter metadata for palette-selected
commands and bindings. It collects each declared text, built-in selector, or
built-in transformation value in a host-owned prompt, then produces the
existing typed `Command_invocation`. Required values are validated before the
handler runs and `Escape` cancels the whole invocation. This adds no command
language, raw terminal access, or mutation path to an editing model or
extension. Component ABI v1 remains parameterless because its registration
record is stable; a future ABI version must add that metadata explicitly.
