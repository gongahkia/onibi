# ADR 0009: model-neutral navigation selectors

## Context

M3's real modal models needed word, line, document, vertical, and occurrence
targets. The M2 text-unit selectors were sufficient for proof models but could
not express `dw`, `dd`, visible word selection, or literal multi-selection
without duplicating target calculations in each model.

## Decision

Extend kernel `Selector` with deterministic word, line, document, vertical,
and literal-occurrence selectors. Extend `Transformation` with
`collapse-to-start` and `collapse-to-end`, allowing a model to use a selector
for caret navigation without naming a motion in the kernel.

Define word classes and line/newline behavior in the editing protocol. Keep
vertical movement scalar-column based and clamped; desired display columns,
graphemes, and terminal cells remain deferred.

## Alternatives considered

- Implement word and line scans separately inside the Vim-style and
  selection-first models: duplicates semantics and destroys cross-model
  equivalence.
- Add Vim motions/operators to the kernel: makes one grammar foundational.
- Introduce full display geometry now: requires a terminal/layout layer beyond
  M3.

## Consequences

Both models reuse the same target calculations while deciding independently
whether a target moves a caret, becomes a visible selection, is deleted, or is
copied. Selector names describe document semantics, not model grammar.
