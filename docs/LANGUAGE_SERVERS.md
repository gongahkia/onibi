# Verified language-server workflows

Language servers are optional external programs. Shack's executable includes no
language runtime, compiler, package manager or server. Basic editing, syntax
highlighting, Markdown/Org previews and structural folding work without them.
Use `--no-lsp` on machines where language services are unnecessary.

## Live compatibility matrix

Observed on macOS amd64, Go 1.27, 2026-10-26. These are isolated small-workspace
integration checks, not claims about every server feature or large monorepo.

| Server | Tested version | Language | Formatting |
| --- | --- | --- | --- |
| gopls | 0.24.0 | Go | Reviewed edits and undo passed |
| rust-analyzer | 1.98.0 (88d9e12a) | Rust | rustfmt edits and undo passed |
| Pyright | 1.1.414 | Python | Not provided by this server |
| python-lsp-server | 1.15.0, with optional formatter plugins | Python | Reviewed edits and undo passed |
| typescript-language-server | 6.0.2, TypeScript 6.0.3 | TypeScript | Reviewed edits and undo passed |

All five exercise completion, hover, definitions, references, document symbols,
rename, incremental unsaved changes, diagnostic publication/clearing and process
cleanup through the actual LSP subprocess transport. Fixtures place an emoji
before a reference on the same line to check UTF-16/UTF-8 coordinate conversion.
The editor integration additionally checks actual completion selection/undo,
definition navigation, reviewed rename/group undo, formatting preview/undo where
supported, diagnostic picker/clear and saving the restored Unicode source.

Code-action requests returned actions for the Go and TypeScript error fixtures;
resolvable actions were resolved. The Rust and Python fixtures returned no actions.
Bidirectional command execution, review cancellation and resource-operation
application have separate deterministic subprocess and tmux tests; this matrix
alone does not prove every real server's custom commands or refactorings.
Existing tests also exercise installed clangd's navigation and intelligence APIs.

Workspace loading is asynchronous. Rust may return syntactic navigation before
semantic hover/rename/completion are ready, and may briefly return no completions
after an edit. The live tests poll real results during loading instead of assuming
that an initialized transport means the workspace is fully indexed. Shack retries
`ContentModified` failures up to three times for read/compute requests while its
synchronized document generation is unchanged, within the original request
deadline. It never replays commands, initialization or resolve requests. This follows
the [LSP guidance on internal server state changes](https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#implementationConsiderations).

## Diagnostics without server versions

Shack accepts diagnostics that omit a version, as used by TypeScript. The client
records the document version and open-buffer incarnation being synchronized when
such a notification arrives. The UI rejects a notification if that local snapshot
has since changed. Explicit server versions continue to receive strict matching.

For an unversioned notification, this is necessarily best-effort: the server does
not identify the source revision it analysed. An old server computation arriving
after a newer sync can temporarily appear until the next diagnostic update. Editing
clears displayed diagnostics; the server's next publication replaces them.

## Reproduce the checks

Install the optional development servers and their toolchains: gopls/Go,
rust-analyzer/Cargo/rustc/rust-src/rustfmt, Pyright/Node, python-lsp-server/Python with
its formatting dependencies, and typescript-language-server/TypeScript/Node.
Nothing in this procedure changes release dependencies.

```sh
python3 scripts/lsp_live.py --race
```

The runner uses executables on PATH or the ignored local development installations
under `build/`. Override individual paths with `SHACK_GOPLS`,
`SHACK_RUST_ANALYZER`, `SHACK_PYRIGHT`, `SHACK_PYLSP`, or `SHACK_TYPESCRIPT_LS`.
It runs both `internal/lsp` and `internal/editor` live suites. Missing selected
servers fail explicitly. Normal `make check` skips these opt-in suites and runs
all deterministic protocol/editor regressions. Temporary projects contain no
third-party project dependencies; their files and processes are cleaned up.
