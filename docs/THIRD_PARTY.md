# Third-party components

Shack directly uses tcell v2 (terminal handling), Chroma v2 (syntax lexers), uniseg (Unicode grapheme segmentation), creack/pty (PTY lifecycle), Charm
vt/ultraviolet/ansi (terminal emulation and input), regexp2 (snippet transforms), Goldmark and go-org (document parsing), mermaidascii v0.2.0 (terminal Mermaid layout), and x/sys (OS interfaces). Transitive runtime dependencies are pinned
in go.mod and go.sum. They are compiled into the executable.

mermaidascii uses a pinned local copy in `third_party/mermaidascii`, with a
bounded edge-routing search to prevent enclosed targets from causing unbounded
work. Upstream tests, fixtures and license/notice files are retained. See
[the patch notes](../third_party/mermaidascii/SHACK_PATCH.md) for provenance,
changes and validation; `make check` includes this module's tests.

Release archives include THIRD_PARTY_LICENSES.txt with the license and notice
files from modules used by the build. The Go runtime is distributed under the Go
BSD-style license, included in that file as well. These notices apply to the
bundled third-party components.
