# Shack architecture

Go with CGO disabled keeps builds and distribution independent of C toolchains
and separately installed runtimes. This is a distribution and maintenance choice,
not evidence that Go is faster than another language.

`internal/text` owns a persistent, newline-indexed piece tree. Nodes also flag
non-ASCII/control bytes so printable ASCII positioning can avoid scanning large
prefixes. Unchanged branches are shared. UTF-8 edits validate against one source
version. Undo retains 256 groups, with bounded typing coalescing and inverse edit
sequences for rebasing other pane selections. Staging clones share immutable text
while isolating mutable history. Selection movement does not copy the document.

`internal/editor` owns the single event loop, buffers, panes, commands, safe file
operations, previews and background results. tcell supplies terminal I/O; Chroma
supplies bundled lexers. Ordinary rendering updates the logical screen and lets
tcell emit changed cells; it does not request a physical full-screen clear.

Pane layout is a bounded binary split tree with per-divider ratios; session
restoration prunes missing leaves and preserves nested orientation. The file tree
is built from the existing bounded project index, never from per-frame filesystem
walks. The optional minimap samples short line prefixes into cached immutable
rows on a coalescing worker; neither syntax lexing nor a full-file copy is needed.

`internal/terminal` owns each explicitly started PTY and terminal emulator. An
actor serializes emulation, input, resize and scrollback, publishing immutable
frames through an atomic pointer. Separate goroutines read/write the PTY. The UI
never waits on terminal I/O or acquires an emulator lock. Queues, dimensions,
scrollback cells and update frequency are capped; no terminal resources are
allocated until requested. Closing cancels blocked I/O and signals the shell and
foreground process groups. Only the active view controls a shared PTY's size.

Workspace previews retain bounded document snapshots. Every included target is
validated and staged before publishing any change to live buffers. Version and
disk-fingerprint checks reject stale edits. The latest workspace transaction has
guarded group undo/redo. File saves remain individually atomic, not a multi-file
filesystem transaction. Reports are read-only transient buffers without edit history.

`internal/project` discovers up to 10,000 files using Git's native ignore semantics
when available, or a standalone directory-exclusion walker. Canonical paths prevent
symlinked roots from duplicating an already-open file. Search runs off the UI thread
with limits on input file size, retained bytes and matches. Cancellation and request
identity prevent outdated search results from replacing current results.

`internal/lsp` owns framed JSON-RPC, subprocess lifecycle and UTF-16 conversion.
One process is shared per root/server command, up to four combinations. Documents
carry session identities as well as versions, so delayed sync/close work cannot
replace a newly reopened document. UI results check source versions and positions.
Workspace refactoring enters the same preview path as project replacement.
Unsupported resource operations/server commands fail without partial application.

`internal/recovery` stores bounded, private, atomically replaced snapshots with
live-session locks and monotonically sequenced writes. Recovery retains original
fingerprints, protecting later saves against outside changes. Project session
metadata is stored separately and never substitutes for dirty-buffer recovery.

`internal/process` runs explicit noninteractive tasks with bounded output,
process-group cancellation and throttled updates. The same boundary supports
optional Git commands. Tasks do not run on project open. No new runtime dependency
is required for ordinary editing, recovery, splits, search, or configuration.

Large-file mode bounds optional services. The editor does not claim constant-memory
opening, complete Unicode long-line optimization, universal encodings or mature
IDE parity. Original Zenbu remains in Git history and `archive/zenbu-before-shack`.
