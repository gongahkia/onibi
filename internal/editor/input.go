package editor

import (
	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/text"
	"github.com/rivo/uniseg"
	"slices"
	"sort"
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
			a.pasteOverflow = false
		} else if a.pasting {
			a.pasting = false
			if a.pasteOverflow {
				a.Status = "Paste exceeds configured file limit"
			} else {
				a.pasteText(a.paste.String())
			}
			a.paste.Reset()
		}
	case *tcell.EventClipboard:
		if a.clipboardRequested {
			a.clipboardRequested = false
			if a.clipboardTarget == a.Current() && a.clipboardVersion == a.Current().Doc.Version && slices.Equal(a.clipboardSelections, a.Current().Doc.Selections) && a.Dialog == nil && a.Preview == nil {
				a.pasteText(string(ev.Data()))
			} else {
				a.Status = "Discarded clipboard reply after editing position changed"
			}
		}
	case *tcell.EventMouse:
		a.mouse(ev)
	case *tcell.EventKey:
		if a.pasting {
			if a.pasteOverflow {
				return
			}
			if a.paste.Len() > a.Config.MaxFileMiB<<20 {
				a.pasteOverflow = true
				a.paste.Reset()
				return
			}
			if ev.Key() == tcell.KeyRune {
				a.paste.WriteRune(ev.Rune())
			} else if ev.Key() >= tcell.KeyCtrlSpace && ev.Key() <= tcell.KeyCtrlUnderscore {
				a.paste.WriteByte(byte(ev.Key() - tcell.KeyCtrlSpace))
			} else if ev.Key() >= 0 && ev.Key() <= 31 {
				a.paste.WriteByte(byte(ev.Key()))
			}
			return
		}
		defer a.reveal()
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
	if ev.Key() != tcell.KeyRune || ev.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0 {
		a.typingDoc = nil
	}
	if a.languageKey(ev.Key()) {
		return
	}
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
		a.Current().Doc.Selections = []text.Selection{{Anchor: 0, Head: a.Current().Doc.Text.Len()}}
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
		if a.searchCancel != nil {
			a.searchCancel()
		}
		if a.workspaceCancel != nil {
			a.workspaceCancel()
			a.workspaceSeq++
		}
		a.clipboardRequested = false
		p := a.Current().Doc.Selections[0].Head
		a.Current().Doc.Selections = []text.Selection{{Anchor: p, Head: p}}
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
		a.indent(false)
	case tcell.KeyBacktab:
		a.indent(true)
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
	if p > 0 && r.Plain(p-1, p) {
		return p - 1
	}
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
	if p < r.Len() && r.Plain(p, min(r.Len(), p+2)) {
		return p + 1
	}
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
	if a.wrap && dy != 0 {
		a.moveVisual(dy, extend)
		return
	}
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
			col := columnAt(r, p, a.Config.TabSize)
			target := max(0, min(r.Lines()-1, line+dy))
			p = lineOffset(r, target, col, a.Config.TabSize)
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
	sort.SliceStable(d.Selections, func(i, j int) bool { return d.Selections[i].Start() < d.Selections[j].Start() })
	out := make([]text.Selection, 0, len(d.Selections))
	for _, s := range d.Selections {
		if len(out) == 0 || s.Start() > out[len(out)-1].End() {
			out = append(out, s)
		} else {
			last := &out[len(out)-1]
			lo := last.Start()
			hi := max(last.End(), s.End())
			if last.Head < last.Anchor {
				last.Head = lo
				last.Anchor = hi
			} else {
				last.Anchor = lo
				last.Head = hi
			}
		}
	}
	d.Selections = out
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
	if ev.Key() == tcell.KeyTab {
		p.Detail = !p.Detail
		p.DetailTop = 0
		p.DetailLeft = 0
		if p.Detail {
			e := p.Edits[p.Index]
			p.Before = strings.Split(p.documentAt(p.Index).Text.Slice(e.Start, e.End), "\n")
			p.After = strings.Split(e.Text, "\n")
		} else {
			p.Before = nil
			p.After = nil
		}
		return
	}
	if p.Detail {
		limit := max(len(p.Before), len(p.After)) - 1
		switch ev.Key() {
		case tcell.KeyUp:
			p.DetailTop = max(0, p.DetailTop-1)
			return
		case tcell.KeyDown:
			p.DetailTop = min(limit, p.DetailTop+1)
			return
		case tcell.KeyPgUp:
			p.DetailTop = max(0, p.DetailTop-10)
			return
		case tcell.KeyPgDn:
			p.DetailTop = min(limit, p.DetailTop+10)
			return
		case tcell.KeyLeft:
			p.DetailLeft = max(0, p.DetailLeft-10)
			return
		case tcell.KeyRight:
			p.DetailLeft += 10
			return
		}
	}

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
		if p.Targets != nil {
			a.applyWorkspacePreview()
			return
		}
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
	a.ensurePanes()
	x, y := ev.Position()
	buttons := ev.Buttons()
	if y == 0 && buttons&tcell.Button1 != 0 {
		position := 0
		for i, b := range a.Buffers {
			name := " " + bufferName(b)
			if b.Doc.Dirty() {
				name += " *"
			}
			name += " "
			width := uniseg.StringWidth(name)
			if x >= position && x < position+width {
				a.Active = i
				a.Matches = nil
				a.schedule()
				return
			}
			position += width
		}
		return
	}
	target := -1
	for i := range a.panes {
		r := a.rect(i)
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			target = i
			break
		}
	}
	if target < 0 {
		return
	}
	if target != a.focus {
		a.focusPane(target)
		a.dragging = false
	}
	if buttons&tcell.WheelUp != 0 {
		a.scrollRows(-3)
		return
	}
	if buttons&tcell.WheelDown != 0 {
		a.scrollRows(3)
		return
	}
	if buttons&tcell.Button1 == 0 {
		a.dragging = false
		return
	}
	r := a.rect(target)
	rows := a.panes[target].Rows
	row := y - r.Y
	if row < 0 || row >= len(rows) {
		return
	}
	projected := rows[row]
	b := a.Current()
	column := max(0, x-r.X-a.gutter()) + projected.Column
	offset := min(projected.End, lineOffset(b.Doc.Text, projected.Line, column, a.Config.TabSize))
	if !a.dragging {
		b.Doc.Selections = []text.Selection{{Anchor: offset, Head: offset}}
		a.dragging = true
	} else {
		b.Doc.Selections[0].Head = offset
	}
}

func (a *App) indent(outdent bool) {
	b := a.Current()
	d := b.Doc
	if !outdent && len(d.Selections) == 1 && d.Selections[0].Start() == d.Selections[0].End() {
		s := "\t"
		if a.Config.InsertSpaces {
			p := d.Selections[0].Head
			col := columnAt(d.Text, p, a.Config.TabSize)
			s = strings.Repeat(" ", a.Config.TabSize-col%a.Config.TabSize)
		}
		a.insert(s)
		return
	}
	seen := map[int]bool{}
	edits := []text.Edit{}
	for _, s := range d.Selections {
		first, last := d.Text.LineAt(s.Start()), d.Text.LineAt(s.End())
		if s.End() > s.Start() && s.End() == d.Text.LineStart(last) {
			last--
		}
		for line := first; line <= last; line++ {
			if seen[line] {
				continue
			}
			seen[line] = true
			start := d.Text.LineStart(line)
			value := d.Text.Line(line)
			if outdent {
				count := 0
				for count < len(value) && count < a.Config.TabSize && value[count] == ' ' {
					count++
				}
				if strings.HasPrefix(value, "\t") {
					count = 1
				}
				if count > 0 {
					edits = append(edits, text.Edit{Start: start, End: start + count})
				}
			} else {
				indent := "\t"
				if a.Config.InsertSpaces {
					indent = strings.Repeat(" ", a.Config.TabSize)
				}
				edits = append(edits, text.Edit{Start: start, End: start, Text: indent})
			}
		}
	}
	a.apply(edits)
}

func columnAt(r text.Rope, p, tab int) int {
	start := r.LineStart(r.LineAt(p))
	if r.Plain(start, p) {
		return p - start
	}
	return visualColumn(r.Slice(start, p), tab)
}
func lineOffset(r text.Rope, line, col, tab int) int {
	start, end := r.LineStart(line), r.LineEnd(line)
	if r.Plain(start, end) {
		return min(end, start+col)
	}
	return start + offsetAt(r.Slice(start, end), col, tab)
}
