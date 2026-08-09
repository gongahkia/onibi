# Editing protocol

## Coordinates and snapshots

M0/M1 positions are UTF-8 **byte offsets** measured from the start of the
stored text. Valid offsets are `0..byte_length` at UTF-8 code-point boundaries.
They are not line/column values, Unicode grapheme indexes, or terminal display
cells. A combining mark is therefore separately addressable; display width is
an M4 concern.

An `Anchor` names a document id, document version, and valid snapshot-local
offset. It is not a persistent marker. When an edit is committed, supplied or
carried selections are deterministically rebased to the new snapshot, but an
old anchor itself remains valid only for its old snapshot.

Ranges are half-open: `[start, stop)`. Empty ranges represent insertion points.

## Transaction construction and commit

Construction validates that edits and an optional selection change agree with
the transaction's source document id/version, that ranges are ordered, and
that replacement text is valid UTF-8. Construction sorts edits by source range
and preserves their declaration ordinal for ties.

Commit validates the transaction source against the current document, then
validates each endpoint against the current buffer. No state is changed until
all validation succeeds. A stale version, wrong document, invalid endpoint,
invalid selection, or edit conflict is an explicit result error.

Two non-empty edits conflict when their half-open ranges overlap. An insertion
conflicts with a non-empty edit only when its point lies strictly inside that
edit; insertions at either endpoint are permitted. Insertions at one point do
not conflict. At a common start position, insertions are emitted in declaration
order before a replacement/deletion starting there. An insertion at an edit's
stop is emitted after that edit. This produces one deterministic result without
relying on in-place mutation order.

If a transaction has no explicit selection change, its input selection set is
carried forward. In either case, selection offsets are rebased with right
affinity: a point at or inside an edited range maps to the end of replacement
text; points after it are shifted by its byte-length delta. This makes a caret
used for an insertion land after inserted text. Rebased selections are
normalized and checked again.

## Semantic layer and history

M1 intents are `insert text`, `delete selected ranges`, `replace selected
ranges`, and `set selections`. They resolve against a snapshot into ordinary
transactions; raw keyboard events never appear in history. A history change
has a monotonically allocated change id, transaction metadata (source, intent
identity, description), and before/after versions. History retains child nodes,
allowing branches after undo. Redo chooses the latest child by default or a
specified child id.

Replay fixtures use the inspectable `zenbu-replay-v1` line format. Every text
field is `String.escaped` and each action is either an intent or a fully
specified transaction. Replay runs from supplied contents and selections,
records the first failing zero-based action index, and is deterministic for the
same input.

## M2/M3 selector/transformation composition

M2 preserves the M1 intent variants and adds a model-neutral `Apply` intent:

```text
selector + transformation -> semantic intent -> transaction
```

M3 extends the selector vocabulary after real-model pressure testing. The
primitive selectors are `current-selections`, `document`, `next-text-unit`,
`previous-text-unit`, `next-word`, `previous-word`, `word-end`, `current-word`,
`around-word`, `current-line`, `line-start`, `line-end`, `first-nonblank`,
`document-start`, `document-end`, `next-line`, `previous-line`, and
`all-occurrences`.

A text unit is exactly one UTF-8 code point beginning or ending at each
selection head; it is not a grapheme cluster or terminal cell. `next` and
`previous` reject a selection at the corresponding document boundary.

M3 word classes are deterministic and intentionally simpler than Vim's
`word`/`WORD` rules. Space, tab, carriage return, and newline are whitespace.
ASCII letters, digits, `_`, and every non-ASCII UTF-8 code point are word
characters. Other code points are punctuation. `current-word` selects one
maximal non-whitespace class run. `next-word` spans the current class and any
following whitespace, stopping before the next class; `previous-word` spans
back to the preceding class start; `word-end` spans to the current or next
non-whitespace class end. `around-word` adds trailing whitespace. A selector
on an unavailable boundary returns an explicit error rather than clipping.

`current-line` selects the full physical line and includes its terminating
newline when present. `line-end` stops before a newline. The final line needs
no newline, and a trailing newline does not create a navigable virtual line.
Vertical selectors preserve a Unicode-scalar column within the adjacent line,
clamped to its length; they do not retain a desired display column across
several vertical moves. `all-occurrences` selects all non-overlapping literal,
case-sensitive occurrences of the non-empty primary selection.

The transformations are `select`, `delete`, `replace-text`,
`collapse-to-start`, and `collapse-to-end`. The collapse transformations turn a
selector's target regions into carets, allowing navigation without making
"motion" a kernel concept. A selection transformation changes the selection
set without text edits. Delete and replace first resolve the selector, then use
those target ranges for both edits and post-transaction selection rebasing. The
same selector and transformation values therefore compose independently of the
model that requested them.

Semantic replay serializes `Apply` as a selector/transformation operation. It
does not record logical input events or model states.

## M5 syntax snapshots and structural selection

`zenbu.syntax` is not a kernel selector vocabulary. It is an optional,
model-neutral service that parses one immutable `Document_snapshot` into a
`Syntax.Snapshot` for exactly the same document id and version. Syntax nodes
are opaque, version-bound references exposing a grammar-defined textual kind,
byte range, named/error/missing flags, named traversal, and no parser pointer.
`Snapshot.matches_document` is the explicit identity check; the model runtime
only puts a matching syntax snapshot into `Editor_context`.

A syntax node converts to an ordinary kernel `Range` through the source
document snapshot. That validates the same document/version and UTF-8
code-point boundaries used by every other selection. Structural models then
convert node ranges into ordinary `set-selections` intents; the kernel neither
knows nor needs to know that those offsets came from syntax.

`Syntax.Selector` deliberately names editing operations rather than OCaml
grammar rules: focus primary, containing node, parent, first child, next or
previous named sibling, expand, and same-kind named siblings. Same-kind selects
siblings under the current parent, giving deterministic non-overlapping
multi-selection. A selector has no raw Tree-sitter query string in its public
contract.

The service receives committed transactions after the normal document commit.
For a cached predecessor it copies the backend tree, derives Tree-sitter edit
byte/point data from the existing transaction edits in reverse source order,
and incrementally reparses the resulting source. A selection-only transaction
copies the tree without parsing. If there is no matching cached predecessor,
including undo/redo, it fully reparses. Concrete syntax selections resolve to
ordinary transactions and therefore retain M1 deterministic replay; M5 does
not attempt future semantic re-evaluation of a structural selector during
replay.

## M3 clipboard, history, and repeat effects

The model runtime owns immutable model-neutral clipboard slots. A slot contains
validated UTF-8 text and either `characterwise` or `linewise` shape. Copying a
selector stores text without mutating document history. Characterwise paste
inserts before a selected range start, after its end, or replaces it. Linewise
paste finds each current physical line and inserts before its start or after
its terminating boundary. First-party Vim-style `"a` syntax is only a grammar
for choosing a slot; the stored slot itself is shared editor semantic state,
also available to the selection-first model.

`undo` and `redo` are declarative runtime effects that navigate the existing
immutable history tree. They do not create private model histories. The runtime
also remembers the latest non-empty batch of textual semantic intents requested
through direct intent execution or a command. `repeat-last-edit` reapplies that
semantic batch, not literal input. M3 intentionally groups each committed text
input as one transaction, so dot repeat of an insert session repeats its latest
committed text input rather than the entire session. Paste is not repeatable in
M3 because its target placement is context-sensitive.
