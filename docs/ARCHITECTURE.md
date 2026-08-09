# Architecture

Zenbu M0/M1 is a functional semantic editing kernel. The central transition is
conceptually:

```text
old document + transaction -> new document
```

`Document.t` is immutable and owns a document id, version, text buffer, and
selection set. `Document.snapshot` creates an immutable
`Document_snapshot.t`, which is the only source of anchors and ranges. A
snapshot is identified by its document id and version and is safe to retain for
selectors, tests, and future background services.

`Text_buffer.t` is abstract. M0 uses a correct UTF-8 string implementation,
but no document-facing API reveals that choice. Future rope or piece-table
work belongs behind that abstraction.

Intent resolution is separate from mutation:

```text
semantic Intent + snapshot -> validated Transaction
Transaction + current matching document -> new document
```

Transactions, not callbacks or mutable editor state, are the only mutation
mechanism. The kernel validates the source document/version, edit ranges,
conflicts, UTF-8 text, and selections before constructing a new document. A
failed application returns an explicit error and leaves the old value intact.

`History.t` stores immutable nodes containing a transaction, semantic metadata,
and before/after documents. Nodes retain children, so committing after undo
creates a branch instead of discarding future history. `Replay` serializes
headless intents and transaction specifications, then reports the index of the
first failing action.

## Extension boundary

> First-party editing models and first-party plugins must eventually use only
> the same public editing APIs available to third parties. No editing model
> receives privileged access to editor mutation.

There is no `Editor` object with a public mutable escape hatch. M2 will define
an editing-model/state-machine API that resolves input into the M1 `Intent` and
`Transaction` interfaces. M0/M1 does not decide whether commands are
operator-motion, selection-action, structural, or something else.

