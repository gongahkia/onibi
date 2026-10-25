package editor

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/text"
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
func (a *App) draw() {
	w, h := a.Screen.Size()
	if w < 1 || h < 1 {
		return
	}
	p := a.styles()
	a.Screen.SetStyle(p.text)
	for row := 0; row < h; row++ {
		a.fill(0, row, w, p.text)
	}
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
			fg, _, _ := p.accent.Decompose()
			style = style.Foreground(fg).Bold(true)
		}
		a.put(x, 0, w-x, name, style)
		x += uniseg.StringWidth(name)
		if x >= w {
			break
		}
	}
	a.ensurePanes()
	for i := range a.panes {
		a.drawPane(i, p)
	}
	pos := d.Selections[0].Head
	line := d.Text.LineAt(pos)
	column := columnAt(d.Text, pos, a.Config.TabSize)
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

	if a.Dialog != nil {
		if a.Dialog.Inline {
			a.drawCompletion(p)
		} else {
			a.drawDialog(p)
		}
	}
	if a.Preview != nil {
		a.drawPreview(p)
	}
	a.Screen.Show()
}
func (a *App) drawPane(index int, p palette) {
	pane := &a.panes[index]
	b := pane.Buffer
	d := b.Doc
	r := a.rect(index)
	active := index == a.focus
	selections := append([]text.Selection(nil), pane.Selections...)
	if active {
		selections = append([]text.Selection(nil), d.Selections...)
	}
	sort.Slice(selections, func(i, j int) bool { return selections[i].Start() < selections[j].Start() })
	carets := map[int]bool{}
	for _, s := range selections {
		if s.Start() == s.End() {
			carets[s.Head] = true
		}
	}
	if len(a.panes) > 1 {
		style := p.dim
		if active {
			style = p.accent
		}
		a.put(r.X, r.Y-1, r.W, bufferName(b), style)
	}
	gutter := len(strconv.Itoa(d.Text.Lines())) + 2
	width := max(1, r.W-gutter)
	line, wrapRow := pane.Top, pane.WrapTop
	pane.Rows = nil
	for row := 0; row < r.H && line < d.Text.Lines(); row++ {
		seg := segment{d.Text.LineStart(line), d.Text.LineEnd(line), 0}
		if a.wrap {
			seg = rowSegment(d.Text, line, wrapRow, width, a.Config.TabSize)
		}
		if !a.wrap || wrapRow == 0 {
			a.put(r.X, r.Y+row, gutter-1, fmt.Sprintf("%*d", gutter-2, line+1), p.dim)
		} else {
			a.put(r.X, r.Y+row, gutter-1, "↪", p.dim)
		}
		from, end, col := seg.Start, seg.End, seg.Column
		left := pane.Left
		if a.wrap {
			left = seg.Column
		}
		if d.Text.Plain(from, end) {
			skip := min(end-from, max(0, left-col))
			from += skip
			col += skip
			end = min(end, from+width)
		}
		pane.Rows = append(pane.Rows, screenRow{line, seg.Start, seg.End, left})
		value := d.Text.Slice(from, end)
		g := uniseg.NewGraphemes(value)
		for g.Next() {
			offset, _ := g.Positions()
			offset += from
			cw := g.Width()
			grapheme := g.Str()
			if grapheme == "\t" {
				cw = a.Config.TabSize - col%a.Config.TabSize
			}
			if col+cw <= left {
				col += cw
				continue
			}
			if col-left >= width {
				break
			}
			style := p.text
			if b.SyntaxReady && b.SyntaxVersion == d.Version {
				si := sort.Search(len(b.Spans), func(i int) bool { return b.Spans[i].End > offset })
				if si < len(b.Spans) && b.Spans[si].Start <= offset {
					style = style.Foreground(b.Spans[si].Color)
				}
			}
			si := sort.Search(len(selections), func(i int) bool { return selections[i].End() > offset })
			if si < len(selections) && selections[si].Start() <= offset {
				style = style.Background(p.selectionBackground())
			}
			if carets[offset] && len(selections) > 1 {
				style = style.Reverse(true)
			}
			if a.matchBuffer == b && a.matchVersion == d.Version {
				mi := sort.Search(len(a.Matches), func(i int) bool { return a.Matches[i].End > offset })
				if mi < len(a.Matches) && a.Matches[mi].Start <= offset {
					style = style.Underline(true)
				}
			}
			for _, diag := range b.Diagnostics {
				if diag.Range.Start.Line == line && b.DiagnosticVersion == d.Version {
					style = style.Underline(true)
					break
				}
			}
			x := r.X + gutter + col - left
			if grapheme == "\t" {
				for k := 0; k < cw; k++ {
					if x+k >= r.X+gutter && x+k < r.X+r.W {
						a.Screen.SetContent(x+k, r.Y+row, ' ', nil, style)
					}
				}
			} else if x >= r.X+gutter && x+cw <= r.X+r.W {
				runes := g.Runes()
				if len(runes) > 0 && unicode.IsControl(runes[0]) {
					grapheme = "·"
				}
				a.Screen.Put(x, r.Y+row, grapheme, style)
			}
			col += cw
		}
		if active && (a.Dialog == nil || a.Dialog.Inline) && a.Preview == nil {
			pos := d.Selections[0].Head
			last := seg.End == d.Text.LineEnd(line)
			if pos >= seg.Start && (pos < seg.End || (last && pos == seg.End)) && d.Text.LineAt(pos) == line {
				cx := columnAt(d.Text, pos, a.Config.TabSize) - left
				if cx >= 0 && cx < width {
					a.Screen.ShowCursor(r.X+gutter+cx, r.Y+row)
				}
			}
		}
		if a.wrap && wrapRow+1 < rowCount(d.Text, line, width, a.Config.TabSize) {
			wrapRow++
		} else {
			line++
			wrapRow = 0
		}
	}
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
	if v.Detail {
		half := (w - 3) / 2
		a.put(x+1, y+1, half, "Before", p.warning)
		a.put(x+half+2, y+1, half, "After", p.accent)
		for row := 0; row < h-4; row++ {
			line := v.DetailTop + row
			if line < len(v.Before) {
				value := strings.TrimSuffix(v.Before[line], "\r")
				cut := offsetAt(value, v.DetailLeft, a.Config.TabSize)
				a.put(x+1, y+2+row, half, value[cut:], p.warning)
			}
			if line < len(v.After) {
				value := strings.TrimSuffix(v.After[line], "\r")
				cut := offsetAt(value, v.DetailLeft, a.Config.TabSize)
				a.put(x+half+2, y+2+row, half, value[cut:], p.accent)
			}
		}
		a.put(x+1, y+h-1, w-2, "↑↓ scroll · ←→ pan · Tab list · Enter apply · Esc cancel", p.dim)
		a.Screen.HideCursor()
		return
	}

	visible := max(1, (h-3)/3)
	start := max(0, v.Index-visible+1)
	for i := start; i < len(v.Edits) && i < start+visible; i++ {
		e := v.Edits[i]
		b := v.bufferAt(i)
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
		a.put(x+1, row, w-2, fmt.Sprintf("%s %s:%d", mark, bufferName(b), v.documentAt(i).Text.LineAt(e.Start)+1), style)
		a.put(x+2, row+1, w-4, "− "+displayExcerpt(v.documentAt(i).Text.Slice(e.Start, min(e.End, e.Start+200))), p.warning)
		a.put(x+2, row+2, w-4, "+ "+displayExcerpt(e.Text), p.accent)
	}
	a.put(x+1, y+h-1, w-2, "Space toggle · Tab inspect · Enter apply · Esc cancel", p.dim)
	a.Screen.HideCursor()
}
