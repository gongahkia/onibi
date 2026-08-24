# ADR 0014: Tree-sitter is a private syntax backend

## Context

M5 needs an incremental concrete-syntax parser for a structural editing model,
but parser objects, C lifetime rules, query strings, and grammar-specific node
types are unsuitable public editor semantics.

## Decision

Use OPAM `tree-sitter` 0.1.0 from Mosaic. It provides GC-managed parser/tree
objects, OCaml and JSON grammar packages, named-node traversal, query support,
and the required `Tree.edit` plus `Parser.parse_string ~old` incremental path.
`syntax/Tree_sitter_backend` is a private Dune module. Public callers use only
`zenbu.syntax` language, snapshot, node, selector, and service values.

M5 uses node traversal rather than queries for core structural selection. A
later bounded `Syntax.Query` facade may compile against an error-free,
version-bound snapshot and project only validated capture ranges or normal
kernel selections. It does not expose a Tree-sitter query, cursor, parser, or
tree to a model or plugin.

## Alternatives considered

- Direct C stubs: unnecessary because the binding exposes the needed parser,
  tree copying, edit, incremental parse, grammar, and traversal APIs.
- Full reparsing only: correct but leaves known transaction edit information
  unused and would make later interactive performance work harder to assess.
- Exposing Tree-sitter nodes: couples every model and future plugin to one FFI
  library and its ownership rules.

## Consequences

Tree-sitter can theoretically be replaced for a language without rewriting the
structural model. The package is a new build/runtime dependency and M5 tracks
its exact currently available 0.1.0 release. M10 adds syntax highlighting only
through public snapshot-derived range/classes. The bounded query facade retains
the same private backend boundary, while unbounded or plugin-hosted query
execution, LSP, and arbitrary grammar distribution remain out of scope.
