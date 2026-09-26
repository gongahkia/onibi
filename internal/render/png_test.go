package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderPNGBuiltinFontsDecodeUnicode(t *testing.T) {
	for _, choice := range BuiltinFonts() {
		t.Run(choice.ID, func(t *testing.T) {
			out, err := RenderPNG([]byte("╭─ Onibi  ─╮\n│ ✓ complete │\n╰────────────╯"), PNGOptions{Rows: 4, Cols: 24, Scale: 1, Font: choice.ID})
			if err != nil {
				t.Fatal(err)
			}
			img, err := png.Decode(bytes.NewReader(out))
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 || countChanged(img) == 0 {
				t.Fatal("invalid or blank image")
			}
		})
	}
}

func TestRenderPNGCustomFont(t *testing.T) {
	path := filepath.Join(t.TempDir(), "font.ttf")
	if err := os.WriteFile(path, jetBrainsMonoNerdTTF, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RenderPNG([]byte("custom"), PNGOptions{Rows: 2, Cols: 12, Scale: 1, Font: FontCustom, FontPath: path}); err != nil {
		t.Fatal(err)
	}
}

func TestScreenLinesSanitizesTerminalControls(t *testing.T) {
	lines := screenLines([]byte("safe\x1b]52;c;clipboard\x07\x00\ttext\n\u202eevil"), 2, 20)
	got := strings.Join(lines, "\n")
	if strings.ContainsAny(got, "\x1b\x00\u202e") || strings.Contains(got, "clipboard") {
		t.Fatalf("controls leaked: %q", got)
	}
	if !strings.Contains(got, "safe    text") || !strings.Contains(got, "evil") {
		t.Fatalf("visible text missing: %q", got)
	}
}

func TestScreenLinesKeepsLastRowsAndClipsColumns(t *testing.T) {
	lines := screenLines([]byte("one\ntwo\nthree"), 2, 3)
	if strings.Join(lines, "|") != "two|thr" {
		t.Fatalf("lines=%q", lines)
	}
}

func TestScreenCellsPreserveSGRColors(t *testing.T) {
	lines := screenCells([]byte("\x1b[31mred\x1b[0m \x1b[38;2;1;2;3mrgb\x1b[48;5;27m!"), 1, 16)
	line := lines[0]
	if got := line[0].fg; got != ansiColor(1) {
		t.Fatalf("red foreground=%#v", got)
	}
	if got := line[4].fg; got != (color.RGBA{R: 1, G: 2, B: 3, A: 255}) {
		t.Fatalf("rgb foreground=%#v", got)
	}
	wantBG, _ := ansi256Color(27)
	if got := line[7].bg; got != wantBG {
		t.Fatalf("indexed background=%#v", got)
	}
}

func TestScreenCellsIgnoreUnsafeTerminalSequences(t *testing.T) {
	lines := screenCells([]byte("safe\x1b]52;c;clipboard\x07\x1b[31mred\x1b[0m\u202eevil"), 1, 24)
	var b strings.Builder
	for _, cell := range lines[0] {
		b.WriteRune(cell.r)
	}
	if got := b.String(); got != "saferedevil" {
		t.Fatalf("text=%q", got)
	}
}

func TestTerminalTracksGraphemesStylesAndCursor(t *testing.T) {
	screen := parseTerminal([]byte("A界e\u0301\x1b[1;3;4;9mB\x1b]8;;https://example.test\x07C"), 1, 8)
	line := screen.cells[0]
	if line[1].text != "界" || line[1].width != 2 || !line[2].continuation {
		t.Fatalf("wide glyph=%+v", line[1:3])
	}
	if line[3].text != "e\u0301" || line[3].width != 1 {
		t.Fatalf("combining glyph=%+v", line[3])
	}
	if !line[4].bold || !line[4].italic || !line[4].underline || !line[4].strike {
		t.Fatalf("style=%+v", line[4])
	}
	if line[5].hyperlink != "https://example.test" || screen.cursorCol != 6 || !screen.cursorVisible {
		t.Fatalf("link/cursor=%+v cursor=%d", line[5], screen.cursorCol)
	}
}

func TestTerminalCSIAlternateBufferAndCursorVisibility(t *testing.T) {
	screen := parseTerminal([]byte("main\x1b[?1049halt\x1b[?1049l\x1b[?25l"), 1, 8)
	if got := screenLines([]byte("main\x1b[?1049halt\x1b[?1049l"), 1, 8)[0]; got != "main" {
		t.Fatalf("primary buffer=%q", got)
	}
	if screen.cursorVisible {
		t.Fatal("cursor remained visible")
	}
	screen = parseTerminal([]byte("abcdef\rX\x1b[K"), 1, 8)
	if got := terminalText(screen.cells[0]); got != "X" {
		t.Fatalf("erase line=%q", got)
	}
}

func terminalText(line []screenCell) string {
	var out strings.Builder
	for _, cell := range line {
		if cell.r != 0 && !cell.continuation {
			out.WriteString(cell.text)
		}
	}
	return out.String()
}

func countChanged(img image.Image) int {
	b := img.Bounds()
	base := img.At(b.Min.X, b.Min.Y)
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.At(x, y) != base {
				n++
			}
		}
	}
	return n
}
