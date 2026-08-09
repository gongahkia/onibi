# ADR 0002: text storage is abstract

## Context

The initial kernel needs correct Unicode boundary handling but does not need an
industrial rope. Storage choices must remain replaceable.

## Decision

Expose `Text_buffer` as an abstract type. M0 implements it with validated
UTF-8 strings and byte-offset operations.

## Alternatives considered

- Expose strings throughout the document API: makes later replacement costly.
- Introduce a rope immediately: adds complexity before performance evidence.

## Consequences

The simple representation is localized. Callers can read contents but cannot
depend on a concrete storage structure.

