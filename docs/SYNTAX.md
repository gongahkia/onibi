# Syntax service

M5 introduces `zenbu.syntax`: a model-neutral, synchronous syntax service that
turns an immutable kernel `Document_snapshot` into an immutable, version-bound
Zenbu `Syntax.Snapshot`.

```text
Document_snapshot
       ↓
Syntax.Service
       ↓
Syntax.Snapshot ──→ opaque nodes / structural selectors
       ↓
optional Editor_context.syntax
       ↓
editing model → ordinary semantic intents → transactions
```

Tree-sitter is the current private backend, not this API's vocabulary. The
public syntax library does not expose a Tree-sitter parser, tree, node, query,
FFI address, or ownership rule. The backend can therefore be replaced without
rewriting a syntax-aware model.

## Languages and activation

`Syntax.Language` has a stable textual id, display name, and extensions. M5
registers:

| id | extensions | grammar |
| --- | --- | --- |
| `ocaml` | `.ml`, `.mli` | OCaml implementation/interface grammar |
| `json` | `.json` | JSON grammar |

The session detects from a path extension, unless `zenbu --language ID` supplies
an explicit registered id. An unknown extension creates no service; Zenbu stays
an ordinary text editor. Parser initialization or parsing failure is also
represented as absent syntax in model context rather than stale or invented
structural ranges.

## Snapshot and node contract

A `Syntax.Snapshot` records the source document id/version, language, parse
tree internally, and whether the root contains error syntax. A caller can use
`Snapshot.matches_document` to make the version relation explicit. The runtime
refreshes synchronously before every model invocation and `Editor_context`
filters any nonmatching snapshot, so a structural selector cannot commit a
range from an old context version.

`Snapshot.Node` is opaque and version-bound. It exposes document identity,
kind through `Syntax.Kind`, named/error/missing flags, start/stop byte offsets,
safe conversion to a kernel `Range`, and editing-useful named traversal:
parent, first/named children, and next/previous named siblings. Node equality
is intentionally not a cross-snapshot promise. A node does not survive a
document mutation; obtain a node from the refreshed snapshot instead.

The range conversion calls `Document_snapshot.range`, so the existing
document/version and UTF-8 code-point-boundary checks remain authoritative.
The kernel stays unaware that a valid range originated in syntax.

## Generic selectors and commands

`Syntax.Selector` resolves from the current primary selection to generic
editing targets: focus/containing node, parent, first child, next/previous
named sibling, expand, and same-kind named siblings. Node kinds are textual
grammar vocabulary at this boundary; Zenbu does not maintain a universal AST
kind enum. Same-kind selection is scoped to the current parent, making the
result deterministic and non-overlapping for the normal selection set.

`zenbu.model_api.Syntax_commands` publishes corresponding stable descriptors:
`syntax.focus`, `syntax.parent`, `syntax.child`, `syntax.next-sibling`,
`syntax.previous-sibling`, `syntax.expand`, and `syntax.select-same-kind`.
They return ordinary `set-selections` intents. They do not encode structural
model keybindings or language-specific commands.

## Parsing lifecycle, incrementality, and cache

Each `Syntax.Service` owns one backend parser and at most one cached current
syntax snapshot. There is no global parser registry or global mutable syntax
state. The public operation is still snapshot oriented:

```text
old snapshot + committed transaction + resulting document snapshot
       ↓
new syntax snapshot
```

For a transaction whose predecessor matches the service cache, M5 copies the
old Tree-sitter tree, translates each source edit to byte/point data, applies
the edits in reverse source order, and invokes incremental parse with that
edited tree as `~old`. Reversing preserves the transaction's simultaneous
source coordinates, including same-position insertions. Selection-only commits
copy the tree without parsing. Without a matching cached predecessor (notably
undo/redo), the service correctly fully reparses the current immutable source.

The binding's nodes retain their parent tree, and the service copies before
editing, so retained public snapshots cannot observe backend tree mutation.
OCaml GC owns backend parser/tree release; M5 creates no manual free path and
retains no syntax history. Tests compare exposed named-tree shape for fresh and
incremental parses across insertion, deletion, replacement, newline/boundary
edits, invalid code, and repair.

## Invalid source and current limits

Incomplete code remains a normal syntax state. Tree-sitter returns a tree with
error/missing information when possible; the service and structural model keep
working with available named structure and never substitute byte heuristics.

M5 intentionally omits highlighting, query strings as public semantics, async
workers, embedded languages, arbitrary grammar downloads, LSP, diagnostics,
and language-specific refactoring. Parsing is synchronous and adequate only for
the tested small/moderate fixtures; M6 observability should measure real editor
latency before introducing a background worker.

`zenbu-headless syntax FILE` is the supported inspection surface. It prints
language/document identity, root error state, and stable named-node metadata,
not backend handles.
