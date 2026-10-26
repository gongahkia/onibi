package mermaidascii

import "testing"

func TestDrawNodeSingleLetter(t *testing.T) {
	b := measure("A", 1)
	if b.w != 5 || b.h != 5 {
		t.Fatalf("measure = %dx%d, want 5x5", b.w, b.h)
	}
	got := drawNode("A", Rectangle, b, unicodeSet, false).String()
	want := "┌───┐\n│   │\n│ A │\n│   │\n└───┘\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDrawNodeASCIIMode(t *testing.T) {
	got := drawNode("A", Rectangle, measure("A", 1), asciiSet, true).String()
	want := "+---+\n|   |\n| A |\n|   |\n+---+\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestDrawNodeShapeCorners(t *testing.T) {
	for _, tc := range []struct {
		shape Shape
		tl    rune
	}{
		{Rectangle, '┌'},
		{Rounded, '╭'},
		{Diamond, '◇'},
		{Hexagon, '⌜'},
		{Stadium, '('},
	} {
		c := drawNode("X", tc.shape, measure("X", 1), unicodeSet, false)
		if got := c.at(0, 0); got != tc.tl {
			t.Errorf("shape %v: top left = %q, want %q", tc.shape, got, tc.tl)
		}
	}
}

// TestMeasureUnicodeLabel guards against counting bytes instead of runes —
// "Größe" is five characters but seven bytes.
func TestMeasureUnicodeLabel(t *testing.T) {
	if got := labelWidth("Größe"); got != 5 {
		t.Errorf("labelWidth = %d, want 5", got)
	}
	if got := measure("Größe", 1); got.w != 9 {
		t.Errorf("width = %d, want 9", got.w)
	}
}

func TestMeasureMultiline(t *testing.T) {
	b := measure("one\ntwo", 1)
	// Two lines plus padding is four, which is even, so it grows to five.
	if b.h != 7 {
		t.Errorf("height = %d, want 7", b.h)
	}
	// "one" is three wide, plus padding on both sides, plus two borders.
	if b.w != 7 {
		t.Errorf("width = %d, want 7", b.w)
	}
}
