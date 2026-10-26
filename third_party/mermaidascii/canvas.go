package mermaidascii

import "strings"

// canvas is a rectangular field of characters, addressed column first:
// c.at(x, y) is column x, row y.
//
// Column-major looks odd in Go, where a row of text is the natural unit, but
// it is what the drawing code wants: an edge running downwards touches one
// column and many rows, and every routine that draws lines is written that
// way. Only String reassembles rows.
type canvas struct {
	cells [][]rune
	w, h  int
}

// newCanvas returns a canvas of w columns and h rows, filled with spaces.
func newCanvas(w, h int) *canvas {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	c := &canvas{cells: make([][]rune, w), w: w, h: h}
	for x := range c.cells {
		col := make([]rune, h)
		for y := range col {
			col[y] = ' '
		}
		c.cells[x] = col
	}
	return c
}

// set writes r at (x, y). Coordinates outside the canvas are dropped, which
// keeps every caller from repeating the same bounds check.
func (c *canvas) set(x, y int, r rune) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	c.cells[x][y] = r
}

// at reads the character at (x, y), or a space outside the canvas.
func (c *canvas) at(x, y int) rune {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return ' '
	}
	return c.cells[x][y]
}

// draw copies other onto c with its top left corner at (x, y).
//
// Spaces in other are transparent: a box drawn over a line leaves the line
// visible where the box has no ink of its own.
func (c *canvas) draw(other *canvas, x, y int) {
	for ox := 0; ox < other.w; ox++ {
		for oy := 0; oy < other.h; oy++ {
			if r := other.cells[ox][oy]; r != ' ' {
				c.set(x+ox, y+oy, r)
			}
		}
	}
}

// text writes s starting at (x, y), left to right.
func (c *canvas) text(x, y int, s string) {
	for i, r := range []rune(s) {
		c.set(x+i, y, r)
	}
}

// textIfFree writes s at (x, y), but only if every cell it would cover is
// still blank. A label that overwrote a border or a line would destroy the
// very thing it describes, so it is dropped whole rather than written in
// part.
func (c *canvas) textIfFree(x, y int, s string) bool {
	runes := []rune(s)
	if y < 0 || y >= c.h || x < 0 || x+len(runes) > c.w {
		return false
	}
	for i := range runes {
		if c.at(x+i, y) != ' ' {
			return false
		}
	}
	c.text(x, y, s)
	return true
}

// String renders the canvas as lines, trailing spaces removed.
//
// The trailing spaces are an artefact of a fixed-size field, not part of the
// picture; leaving them in makes every diff and every golden comparison noisy.
func (c *canvas) String() string {
	var b strings.Builder
	for y := 0; y < c.h; y++ {
		row := make([]rune, c.w)
		for x := 0; x < c.w; x++ {
			row[x] = c.cells[x][y]
		}
		b.WriteString(strings.TrimRight(string(row), " "))
		b.WriteByte('\n')
	}
	return b.String()
}
