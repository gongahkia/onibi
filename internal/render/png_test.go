package render

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/jaguilar/vt100"
)

func TestRenderPNGDecodesAndIsNonBlank(t *testing.T) {
	out, err := RenderPNG([]byte("\x1b[31mERR\x1b[0m ok"), PNGOptions{Rows: 4, Cols: 12, Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
		t.Fatalf("empty bounds: %v", img.Bounds())
	}
	if countChanged(img) == 0 {
		t.Fatal("png is blank")
	}
}

func TestRenderPNGSupports256ColorSGR(t *testing.T) {
	out, err := RenderPNG([]byte("\x1b[38;5;196mX"), PNGOptions{Rows: 2, Cols: 2, Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if countRed(img) == 0 {
		t.Fatal("expected red glyph pixels")
	}
}

func TestRenderPNGDrawsActiveBorder(t *testing.T) {
	out, err := RenderPNG([]byte("ok"), PNGOptions{Rows: 2, Cols: 4, Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if !sameColor(img.At(b.Min.X, b.Min.Y), activeBorder) {
		t.Fatalf("top-left border = %#v", img.At(b.Min.X, b.Min.Y))
	}
	if !sameColor(img.At(b.Max.X-1, b.Max.Y-1), activeBorder) {
		t.Fatalf("bottom-right border = %#v", img.At(b.Max.X-1, b.Max.Y-1))
	}
}

func TestRenderPNGClampsCursorAfterBottomLinefeed(t *testing.T) {
	term := vt100.NewVT100(24, 80)
	replay(term, []byte("\x1b[24;1H\nX"))
	if term.Content[23][0] != 'X' {
		t.Fatalf("bottom row = %q", string(term.Content[23][:1]))
	}
	if _, err := RenderPNG([]byte("\x1b[24;1H\nX"), PNGOptions{Rows: 24, Cols: 80, Scale: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestReplayScrollsPlainOutput(t *testing.T) {
	term := vt100.NewVT100(3, 12)
	replay(term, []byte("one\ntwo\nthree\nfour"))
	if rowText(term, 0)[:3] != "two" || rowText(term, 1)[:5] != "three" || rowText(term, 2)[:4] != "four" {
		t.Fatalf("rows = %q | %q | %q", rowText(term, 0), rowText(term, 1), rowText(term, 2))
	}
}

func TestDetectMode(t *testing.T) {
	if got := DetectMode([]byte("npm test\nok\n")); got != ModeText {
		t.Fatalf("plain mode = %s", got)
	}
	if got := DetectMode([]byte("\x1b[?1049h\x1b[Hmenu")); got != ModePNG {
		t.Fatalf("altscreen mode = %s", got)
	}
	if got := DetectMode([]byte("\x1b[2;3Hone\x1b[3;4Htwo")); got != ModePNG {
		t.Fatalf("cursor mode = %s", got)
	}
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

func countRed(img image.Image) int {
	b := img.Bounds()
	n := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r > 50000 && g < 20000 && bl < 20000 {
				n++
			}
		}
	}
	return n
}

func sameColor(got color.Color, want color.RGBA) bool {
	r, g, b, a := got.RGBA()
	return uint8(r>>8) == want.R && uint8(g>>8) == want.G && uint8(b>>8) == want.B && uint8(a>>8) == want.A
}

func rowText(v *vt100.VT100, y int) string {
	return string(v.Content[y])
}
