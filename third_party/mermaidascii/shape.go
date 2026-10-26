package mermaidascii

import (
	"strings"
	"unicode/utf8"
)

// box is the drawn size of a node.
type box struct {
	w, h int
}

// labelLines splits a label at its line breaks.
func labelLines(label string) []string {
	if label == "" {
		return []string{""}
	}
	return strings.Split(label, "\n")
}

// labelWidth is the width of the widest line, counted in characters.
//
// Counted in runes, not bytes, so that "Größe" is five wide. Characters that
// occupy two columns in a terminal — CJK, most emoji — still count as one,
// which draws their boxes too narrow. Fixing that needs an East Asian width
// table; see docs/DEVIATIONS.md.
func labelWidth(label string) int {
	max := 0
	for _, l := range labelLines(label) {
		if n := utf8.RuneCountInString(l); n > max {
			max = n
		}
	}
	return max
}

// measure returns the outer size of a node's box.
//
// The inner height is forced to an odd number so that a single line of text
// sits exactly in the middle, with the same number of blank rows above and
// below. That middle row is also where horizontal edges attach.
func measure(label string, padding int) box {
	inner := 2*padding + labelWidth(label)
	innerH := len(labelLines(label)) + 2*padding
	if innerH%2 == 0 {
		innerH++
	}
	return box{w: inner + 2, h: innerH + 2}
}

// drawNode renders one node onto its own canvas.
func drawNode(label string, shape Shape, b box, cs charset, useASCII bool) *canvas {
	c := newCanvas(b.w, b.h)
	corner := cornersFor(shape, useASCII)

	right, bottom := b.w-1, b.h-1
	for x := 1; x < right; x++ {
		c.set(x, 0, cs.horizontal)
		c.set(x, bottom, cs.horizontal)
	}
	for y := 1; y < bottom; y++ {
		c.set(0, y, cs.vertical)
		c.set(right, y, cs.vertical)
	}
	c.set(0, 0, corner.tl)
	c.set(right, 0, corner.tr)
	c.set(0, bottom, corner.bl)
	c.set(right, bottom, corner.br)

	// Centre the label block vertically, then each line horizontally.
	lines := labelLines(label)
	startY := bottom/2 - (len(lines)-1)/2
	for i, line := range lines {
		n := utf8.RuneCountInString(line)
		x := right/2 - (n+1)/2 + 1
		c.text(x, startY+i, line)
	}
	return c
}
