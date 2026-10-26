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

## DAP source-debugging increment

Same host/build settings, 2026-10-25. The stripped macOS amd64 executable is now
**9,630,352 bytes (9.18 MiB)**; Linux amd64 is **9,482,400 bytes (9.04 MiB)**.
All five targets build. With no debug session or language server, the native PTY
check observed **11.5 MiB** first-frame RSS and **22.7 ms** startup (one warm run,
not directly comparable to a cold launch). Core editing again passes the **64 MiB /
0.5 CPU** scratch-container test. Debug adapters and debuggee processes have
additional, language-dependent memory costs and were not included in idle RSS.

No adapter, output ring, debug worker or polling timer starts before a debugging
request. Active output uses a 1 MiB byte ring; copying/rendering its transcript is
throttled and begins only when the user opens Debug output. Protocol payloads,
pending requests, event queues, breakpoint counts and variable pages are bounded.
Live debugpy and real tmux tests cover launch/attach, input ownership, breakpoint
hits, scopes/variables, evaluation, stepping, pause, normal stop and abrupt
transport cleanup. These demonstrate working behavior, not universal adapter
compatibility or a debugger memory budget.

## Snippet expansion increment

Same Intel macOS host, Go 1.27, 2026-10-25: cached expansion of 32 linked nested
placeholders measured **6.84 µs**, **5,352 bytes / 39 allocations** per operation
(`BenchmarkCachedLinkedExpansion`). This isolates expansion, not document edits
or terminal latency. Regex transforms run on a worker when entering/leaving a
stop; ordinary linked typing uses cached results. Parser size/depth, expansion,
reference work and regex execution are bounded. Timeout checks are coarse and
are not a strict wall-clock deadline. No snippet worker or regex timer is started
merely by launching the editor.

## Bundled Markdown/Org preview increment

Same Intel macOS host, Go 1.27, 2026-10-25, individual benchmark runs:

- Markdown, 24,000 bytes of repeated headings/emphasis/links/lists: **3.73 ms**,
  **1,715,352 B / 12,813 allocations** per full render.
- Org, 23,700 bytes of equivalent content: **5.54 ms**,
  **1,629,311 B / 22,894 allocations** per full render.
- Cached Unicode Markdown preview, 100×32 simulation screen, 500 sections:
  **0.708 ms**, **37,671 B / 5,145 allocations** per full logical redraw.
  This includes the editor's screen traversal, not terminal transport/painting.

Rendering starts only when a preview is requested. Source edits debounce for
200 ms; a single background renderer coalesces pending versions per view. Hidden
views do not reparse until displayed. Style lookup during painting is binary
search over cached ranges. Source, output, node/span/table limits and the eight-view
cap are documented in USAGE.md; parser intermediate allocations can exceed retained
output, so these figures are examples rather than memory ceilings. Final artifact,
RSS and constrained-Linux measurements still need repeating after folding.

```sh
go test ./internal/markup -run '^$' -bench BenchmarkRender -benchmem
go test ./internal/editor -run '^$' -bench BenchmarkReadingViewport -benchmem
python3 scripts/markup_smoke.py
```

## Folding increment

Same host/settings, 2026-10-25. Scanning 495,000 bytes / 45,000 lines of repeated Go
functions with Unicode strings took **6.79 ms**, **8,103,972 B / 49 allocations**
per scan. Scans materialise a source snapshot and build a line/region index off the
UI thread; none run during ordinary redraws. One running scan and one coalesced
pending request are permitted. Limits are documented in USAGE.md.

A regression test collapses a section containing 100,000 Unicode lines and checks
viewport rows, navigation and reveal without visiting the hidden lines. Closed
region lookup is binary search; edit rebasing sweeps ordered edits and regions
instead of multiplying the two counts. This is functional/scaling evidence, not
an end-to-end latency measurement. Final transport, Unicode long-line, RSS and
release measurements remain in the milestone ledger.

```sh
go test ./internal/folding -run '^$' -bench BenchmarkLargeCode -benchmem
python3 scripts/folding_smoke.py
```

## Real SSH transport checks

OpenSSH 9.9p2 over loopback on the same Intel macOS host, 2026-10-26. Two 20-edit
samples with the current native binary observed median first-output delays of
**8.33 ms** and **12.12 ms**, with maxima **14.15 ms** and **21.68 ms**. Both emitted
**444 median / 453 maximum terminal bytes per edit**, without full-screen clears.
These figures include local SSH/PTY scheduling, exclude desktop painting and
TCP/encryption framing bytes, and do not predict WAN/low-bandwidth behaviour.
The variation is a reason to retain raw observations rather than claim a fixed
latency budget. Reproduce with `python3 scripts/ssh_smoke.py`.

The same harness verifies eight TERM profiles, real tmux-over-SSH and an embedded
shell/Vim workflow with resize and process cleanup. See TERMINAL_COMPAT.md for the
capability matrix and clipboard restrictions. Final Unicode worst-case and release
RSS/artifact measurements are still outstanding.
