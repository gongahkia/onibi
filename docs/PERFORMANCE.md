# Performance baseline

`make benchmark` runs the deterministic M10 single-process sanity benchmark.
It uses a generated 1 MiB OCaml buffer, a 100x30 frame, the checked-in Lua
configuration, and the checked-in Wasm Component conformance guest. It is a
diagnostic baseline, not a performance guarantee or benchmark competition.

Command:

```sh
make benchmark
```

Recorded on Fedora 43, Linux 7.1.7, x86_64, 13th Gen Intel Core i7-1355U
(12 logical CPUs; CPU scaling at 65%), OCaml 5.3.0, Dune 3.24.2, and the pinned
Wasmtime 47.0.3 C API:

| scenario | wall time |
| --- | ---: |
| empty-session initialization | 0.020 ms |
| open and parse generated 1 MiB OCaml | 142.104 ms |
| first 100x30 highlighted frame | 142.755 ms |
| cached 100x30 highlighted frame | 0.629 ms |
| literal search over 1 MiB | 16.731 ms |
| Vim committed text edit | 0.032 ms |
| Lua callback plus transaction | 0.141 ms |
| Wasm Component callback plus transaction | 0.241 ms |

The headless executable's `version` process-start measurement was below the
host `time` command's 0.01 s resolution on this machine. Interactive startup
also includes terminal initialization and normal plugin/config staging.

The first highlighted frame deliberately walks the current syntax snapshot to
make Zenbu-owned highlight spans. Subsequent frames cache source-line metadata
and spans by document contents, then filter syntax/search ranges to the visible
viewport. The remaining obvious bottleneck is therefore initial highlighting
of very large files; M10 keeps parsing and highlighting synchronous and does
not add an asynchronous syntax worker. Re-profile before pursuing that work.
