# ADR 0018: model-owned input introspection

## Context

Models have multi-step and text-input grammars that cannot truthfully be forced
into a universal flat keymap.

## Decision

Extend `Editing_model.S` with state-specific `Input_rule` values. Rules describe
exact/named/range/text patterns, binding/prefix/catch-all class, semantic ids
where known, next status, and syntax requirement. Models own these values; the
runtime and inspector do not inspect opaque model state.

## Consequences

Bindings are discoverable for first-party and future third-party models without
model-specific inspector branches. Enumeration remains intentionally partial
where a grammar is dynamic or committed text is open-ended.
