package editor

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/text"
	"github.com/rivo/uniseg"
	"strconv"
	"time"
	"unicode/utf8"
)

type contextMenu struct {
	X, Y, Index int
	Items       []choice
}

func (a *App) menuRect() rectangle {
	w, h := a.Screen.Size()
	width := 20
	for _, item := range a.Menu.Items {
		width = max(width, uniseg.StringWidth(item.Label)+2)
	}
	width = min(width, max(1, w))
	height := min(len(a.Menu.Items)+2, max(1, h))
	return rectangle{max(0, min(a.Menu.X, w-width)), max(0, min(a.Menu.Y, h-height)), width, height}
}
func (a *App) showContext(x, y int, items []choice) {
	a.Menu = &contextMenu{X: x, Y: y, Items: items}
	a.dragging = false
	a.typingDoc = nil
}
func (a *App) drawContext(p palette) {
	r := a.menuRect()
	for y := 0; y < r.H; y++ {
		a.fill(r.X, r.Y+y, r.W, p.bar)
	}
	a.put(r.X+1, r.Y, r.W-2, "Actions", p.accent)
	start := max(0, a.Menu.Index-max(0, r.H-3))
	for i := start; i < len(a.Menu.Items) && i < start+r.H-2; i++ {
		style := p.bar
		if i == a.Menu.Index {
			style = p.selection
		}
		a.fill(r.X, r.Y+1+i-start, r.W, style)
		a.put(r.X+1, r.Y+1+i-start, r.W-2, a.Menu.Items[i].Label, style)
	}
	a.Screen.HideCursor()
}
func (a *App) runContext() {
	m := a.Menu
	a.Menu = nil
	if m != nil && m.Index >= 0 && m.Index < len(m.Items) {
		m.Items[m.Index].Run()
	}
}
func (a *App) contextKey(ev *tcell.EventKey) {
	switch ev.Key() {
	case tcell.KeyUp:
		a.Menu.Index = max(0, a.Menu.Index-1)
	case tcell.KeyDown:
		a.Menu.Index = min(len(a.Menu.Items)-1, a.Menu.Index+1)
	case tcell.KeyHome:
		a.Menu.Index = 0
	case tcell.KeyEnd:
		a.Menu.Index = len(a.Menu.Items) - 1
	case tcell.KeyEnter:
		a.runContext()
	case tcell.KeyEscape:
		a.Menu = nil
	}
}
func (a *App) editingContext(x, y int) {
	items := []choice{{"Copy", func() { a.copy(false) }}, {"Cut", func() { a.copy(true) }}, {"Paste", a.pasteClipboard}, {"Select all", func() { a.Current().Doc.Selections = []text.Selection{{Anchor: 0, Head: a.Current().Doc.Text.Len()}} }}, {"Go to definition", func() { a.languageRequest("definition") }}, {"Hover documentation", func() { a.languageRequest("hover") }}, {"Code actions", func() { a.runCommand("Code actions") }}, {"Toggle line comments", a.commentLines}, {"Split vertically", func() { a.split(false) }}, {"Split horizontally", func() { a.split(true) }}, {"Close pane", a.closePane}, {"Save", a.Save}, {"Toggle fold", func() { a.foldCommand("toggle", -1) }}, {"Unfold all", func() { a.foldCommand("unfold", -1) }}}
	if a.Current().Transient || a.Readonly {
		items = []choice{{"Copy", func() { a.copy(false) }}, {"Save as", a.saveAs}, {"Close buffer", a.closeBuffer}, {"Split vertically", func() { a.split(false) }}}
	}
	if a.Current().Reading != nil {
		items = append([]choice{{"Return to source", func() { a.documentPreview("", false) }}, {"Document headings", a.readingHeadings}, {"Follow document link", a.followReadingLink}, {"Document links", a.readingLinks}, {"Copy document link", a.copyReadingLink}}, items...)
	}
	a.showContext(x, y, items)
}
func (a *App) runCommand(name string) {
	for _, c := range a.Commands {
		if c.Name == name {
			c.Run()
			return
		}
	}
}
func (a *App) treeContext(x, y int) {
	items := []choice{}
	if a.tree != nil && len(a.tree.Rows) > 0 {
		n := a.tree.Rows[a.tree.Index].Node
		label := "Open file"
		if n.Dir {
			label = "Expand / collapse folder"
		}
		items = append(items, choice{label, a.treeOpen}, choice{"Copy relative path", func() {
			a.clipboard = n.Path
			a.Screen.SetClipboard([]byte(n.Path))
			a.Status = "Path copied internally; terminal clipboard write requested"
		}})
	}
	items = append(items, choice{"Refresh project files", a.indexFiles}, choice{"Find project file", a.filePicker}, choice{"Hide file tree", func() { a.treeVisible = false; a.treeFocus = false; a.reveal() }})
	a.showContext(x, y, items)
}
func (a *App) openContextAtCaret() {
	a.ensurePanes()
	if a.treeFocus {
		a.treeContext(2, 2)
		return
	}
	r := a.rect(a.focus)
	a.editingContext(r.X+a.gutter(), r.Y+1)
}
func (a *App) panelRect() rectangle {
	w, h := a.Screen.Size()
	width, height := min(90, w-4), min(20, h-4)
	return rectangle{(w - width) / 2, (h - height) / 2, width, height}
}
func (a *App) modalMouse(ev *tcell.EventMouse, pressed, right bool) bool {
	x, y := ev.Position()
	r := a.panelRect()
	if a.Menu != nil {
		m := a.menuRect()
		if ev.Buttons()&tcell.WheelDown != 0 {
			a.Menu.Index = min(len(a.Menu.Items)-1, a.Menu.Index+3)
		} else if ev.Buttons()&tcell.WheelUp != 0 {
			a.Menu.Index = max(0, a.Menu.Index-3)
		} else if pressed {
			if x < m.X || x >= m.X+m.W || y <= m.Y || y >= m.Y+m.H-1 {
				a.Menu = nil
				return true
			}
			start := max(0, a.Menu.Index-max(0, m.H-3))
			a.Menu.Index = min(len(a.Menu.Items)-1, start+y-m.Y-1)
			a.runContext()
		} else if right {
			a.Menu = nil
		}
		return true
	}
	if a.Dialog != nil && !a.Dialog.Inline {
		d := a.Dialog
		if x < r.X || x >= r.X+r.W || y < r.Y || y >= r.Y+r.H {
			return true
		}
		if d.Items != nil {
			items := a.filtered()
			if ev.Buttons()&tcell.WheelDown != 0 {
				d.Index = min(max(0, len(items)-1), d.Index+3)
			} else if ev.Buttons()&tcell.WheelUp != 0 {
				d.Index = max(0, d.Index-3)
			} else if pressed && y >= r.Y+4 && y < r.Y+r.H-1 {
				start := max(0, d.Index-max(0, r.H-5)+1)
				i := start + y - r.Y - 4
				if i < len(items) {
					a.Dialog = nil
					items[i].Run()
				}
			}
		}
		if pressed && y == r.Y+2 {
			before := string([]rune(d.Input)[:d.Cursor])
			shift := max(0, uniseg.StringWidth(before)-(r.W-5))
			offset := offsetAt(d.Input, shift+max(0, x-r.X-2), a.Config.TabSize)
			d.Cursor = utf8.RuneCountInString(d.Input[:offset])
		}
		return true
	}
	if a.Preview != nil {
		p := a.Preview
		if right {
			a.showContext(x, y, []choice{{"Inspect selected change", func() { a.previewKey(tcell.NewEventKey(tcell.KeyTab, 0, 0)) }}, {"Apply selected changes", func() { a.previewKey(tcell.NewEventKey(tcell.KeyEnter, 0, 0)) }}, {"Cancel preview", func() { a.cancelPreview("Cancelled by user") }}})
			return true
		}
		if pressed && y == r.Y+r.H-1 && x >= r.X+r.W-17 {
			if x < r.X+r.W-9 {
				a.previewKey(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
			} else {
				a.cancelPreview("Cancelled by user")
			}
			return true
		}
		if p.Detail {
			if ev.Buttons()&tcell.WheelDown != 0 {
				p.DetailTop = min(max(len(p.Before), len(p.After))-1, p.DetailTop+3)
			} else if ev.Buttons()&tcell.WheelUp != 0 {
				p.DetailTop = max(0, p.DetailTop-3)
			}
			return true
		}
		visible := max(1, (r.H-3)/3)
		if ev.Buttons()&tcell.WheelDown != 0 {
			p.Index = min(len(p.Edits)-1, p.Index+1)
		} else if ev.Buttons()&tcell.WheelUp != 0 {
			p.Index = max(0, p.Index-1)
		} else if pressed && x >= r.X && x < r.X+r.W && y > r.Y && y < r.Y+r.H-1 {
			start := max(0, p.Index-visible+1)
			i := start + (y-r.Y-1)/3
			if i < len(p.Edits) {
				p.Index = i
				if x < r.X+6 {
					if !p.Strict {
						p.Checked[i] = !p.Checked[i]
					}
				}
			}
		}
		return true
	}
	return false
}
func (a *App) mouse(ev *tcell.EventMouse) {
	x, y := ev.Position()
	buttons := ev.Buttons()
	pressed := buttons&tcell.Button1 != 0 && a.mouseButtons&tcell.Button1 == 0
	right := buttons&tcell.Button2 != 0 && a.mouseButtons&tcell.Button2 == 0
	a.mouseButtons = buttons
	if buttons&tcell.Button1 == 0 {
		a.dragging = false
		a.dividerDrag = nil
		a.treeDrag = false
		if a.miniDrag {
			a.rememberJump()
		}
		a.miniDrag = false
	}
	if a.modalMouse(ev, pressed, right) {
		return
	}
	if pressed || right {
		a.snippet = nil
		a.typingDoc = nil
		if a.Dialog != nil && a.Dialog.Inline {
			a.Dialog = nil
		}
	}
	if a.dividerDrag != nil {
		d := a.dividerDrag
		position, length := x-d.Area.X, d.Area.W-1
		if d.Node.Horizontal {
			position, length = y-d.Area.Y, d.Area.H-1
		}
		d.Node.Ratio = max(100, min(900, position*1000/max(1, length)))
		a.reveal()
		return
	}
	if a.treeDrag {
		w, _ := a.Screen.Size()
		a.treeWidth = max(18, min(min(60, w/3), x+1))
		a.reveal()
		return
	}
	if a.miniDrag {
		a.minimapJump(y, ev.Modifiers()&tcell.ModShift != 0)
		return
	}
	a.ensurePanes()
	if y == 0 {
		if pressed || right {
			position := 0
			for i, b := range a.Buffers {
				name := " " + bufferName(b)
				if b.Doc.Dirty() {
					name += " *"
				}
				width := uniseg.StringWidth(name + " ")
				if x >= position && x < position+width {
					a.Active = i
					a.treeFocus = false
					a.Matches = nil
					a.schedule()
					a.reveal()
					if right {
						a.showContext(x, y, []choice{{"Save", a.Save}, {"Close buffer", a.closeBuffer}, {"New buffer", a.NewBuffer}})
					}
					return
				}
				position += width
			}
		}
		return
	}
	width := a.sidebarWidth()
	if width > 0 && x == width-1 && pressed {
		a.treeDrag = true
		return
	}
	if width > 0 && x >= 0 && x < width && y >= 1 {
		if !pressed && !right && buttons&(tcell.WheelUp|tcell.WheelDown) == 0 {
			return
		}
		a.treeFocus = true
		if a.tree == nil {
			a.rebuildTree()
		}
		t := a.tree
		if buttons&tcell.WheelUp != 0 {
			t.Index = max(0, t.Index-3)
		} else if buttons&tcell.WheelDown != 0 {
			t.Index = min(max(0, len(t.Rows)-1), t.Index+3)
		} else if pressed || right {
			index := t.Top + y - 2
			if index >= 0 && index < len(t.Rows) {
				t.Index = index
				if right {
					a.treeContext(x, y)
				} else {
					a.treeOpen()
				}
			} else if right {
				a.treeContext(x, y)
			}
		}
		return
	}
	mini := a.minimapRect()
	if mini.W > 0 && x >= mini.X && y >= mini.Y && y < mini.Y+mini.H {
		if pressed {
			a.treeFocus = false
			a.rememberJump()
			a.miniDrag = true
			a.minimapJump(y, ev.Modifiers()&tcell.ModShift != 0)
		} else if buttons&tcell.WheelUp != 0 {
			a.scrollRows(-3)
		} else if buttons&tcell.WheelDown != 0 {
			a.scrollRows(3)
		} else if right {
			a.showContext(x, y, []choice{{"Hide minimap", func() { a.minimapVisible = false; a.miniCache = nil; a.reveal() }}})
		}
		return
	}
	var boxes [maxPanes]rectangle
	var dividers []divider
	a.layout.arrange(a.workspaceRect(), &boxes, &dividers)
	for _, d := range dividers {
		hit := x == d.X && y >= d.Y && y < d.Y+d.Length
		if d.Node.Horizontal {
			hit = y == d.Y && x >= d.X && x < d.X+d.Length
		}
		if hit && pressed {
			copy := d
			a.dividerDrag = &copy
			return
		}
	}
	target := -1
	header := false
	for i := range a.panes {
		r := a.rect(i)
		if x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H {
			target = i
			break
		}
		if len(a.panes) > 1 && y == r.Y-1 && x >= r.X && x < r.X+r.W {
			target = i
			header = true
			break
		}
	}
	if target < 0 {
		return
	}
	r := a.rect(target)
	pane := a.panes[target]
	offset := pane.Buffer.Doc.Text.Len()
	if row := y - r.Y; !header && row >= 0 && row < len(pane.Rows) {
		projected := pane.Rows[row]
		gutter := len(fmt.Sprint(pane.Buffer.Doc.Text.Lines())) + 3
		column := max(0, x-r.X-gutter) + projected.Column
		offset = min(projected.End, a.lineOffset(pane.Buffer, projected.Line, column))
	}
	if target != a.focus {
		a.focusPane(target)
	}
	a.treeFocus = false
	if a.Current().Terminal != nil && !header {
		a.terminalMouse(ev, r, pressed, right)
		return
	}
	if buttons&tcell.WheelUp != 0 {
		a.scrollRows(-3)
		return
	}
	if buttons&tcell.WheelDown != 0 {
		a.scrollRows(3)
		return
	}
	if header {
		if right {
			a.editingContext(x, y)
		}
		return
	}
	b := a.Current()
	if pressed && x == r.X+len(strconv.Itoa(b.Doc.Text.Lines()))+2 {
		row := y - r.Y
		if row >= 0 && row < len(a.panes[a.focus].Rows) {
			a.foldCommand("toggle", a.panes[a.focus].Rows[row].Line)
		}
		return
	}
	if pressed && !b.Transient && b.File.Path != "" && x == r.X+len(strconv.Itoa(b.Doc.Text.Lines()))+1 {
		row := y - r.Y
		if row >= 0 && row < len(a.panes[a.focus].Rows) {
			a.toggleBreakpointAt(b, a.panes[a.focus].Rows[row].Line+1)
		}
		return
	}
	if right {
		inside := false
		for _, s := range b.Doc.Selections {
			inside = inside || (offset >= s.Start() && offset < s.End())
		}
		if !inside {
			b.Doc.Selections = []text.Selection{{Anchor: offset, Head: offset}}
		}
		a.editingContext(x, y)
		return
	}
	if pressed && b.Reading != nil && ev.Modifiers()&tcell.ModCtrl != 0 {
		b.Doc.Selections = []text.Selection{{Anchor: offset, Head: offset}}
		a.followReadingLink()
		return
	}
	if pressed {
		if x == a.mouseClickX && y == a.mouseClickY && time.Since(a.mouseClickTime) < 400*time.Millisecond {
			a.mouseClicks = a.mouseClicks%3 + 1
		} else {
			a.mouseClicks = 1
		}
		a.mouseClickX, a.mouseClickY, a.mouseClickTime = x, y, time.Now()
		anchor := offset
		if ev.Modifiers()&tcell.ModShift != 0 {
			anchor = b.Doc.Selections[0].Anchor
			a.mouseClicks = 1
		}
		b.Doc.Selections = []text.Selection{{Anchor: anchor, Head: offset}}
		if a.mouseClicks == 2 {
			start, end := wordRange(b.Doc.Text, offset)
			b.Doc.Selections = []text.Selection{{Anchor: start, Head: end}}
		} else if a.mouseClicks == 3 {
			line := b.Doc.Text.LineAt(offset)
			b.Doc.Selections = []text.Selection{{Anchor: b.Doc.Text.LineStart(line), Head: b.Doc.Text.LineStart(line + 1)}}
		}
		a.dragging = true
	} else if buttons&tcell.Button1 != 0 && a.dragging {
		b.Doc.Selections[0].Head = offset
	}
}
func wordRange(r text.Rope, offset int) (int, int) {
	start, end := r.LineStart(r.LineAt(offset)), r.LineEnd(r.LineAt(offset))
	source := r.Slice(start, end)
	at := min(len(source), max(0, offset-start))
	if at == len(source) && at > 0 {
		_, n := utf8.DecodeLastRuneInString(source)
		at -= n
	}
	if at == len(source) {
		return offset, offset
	}
	v, _ := utf8.DecodeRuneInString(source[at:])
	class := wordClass(v)
	lo, hi := at, at
	for lo > 0 {
		r, n := utf8.DecodeLastRuneInString(source[:lo])
		if wordClass(r) != class {
			break
		}
		lo -= n
	}
	for hi < len(source) {
		r, n := utf8.DecodeRuneInString(source[hi:])
		if wordClass(r) != class {
			break
		}
		hi += n
	}
	return start + lo, start + hi
}
func (a *App) previewFooter(x, y, w int, p palette, detail bool) {
	label := "Space toggle · Tab inspect"
	if a.Preview != nil && a.Preview.Strict {
		label = "Whole action · Tab inspect"
	}
	if detail {
		label = "↑↓ scroll · ←→ pan · Tab list"
	}
	a.put(x+1, y, w-19, label, p.dim)
	a.put(x+w-17, y, 17, "[Apply] [Cancel]", p.accent)
}
