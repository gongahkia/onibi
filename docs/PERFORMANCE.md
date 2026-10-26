# Performance observations

Measured on 2026-10-25: macOS 15.6 (Darwin 24.6.0), x86-64, Intel Core i5-8279U
at 2.40 GHz, Go 1.27.0. The development build uses `CGO_ENABLED=0`, `-trimpath`,
and `-ldflags='-s -w'`. The historical milestone measurements are not comparisons against other editors.
A separate matched local comparison appears at the end of this document; none of
these observations guarantees performance on another terminal, processor or file.

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

## Measurement limits and future work

Measure cold startup systematically; complete key-to-frame latency and worst-case
percentiles under sustained load; many selections, large pastes, sustained history,
real low-bandwidth SSH traffic, and peak memory with real language servers.

The final milestone below adds bounded Unicode layout/word traversal and actual
SSH/PTy measurements. Cold distant navigation still scans the required prefix.
Files are loaded into memory, and
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
Final RSS and constrained-Linux repeats are recorded in the completed milestone below.

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
release measurements appear below.

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
capability matrix and clipboard restrictions. Final Unicode and release
RSS/artifact measurements follow below.


## Unicode long-line layout indexing

Intel macOS / Go 1.27, 2026-10-26, 100×30 simulated terminal. The workload is
one line repeating CJK, combining accents, ZWJ emoji, regional-indicator flags
and tabs, with the cursor at EOF. These are editor reveal + draw measurements,
not terminal/network latency. Before changes, a single warmed redraw measured:

| Line bytes | Unwrapped | Wrapped |
| --- | ---: | ---: |
| 128 KiB | 42.1 ms / 0.52 MiB allocated | 635 ms / 9.86 MiB allocated |
| 1 MiB | 351 ms / 4.02 MiB allocated | 6,517 ms / 82.05 MiB allocated |

Two subsequent ten-iteration samples measured **0.42–0.72 ms unwrapped** and
**1.17–2.06 ms wrapped** across those sizes. At 1 MiB, allocations fell to about
**19.5 KiB unwrapped / 247 KiB wrapped per draw**. These measurements are warm
viewport costs; they do not conceal the cost of initially indexing distant text.

A separate 928 KiB line sample measured a **26.4 ms cold jump to EOF**, **9.7 µs
cold first-row lookup**, **11.2 µs warm previous-grapheme lookup**, and **23.5 µs
end edit + undo / 56.4 µs middle edit + undo**, including row/column queries.
The index benchmark now uses the same approximately 1 MiB length as the viewport
benchmark; rerun it when comparing exact values. Cold distant navigation and a
single extremely long combining sequence still require scanning their text.

The UI now shares a lazy grapheme/column/wrap index, limited to 64 line/width
entries with at most 512 checkpoints each (1.25 MiB of checkpoint storage on
64-bit hosts, plus small entry/slice overhead). Sparse checkpoints compact for
very long lines. Edits preserve only verified unchanged prefixes; skipped
versions and replaced documents discard stale indexes. ASCII arithmetic remains
available without indexing. Readers use small windows and extend lookahead at
cluster boundaries, instead of copying a whole line for each visible row.
Painting replaces any individual cluster longer than 256 UTF-8 bytes with `�`;
the original text, selection offsets, copying and saving remain intact.

Coverage includes forward/backward cursor boundaries (including Unicode Prepend
characters), tabs, CRLF, chunk-spanning ZWJ/flag/combining sequences, edit/undo,
cache compaction/eviction and random edits. `make check` passed; a 20-second
layout fuzz run completed 46,633 cases. Subsequent executable/resource and real PTY
checks are recorded below.

```sh
go test ./internal/editor -run '^$' -bench 'BenchmarkUnicodeLongLine(Index|Viewport)' -benchmem
go test ./internal/editor -run '^$' -fuzz '^FuzzLineLayout$' -fuzztime=20s
```

### Long identifiers and word selection

A 1 MiB uninterrupted identifier alternating CJK and combining accents exposed a
separate repeated-scan problem: indexed character-by-character word movement still
measured **4.97 s left / 2.51 s right**, allocating **3.15 / 1.57 GB cumulatively**
(not peak RSS). Streaming class runs now serve Ctrl+Arrow and word selection.
Three-iteration samples measured **25.0 ms left / 24.7 ms right**, with
**1.19 / 3.46 MB allocated**. This scans the whole megabyte-long word by necessity;
ordinary nearby words stop after their local run. Backward traversal reuses sparse
grapheme checkpoints instead of materializing or repeatedly rescanning the prefix.
Reference comparisons cover Unicode classes, punctuation, CRLF, selection and
whitespace spanning multiple windows. Reproduce with:

```sh
go test ./internal/editor -run '^$' -bench BenchmarkUnicodeLongWord -benchmem
```


### Multiple long lines and oversized clusters

The cache now accommodates 64 line/width entries while keeping the same
1.25 MiB checkpoint ceiling. This avoids eviction churn across a typical tall
viewport. A 100×40 simulated viewport horizontally scrolled through forty
128 KiB mixed-Unicode lines measured **110 ms** with the initial 16-entry cache
and **2.1–3.2 ms** with the revised cache (ten-iteration samples). The 8 KiB-per-line
case measured **1.6–2.4 ms**, down from 7.1 ms. Eight recent boundaries per entry
keep sequential rows local after sparse checkpoints compact.

Each entry also remembers the extent and width of up to sixteen clusters longer
than 256 bytes, retaining no source strings. A viewport containing one 1 MiB
combining sequence measured **0.372 ms warmed**; the first inspection still needs
to read the cluster. Text remains intact while its display uses a placeholder.
These small fixed metadata arrays add about 45 KiB at the full 64-entry limit.
`End` now uses the rope's line-end index instead of copying the line.

A real native 1 MiB Unicode PTY check passed whole-grapheme deletion of a flag,
ZWJ emoji and combining accent, undo/save, wrapping, shrink/enlarge and quit.
Its first cold End produced output in **22.47 ms**; twenty wrapped edits observed
**0.829 ms median / 2.533 ms maximum to first output**, **518 / 528 terminal bytes**,
and **17.9 MiB editor RSS** after editing. This excludes GUI painting and is not
whole-frame latency. `make check` passed after the final cache changes; another
15-second fuzz run completed 123,951 cases. Reproduce with
`python3 scripts/unicode_smoke.py` after `make build`.

## Completed workflow milestone — 2026-10-26

Final executable checks use the same Intel macOS host and Go 1.27, CGO disabled,
`-trimpath -ldflags='-s -w'`. All five release archives were rebuilt; executable
bytes, every packaged document, execute permissions and SHA-256 manifests were
verified with `scripts/verify_release.py`. Linux `file` inspection confirms static
linking. macOS amd64 and Linux amd64 were executed; other architectures were
cross-built, not run on physical ARM hardware.

| Target | Executable | Compressed archive (approximately) |
| --- | ---: | ---: |
| macOS amd64 | 10.550 MiB | 3.75 MiB |
| macOS arm64 | 10.014 MiB | 3.50 MiB |
| Linux amd64 | 10.367 MiB | 3.70 MiB |
| Linux arm64 | 9.813 MiB | 3.37 MiB |
| Linux ARMv7 | 9.875 MiB | 3.49 MiB |

Compared with the earlier 0.2 baseline, the native executable grew from about
8.0 to 10.55 MiB, and first-frame editor RSS from about 11.2 to **14.1 MiB**.
This includes the added terminal, debugger, snippet, markup and folding support.
A separate embedded-terminal workflow observed **14.4 MiB before / 17.5 MiB with
an idle shell terminal**, excluding child memory. Three-second idle observations
with and without an embedded shell produced **zero terminal bytes and 0.00 s
additional reported editor CPU** at the system counter's resolution. This is an
idle sample, not a claim of zero resource use.

The native basic PTY check observed a **28.3 ms warm first frame**. A first launch
from the rebuilt macOS release path also passed, taking **498 ms** with **13.8 MiB
RSS**; launch-path/cache/security checks make startup observations variable. Thirty ordinary
edits measured **0.651 ms median / 1.295 ms maximum to first output** and
**600 / 615 terminal bytes**, without full-screen clears. The original 0.2 sample
was 0.767 / 1.457 ms and 316 / 330 bytes: the expanded UI emits more bytes, while
these samples show comparable local response times. They are not percentile SLAs
or measurements of completed desktop painting. The final loopback SSH repeat
measured **10.01 / 12.47 ms** and **444 / 453 terminal bytes**; all eight profiles
and both nested tmux/embedded-terminal workflows passed.

Three half-second benchmark samples retained **0.412–0.418 ms** simulated ASCII
long-line redraws and about **21.7 KB / 3,925 allocations**, comparable to 0.2's
0.440–0.448 ms. Core large-file insertion observed **8.72–14.19 µs** (one slower
sample), **7,878 B / 70 allocations**; indexed line lookup was **36.75–37.46 ns**
with no allocations. The final ten-iteration Unicode sample observed:

| Workload | Time |
| --- | ---: |
| Warm 1 MiB unwrapped viewport | 0.482 ms |
| Warm 1 MiB wrapped viewport | 1.303 ms |
| Cold first row / cold EOF layout query | 9.6 µs / 18.9 ms |
| Warm previous grapheme near EOF | 1.47 µs |
| End edit + undo + row/column queries | 39.2 µs |
| Middle edit + undo + row/column queries | 75.4 µs |
| Whole 1 MiB word, left / right | 23.9 / 25.6 ms |
| Forty 128 KiB Unicode lines, scrolled horizontally | 1.62 ms |
| Warm viewport with one 1 MiB combining cluster | 0.401 ms |

The final Linux amd64 binary passed both basic and 1 MiB Unicode PTY editing in
a Docker **scratch** container with **64 MiB RAM, 0.5 CPU, no network, no Go,
no shell and no libc installation**. The Unicode test includes deletion/undo of
whole flag/ZWJ/accent clusters, wrapping, resize, save and exit. One constrained
run observed **4.00 ms median / 31.14 ms maximum first-output delay** during twenty
wrapped edits; its cold End was **150 ms**. Basic startup was **1.46 s including
Docker startup/attachment**, during other development checks. These are observations
under a CPU quota, not a comparison with native startup or a guarantee that every
file fits in 64 MiB. Recovery, retained undo, large source files and optional child
processes can consume more memory.

Final validation also passed `make check`, live Go/Rust/Python/TypeScript
protocol/editor workflows under the race detector, live debugpy launch/attach/
cleanup tests, and all tmux feature smoke checks. A debugger smoke invocation
initially used a resolved Python symlink, which selected the base interpreter
without debugpy; preserving the venv path passed three consecutive terminal runs.
The harness now checks the optional dependency before launch and retains adapter
output on failure. Packaging no longer depends on the removed README and excludes
macOS AppleDouble metadata; USAGE.md is included as the installation/user guide.

The milestone measurements do not establish a cross-editor ranking, universal
terminal certification, constant-time cold Unicode navigation, mature IDE parity
or a total-memory bound for external language servers. The measured limits and future work above remain explicit.


## Matched local comparison with Micro, Vim and Neovim

2026-10-26 on the same Intel macOS host. `scripts/editor_compare.py` runs Shack
0.2.0-dev, Micro 2.0.15, Apple's Vim 9.1 (patches 1–1752) and Neovim 0.12.4 through
100×30 xterm-256color PTYs. It uses isolated/disabled user configuration,
syntax/services off, wrapping off, one discarded warmup, then three interleaved
runs with twenty measured ASCII insertions each. Every run verifies saved text
and clean exit. The small fixture contains a marker plus 1,000 ordinary text
lines; insertion is at its beginning. The other fixture is a single approximately
1 MiB line mixing CJK, combining accents, ZWJ emoji, flags and tabs; insertion is
at EOF after its first navigation/indexing cost. This deliberately stresses long
Unicode lines, not ordinary multiline files or complete IDE workloads.

All response values below are input-write to **first readable terminal output**,
not full-frame completion, GUI painting or human-perceived latency. Memory is
median RSS after editing, summed over each editor's full process tree. RSS can
double-count shared pages and is not peak/private memory. Neovim uses separate
TUI and editor processes, both included, as described in its
[official TUI documentation](https://neovim.io/doc/user/tui/).

| Editor | Small-file response, median / maximum | Small-file RSS | 1 MiB Unicode-line response, median / maximum | Unicode-line RSS |
| --- | ---: | ---: | ---: | ---: |
| Shack | 1.71 / 4.61 ms | 14.9 MiB | 1.01 / 5.96 ms | 16.9 MiB |
| Micro | 1.40 / 3.39 ms | 15.3 MiB | 192.75 / 349.33 ms | 25.0 MiB |
| Vim | 0.20 / 0.62 ms | 4.9 MiB | 36.75 / 45.90 ms | 12.7 MiB |
| Neovim | 0.60 / 1.21 ms | 10.4 MiB | 19.86 / 48.85 ms | 18.3 MiB |

Shack is in Micro's general response/memory range on the small file; lean Vim and
Neovim respond faster and use less memory in that workload. Shack has a large
advantage in this specific long-Unicode-line workload. Neither observation proves
that it is faster or lighter for most editing workloads. An initial timing pass
also put Shack/Micro close together on small-file typing, with their order reversed;
the approximately 0.3 ms gap is not a stable general performance claim.

A clear remaining weakness is terminal traffic: median bytes per small-file edit
were **1,824 Shack / 133 Micro / 33 Vim / 124 Neovim**. Long-line values were
**811 / 1,883 / 13 / 118 bytes** respectively. These are terminal bytes, excluding
SSH framing. Shack's excess ordinary-edit output could matter on low-bandwidth
connections even when local rendering is quick; it is a concrete optimization
target. There is no implementation change in this comparison.

Warm launch-to-first-file-marker medians were 37.1 / 34.7 / 27.6 / 156.8 ms for
Shack/Micro/Vim/Neovim on the small fixture. The harness does not emulate terminal
query replies, and startup includes terminal probing/handshakes. These figures
should not be generalized into real-terminal startup rankings; they are retained
in the raw data for reproducibility.

GNU Nano, Helix and Emacs were not measured. This Mac's `/usr/bin/nano` resolves
to Pico, so treating it as a GNU Nano result would be incorrect. External published
results use different hosts, versions and endpoints; they are not merged into this
table. Future comparisons should add these editors, ordinary code with syntax/LSP,
multiline large files and completed-frame/real-SSH measurements.

[Raw runs, versions and binary hashes](benchmarks/editor-comparison-macos-2026-10-26.json)
are retained with the repository. Reproduce on macOS with installed participants:

```sh
python3 scripts/editor_compare.py > build/editor-comparison.json
```


## Redraw optimization: matched before/after

The original 1,000-line plain-text fixture was rerun against the retained pre-change
binary and the new binary, three measured runs each after a discarded warmup. Median
terminal traffic fell from **1,823.5 to 88 bytes per edit (95.2% less)**. Editor RSS
was 15.02 versus 15.13 MiB. First-output medians were 0.90 versus 1.49 ms in these
successive batches: this is a bandwidth improvement, not evidence of reduced CPU
cost or faster key response. The changed renderer composes a complete logical frame
before publishing final cell differences, adding comparison work to avoid redundant
terminal writes. A separate syntax fix retains/rebases unaffected token colors
until the background lexer refreshes them; edited spans are discarded immediately,
and changed lexical context elsewhere is corrected on that refresh.

[Raw before/after samples and binary hashes](benchmarks/redraw-comparison-macos-2026-10-26.json)
are retained. Reproduce each binary with:

```sh
python3 scripts/editor_compare.py --editors Shack --fixtures small --shack-binary /path/to/shack
```

Validation passed `make check` (format, vet and all race tests), plus tmux startup,
Markdown/Org, folding, embedded terminal and long-Unicode PTY smoke checks. Regression
tests cover unchanged-frame writes, wide cells, overlays/resizing, syntax retention,
undo and skipped document versions.


## Code editing with language services: Micro, configured Neovim, Helix and Kakoune

2026-10-26, same Intel macOS host, after Shack's frame composition and provisional
syntax-color preservation changes. This comparison uses the requested IDE-oriented
setups; the older bare-editor table above remains a historical baseline.

`scripts/ide_compare.py` opens the same 1,004-line Go source file in a small two-file
module. Syntax highlighting is active. The harness waits for a diagnostic about an
intentional unresolved identifier, then inserts twenty characters at the end of the
first comment, verifies the exact saved file, and checks clean exit. Each LSP profile
must produce diagnostic publications and forward document changes to the server.
These are **comment-typing measurements with language services running**, not a
completion, navigation, formatting or large-workspace benchmark.

Profiles:

- Shack: built-in syntax and LSP, user configuration/session/recovery disabled.
- Micro 2.0.15: default syntax with the
  [Micro LSP plugin](https://github.com/AndCake/micro-plugin-lsp), explicitly bound to
  gopls; its optional format-on-save is disabled. The plugin describes itself as a
  proof of concept; this is not a claim of feature parity with the other clients.
- Neovim 0.12.4, current: a private copy of the user's actual configuration, plugin
  installations and parsers, including Treesitter, completion, statusline and other
  IDE plugins. No language server is attached for this Go file. The live configuration
  is never modified. Runtime inspection verifies Treesitter is active.
- Neovim + gopls: the same private copy, plus a benchmark-only `vim.lsp.start` call
  using the configuration's completion capabilities. Runtime inspection verifies an
  attached gopls client. The existing lsp-zero integration emits a compatibility
  warning; the harness acknowledges its hit-enter prompt before measuring. This
  profile is not a silently repaired version of the user's configuration.
- Helix 25.07.1: bundled Go Treesitter grammar and
  [default Go language-server integration](https://docs.helix-editor.com/lang-support.html),
  with true color enabled; normal auto-completion and format-on-save remain enabled.
- Kakoune 2026.05.21: default Go highlighting plus
  [kak-lsp](https://github.com/kakoune-lsp/kakoune-lsp) 22.0.0, enabled in isolated kakrc.

All server-backed profiles use the same local gopls 0.24.0 binary. A small Python
stdio recorder verifies protocol activity without recording document content. Its
process and the server's descendants are included in total RSS. The editor column
excludes the recorder/server subtree, but includes the Kakoune LSP bridge and both
Neovim UI/editor processes. RSS is sampled after typing; it is neither peak memory
nor private memory and can count shared pages more than once. Server memory can
vary with garbage collection and client capabilities even with an identical server.

The PTYs are 100×30, xterm-256color. Each setup gets a discarded warmup followed by
three interleaved measured runs (60 keys per setup). Response time is from writing
a key to the **first readable terminal output**, not a completed frame or screen
painting. Immediate traffic includes output until a 30 ms quiet period; settled
traffic additionally drains until a 250 ms quiet period, capturing debounced syntax
and LSP redraws. Later activity can fall outside that window. Values exclude SSH
framing and are not measurements over an actual SSH/tmux connection. Different
statuslines, colors, diagnostics and plugin defaults remain part of each setup.

| Setup | First-output median / p95 / maximum (ms) | Editor + bridge RSS (MiB) | Total RSS (MiB) | Immediate / settled bytes per edit |
| --- | ---: | ---: | ---: | ---: |
| Shack | 1.51 / 2.28 / 3.23 | 20.0 | 162.4 | 151 / 485 |
| Micro+LSP | 26.92 / 33.56 / 200.93 | 57.2 | 267.6 | 119 / 119 |
| Neovim-current | 5.22 / 9.70 / 10.47 | 37.4 | 37.4 | 130 / 130 |
| Neovim+gopls | 7.30 / 13.76 / 16.94 | 38.9 | 276.9 | 130 / 130 |
| Helix | 5.42 / 7.17 / 9.24 | 18.4 | 255.4 | 150 / 278 |
| Kakoune+LSP | 1.37 / 2.06 / 17.13 | 16.1 | 258.4 | 231.5 / 245.5 |

Shack and Kakoune have the lowest median first-output times in this particular
workload. Shack's editor footprint is close to Helix/Kakoune and lower than these
Micro/Neovim configurations. This does not isolate which plugins or implementation
choices cause the differences. In particular, Micro's result includes this specific
LSP plugin; it must not be generalized to Micro's core typing speed. The single
200.93 ms Micro outlier is retained, not removed from the sample.

Shack's immediate traffic is competitive with Helix, but its delayed syntax and
service updates raise total observed traffic to 485 bytes/edit, more than every
other setup here. Incremental highlighting and diagnostic redraws remain useful
optimization targets. Total language-service memory varies substantially across
runs and must not be mistaken for the editor's own footprint. Shack's total samples
range from 90.7 to 257.7 MiB, and Kakoune's from 117.4 to 259.6 MiB; the table reports
medians, not reliable total-memory budgets or a ranking of server efficiency.

[Raw IDE runs, configuration fingerprints and protocol evidence](benchmarks/ide-comparison-macos-2026-10-26.json)
are retained alongside these results.


Startup and language-server readiness timings are retained in the raw data but
are not used for a startup ranking: the harness does not emulate terminal query
replies, caches are warm, and Neovim's compatibility prompt needs acknowledgement.
This small Go module does not establish whole-IDE parity, performance in large
repositories, Linux/low-memory results, sustained typing behavior or complete-frame
latency. In particular, the no-server Neovim row is context for the user's current
setup and must not be treated as an equal-language-service memory comparison.

Reproduction requires the pinned editors, gopls in `build/tools/gopls`, Helix's
release directory/runtime and kak-lsp in `build/compare-tools`, and a checkout of the
Micro plugin at `build/compare-tools/micro-lsp`. Copy the desired Neovim config and
data into `build/compare-tools/nvim-snapshot/{config,data}/nvim`; do not point the
snapshot paths at live configuration. A nested `build/compare-tools/go.mod` keeps
third-party Go test fixtures out of Shack's `go test ./...` traversal. The harness
records binary/script/config hashes, the Micro plugin revision, Neovim plugin locks,
per-key samples, process counts and protocol metadata. Private Neovim configuration
contents are not published, so reproducing that exact profile requires the local
snapshot. All other profile setup is in the script.

```sh
make build
python3 scripts/ide_compare.py --runs 3 --output build/ide-comparison.json
```


## Expanded Markdown preview coverage

2026-10-26, same Intel macOS host. Markdown previews now enable Goldmark's GFM,
footnote and definition-list extensions, preserve list/quote continuation indentation,
respect table column alignment and retain image destinations in text placeholders.
The parser/output bounds, 200 ms source-update debounce, single background renderer
and cached ordinary redraw path remain in place. No new runtime dependency was added.

Three non-race benchmark repetitions observed:

| Fixture | Before | After | Allocated bytes / allocations per render after |
| --- | ---: | ---: | ---: |
| Existing Markdown fixture, about 24 KB | 3.19–4.00 ms | 3.01–3.50 ms | 1.72 MB / 12,851 |
| Existing Org fixture, about 24 KB | 4.80–6.07 ms | 5.00–5.68 ms | 1.63 MB / 22,894 |
| New nested quote/list, definition and aligned-table fixture, about 34 KB | — | 5.54–7.24 ms | 3.70 MB / 26,354 |

The existing Markdown fixture previously allocated about 1.72 MB / 12,813 allocations;
the extra parser extensions add little overhead on that fixture. These are complete
background-render samples, not keypress latency, peak memory or proof of a speedup.
The richer fixture has more layout/style work and is not a before/after comparison.

`make check`, the rebuilt executable's Markdown/Org tmux workflow and a 20-second
fuzz run passed (217,130 executions including the cached corpus). New regression
fixtures cover core Markdown node families, enabled extensions, Unicode table
alignment, container heading offsets and combined text/link styles. The tmux check
also verifies footnote/definition/table rendering, live refresh, read-only behavior,
source preservation, outline navigation and URL copying. This is not certification
against the full CommonMark/GFM HTML conformance suite; terminal adaptations and
remaining dialect/visual limitations are documented in [USAGE.md](USAGE.md#markdown-and-org-previews).

[Before samples](benchmarks/markdown-before-macos-2026-10-26.txt) and
[after samples](benchmarks/markdown-after-macos-2026-10-26.txt) are retained.
Reproduce with:

```sh
go test ./internal/markup -run '^$' -bench 'BenchmarkRender(Markdown|Org|ExtendedMarkdown)$' -benchmem -count=3
```
