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
       └──────────→ public highlight spans / terminal view classes
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

`Syntax.Language` has a stable textual id, display name, and extensions. The
default host registry activates these statically linked bundles:

| id | extensions | source | Tree-sitter ABI |
| --- | --- | --- | --- |
| `ocaml` | `.ml`, `.mli` | `tree-sitter.ocaml@0.1.0` | 15 |
| `json` | `.json` | `tree-sitter.json@0.1.0` | 15 |

The session detects from a path extension, unless `zenbu --language ID` supplies
an explicit registered id. An unknown extension creates no service; Zenbu stays
an ordinary text editor. Parser initialization or parsing failure is also
represented as absent syntax in model context rather than stale or invented
structural ranges.

### Host-owned runtime registry

`Syntax.Grammar.Registry` is the only runtime registration surface. A host
stages a *complete* candidate registry, then calls `reload`; validation and a
parser-creation probe finish before the active registry pointer changes. A
failed stage/reload therefore leaves the existing registry and every existing
`Syntax.Service` parser untouched. Existing services intentionally retain their
language snapshot; a host that wants an already-open buffer to use a newly
activated grammar creates a new service at its normal host boundary.

Every candidate declares a stable id, display name, extension map, source
package/revision, artifact version, Tree-sitter ABI, and `sha256:` integrity
attestation. Zenbu validates bounded counts, ids/extensions, duplicate ids,
duplicate extensions, source/version/attestation agreement with the selected
bundle, ABI range 13–15, and the linked grammar's name/symbol/field metadata.
The latter catches accidental bundle/build mismatches before activation. The
integrity token identifies the reviewed, statically linked bundle manifest; it
is not a claim that Zenbu hashes a dynamically loaded native library.

The selectable bundle catalog is closed to the compiled OCaml and JSON
grammars. There is no path, URL, `dlopen`, plugin, Lua, Component, or parser
pointer input. This is intentional: a Tree-sitter grammar is native code, so a
future distribution mechanism must add a separately reviewed package and
build-time link rather than turn a configuration file into code-loading
authority. Grammar downloads, a marketplace, and a general native grammar
loader remain out of scope.

The registry sorts languages by id and rejects ambiguous extension ownership.
Unknown files deterministically have no syntax language and receive plain-text
rendering. `zenbu-headless syntax FILE` reports the selected source, version,
ABI, and integrity attestation alongside the document tree.

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

## M10 presentation spans

`Syntax.Highlight.spans` derives a small stable presentation projection from a
current snapshot: `keyword`, `string`, `number`, `comment`, `type`, and
`constructor` byte ranges. It is deliberately a range/class API, not a
Tree-sitter query or terminal-colour API. The session maps these values into
`zenbu.view` classes; the terminal chooses colours. The renderer applies
selection > search > syntax > plain precedence, so presentation cannot alter
selections or parser state.

OCaml and JSON are covered by the supplied classifiers. Unsupported languages
have no syntax service and therefore no spans. A grammar-specific renderer may
add classifications only by extending this public Zenbu projection, never by
making a Tree-sitter node/query public.

## Parsing lifecycle, incrementality, and cache

Each `Syntax.Service` owns one backend parser and at most one cached current
syntax snapshot. The host-owned grammar registry is global only for *future*
language selection; services do not share parsers or mutable trees. The public
operation is still snapshot oriented:

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

M10 adds bounded synchronous presentation spans. M11 adds a separate optional
language-service layer; diagnostics and LSP edits are Zenbu-owned host data and
never Tree-sitter values. Syntax still omits query strings as public semantics,
async workers, embedded languages, arbitrary grammar downloads, and
language-specific refactoring. A single source snapshot is capped at 8 MiB
before parser invocation, and a registry has at most 32 grammars with at most
16 extensions each. The current binding does not expose Tree-sitter's
progress-callback cancellation API, so Zenbu does not claim a CPU-time or hard
memory sandbox; the source cap is the explicit deterministic parser resource
limit. Parsing is synchronous and adequate only for the tested small/moderate
fixtures; profile real editor latency before introducing a background worker.

`zenbu-headless syntax FILE` is the supported inspection surface. It prints
language provenance/version/ABI/integrity, document identity, root error state,
and stable named-node metadata, not backend handles.

## M6 observability

`Syntax.Service.status` exposes only Zenbu-owned service information: language,
cached document version, and latest strategy (`cached`, `full`, `incremental`,
or `tree-copy`). Runtime traces emit language, document version, strategy, and
error state after refresh. `Inspector.syntax` enriches the primary selection
with node kind/range, named parent kind, and child count. None of these APIs
expose Tree-sitter trees, nodes, pointers, or ownership details.

## M7 data-only scripting view

M7 scripts can call `zenbu.syntax()` for the primary selection or
`zenbu.syntax(start, stop)` for an explicit current-document byte range. The
result is `nil` without a matching syntax snapshot, otherwise copied language,
version, and error state plus a compact smallest-named-node summary and compact
named relatives. It intentionally does not expose `Syntax.Snapshot.Node` or a
Tree-sitter value; a callback can use returned offsets to make another
data-only query. Script descriptor `requires_syntax` participates in generic
discovery but does not cause a parser to be created for an unsupported buffer.
See [scripting](SCRIPTING.md).

## M8 capability-scoped extension view

M8 plugin callbacks use the same data-only syntax representation through
`Extension_host`. A package must declare `syntax.read` before `syntax` data or
`zenbu.syntax` is available; otherwise the host returns a structured
capability-denied error. The copied tree/summary remains Zenbu-owned data,
never a `Syntax.Snapshot.Node` or Tree-sitter object. Plugins can combine its
byte ranges with declarative selectors, transformations, and actions, but all
transaction and version validation remains in the normal runtime. See
[extensions](EXTENSIONS.md).
