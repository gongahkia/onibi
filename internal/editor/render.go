package editor

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type palette struct{ text, dim, bar, accent, selection, warning tcell.Style }

func (a *App) styles() palette {
	fg, bg, dim, bar, accent, selected := tcell.ColorWhite, tcell.NewHexColor(0x141a22), tcell.NewHexColor(0x8c9aab), tcell.NewHexColor(0x263242), tcell.NewHexColor(0x76d6b5), tcell.NewHexColor(0x35536e)
	if a.Config.Theme == "light" {
		fg = tcell.NewHexColor(0x243044)
		bg = tcell.NewHexColor(0xf4f5f7)
		dim = tcell.NewHexColor(0x526176)
		bar = tcell.NewHexColor(0xe0e5ed)
		accent = tcell.NewHexColor(0x08775c)
		selected = tcell.NewHexColor(0xc0dbf4)
	}
	base := tcell.StyleDefault.Foreground(fg).Background(bg)
	return palette{base, base.Foreground(dim), base.Background(bar), base.Foreground(accent), base.Background(selected), base.Foreground(tcell.ColorOrange)}
}
func (a *App) put(x, y, width int, value string, style tcell.Style) {
	if width <= 0 {
		return
	}
	used := 0
	g := uniseg.NewGraphemes(value)
	for g.Next() {
		s := g.Str()
		w := g.Width()
		r := g.Runes()
		if len(r) == 0 {
			continue
		}
		if unicode.IsControl(r[0]) {
			s = " "
			w = 1
		}
		if used+w > width {
			break
		}
		a.Screen.Put(x+used, y, s, style)
		used += w
	}
}
func (a *App) fill(x, y, width int, style tcell.Style) {
	for i := 0; i < width; i++ {
		a.Screen.SetContent(x+i, y, ' ', nil, style)
	}
}
func (a *App) gutter() int { return len(strconv.Itoa(a.Current().Doc.Text.Lines())) + 2 }
func (a *App) reveal() {
	b := a.Current()
	_, h := a.Screen.Size()
	height := max(1, h-3)
	p := b.Doc.Selections[0].Head
	line := b.Doc.Text.LineAt(p)
	if line < b.Top {
		b.Top = line
	}
	if line >= b.Top+height {
		b.Top = line - height + 1
	}
	w, _ := a.Screen.Size()
	column := visualColumn(b.Doc.Text.Slice(b.Doc.Text.LineStart(line), p), a.Config.TabSize)
	width := max(1, w-a.gutter())
	if column < b.Left {
		b.Left = column
	}
	if column >= b.Left+width {
		b.Left = column - width + 1
	}
}
func (a *App) draw() {
	w, h := a.Screen.Size()
	if w < 1 || h < 1 {
		return
	}
	p := a.styles()
	a.Screen.SetStyle(p.text)
	a.Screen.Clear()
	a.Screen.HideCursor()
	b := a.Current()
	d := b.Doc
	if w < 20 || h < 5 {
		a.put(0, 0, w, "Shack · enlarge terminal", p.text)
		a.Screen.Show()
		return
	}
	// Keep keyboard edits visible without forcing a mouse-scrolled viewport back.
	a.fill(0, 0, w, p.bar)
	x := 0
	for i, buf := range a.Buffers {
		name := " " + bufferName(buf)
		if buf.Doc.Dirty() {
			name += " *"
		}
		name += " "
		style := p.bar
		if i == a.Active {
			style = style.Foreground(tcell.ColorAqua).Bold(true)
		}
		a.put(x, 0, w-x, name, style)
		x += uniseg.StringWidth(name)
		if x >= w {
			break
		}
	}
	gutter := a.gutter()
	height := h - 3
	for row := 0; row < height; row++ {
		line := b.Top + row
		if line >= d.Text.Lines() {
			break
		}
		a.put(0, row+1, gutter-1, fmt.Sprintf("%*d", gutter-2, line+1), p.dim)
		value := d.Text.Line(line)
		start := d.Text.LineStart(line)
		col := 0
		g := uniseg.NewGraphemes(value)
		for g.Next() {
			from, _ := g.Positions()
			cellWidth := g.Width()
			grapheme := g.Str()
			if grapheme == "\t" {
				cellWidth = a.Config.TabSize - col%a.Config.TabSize
			}
			if col+cellWidth <= b.Left {
				col += cellWidth
				continue
			}
			if col-b.Left >= w-gutter {
				break
			}
			style := p.text
			offset := start + from
			if b.SyntaxReady && b.SyntaxVersion == d.Version {
				index := sort.Search(len(b.Spans), func(i int) bool { return b.Spans[i].End > offset })
				if index < len(b.Spans) && b.Spans[index].Start <= offset {
					style = style.Foreground(b.Spans[index].Color)
				}
			}
			for _, sel := range d.Selections {
				if offset >= sel.Start() && offset < sel.End() {
					style = style.Background(p.selectionBackground())
				}
			}
			if a.matchBuffer == b && a.matchVersion == d.Version {
				index := sort.Search(len(a.Matches), func(i int) bool { return a.Matches[i].End > offset })
				if index < len(a.Matches) && a.Matches[index].Start <= offset {
					style = style.Underline(true)
				}
			}
			screenX := gutter + col - b.Left
			if grapheme == "\t" {
				for j := 0; j < cellWidth; j++ {
					if screenX+j >= gutter && screenX+j < w {
						a.Screen.SetContent(screenX+j, row+1, ' ', nil, style)
					}
				}
			} else if screenX >= gutter && screenX+cellWidth <= w {
				r := g.Runes()
				if len(r) > 0 && unicode.IsControl(r[0]) {
					grapheme = "·"
				}
				a.Screen.Put(screenX, row+1, grapheme, style)
			}
			col += cellWidth
		}
	}
	pos := d.Selections[0].Head
	line := d.Text.LineAt(pos)
	column := visualColumn(d.Text.Slice(d.Text.LineStart(line), pos), a.Config.TabSize)
	state := fmt.Sprintf(" Shack  %d:%d  %d selection(s)", line+1, column+1, len(d.Selections))
	if len(b.Diagnostics) > 0 {
		state += fmt.Sprintf("  %d diagnostics", len(b.Diagnostics))
	}
	if a.Readonly {
		state += "  READ ONLY"
	}
	if d.Text.Len() > a.Config.ServiceMaxBytes {
		state += "  LARGE FILE"
	}
	a.fill(0, h-2, w, p.bar)
	a.put(0, h-2, w, state, p.bar)
	a.put(0, h-1, w, a.Status, p.dim)
	if a.Dialog == nil && a.Preview == nil && line >= b.Top && line < b.Top+height && column >= b.Left && column-b.Left < w-gutter {
		a.Screen.ShowCursor(gutter+column-b.Left, line-b.Top+1)
	}
	if a.Dialog != nil {
		a.drawDialog(p)
	}
	if a.Preview != nil {
		a.drawPreview(p)
	}
	a.Screen.Show()
}
func (p palette) selectionBackground() tcell.Color { _, bg, _ := p.selection.Decompose(); return bg }
func (a *App) panel(title string, p palette) (int, int, int, int) {
	w, h := a.Screen.Size()
	width := min(90, w-4)
	height := min(20, h-4)
	x := (w - width) / 2
	y := (h - height) / 2
	for row := 0; row < height; row++ {
		a.fill(x, y+row, width, p.bar)
	}
	a.put(x+1, y, width-2, title, p.accent.Bold(true))
	return x, y, width, height
}
func (a *App) drawDialog(p palette) {
	d := a.Dialog
	x, y, w, h := a.panel(d.Title, p)
	a.fill(x+1, y+2, w-2, p.text)
	// Prompts scroll horizontally with the caret.
	input := []rune(d.Input)
	before := string(input[:d.Cursor])
	shift := max(0, uniseg.StringWidth(before)-(w-5))
	cut := offsetAt(d.Input, shift, a.Config.TabSize)
	a.put(x+2, y+2, w-4, d.Input[cut:], p.text)
	cursor := uniseg.StringWidth(before) - shift
	a.Screen.ShowCursor(min(x+w-2, x+2+cursor), y+2)
	if d.Items != nil {
		items := a.filtered()
		d.Index = max(0, min(d.Index, len(items)-1))
		visible := max(0, h-5)
		start := max(0, d.Index-visible+1)
		for i := start; i < len(items) && i < start+visible; i++ {
			style := p.bar
			if i == d.Index {
				style = p.selection
			}
			a.fill(x+1, y+4+i-start, w-2, style)
			a.put(x+2, y+4+i-start, w-4, items[i].Label, style)
		}
		if len(items) == 0 {
			a.put(x+2, y+4, w-4, "No matches", p.dim)
		}
	}
	a.put(x+1, y+h-1, w-2, "Enter choose · Esc cancel · Ctrl+U clear", p.dim)
}
func displayExcerpt(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ↵ ")
	s = strings.ReplaceAll(s, "\n", " ↵ ")
	s = strings.ReplaceAll(s, "\t", " → ")
	return s
}
func (a *App) drawPreview(p palette) {
	v := a.Preview
	count := 0
	for _, checked := range v.Checked {
		if checked {
			count++
		}
	}
	x, y, w, h := a.panel(fmt.Sprintf("%s · %d/%d changes", v.Title, count, len(v.Edits)), p)
	visible := max(1, (h-3)/3)
	start := max(0, v.Index-visible+1)
	for i := start; i < len(v.Edits) && i < start+visible; i++ {
		e := v.Edits[i]
		row := y + 1 + (i-start)*3
		style := p.bar
		if i == v.Index {
			style = p.selection
		}
		mark := "[ ]"
		if v.Checked[i] {
			mark = "[x]"
		}
		a.fill(x+1, row, w-2, style)
		a.put(x+1, row, w-2, fmt.Sprintf("%s line %d", mark, v.Buffer.Doc.Text.LineAt(e.Start)+1), style)
		a.put(x+2, row+1, w-4, "− "+displayExcerpt(v.Buffer.Doc.Text.Slice(e.Start, min(e.End, e.Start+200))), p.warning)
		a.put(x+2, row+2, w-4, "+ "+displayExcerpt(e.Text), p.accent)
	}
	a.put(x+1, y+h-1, w-2, "Space toggle · A all · Enter apply · Esc cancel", p.dim)
	a.Screen.HideCursor()
}
