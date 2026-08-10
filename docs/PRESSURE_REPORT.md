# M10 pressure report

This report defines the release-hardening pressure boundaries and the tests
that guard them. The final release record should append command output and the
commit SHA; this document does not claim a platform run by itself.

| Pressure | Guard |
| --- | --- |
| Fatal Component callback | M9 Component test asserts fuel/trap classification, an unavailable health view, non-mutating repeated invocation, normal host edits, and successful reload recovery. |
| Host interaction isolation | M10 host test drives Unicode search, palette dispatch, save-as, model switch, and paste decoding through `Session`, without model-private APIs. |
| Snapshot presentation | M10 host test checks OCaml and JSON span classification plus selection > search > syntax rendering precedence. |
| Syntax lifecycle | M5 tests compare incremental and full parses and reject stale snapshot use. |
| Script/plugin lifecycle | M7/M8/M9 tests cover registration, reload replacement/failure retention, capabilities, provenance, and runtime limits. |
| Regression breadth | `make check` runs formatter validation, all builds, and every test executable; `make demo` exercises the public headless path. |

Known bounded risks remain: the Component runtime is native Linux x86_64 code;
bracketed paste is mediated by terminal markers that Notty documents as
best-effort; and the renderer is synchronous/full-frame rather than a terminal
capability-negotiating UI framework.

## M10 local evidence

The M10 worktree validation ran `make check`, `dune runtest --force`,
`make demo`, `make extension-docs`, and the deterministic Unicode
`search-session` fixture successfully. A real PTY session also exercised the
search prompt, the model picker, and multiline Unicode bracketed paste before
clean terminal restoration. Re-run the release gate after every later change;
this evidence is not a substitute for the fresh-clone gate.
