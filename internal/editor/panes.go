package editor

import (
	"fmt"
	"github.com/gongahkia/shack/internal/text"
	"github.com/rivo/uniseg"
	"sort"
	"strconv"
)

type pane struct {
	Buffer             *Buffer
	Top, Left, WrapTop int
	Selections         []text.Selection
	Version            uint64
	Rows               []screenRow
}
type rectangle struct{ X, Y, W, H int }
type screenRow struct{ Line, Start, End, Column int }
type segment struct{ Start, End, Column int }

func (a *App) ensurePanes() {
	if len(a.panes) == 0 {
		a.panes = []pane{{Buffer: a.Current()}}
	}
	if a.layout == nil {
		a.layout = legacyLayout(len(a.panes), a.horizontal)
	}
	a.savePane()
}
func (a *App) savePane() {
	if len(a.panes) == 0 {
		return
	}
	b := a.Current()
	p := &a.panes[a.focus]
	p.Buffer = b
	p.Top = b.Top
	p.Left = b.Left
	p.WrapTop = b.WrapTop
	p.Version = b.Doc.Version
	p.Selections = append([]text.Selection(nil), b.Doc.Selections...)
}
func (a *App) focusPane(index int) {
	a.treeFocus = false
	a.ensurePanes()
	a.savePane()
	a.focus = (index + len(a.panes)) % len(a.panes)
	p := &a.panes[a.focus]
	found := false
	for i, b := range a.Buffers {
		if b == p.Buffer {
			a.Active = i
			found = true
			break
		}
	}
	if !found {
		p.Buffer = a.Current()
	}
	b := a.Current()
	b.Top = p.Top
	b.Left = p.Left
	b.WrapTop = p.WrapTop
	if len(p.Selections) > 0 {
		b.Doc.Selections = append([]text.Selection(nil), p.Selections...)
	}
	a.clampSelections(b)
	a.typingDoc = nil
	a.Matches = nil
	a.schedule()
	a.reveal()
}
func (a *App) clampSelections(b *Buffer) {
	for i, s := range b.Doc.Selections {
		s.Anchor = max(0, min(s.Anchor, b.Doc.Text.Len()))
		s.Head = max(0, min(s.Head, b.Doc.Text.Len()))
		for !b.Doc.Text.Boundary(s.Anchor) {
			s.Anchor--
		}
		for !b.Doc.Text.Boundary(s.Head) {
			s.Head--
		}
		b.Doc.Selections[i] = s
	}
}
func (a *App) split(horizontal bool) {
	a.ensurePanes()
	if len(a.panes) >= maxPanes {
		a.Status = "Up to sixteen panes are supported"
		return
	}
	r := a.rect(a.focus)
	if (horizontal && r.H < 7) || (!horizontal && r.W < 28) {
		a.Status = "Enlarge this pane before splitting it"
		return
	}
	node := a.layout.find(a.focus)
	if node == nil {
		return
	}
	*node = splitNode{Horizontal: horizontal, Ratio: 500, First: &splitNode{Pane: a.focus}, Second: &splitNode{Pane: len(a.panes)}}
	p := a.panes[a.focus]
	p.Selections = append([]text.Selection(nil), p.Selections...)
	p.Rows = nil
	a.panes = append(a.panes, p)
	a.focusPane(len(a.panes) - 1)
}
func (a *App) closePane() {
	if len(a.panes) <= 1 {
		a.Status = "This is the only pane"
		return
	}
	a.savePane()
	index := a.focus
	a.layout = a.layout.remove(index)
	a.panes = append(a.panes[:index], a.panes[index+1:]...)
	a.focus = min(index, len(a.panes)-1)
	p := a.panes[a.focus]
	a.panes[a.focus] = p
	for i, b := range a.Buffers {
		if b == p.Buffer {
			a.Active = i
			break
		}
	}
	a.Current().Top = p.Top
	a.Current().Left = p.Left
	a.Current().WrapTop = p.WrapTop
	a.Current().Doc.Selections = append([]text.Selection(nil), p.Selections...)
	a.reveal()
}
func (a *App) rebasePanes() {
	b := a.Current()
	for i := range a.panes {
		p := &a.panes[i]
		if i == a.focus || p.Buffer != b || p.Version == b.Doc.Version {
			continue
		}
		for _, edits := range b.Doc.LastChanges {
			for j, s := range p.Selections {
				s.Anchor = text.Rebase(s.Anchor, edits)
				s.Head = text.Rebase(s.Head, edits)
				p.Selections[j] = s
			}
		}
		p.Version = b.Doc.Version
	}
	a.savePane()
}
func segments(r text.Rope, line, width, tab int) []segment {
	start, end := r.LineStart(line), r.LineEnd(line)
	width = max(1, width)
	value := r.Slice(start, end)
	g := uniseg.NewGraphemes(value)
	out := []segment{}
	rowStart, base, col := 0, 0, 0
	for g.Next() {
		from, to := g.Positions()
		cw := g.Width()
		if g.Str() == "\t" {
			cw = tab - col%tab
		}
		if col-base+cw > width && from > rowStart {
			out = append(out, segment{start + rowStart, start + from, base})
			rowStart = from
			base = col
		}
		col += cw
		if col-base >= width {
			out = append(out, segment{start + rowStart, start + to, base})
			rowStart = to
			base = col
		}
	}
	return append(out, segment{start + rowStart, end, base})
}
func rowCount(r text.Rope, line, width, tab int) int {
	start, end := r.LineStart(line), r.LineEnd(line)
	if r.Plain(start, end) {
		return (end-start)/max(1, width) + 1
	}
	return len(segments(r, line, width, tab))
}
func rowSegment(r text.Rope, line, row, width, tab int) segment {
	start, end := r.LineStart(line), r.LineEnd(line)
	if r.Plain(start, end) {
		from := min(end, start+row*max(1, width))
		return segment{from, min(end, from+max(1, width)), row * max(1, width)}
	}
	s := segments(r, line, width, tab)
	return s[min(max(0, row), len(s)-1)]
}
func rowAt(r text.Rope, line, offset, width, tab int) int {
	start, end := r.LineStart(line), r.LineEnd(line)
	if r.Plain(start, end) {
		return (offset - start) / max(1, width)
	}
	s := segments(r, line, width, tab)
	return max(0, sort.Search(len(s), func(i int) bool { return s[i].Start > offset })-1)
}
func (a *App) contentWidth(b *Buffer, r rectangle) int {
	return max(1, r.W-len(strconv.Itoa(b.Doc.Text.Lines()))-2)
}
func (a *App) reveal() {
	b := a.Current()
	if b.Terminal != nil {
		return
	}
	r := a.rect(a.focus)
	p := b.Doc.Selections[0].Head
	line := b.Doc.Text.LineAt(p)
	width := a.contentWidth(b, r)
	column := columnAt(b.Doc.Text, p, a.Config.TabSize)
	if !a.wrap {
		if line < b.Top {
			b.Top = line
		}
		if line >= b.Top+r.H {
			b.Top = line - r.H + 1
		}
		if column < b.Left {
			b.Left = column
		}
		if column >= b.Left+width {
			b.Left = column - width + 1
		}
		return
	}
	b.Left = 0
	row := rowAt(b.Doc.Text, line, p, width, a.Config.TabSize)
	if line < b.Top || (line == b.Top && row < b.WrapTop) {
		b.Top = line
		b.WrapTop = row
		return
	}
	distance := row - b.WrapTop
	if line-b.Top >= r.H {
		distance = r.H
	} else {
		for l := b.Top; l < line; l++ {
			distance += rowCount(b.Doc.Text, l, width, a.Config.TabSize)
			if distance >= r.H {
				break
			}
		}
	}
	if distance >= r.H {
		b.Top = line
		b.WrapTop = max(0, row-r.H+1)
	}
}
func (a *App) scrollRows(delta int) {
	b := a.Current()
	if !a.wrap {
		b.Top = max(0, min(b.Doc.Text.Lines()-1, b.Top+delta))
		return
	}
	width := a.contentWidth(b, a.rect(a.focus))
	for delta > 0 {
		n := rowCount(b.Doc.Text, b.Top, width, a.Config.TabSize)
		if b.WrapTop+1 < n {
			b.WrapTop++
		} else if b.Top < b.Doc.Text.Lines()-1 {
			b.Top++
			b.WrapTop = 0
		}
		delta--
	}
	for delta < 0 {
		if b.WrapTop > 0 {
			b.WrapTop--
		} else if b.Top > 0 {
			b.Top--
			b.WrapTop = rowCount(b.Doc.Text, b.Top, width, a.Config.TabSize) - 1
		}
		delta++
	}
}
func (a *App) moveVisual(delta int, extend bool) {
	b := a.Current()
	r := b.Doc.Text
	width := a.contentWidth(b, a.rect(a.focus))
	for i, s := range b.Doc.Selections {
		line := r.LineAt(s.Head)
		row := rowAt(r, line, s.Head, width, a.Config.TabSize)
		seg := rowSegment(r, line, row, width, a.Config.TabSize)
		col := columnAt(r, s.Head, a.Config.TabSize) - seg.Column
		steps := delta
		for steps > 0 {
			if row+1 < rowCount(r, line, width, a.Config.TabSize) {
				row++
			} else if line < r.Lines()-1 {
				line++
				row = 0
			}
			steps--
		}
		for steps < 0 {
			if row > 0 {
				row--
			} else if line > 0 {
				line--
				row = rowCount(r, line, width, a.Config.TabSize) - 1
			}
			steps++
		}
		target := rowSegment(r, line, row, width, a.Config.TabSize)
		p := min(target.End, lineOffset(r, line, target.Column+col, a.Config.TabSize))
		s.Head = p
		if !extend {
			s.Anchor = p
		}
		b.Doc.Selections[i] = s
	}
	a.normalizeSelections()
	a.reveal()
}
func (a *App) paneCommands() {
	a.Commands = append(a.Commands,
		command{"Grow pane", "", func() { a.resizePane(50) }}, command{"Shrink pane", "", func() { a.resizePane(-50) }},
		command{"Focus pane left", "", func() { a.focusDirection(-1, 0) }}, command{"Focus pane right", "", func() { a.focusDirection(1, 0) }}, command{"Focus pane above", "", func() { a.focusDirection(0, -1) }}, command{"Focus pane below", "", func() { a.focusDirection(0, 1) }},
		command{"Split vertically", "", func() { a.split(false) }}, command{"Split horizontally", "", func() { a.split(true) }}, command{"Focus next pane", "", func() { a.focusPane(a.focus + 1) }}, command{"Close pane", "", a.closePane}, command{"Toggle soft wrapping", "", func() {
			a.wrap = !a.wrap
			for _, b := range a.Buffers {
				b.WrapTop = 0
				b.Left = 0
			}
			for i := range a.panes {
				a.panes[i].WrapTop = 0
				a.panes[i].Left = 0
			}
			a.reveal()
			a.Status = fmt.Sprintf("Soft wrapping: %t", a.wrap)
		}})
}
