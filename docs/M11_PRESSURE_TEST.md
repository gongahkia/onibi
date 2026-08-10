# M11 language-service pressure test

M11's acceptance target is asynchronous language intelligence without a second
editing path or a protocol leak into models. Passing the checks is not a claim
of multi-file IDE completeness.

| Pressure | Guard |
| --- | --- |
| Coordinate correctness | `test_m11_language` round-trips UTF-8/16/32 positions across accented, CJK, emoji, combining, tab, empty, and CRLF lines; invalid UTF-16/CRLF boundaries fail. |
| Transaction synchronization | The same test checks full and incremental fake-server sync, multi-edit reconstruction, and completion additional edits. |
| Stale/cancel safety | A delayed hover is cancelled by an edit and never reaches the session; diagnostics are version-gated. |
| Process failure | Fake malformed input and a one-time crash move the client to failure; explicit restart reaches ready again. |
| Main-thread ownership | Fake diagnostics, hover, definition, completion, rename, and server apply-edit are driven through `Session.poll_language` and normal effects/transactions. |
| Real interoperability | `test_m11_ocamllsp` launches the bootstrapped `ocamllsp` on a small Dune fixture, reaches ready, and receives a hover response. |
| Headless path | `language-fake-session`, `language-status`, and `lsp-position` exercise fake integration, status inspection, and coordinate conversion without a TTY. |

The deliberate limits are important: M11 drops unversioned diagnostics after
the first edit, rejects edits for another URI instead of partially applying
them, and has no buffers/file-watch/project-search system. The LSP client is a
trusted-local subprocess adapter, not a security sandbox. See
[Language services](LANGUAGE_SERVICES.md) and ADRs 0030-0032.
