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

## M2 selector/transformation composition

M2 preserves the M1 intent variants and adds a model-neutral `Apply` intent:

```text
selector + transformation -> semantic intent -> transaction
```

The primitive selectors are `current-selections`, `document`,
`next-text-unit`, and `previous-text-unit`. A text unit is exactly one UTF-8
code point beginning or ending at each selection head; it is not a word,
grapheme cluster, terminal cell, or Vim motion. `next` and `previous` reject a
selection at the corresponding document boundary.

The initial transformations are `select`, `delete`, and `replace-text`. A
selection transformation changes the selection set without text edits. Delete
and replace first resolve the selector, then use those target ranges for both
edits and post-transaction selection rebasing. The same selector and
transformation values therefore compose independently of the model that
requested them.

Semantic replay serializes `Apply` as a selector/transformation operation. It
does not record logical input events or model states.
