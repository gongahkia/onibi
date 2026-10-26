package editor

import (
	"github.com/gdamore/tcell/v2"
	"unicode"
	"unicode/utf8"
)

func (a *App) autoCompletion() {
	if !a.Config.AutoComplete || !a.wantCompletion || a.Readonly || a.Current().Transient || a.snippet != nil || a.Dialog != nil || a.Preview != nil || a.languageBusy {
		return
	}
	b := a.Current()
	if len(b.Doc.Selections) != 1 || b.Doc.Text.Len() > a.Config.ServiceMaxBytes {
		return
	}
	p := b.Doc.Selections[0].Head
	s := b.Doc.Text.Slice(max(0, p-128), p)
	count := 0
	for len(s) > 0 {
		r, n := utf8.DecodeLastRuneInString(s)
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		count++
		s = s[:len(s)-n]
	}
	dot := p > 0 && b.Doc.Text.Slice(p-1, p) == "."
	if count < 2 && !dot {
		a.wantCompletion = false
		return
	}
	a.completionAutomatic = true
	a.languageRequest("completion")
	a.completionAutomatic = false
}
func (a *App) completionKey(ev *tcell.EventKey) bool {
	d := a.Dialog
	switch ev.Key() {
	case tcell.KeyDown:
		d.Index = min(len(d.Items)-1, d.Index+1)
		return true
	case tcell.KeyUp:
		d.Index = max(0, d.Index-1)
		return true
	case tcell.KeyTab, tcell.KeyEnter:
		a.Dialog = nil
		if len(d.Items) > 0 {
			d.Items[d.Index].Run()
		}
		return true
	case tcell.KeyEscape:
		a.Dialog = nil
		a.wantCompletion = false
		return true
	}
	a.Dialog = nil
	return false
}
func (a *App) drawCompletion(p palette) {
	d := a.Dialog
	r := a.rect(a.focus)
	b := a.Current()
	caret := b.Doc.Selections[0].Head
	x, y := r.X+a.gutter(), r.Y
	for i, row := range a.panes[a.focus].Rows {
		if caret >= row.Start && caret <= row.End {
			x = r.X + a.gutter() + a.columnAt(b, caret) - row.Column
			if !a.wraps(b) {
				x -= b.Left
			}
			y = r.Y + i
		}
	}
	sw, sh := a.Screen.Size()
	width := min(58, sw)
	height := min(8, len(d.Items)) + 1
	x = max(0, min(x, sw-width))
	top := y + 1
	if top+height > sh-2 {
		top = max(0, y-height)
	}
	height = min(height, sh-2-top)
	if height <= 0 {
		return
	}
	for row := 0; row < height; row++ {
		a.fill(x, top+row, width, p.bar)
	}
	start := max(0, d.Index-max(0, height-2))
	for i := start; i < len(d.Items) && i < start+height-1; i++ {
		style := p.bar
		if i == d.Index {
			style = p.selection
		}
		a.fill(x, top+i-start, width, style)
		a.put(x+1, top+i-start, width-2, d.Items[i].Label, style)
	}
	a.put(x+1, top+height-1, width-2, "↑↓ choose · Tab accept · Esc dismiss", p.dim)
}
