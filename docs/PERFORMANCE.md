# Performance baseline

`make benchmark` runs the deterministic M11 single-process sanity benchmark.
It uses a generated 1 MiB OCaml buffer, a 100x30 frame, the checked-in Lua
configuration, and the checked-in Wasm Component conformance guest. It is a
diagnostic baseline, not a performance guarantee or benchmark competition.

Command:

```sh
make benchmark
```

Recorded during the M11 acceptance run on Fedora 43, Linux 7.1.7, x86_64, 13th Gen Intel Core i7-1355U
(12 logical CPUs; CPU scaling at 65%), OCaml 5.3.0, Dune 3.24.2, and the pinned
Wasmtime 47.0.3 C API:

| scenario | wall time |
| --- | ---: |
| empty-session initialization | 0.017 ms |
| LSP UTF-16 position conversion over 1 MiB | 3.729 ms |
| LSP incremental sync construction | 0.002 ms |
| open and parse generated 1 MiB OCaml | 118.677 ms |
| first 100x30 highlighted frame | 113.885 ms |
| cached 100x30 highlighted frame | 0.636 ms |
| literal search over 1 MiB | 15.110 ms |
| Vim committed text edit | 0.034 ms |
| Lua callback plus transaction | 0.116 ms |
| Wasm Component callback plus transaction | 0.146 ms |
| fake language server initialize plus didOpen | 1.554 ms |
| fake language server hover round trip | 0.117 ms |

The headless executable's `version` process-start measurement was below the
host `time` command's 0.01 s resolution on this machine. Interactive startup
also includes terminal initialization and normal plugin/config staging.

The language numbers are a local fake-server/control baseline, not an LSP
server performance promise. `Language.Position` currently scans source text,
and `Language.Sync` validates reconstruction; re-profile before replacing those
correctness-first choices with indexing or batching.

The first highlighted frame deliberately walks the current syntax snapshot to
make Zenbu-owned highlight spans. Subsequent frames cache source-line metadata
and spans by document contents, then filter syntax/search ranges to the visible
viewport. The remaining obvious bottleneck is therefore initial highlighting
of very large files; M11 keeps parsing and highlighting synchronous and does
not add an asynchronous syntax worker. Re-profile before pursuing that work.
