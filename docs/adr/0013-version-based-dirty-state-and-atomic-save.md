# ADR 0013: version-based dirty state and atomic save

## Context

M4 needs a minimal persistence story without giving a model authority over the
filesystem or introducing a mutable editor singleton. Undoing to saved content
should naturally make the session clean.

## Decision

`zenbu.app.Session` stores the document version and source contents last saved
to its current file. A matching version is immediately clean; a later version
is dirty only when its source contents differ. This deliberately ignores
selection-only history transitions, which have no filesystem representation.
`Ctrl-S` is a host command available independently of the selected model. For
an existing file, `File_io.save_atomic` writes an adjacent exclusive temporary
file, fsyncs it, preserves the target's mode when present, then renames it over
the target. At the time of this ADR, an unnamed buffer reported that save-as
was not implemented.

## Alternatives considered

- Let models emit save effects: couples grammar models to paths and filesystem
  policy.
- Overwrite the target in place: risks a truncated file after a write failure.
- Determine dirty state by comparing contents: adds repeated text comparison and
  obscures the history/document state that already identifies a saved point.

## Consequences

Undo or redo to saved source contents is clean, including through selection-only
history changes. A dirty `Ctrl-Q` needs a second `Ctrl-Q`; clean quit exits
immediately. The current save implementation does not fsync the containing
directory, watch files, or resolve external modifications.

## M10 update

M10 supersedes only the save-as deferral: the host prompts for a destination
path and uses the same adjacent-temp-file atomic writer, then records the
active path and saved contents/version. Parent-directory fsync and external
modification detection remain deferred.
