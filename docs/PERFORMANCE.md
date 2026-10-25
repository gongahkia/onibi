# Performance observations

Measured on 2026-10-25: macOS 15.6 (Darwin 24.6.0), x86-64, Intel Core i5-8279U
at 2.40 GHz, Go 1.27.0. The development build uses `CGO_ENABLED=0`, `-trimpath`,
and `-ldflags='-s -w'`. These observations are not a comparative benchmark against
Micro or a guarantee for another terminal, processor, or file.

## Original 0.1 baseline

These observations predate the 0.2 milestone and are retained for comparison.

### Distribution and ordinary operation

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

### Original text-core benchmarks

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

## 0.2 development milestone

Same development machine and compiler as above:

- Stripped native executable: approximately **8.0 MiB**; five target archives are
  approximately **2.5–2.8 MiB**. Features were added without another Go module or
  mandatory runtime dependency.
- The final text-core insertion benchmark measured **8.782–8.867 µs**, with
  **7,876 bytes / 70 allocations**; line lookup measured **37.03 ns**, with no
  allocations. The index has a measurable storage/update cost compared with the
  original baseline; it benefits viewport positioning, not every operation.
- Real native PTY editing test: **11.2 MiB RSS** at the first frame with LSP off.
  First launches after rebuilding took **412.7–515.5 ms**; this is not a cold-start SLA.
- `BenchmarkLongLineViewport`: **0.440–0.448 ms** for reveal plus complete simulated
  100×30 rendering at the end of a one-MiB printable ASCII line; approximately
  **21.7 KB / 3,925 allocations** per operation. This excludes real terminal I/O,
  insertion, language servers and syntax work. It is not end-to-end key latency.
- `scripts/redraw_bench.py`: thirty ordinary edits through a local 80×24 PTY,
  with plain mode and state persistence disabled, measured **0.767 ms median /
  1.457 ms maximum** from writing the input byte until output became readable.
  Output was **316 bytes median / 330 maximum per edit**, with no physical
  full-screen clears. First output is not completion of the whole frame. This is
  not an SSH/network benchmark or a comparison with another editor.

The 0.2 binary also passed the terminal editing test in the same **64 MiB /
0.5 CPU Linux scratch container**, with no installed runtime, libc, shell or
network. One run took **634.4 ms including Docker startup and attachment**.

The long-line optimization indexes printable ASCII spans and slices the visible
part before grapheme iteration. The renderer still visits screen cells to let
tcell compare its logical buffers. This reduces terminal traffic but does not
eliminate per-frame CPU or allocations.

Reproduce the added measurements with:

```sh
make build
python3 scripts/redraw_bench.py
go test ./internal/editor -run '^$' -bench BenchmarkLongLineViewport -benchmem -count=3
```

## Outstanding performance work

Measure cold startup systematically; complete key-to-frame latency and worst-case
stalls; Unicode-heavy long lines, many selections, large pastes, sustained history,
real low-bandwidth SSH traffic, and peak memory with real language servers.

Mixed-Unicode grapheme positioning and wrapping can still scan an entire line;
soft-wrap segments are currently recomputed. Files are loaded into memory, and
search/transform workers materialise strings. Syntax/LSP defaults to a 1 MiB file
threshold; project result snapshots are capped at 8 MiB / 2,000 matches, recovery
at 8 MiB combined text, task output at 1 MiB, session restoration at 16 MiB, and
core opening at 256 MiB per file. These are input/retention bounds, not total
process-memory guarantees. Language servers and build tools have separate memory
costs that are not constrained by the editor's output/text limits.

## Nested layout, workspace views and embedded terminal increment

Measured on the same Intel macOS development host, 2026-10-25, Go 1.27, stripped
CGO-disabled builds. These are individual local observations, not tail-latency
budgets or a cross-machine comparison:

- macOS amd64 binary: **9,039,856 bytes (8.62 MiB)**; Linux amd64:
  **8,958,112 bytes (8.54 MiB)**. All five release targets build.
- Native PTY: **78.8 ms** first frame, **11.4 MiB** first-frame RSS without LSP.
- An isolated tmux run with the tree disabled measured **11.5 MiB** before opening
  a terminal and **15.2 MiB** with an idle shell terminal. Shell child RSS is
  excluded; scrollback and sustained output increase memory beyond this sample.
- 30 local PTY edits with the default project tree: first-output median
  **0.672 ms**, maximum **1.212 ms**; median **600 bytes/edit**, maximum **615**,
  no full-screen clears. Output timing excludes terminal painting/network delay.
- Sampling a minimap for 500,000 Unicode lines (26 display rows): **0.569 ms**,
  **8,592 B / 372 allocations** per sample; performed off the UI thread.
- The updated Linux binary passes editing, paste, selective preview, undo/redo,
  save and quit in a scratch container limited to **64 MiB / 0.5 CPU**, with no
  runtime, shell, shared libc or network. This validates core editing; terminal
  sessions and debugging require their respective programs and additional memory.

Terminal emulators are created only on request. Their fixed parser buffer,
screens and optional history are per-session costs. Frames are capped at 30 Hz;
there is no idle frame timer. PTY queues, history cells, session count and screen
size are bounded, as described in USAGE.md. This is not a constant-RSS promise
for arbitrary terminal text. Minimap refreshes are debounced and coalesced.
