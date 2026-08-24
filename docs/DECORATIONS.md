# Bounded display decorations

`zenbu.view.Decoration` is a data-only protocol for an embedding host to add
presentation contributions to a document snapshot. A provider submits a
`result`: either one immutable contribution or a concise failure string.
`Session.set_view_decorations` retains those results at the host layer; editing
models, Lua configuration, Components, the renderer, and the terminal do not
receive a callback, terminal handle, event loop, or input target.

Every contribution names its provider, priority, document id, and document
version, then contains one or more items:

- `Inline` uses an anchored source byte to select one visible source line and
  appends a labelled trailing annotation to that line.
- `Virtual_line` inserts one labelled row immediately before or after its
  anchored visible source line.

The renderer accepts contributions only for the exact current id/version. A
provider error, duplicate provider result, stale snapshot, invalid UTF-8
anchor, invalid text, or size-limit rejection is omitted from the frame. The
active-session `Decorations` inspector reports accepted provider/item counts
and every rejection. No rejected contribution changes document contents,
selections, history, model state, or the remaining contributions.

## Bounds and order

Provider ids are valid non-control UTF-8 values of at most 96 bytes; priorities
are in `[-1024, 1024]`. A contribution has at most 64 items, a session accepts
at most 32 providers and 256 items, each text payload is at most 512 bytes,
and all accepted payloads total at most 16 KiB. Text must be nonempty UTF-8
without line breaks; anchors must be source UTF-8 boundaries.

Accepted items are ordered by increasing priority, provider id, then declared
item order. The projection keeps that order within each before/inline/after
placement. A virtual row is an ordinary display row for scrolling and paging;
it has no source pointer target. A source row after virtual rows maps to its
original byte coordinates. Inline text has no source bytes, so pointer columns
in its trailing annotation map to the source line end. Cursor and selections
remain source-based. Selection, search, diagnostic, and syntax precedence
continues to apply only to source graphemes; decoration cells use their own
semantic roles.

Folding composes before decorations: entries attached to a hidden source line
are omitted, while entries attached to a visible fold header remain visible.
The physical cursor never moves to a virtual row. Contributions are not
persisted in workspace layouts and are never rebased; a provider must issue a
new snapshot contribution after a version change.

Themes expose `decoration_inline` and `decoration_virtual`. Both rendered forms
also include textual `[provider: annotation]` or `[provider] text` labels, so
their meaning does not depend on colour or italic/underline support.

This does not provide arbitrary drawing, inline insertion at arbitrary display
columns, GUI widgets, renderer callbacks, provider-owned pointer events, or a
provider execution API.
