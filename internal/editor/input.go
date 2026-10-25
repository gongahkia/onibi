package editor

import (
	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/text"
	"github.com/rivo/uniseg"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (a *App) Handle(event tcell.Event) {
	switch ev := event.(type) {
	case *tcell.EventResize:
		a.Screen.Sync()
	case *tcell.EventPaste:
		if ev.Start() {
			a.pasting = true
			a.paste.Reset()
		} else if a.pasting {
			a.pasting = false
			a.pasteText(a.paste.String())
			a.paste.Reset()
		}
	case *tcell.EventClipboard:
		if a.clipboardRequested {
			a.clipboardRequested = false
			a.pasteText(string(ev.Data()))
		}
	case *tcell.EventMouse:
		a.mouse(ev)
	case *tcell.EventKey:
		defer a.reveal()
		if a.pasting {
			switch ev.Key() {
			case tcell.KeyRune:
				a.paste.WriteRune(ev.Rune())
			case tcell.KeyEnter:
				a.paste.WriteByte('\n')
			case tcell.KeyTab:
				a.paste.WriteByte('\t')
			case tcell.KeyBackspace, tcell.KeyBackspace2:
				a.paste.WriteByte('\b')
			}
			return
		}
		if a.Preview != nil {
			a.previewKey(ev)
			return
		}
		if a.Dialog != nil {
			a.dialogKey(ev)
			return
		}
		a.key(ev)
	}
}
func (a *App) pasteText(s string) {
	defer a.reveal()
	if !utf8.ValidString(s) {
		a.Status = "Clipboard is not UTF-8"
		return
	}
	if a.Dialog != nil {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", " ")
		d := a.Dialog
		r := []rune(d.Input)
		insert := []rune(s)
		d.Input = string(r[:d.Cursor]) + string(insert) + string(r[d.Cursor:])
		d.Cursor += len(insert)
		return
	}
	if a.Preview != nil {
		return
	}
	if a.Current().Newline == "\r\n" {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	}
	a.insert(s)
}
func (a *App) key(ev *tcell.EventKey) {
	shift := ev.Modifiers()&tcell.ModShift != 0
	if ev.Modifiers()&tcell.ModAlt != 0 && ev.Key() == tcell.KeyRune {
		switch ev.Rune() {
		case 'f':
			a.nextMatch()
		case 'r':
			a.beginReplace()
		case 'n':
			a.cycle(1)
		case 'p':
			a.cycle(-1)
		case 'a':
			a.selectMatches()
		}
		return
	}
	switch ev.Key() {
	case tcell.KeyCtrlQ:
		a.requestQuit()
	case tcell.KeyCtrlS:
		a.Save()
	case tcell.KeyCtrlO:
		a.prompt("Open file", "", func(path string) {
			if err := a.Open(path); err != nil {
				a.Status = err.Error()
			}
			a.Matches = nil
			a.schedule()
		})
	case tcell.KeyCtrlN:
		a.NewBuffer()
		a.schedule()
	case tcell.KeyCtrlW:
		a.closeBuffer()
	case tcell.KeyCtrlP:
		a.filePicker()
	case tcell.KeyCtrlE, tcell.KeyF2:
		a.palette()
	case tcell.KeyF1:
		a.help()
	case tcell.KeyCtrlF:
		a.findPrompt()
	case tcell.KeyCtrlR:
		a.beginReplace()
	case tcell.KeyF3:
		a.nextMatch()
	case tcell.KeyCtrlG:
		a.prompt("Go to line", "", a.gotoLine)
	case tcell.KeyCtrlZ:
		if !a.Readonly && a.Current().Doc.Undo() {
			a.changed()
		}
	case tcell.KeyCtrlY:
		if !a.Readonly && a.Current().Doc.Redo() {
			a.changed()
		}
	case tcell.KeyCtrlA:
		a.Current().Doc.Selections = []text.Selection{{0, a.Current().Doc.Text.Len()}}
	case tcell.KeyCtrlD:
		a.addOccurrence()
	case tcell.KeyCtrlC:
		a.copy(false)
	case tcell.KeyCtrlX:
		a.copy(true)
	case tcell.KeyCtrlV:
		a.pasteClipboard()
	case tcell.KeyCtrlL:
		a.Screen.Sync()
	case tcell.KeyEscape:
		p := a.Current().Doc.Selections[0].Head
		a.Current().Doc.Selections = []text.Selection{{p, p}}
		a.Matches = nil
	case tcell.KeyLeft:
		a.move(-1, 0, shift)
	case tcell.KeyRight:
		a.move(1, 0, shift)
	case tcell.KeyUp:
		a.move(0, -1, shift)
	case tcell.KeyDown:
		a.move(0, 1, shift)
	case tcell.KeyPgUp:
		_, h := a.Screen.Size()
		a.move(0, -max(1, h-4), shift)
	case tcell.KeyPgDn:
		_, h := a.Screen.Size()
		a.move(0, max(1, h-4), shift)
	case tcell.KeyHome:
		a.homeEnd(false, shift, ev.Modifiers()&tcell.ModCtrl != 0)
	case tcell.KeyEnd:
		a.homeEnd(true, shift, ev.Modifiers()&tcell.ModCtrl != 0)
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		a.delete(false)
	case tcell.KeyDelete:
		a.delete(true)
	case tcell.KeyEnter:
		a.newline()
	case tcell.KeyTab:
		s := "\t"
		if a.Config.InsertSpaces {
			s = strings.Repeat(" ", a.Config.TabSize)
		}
		a.insert(s)
	case tcell.KeyRune:
		if ev.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) == 0 {
			a.insert(string(ev.Rune()))
		}
	}
}
func (a *App) cycle(delta int) {
	a.Active = (a.Active + delta + len(a.Buffers)) % len(a.Buffers)
	a.Matches = nil
	a.schedule()
}
func previous(r text.Rope, p int) int {
	if p <= 0 {
		return 0
	}
	line := r.LineAt(p)
	start := r.LineStart(line)
	if p == start {
		if p >= 2 && r.Slice(p-2, p) == "\r\n" {
			return p - 2
		}
		return p - 1
	}
	g := uniseg.NewGraphemes(r.Slice(start, p))
	last := start
	for g.Next() {
		from, _ := g.Positions()
		last = start + from
	}
	return last
}
func next(r text.Rope, p int) int {
	if p >= r.Len() {
		return r.Len()
	}
	end := r.LineStart(r.LineAt(p) + 1)
	g := uniseg.NewGraphemes(r.Slice(p, end))
	if g.Next() {
		_, to := g.Positions()
		return p + to
	}
	return min(r.Len(), p+1)
}
func visualColumn(s string, tab int) int {
	col := 0
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		if g.Str() == "\t" {
			col += tab - col%tab
		} else {
			col += g.Width()
		}
	}
	return col
}
func offsetAt(s string, column, tab int) int {
	col := 0
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		from, to := g.Positions()
		width := g.Width()
		if g.Str() == "\t" {
			width = tab - col%tab
		}
		if col+width > column {
			return from
		}
		col += width
		if col == column {
			return to
		}
	}
	return len(s)
}
func (a *App) move(dx, dy int, extend bool) {
	d := a.Current().Doc
	r := d.Text
	for i, s := range d.Selections {
		p := s.Head
		if dx != 0 {
			if !extend && s.Start() != s.End() {
				if dx < 0 {
					p = s.Start()
				} else {
					p = s.End()
				}
			} else if dx < 0 {
				p = previous(r, p)
			} else {
				p = next(r, p)
			}
		}
		if dy != 0 {
			line := r.LineAt(p)
			col := visualColumn(r.Slice(r.LineStart(line), p), a.Config.TabSize)
			target := max(0, min(r.Lines()-1, line+dy))
			p = r.LineStart(target) + offsetAt(r.Line(target), col, a.Config.TabSize)
		}
		if !extend {
			s.Anchor = p
		}
		s.Head = p
		d.Selections[i] = s
	}
	a.normalizeSelections()
}
func (a *App) normalizeSelections() {
	d := a.Current().Doc
	out := []text.Selection{}
	for _, s := range d.Selections {
		duplicate := false
		for _, o := range out {
			if s.Start() <= o.End() && s.End() >= o.Start() {
				duplicate = true
				break
			}
		}
		if !duplicate {
			out = append(out, s)
		}
	}
	if len(out) > 0 {
		d.Selections = out
	}
}
func (a *App) homeEnd(end, extend, document bool) {
	d := a.Current().Doc
	for i, s := range d.Selections {
		line := d.Text.LineAt(s.Head)
		p := d.Text.LineStart(line)
		if end {
			p += len(d.Text.Line(line))
		}
		if document {
			p = 0
			if end {
				p = d.Text.Len()
			}
		}
		if !extend {
			s.Anchor = p
		}
		s.Head = p
		d.Selections[i] = s
	}
	a.normalizeSelections()
}
func (a *App) delete(forward bool) {
	d := a.Current().Doc
	edits := []text.Edit{}
	for _, s := range d.Selections {
		lo, hi := s.Start(), s.End()
		if lo == hi {
			if forward {
				hi = next(d.Text, hi)
			} else {
				lo = previous(d.Text, lo)
			}
		}
		if lo != hi {
			edits = append(edits, text.Edit{Start: lo, End: hi})
		}
	}
	a.apply(edits)
}
func (a *App) newline() {
	b := a.Current()
	edits := []text.Edit{}
	for _, s := range b.Doc.Selections {
		indent := ""
		if a.Config.AutoIndent {
			line := b.Doc.Text.Line(b.Doc.Text.LineAt(s.Start()))
			for _, r := range line {
				if r != ' ' && r != '\t' {
					break
				}
				indent += string(r)
			}
		}
		edits = append(edits, text.Edit{Start: s.Start(), End: s.End(), Text: b.Newline + indent})
	}
	a.apply(edits)
}
func (a *App) wordSelection() {
	d := a.Current().Doc
	s := d.Selections[0]
	if s.Start() != s.End() {
		return
	}
	lo, hi := s.Head, s.Head
	for lo > 0 {
		p := previous(d.Text, lo)
		r, _ := utf8.DecodeRuneInString(d.Text.Slice(p, lo))
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		lo = p
	}
	for hi < d.Text.Len() {
		p := next(d.Text, hi)
		r, _ := utf8.DecodeRuneInString(d.Text.Slice(hi, p))
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			break
		}
		hi = p
	}
	d.Selections[0] = text.Selection{Anchor: lo, Head: hi}
}
func (a *App) dialogKey(ev *tcell.EventKey) {
	d := a.Dialog
	r := []rune(d.Input)
	switch ev.Key() {
	case tcell.KeyEscape:
		a.Dialog = nil
	case tcell.KeyEnter:
		items := a.filtered()
		a.Dialog = nil
		if d.Items != nil {
			if len(items) > 0 {
				items[min(d.Index, len(items)-1)].Run()
			}
		} else if d.Accept != nil {
			d.Accept(d.Input)
		}
	case tcell.KeyDown:
		d.Index++
	case tcell.KeyUp:
		d.Index = max(0, d.Index-1)
	case tcell.KeyLeft:
		d.Cursor = max(0, d.Cursor-1)
	case tcell.KeyRight:
		d.Cursor = min(len(r), d.Cursor+1)
	case tcell.KeyHome:
		d.Cursor = 0
	case tcell.KeyEnd:
		d.Cursor = len(r)
	case tcell.KeyCtrlU:
		d.Input = ""
		d.Cursor = 0
		d.Index = 0
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if d.Cursor > 0 {
			d.Input = string(r[:d.Cursor-1]) + string(r[d.Cursor:])
			d.Cursor--
			d.Index = 0
		}
	case tcell.KeyDelete:
		if d.Cursor < len(r) {
			d.Input = string(r[:d.Cursor]) + string(r[d.Cursor+1:])
			d.Index = 0
		}
	case tcell.KeyRune:
		d.Input = string(r[:d.Cursor]) + string(ev.Rune()) + string(r[d.Cursor:])
		d.Cursor++
		d.Index = 0
	}
}
func (a *App) previewKey(ev *tcell.EventKey) {
	p := a.Preview
	switch ev.Key() {
	case tcell.KeyEscape:
		a.Preview = nil
	case tcell.KeyDown:
		p.Index = min(len(p.Edits)-1, p.Index+1)
	case tcell.KeyUp:
		p.Index = max(0, p.Index-1)
	case tcell.KeyPgDn:
		p.Index = min(len(p.Edits)-1, p.Index+10)
	case tcell.KeyPgUp:
		p.Index = max(0, p.Index-10)
	case tcell.KeyRune:
		if ev.Rune() == ' ' && len(p.Checked) > 0 {
			p.Checked[p.Index] = !p.Checked[p.Index]
		}
		if ev.Rune() == 'a' {
			all := true
			for _, c := range p.Checked {
				all = all && c
			}
			for i := range p.Checked {
				p.Checked[i] = !all
			}
		}
	case tcell.KeyEnter:
		if a.Readonly {
			a.Status = "Read-only mode"
			return
		}
		edits := []text.Edit{}
		for i, e := range p.Edits {
			if p.Checked[i] {
				edits = append(edits, e)
			}
		}
		if err := p.Buffer.Doc.Apply(p.Version, edits); err != nil {
			a.Status = err.Error()
		} else {
			a.Status = "Applied preview as one undoable change"
			a.changed()
		}
		a.Preview = nil
	}
}
func (a *App) mouse(ev *tcell.EventMouse) {
	if a.Dialog != nil || a.Preview != nil {
		return
	}
	x, y := ev.Position()
	buttons := ev.Buttons()
	b := a.Current()
	_, h := a.Screen.Size()
	if buttons&tcell.WheelUp != 0 {
		b.Top = max(0, b.Top-3)
		return
	}
	if buttons&tcell.WheelDown != 0 {
		b.Top = min(max(0, b.Doc.Text.Lines()-1), b.Top+3)
		return
	}
	if buttons&tcell.Button1 == 0 {
		a.dragging = false
		return
	}
	if y < 1 || y >= h-2 {
		return
	}
	line := min(b.Doc.Text.Lines()-1, b.Top+y-1)
	p := b.Doc.Text.LineStart(line) + offsetAt(b.Doc.Text.Line(line), max(0, x-a.gutter()+b.Left), a.Config.TabSize)
	if !a.dragging {
		b.Doc.Selections = []text.Selection{{Anchor: p, Head: p}}
		a.dragging = true
	} else {
		b.Doc.Selections[0].Head = p
	}
}
