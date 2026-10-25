package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func terminalUntil(t *testing.T, a *App, ready func() bool) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for !ready() {
		select {
		case fn := <-a.updates:
			fn()
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("terminal UI timed out:", a.Status)
		}
		a.draw()
	}
}
func TestTerminalKeyboardOwnershipAndTree(t *testing.T) {
	a := testApp(t)
	a.Root = t.TempDir()
	a.Config.TerminalShell = []string{"/bin/sh", "-c", "stty -echo; printf 'READY\\n'; read value; printf 'INPUT:%s\\n' \"$value\"; read value"}
	a.newTerminal()
	t.Cleanup(a.closeTerminals)
	b := a.Current()
	v := b.Terminal
	terminalUntil(t, a, func() bool { return v.Session != nil && strings.Contains(v.Session.Frame().String(), "READY") })
	key(a, tcell.KeyCtrlRightSq)
	if v.Input {
		t.Fatal("escape did not restore editor controls")
	}
	a.saveAs()
	if a.Dialog != nil || !strings.Contains(a.Status, "Export terminal screen") {
		t.Fatal("terminal became an editable empty file")
	}
	v.Input = true
	for _, r := range "hello" {
		a.Handle(tcell.NewEventKey(tcell.KeyRune, r, 0))
	}
	key(a, tcell.KeyEnter)
	terminalUntil(t, a, func() bool { return strings.Contains(v.Session.Frame().String(), "INPUT:hello") })
	if b.Doc.Text.Len() != 0 {
		t.Fatal("terminal input mutated document")
	}
	a.treeVisible = true
	a.Files = []string{"one.txt", "two.txt"}
	a.rebuildTree()
	a.treeFocus = true
	key(a, tcell.KeyDown)
	if a.tree.Index != 1 {
		t.Fatal("terminal stole file tree navigation")
	}
	a.treeFocus = false
	a.Screen.(tcell.SimulationScreen).SetSize(100, 35)
	a.draw()
	terminalUntil(t, a, func() bool {
		return v.Session.Frame().Width == a.rect(a.focus).W && v.Session.Frame().Height == a.rect(a.focus).H
	})
	r := a.rect(a.focus)
	a.mouse(tcell.NewEventMouse(r.X, r.Y, tcell.Button1, 0))
	a.mouse(tcell.NewEventMouse(r.X+4, r.Y, tcell.Button1, 0))
	a.mouse(tcell.NewEventMouse(r.X+4, r.Y, 0, 0))
	if got := terminalSelection(v); got != "READY" {
		t.Fatal("terminal selection", got)
	}
	a.copyTerminalSelection()
	if a.clipboard != "READY" {
		t.Fatal("terminal clipboard")
	}
	a.exportTerminal()
	if a.Current() == b || !strings.Contains(a.Current().Doc.Text.String(), "INPUT:hello") {
		t.Fatal("export did not produce report")
	}
}
