# ADR 0003: snapshot-local byte anchors

## Context

M0/M1 needs versioned positions without falsely claiming complete Unicode
layout or persistent marker semantics.

## Decision

Anchors contain document id, document version, and a UTF-8 byte offset on a
code-point boundary. They are valid only for that snapshot. Transaction commit
rebases carried/supplied selections with documented right affinity.

## Alternatives considered

- Line/column pairs: ambiguous under Unicode and costly to maintain.
- Persistent anchors now: requires edit-transform policy and storage machinery
  beyond this milestone.

## Consequences

The coordinate system is exact and testable, while grapheme, display-cell, and
long-lived-anchor semantics remain explicit future work.

