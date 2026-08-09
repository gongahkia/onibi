# ADR 0016: structural selection is generic and repeat is concrete

## Context

M5 needs model-neutral structural navigation without embedding OCaml commands
or one model's key grammar in the kernel. It also needs an honest relationship
between syntax-derived selections, deterministic replay, and existing repeat.

## Decision

Expose generic syntax operations—focus/containing, parent, first child,
siblings, expand, and same-kind siblings—through `zenbu.syntax` and stable
generic command descriptors. Grammar node kinds remain textual values owned by
the selected language. The structural model owns its arrow-key grammar and a
small offset-only shrink history.

Once a structural target is resolved, M5 records only ordinary selection and
text intents/transactions. `repeat-last-edit` repeats the last shared textual
intent over current selections; it does not replay a prior structural query.

## Consequences

Another syntax-aware model can consume the same API with different bindings.
Concrete transaction replay stays parser-independent and deterministic. Richer
semantic structural replay remains future work and must define how selectors
are evaluated against changed source before it can be added.
