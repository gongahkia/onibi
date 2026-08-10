# M11 language services

M11 adds optional asynchronous language intelligence without changing Zenbu's
editing contract. `zenbu.kernel` and `zenbu.model_api` remain free of LSP,
JSON-RPC, subprocess, URI, and protocol-position types. Language output is
Zenbu-owned data, then ordinary semantic effects and transactions decide
whether it can change the active document.

```text
saved buffer path + Language.Registry
        ↓
zenbu.lsp private process adapter ── LSP / JSON-RPC / stdio ── language server
        ↓ bounded event inbox + wakeup fd
zenbu.app.Session (main thread)
        ↓ owned diagnostics / hover / locations / edits
model runtime → validated transaction → history / provenance / syntax refresh
        ↓
zenbu.view ranges and status → terminal colours
```

The default registry starts `ocamllsp` for `.ml` and `.mli` files. It advertises
the `ocaml` language id and discovers a workspace by walking upward to the
nearest `dune-project`, `dune-workspace`, or `.git` marker. No server is
auto-installed, downloaded, or run through a shell. `make bootstrap` installs
the test dependency `ocaml-lsp-server`; an invocation still reports an
actionable unavailable status if `ocamllsp` is absent from `PATH`.
Workspace discovery is a bounded, mutex-protected cache keyed by starting
directory and configured markers; M11 intentionally has no watcher to
invalidate it during a session.

An embedding host may construct `Language.Server_config` values and register
them in `Language.Registry`. A configuration has an executable, argv vector,
explicit environment overrides, filename extensions/language ids, root markers,
and data-only initialization/options settings. Registry selection is
deterministic by server id. M11 provides no end-user configuration-file grammar
for arbitrary LSP servers.

## Lifecycle, coordinates, and synchronization

The adapter spawns the configured executable directly with
`Unix.create_process_env`, keeps its pipes private, and sends `initialize`,
`initialized`, and `textDocument/didOpen`. Its reader and stderr-drainer
threads only decode and enqueue bounded owned events. `Session.poll_language`,
called from the terminal loop after its wakeup fd becomes readable, is the only
place that changes session state, selections, overlays, or documents. The
terminal can therefore redraw on server output without busy polling.

The client negotiates UTF-8, UTF-16, and UTF-32 position encodings,
text-document synchronization, save text policy, hover, definition,
completion, rename, diagnostics, and workspace settings. It defaults to UTF-16
until the server chooses otherwise. `Language.Position` converts only valid
UTF-8 code-point boundaries and rejects offsets inside a multi-byte code point,
a UTF-16 surrogate pair, or a CRLF pair. Lines are zero-based:

```sh
dune exec bin/zenbu_headless.exe -- lsp-position utf-16 12 FILE.ml
```

Each kernel transaction exposes simultaneous source-snapshot edits. For an
incremental server, `Language.Sync` sends those edits in descending source
order, converting each range against its current intermediate text; applying
the emitted changes must reconstruct the committed snapshot or synchronization
fails. A full-sync server receives current complete text. Selection-only
document-version changes are associated with the latest LSP snapshot, even
though they do not send `didChange`. Save sends `didSave` when negotiated.
Saving under a new path rebinds both syntax and the language-service client
while preserving history and resetting only model-private grammar state.

The adapter maps LSP document version to Zenbu document version and contents.
A versioned diagnostic renders only for the current Zenbu version. Unversioned
diagnostics are accepted only at the untouched original snapshot (version zero)
and are dropped after an edit rather than displayed as possibly current. Hover,
completion, definition, and rename retain document version plus source caret
offset. A later edit/caret move sends `$/cancelRequest` where possible, and
late results are discarded before session mutation. Cancellation is a stale
safety mechanism, not a promise that every server stops immediately.

Limits are explicit: 32 KiB headers, 8 MiB protocol bodies, 16 KiB retained
stderr/hover text, 1,024 diagnostics/completion items, and 2 KiB user-visible
server messages. Protocol/decode/start failures set `failed`, retain a bounded
reason, wake the host, and use bounded descriptor close plus TERM/KILL reap.
`language.restart` is explicit. Session shutdown attempts `shutdown`/`exit`
then uses the same cleanup path.

## Commands, rendering, and current scope

`zenbu.language` descriptors appear in the same palette, command inspection,
and host dispatch as other providers:

- `language.status` reports language/server id, executable, root, lifecycle,
  encoding, sync kind, pending request count, diagnostics count, and last error.
- `language.restart`, `.hover`, `.definition`, `.complete`, and `.rename`
  start the corresponding async operation at the primary caret. Rename opens a
  text prompt.
- `language.diagnostic.next`, `.previous`, and `.describe-current` navigate or
  describe current diagnostics with normal selection semantics.

`Ctrl-Space` requests completion. Hover and completion are terminal overlays.
Completion accepts only plain text edits: snippets are rejected explicitly, and
main plus additional edits use the one semantic transformation
`language.apply-edits`. Rename and server `workspace/applyEdit` similarly
validate all edits as one normal transaction, with language provenance. The
view treats diagnostics as ranges and uses style precedence selection, search,
diagnostic, syntax, then plain. The status row reports error/warning totals.

M11 remains single-buffer. Same-document definitions navigate by selection;
another URI is reported as unsupported. Rename and `workspace/applyEdit` reject
a workspace edit spanning another URI rather than partially applying it. The
server receives `applied: false`. There are no panes/buffers, project search,
watching, code actions, formatting, symbols, semantic tokens, or multi-file
atomic edits.

## Inspection, testing, and trust

`Session.Language` is available interactively and headlessly:

```sh
dune exec bin/zenbu_headless.exe -- language-status FILE.ml
dune exec bin/zenbu_headless.exe -- language-fake-session \
  _build/default/test/fake_lsp_server.exe test/fixtures/lsp_ocaml/sample.ml
```

The fake server covers initialize/negotiation, full and incremental sync,
diagnostics, delayed stale hover, cancellation, completion additional edits,
rename, server apply-edit, malformed frames, crash/restart, and shutdown.
`test_m11_ocamllsp` opens a small Dune fixture with real `ocamllsp` and obtains
a hover response.

`Trace_event.Language_service` records server start, negotiation, sync,
request, cancellation, response, decode, exit, and failure stages with server
id, request id/document version where applicable, outcome, and bounded detail.
The generic `why` view formats these events. The profiler adds language sync,
hover, definition, completion, rename, and decode samples. Neither retains raw
JSON-RPC packets, full source, protocol objects, or server secrets.

Language servers are local processes chosen by the trusted host/embedding
configuration. They are not a Zenbu sandbox, permission system, or extension
capability boundary. Zenbu validates returned coordinates/current-document
edits but cannot make an arbitrary local executable safe; use only trusted
servers.
