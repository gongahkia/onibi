# Third-party components

Shack directly uses tcell v2 (terminal handling), Chroma v2 (syntax lexers), uniseg (Unicode grapheme segmentation), creack/pty (PTY lifecycle), Charm
vt/ultraviolet/ansi (terminal emulation and input), regexp2 (snippet transforms), Goldmark and go-org (document parsing), and x/sys (OS interfaces). Transitive runtime dependencies are pinned
in go.mod and go.sum. They are compiled into the executable.

Release archives include THIRD_PARTY_LICENSES.txt with the license and notice
files from modules used by the build. The Go runtime is distributed under the Go
BSD-style license, included in that file as well. Shack's MIT license does not
replace these upstream licenses.
