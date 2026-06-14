package render

import (
	"bytes"
	"image"
	"image/png"
	"testing"
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

func TestRenderPNGClampsCursorAfterBottomLinefeed(t *testing.T) {
	if _, err := RenderPNG([]byte("\x1b[24;1H\nX"), PNGOptions{Rows: 24, Cols: 80, Scale: 1}); err != nil {
		t.Fatal(err)
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
