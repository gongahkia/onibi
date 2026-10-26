package editor

import (
	"fmt"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/gdamore/tcell/v2"
	term "github.com/gongahkia/shack/internal/terminal"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

type terminalView struct {
	Session         *term.Session
	Input, Starting bool
	Width, Height   int
	Error           string
	Pending         atomic.Bool
	Selection       *term.Frame
	Anchor, Head    int
	Selecting       bool
	LastButton      vt.MouseButton
}

func (a *App) terminalCommands() {
	a.Commands = append(a.Commands, command{"New terminal", "", a.newTerminal}, command{"Resume terminal input", "", func() {
		if v := a.Current().Terminal; v != nil {
			v.Input = true
			v.Selection = nil
			a.Status = "Terminal input · Ctrl+] returns to editor controls"
		}
	}}, command{"Stop terminal", "", func() {
		if v := a.Current().Terminal; v != nil && v.Session != nil {
			v.Session.Close()
			v.Input = false
		}
	}}, command{"Copy terminal selection", "", a.copyTerminalSelection}, command{"Terminal scroll to bottom", "", func() {
		if v := a.Current().Terminal; v != nil && v.Session != nil {
			v.Selection = nil
			a.terminalError(v.Session.Scroll(0))
		}
	}}, command{"Export terminal screen", "", a.exportTerminal}, command{"Send Ctrl+] to terminal", "", func() {
		if v := a.Current().Terminal; v != nil && v.Session != nil {
			a.terminalError(v.Session.Text("\x1d"))
		}
	}})
}
func (a *App) newTerminal() {
	count := 0
	for _, b := range a.Buffers {
		if b.Terminal != nil {
			count++
		}
	}
	if count >= 4 {
		a.Status = "Close a terminal buffer before opening a fifth terminal"
		return
	}
	argv := append([]string(nil), a.Config.TerminalShell...)
	if len(argv) == 0 {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		argv = []string{shell, "-i"}
	}
	a.NewBuffer()
	a.treeFocus = false
	b := a.Current()
	b.Transient = true
	a.terminalSequence++
	b.Title = fmt.Sprintf("Terminal %d", a.terminalSequence)
	view := &terminalView{Input: true, Starting: true}
	b.Terminal = view
	a.ensurePanes()
	r := a.rect(a.focus)
	view.Width, view.Height = max(2, r.W), max(2, r.H)
	root, history := a.Root, a.Config.TerminalScrollback
	width, height := view.Width, view.Height
	a.Status = "Starting terminal · Ctrl+] returns to editor controls"
	go func() {
		s, err := term.Start(a.ctx, argv, root, width, height, history, func() {
			if !view.Pending.Swap(true) {
				a.post(func() {
					view.Pending.Store(false)
					if view.Session != nil {
						if f := view.Session.Frame(); f != nil && f.Exited && b == a.Current() {
							view.Input = false
							a.Status = "Terminal " + f.Exit + " · Ctrl+W closes the buffer"
						}
					}
				})
			}
		})
		a.post(func() {
			view.Starting = false
			if err != nil {
				view.Error = err.Error()
				view.Input = false
				a.Status = "Terminal: " + err.Error()
				return
			}
			found := false
			for _, open := range a.Buffers {
				found = found || open == b
			}
			if !found {
				s.Close()
				return
			}
			view.Session = s
			if b == a.Current() {
				a.Status = "Terminal input · Ctrl+] editor controls · Shift+PgUp scrollback"
			}
		})
	}()
}
func (a *App) terminalError(err error) {
	if err != nil {
		a.Status = err.Error()
	}
}
func (a *App) terminalKey(ev *tcell.EventKey) bool {
	v := a.Current().Terminal
	if v == nil || !v.Input || a.treeFocus {
		return false
	}
	if ev.Key() == tcell.KeyCtrlRightSq {
		v.Input = false
		a.Status = "Editor controls · Ctrl+E commands · Resume terminal input to return"
		return true
	}
	if v.Session == nil {
		a.Status = "Terminal is starting"
		return true
	}
	v.Selection = nil
	v.Selecting = false
	if ev.Modifiers()&tcell.ModShift != 0 && (ev.Key() == tcell.KeyPgUp || ev.Key() == tcell.KeyPgDn) {
		delta := max(1, a.rect(a.focus).H-1)
		if ev.Key() == tcell.KeyPgDn {
			delta = -delta
		}
		a.terminalError(v.Session.Scroll(delta))
		return true
	}
	a.terminalError(sendTerminalKey(v.Session, ev))
	return true
}
func sendTerminalKey(s *term.Session, ev *tcell.EventKey) error {
	mod := vt.KeyMod(0)
	if ev.Modifiers()&tcell.ModShift != 0 {
		mod |= vt.ModShift
	}
	if ev.Modifiers()&tcell.ModAlt != 0 {
		mod |= vt.ModAlt
	}
	if ev.Modifiers()&tcell.ModCtrl != 0 {
		mod |= vt.ModCtrl
	}
	key := ev.Key()
	// tcell separates named Ctrl keys from their ASCII control bytes.
	if key >= tcell.KeyCtrlSpace && key <= tcell.KeyCtrlUnderscore {
		key -= tcell.KeyCtrlSpace
	}
	if key >= 0 && key < 32 {
		value := string([]byte{byte(key)})
		if mod&vt.ModAlt != 0 {
			value = "\x1b" + value
		}
		return s.Text(value)
	}
	if key == tcell.KeyRune {
		if mod&vt.ModCtrl != 0 && ev.Rune() >= 'a' && ev.Rune() <= 'z' {
			value := string([]byte{byte(ev.Rune() - 'a' + 1)})
			if mod&vt.ModAlt != 0 {
				value = "\x1b" + value
			}
			return s.Text(value)
		}
		return s.Key(vt.KeyPressEvent{Code: ev.Rune(), Text: string(ev.Rune()), Mod: mod})
	}
	code := rune(0)
	final := ""
	tilde := 0
	switch key {
	case tcell.KeyLeft:
		code, final = vt.KeyLeft, "D"
	case tcell.KeyRight:
		code, final = vt.KeyRight, "C"
	case tcell.KeyUp:
		code, final = vt.KeyUp, "A"
	case tcell.KeyDown:
		code, final = vt.KeyDown, "B"
	case tcell.KeyHome:
		code, final = vt.KeyHome, "H"
	case tcell.KeyEnd:
		code, final = vt.KeyEnd, "F"
	case tcell.KeyPgUp:
		code, tilde = vt.KeyPgUp, 5
	case tcell.KeyPgDn:
		code, tilde = vt.KeyPgDown, 6
	case tcell.KeyInsert:
		code, tilde = vt.KeyInsert, 2
	case tcell.KeyDelete:
		code, tilde = vt.KeyDelete, 3
	case tcell.KeyBackspace2:
		code = vt.KeyBackspace
	case tcell.KeyBacktab:
		code = vt.KeyTab
		mod |= vt.ModShift
	default:
		if key >= tcell.KeyF1 && key <= tcell.KeyF12 {
			code = vt.KeyF1 + rune(key-tcell.KeyF1)
			if key <= tcell.KeyF4 {
				final = string('P' + rune(key-tcell.KeyF1))
			} else {
				tilde = []int{15, 17, 18, 19, 20, 21, 23, 24}[key-tcell.KeyF5]
			}
		}
	}
	if code == 0 {
		return fmt.Errorf("terminal key %s is unsupported", ev.Name())
	}
	if mod != 0 && (final != "" || tilde > 0) {
		n := 1
		if mod&vt.ModShift != 0 {
			n++
		}
		if mod&vt.ModAlt != 0 {
			n += 2
		}
		if mod&vt.ModCtrl != 0 {
			n += 4
		}
		if final != "" {
			return s.Text(fmt.Sprintf("\x1b[1;%d%s", n, final))
		}
		return s.Text(fmt.Sprintf("\x1b[%d;%d~", tilde, n))
	}
	return s.Key(vt.KeyPressEvent{Code: code, Mod: mod})
}
func (a *App) drawTerminal(index int, p palette) {
	b := a.panes[index].Buffer
	v := b.Terminal
	r := a.rect(index)
	active := index == a.focus
	if len(a.panes) > 1 {
		style := p.dim
		if active {
			style = p.accent
		}
		a.put(r.X, r.Y-1, r.W, fmt.Sprintf("%d · %s", index+1, bufferName(b)), style)
	}
	if v.Session == nil {
		message := "Starting terminal…"
		if v.Error != "" {
			message = v.Error
		}
		a.put(r.X, r.Y, r.W, message, p.warning)
		return
	}
	if active && (v.Width != r.W || v.Height != r.H) {
		v.Width, v.Height = r.W, r.H
		v.Session.Resize(r.W, r.H)
	}
	f := v.Selection
	if f == nil {
		f = v.Session.Frame()
	}
	if f == nil {
		return
	}
	for y := 0; y < min(r.H, f.Height); y++ {
		for x := 0; x < min(r.W, f.Width); x++ {
			i := y*f.Width + x
			c := f.Cells[i]
			if c.Width == 0 {
				continue
			}
			if x+int(c.Width) > r.W {
				break
			}
			style := p.text
			if c.FG >= 0 {
				style = style.Foreground(tcell.NewHexColor(c.FG))
			}
			if c.BG >= 0 {
				style = style.Background(tcell.NewHexColor(c.BG))
			}
			style = style.Bold(c.Attrs&uv.AttrBold != 0).Dim(c.Attrs&uv.AttrFaint != 0).Italic(c.Attrs&uv.AttrItalic != 0).Reverse(c.Attrs&uv.AttrReverse != 0).StrikeThrough(c.Attrs&uv.AttrStrikethrough != 0).Underline(c.Underline != 0)
			if v.Selection != nil && i >= min(v.Anchor, v.Head) && i <= max(v.Anchor, v.Head) {
				style = style.Background(p.selectionBackground())
			}
			value := c.Text
			if value == "" || c.Attrs&uv.AttrConceal != 0 {
				value = " "
			}
			a.paintCell(r.X+x, r.Y+y, value, style)
		}
	}
	if active && v.Input && !f.Hidden && v.Selection == nil && a.Menu == nil && a.Dialog == nil && a.Preview == nil && f.CursorX < r.W && f.CursorY < r.H {
		a.Screen.ShowCursor(r.X+f.CursorX, r.Y+f.CursorY)
	}
}
func (a *App) terminalMouse(ev *tcell.EventMouse, r rectangle, pressed, right bool) {
	v := a.Current().Terminal
	if v == nil || v.Session == nil {
		return
	}
	x, y := ev.Position()
	x, y = x-r.X, y-r.Y
	f := v.Session.Frame()
	if f == nil {
		return
	}
	shift := ev.Modifiers()&tcell.ModShift != 0
	if right && (!f.Mouse || shift) {
		a.showContext(x+r.X, y+r.Y, []choice{{"Copy terminal selection", a.copyTerminalSelection}, {"Paste into terminal", func() { v.Input = true; a.pasteClipboard() }}, {"Resume terminal input", func() { v.Input = true; v.Selection = nil }}, {"Editor controls", func() { v.Input = false }}, {"Export terminal screen", a.exportTerminal}, {"Stop terminal", func() { v.Session.Close(); v.Input = false }}, {"Close terminal buffer", a.closeBuffer}})
		return
	}
	buttons := ev.Buttons()
	if buttons&(tcell.WheelUp|tcell.WheelDown) != 0 && (!f.Mouse || shift) {
		delta := -3
		if buttons&tcell.WheelUp != 0 {
			delta = 3
		}
		v.Selection = nil
		a.terminalError(v.Session.Scroll(delta))
		return
	}
	if f.Mouse && !shift {
		v.Input = true
		mod := vt.KeyMod(0)
		if ev.Modifiers()&tcell.ModAlt != 0 {
			mod |= vt.ModAlt
		}
		if ev.Modifiers()&tcell.ModCtrl != 0 {
			mod |= vt.ModCtrl
		}
		button := vt.MouseNone
		switch {
		case buttons&tcell.Button1 != 0:
			button = vt.MouseLeft
		case buttons&tcell.Button2 != 0:
			button = vt.MouseRight
		case buttons&tcell.Button3 != 0:
			button = vt.MouseMiddle
		case buttons&tcell.WheelUp != 0:
			button = vt.MouseWheelUp
		case buttons&tcell.WheelDown != 0:
			button = vt.MouseWheelDown
		}
		mouse := uv.Mouse{X: x, Y: y, Button: button, Mod: mod}
		var event vt.Mouse
		if button == vt.MouseWheelUp || button == vt.MouseWheelDown {
			event = vt.MouseWheel(mouse)
		} else if button == vt.MouseNone {
			mouse.Button = v.LastButton
			event = vt.MouseRelease(mouse)
		} else if !pressed && !right && v.LastButton == button {
			event = vt.MouseMotion(mouse)
		} else {
			event = vt.MouseClick(mouse)
		}
		v.LastButton = button
		a.terminalError(v.Session.Mouse(event))
		return
	}
	if pressed {
		v.Input = true
		v.Selection = f
		v.Anchor = max(0, min(len(f.Cells)-1, y*f.Width+x))
		v.Head = v.Anchor
		v.Selecting = true
	} else if buttons&tcell.Button1 != 0 && v.Selecting && v.Selection != nil {
		v.Head = max(0, min(len(v.Selection.Cells)-1, y*v.Selection.Width+x))
	} else if buttons&tcell.Button1 == 0 {
		v.Selecting = false
		if v.Anchor == v.Head {
			v.Selection = nil
		}
	}
}
func terminalSelection(v *terminalView) string {
	f := v.Selection
	if f == nil {
		return ""
	}
	start, end := max(0, min(v.Anchor, v.Head)), min(len(f.Cells)-1, max(v.Anchor, v.Head))
	var out, row strings.Builder
	for i := start; i <= end; i++ {
		c := f.Cells[i]
		if c.Width > 0 {
			if c.Text == "" {
				row.WriteByte(' ')
			} else {
				row.WriteString(c.Text)
			}
		}
		if i%f.Width == f.Width-1 || i == end {
			out.WriteString(strings.TrimRight(row.String(), " "))
			row.Reset()
			if i < end {
				out.WriteByte('\n')
			}
		}
	}
	return out.String()
}
func (a *App) copyTerminalSelection() {
	v := a.Current().Terminal
	if v == nil {
		return
	}
	value := terminalSelection(v)
	if value == "" {
		a.Status = "Drag in the terminal to select; hold Shift when the application captures the mouse"
		return
	}
	a.clipboard = value
	a.Screen.SetClipboard([]byte(value))
	a.Status = "Terminal selection copied internally; clipboard write requested"
}
func (a *App) exportTerminal() {
	v := a.Current().Terminal
	if v == nil || v.Session == nil {
		return
	}
	f := v.Session.Frame()
	if f == nil {
		return
	}
	a.report("Terminal screen", f.String())
}
func (a *App) closeTerminals() {
	sessions := []*term.Session{}
	for _, b := range a.Buffers {
		if v := b.Terminal; v != nil && v.Session != nil {
			v.Session.Close()
			sessions = append(sessions, v.Session)
		}
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for _, s := range sessions {
		select {
		case <-s.Done():
		case <-deadline.C:
			return
		}
	}
}
