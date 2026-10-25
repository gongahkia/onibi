# Initial performance observations

Measured on 2026-10-25: macOS 15.6 (Darwin 24.6.0), x86-64, Intel Core i5-8279U
at 2.40 GHz, Go 1.27.0. The development build uses `CGO_ENABLED=0`, `-trimpath`,
and `-ldflags='-s -w'`. These observations are not a comparative benchmark against
Micro or a guarantee for another terminal, processor, or file.

## Distribution and ordinary operation

- Stripped native executable: approximately **7.7 MiB**, including bundled lexers.
- Release archives: approximately **2.4–2.7 MiB**, across five OS/CPU targets.
- Native pseudo-terminal test RSS at first frame: approximately **11 MiB**, with
  a tiny text file and language servers disabled. This is not a peak-memory test.
- Three consecutive launches of the same binary reached the first frame in
  **58.2, 23.6, and 18.0 ms**. Earlier first launches after builds measured
  **397.6 and 522.8 ms**. Cache and launch conditions matter; no cold-start target
  has yet been established and the cause of the larger observations is unproven.
- A statically linked Linux x86-64 executable completed the real terminal editing
  test in a Docker **scratch** container capped at **64 MiB and 0.5 CPU**, with no
  network, shell, libc, Go installation, or language server. The measured 741.6 ms
  includes Docker launch/attachment and must not be compared to native startup.

The terminal test selects all text, pastes two lines with bracketed paste, undoes
and redoes the paste, selectively previews replacement, saves, and quits. A separate
isolated tmux test checks UTF-8 paste and undo/redo. These establish functioning
ordinary editing in constrained conditions; they do not establish large-file
capacity within a 64 MiB budget.

## Text-core benchmarks

Command:

```sh
go test ./internal/text -run '^$' -bench . -benchmem -count=1
```

| Operation | Observed time | Allocated bytes | Allocations |
| --- | ---: | ---: | ---: |
| Insert one byte into a 10,500,000-byte persistent tree | 7.282 µs | 7,071 | 70 |
| Find a line start in a one-million-line tree | 36.90 ns | 0 | 0 |

These benchmark only storage operations after constructing the document. They
exclude terminal output, file I/O, syntax, selection projection, document/history
validation, and language-server work. They cannot substantiate whole-editor
keystroke-latency claims.

## Outstanding performance work

Measure cold startup systematically; end-to-end key latency and worst-case stalls;
very long lines, many selections, and large pastes; sustained history memory;
low-bandwidth SSH redraw traffic; and peak memory with real language servers.

In particular, grapheme-aware horizontal positioning scans a line prefix, so very
long individual lines can still be expensive. Files are loaded into memory, and
search/transform workers materialise strings. Optional syntax/LSP is bounded by a
1 MiB document threshold by default; core editing has a configurable 256 MiB open
limit. Neither threshold is a guarantee about total process memory.
