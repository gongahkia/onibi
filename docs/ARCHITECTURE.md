# Shack architecture

Go with CGO disabled keeps builds and distribution independent of C toolchains
and separately installed runtimes. This is a distribution and maintenance choice,
not evidence that Go is faster than another language.

`internal/text` owns a persistent, newline-indexed piece tree. Unchanged text and
branches are shared across snapshots. Validated UTF-8 edits commit against one
source version. Undo retains at most 256 transactions. Moving selections never
copies the document or creates a history entry.

`internal/editor` owns terminal interaction, buffers, safe file operations,
commands, previews, and background results. tcell provides terminal input/output;
Chroma supplies bundled syntax lexers. Both run inside the executable.

`internal/lsp` owns framed JSON-RPC, subprocess lifecycle, and protocol position
conversion. The event loop applies results after checking source buffer/version.
Missing or failing servers must not prevent editing.

Large-file mode bounds optional services. The initial editor does not claim
constant-memory opening, universal encodings, or full IDE parity. The original
Zenbu implementation remains in Git history and `archive/zenbu-before-shack`.
