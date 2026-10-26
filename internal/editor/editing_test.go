package editor

import (
	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/text"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCustomShortcutsAndValidation(t *testing.T) {
	a := testApp(t)
	a.Config.Keybindings = map[string]string{"Alt+S": "Split vertically", "Ctrl+S": ""}
	if err := a.configureKeys(); err != nil {
		t.Fatal(err)
	}
	for _, c := range a.Commands {
		if c.Name == "Save" && c.Key != "" {
			t.Fatal("palette shows disabled shortcut")
		}
	}
	a.Handle(tcell.NewEventKey(tcell.KeyRune, 's', tcell.ModAlt))
	if len(a.panes) != 2 {
		t.Fatal("key not bound")
	}
	a.Config.Keybindings["F7"] = "Does not exist"
	if err := a.configureKeys(); err == nil {
		t.Fatal("unknown command")
	}
}
func TestLineOperationsKeepCRLFAndUndo(t *testing.T) {
	for _, nl := range []string{"\n", "\r\n"} {
		a := testApp(t)
		b := a.Current()
		b.Newline = nl
		a.insert("one" + nl + "two")
		b.Doc.Selections = []text.Selection{{Anchor: len("one" + nl), Head: len("one" + nl)}}
		a.lines("up")
		if b.Doc.Text.String() != "two"+nl+"one" {
			t.Fatal(b.Doc.Text.String())
		}
		a.lines("down")
		if b.Doc.Text.String() != "one"+nl+"two" {
			t.Fatal(b.Doc.Text.String())
		}
		a.lines("duplicate")
		if b.Doc.Text.String() != "one"+nl+"two"+nl+"two" {
			t.Fatal(b.Doc.Text.String())
		}
		key(a, tcell.KeyCtrlZ)
		if b.Doc.Text.String() != "one"+nl+"two" {
			t.Fatal("undo")
		}
	}
}
func TestWordEditingAndComments(t *testing.T) {
	a := testApp(t)
	a.insert("你好_world next")
	a.word(-1, false, true)
	if a.Current().Doc.Text.String() != "你好_world " {
		t.Fatal(a.Current().Doc.Text.String())
	}
	a.Current().File.Path = filepath.Join(t.TempDir(), "main.go")
	a.commentLines()
	if a.Current().Doc.Text.String() != "// 你好_world " {
		t.Fatal(a.Current().Doc.Text.String())
	}
	a.commentLines()
	if a.Current().Doc.Text.String() != "你好_world " {
		t.Fatal("uncomment")
	}
}
func TestSaveAllContinuesAfterUntitled(t *testing.T) {
	a := testApp(t)
	a.insert("first")
	a.NewBuffer()
	a.insert("second")
	done := false
	a.saveAll(func() { done = true })
	if a.Dialog == nil {
		t.Fatal("missing Save as")
	}
	accept := a.Dialog.Accept
	a.Dialog = nil
	accept(filepath.Join(t.TempDir(), "a"))
	if done || a.Dialog == nil {
		t.Fatal("did not continue to second")
	}
	accept = a.Dialog.Accept
	a.Dialog = nil
	accept(filepath.Join(t.TempDir(), "b"))
	if !done {
		t.Fatal("did not finish")
	}
	for _, b := range a.Buffers {
		if b.Doc.Dirty() {
			t.Fatal("dirty")
		}
	}
}
func TestReloadIsUndoable(t *testing.T) {
	a := testApp(t)
	path := filepath.Join(t.TempDir(), "file")
	os.WriteFile(path, []byte("before"), 0600)
	a.Open(path)
	os.WriteFile(path, []byte("after"), 0600)
	a.reload()
	if a.Current().Doc.Text.String() != "after" || a.Current().Doc.Dirty() {
		t.Fatal(a.Status)
	}
	key(a, tcell.KeyCtrlZ)
	if a.Current().Doc.Text.String() != "before" || !a.Current().Doc.Dirty() {
		t.Fatal("reload undo")
	}
}

func TestPaletteAndHelpShortcuts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event *tcell.EventKey
		title string
	}{
		{"palette lowercase", tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModCtrl), "Commands"},
		{"palette uppercase", tcell.NewEventKey(tcell.KeyRune, 'E', tcell.ModCtrl), "Commands"},
		{"palette control key", tcell.NewEventKey(tcell.KeyCtrlE, 0, 0), "Commands"},
		{"palette fallback", tcell.NewEventKey(tcell.KeyF2, 0, 0), "Commands"},
		{"file picker", tcell.NewEventKey(tcell.KeyCtrlP, 0, 0), "Project files"},
		{"help", tcell.NewEventKey(tcell.KeyRune, 'h', tcell.ModCtrl), "Help"},
		{"help fallback", tcell.NewEventKey(tcell.KeyF1, 0, 0), "Help"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			a.insert("keep me")
			a.Handle(tc.event)
			if a.Dialog == nil || !strings.HasPrefix(a.Dialog.Title, tc.title) {
				t.Fatalf("expected %s dialog, got %+v", tc.title, a.Dialog)
			}
			if a.Current().Doc.Text.String() != "keep me" {
				t.Fatal("shortcut edited text")
			}
		})
	}
	a := testApp(t)
	a.Handle(tcell.NewEventKey(tcell.KeyRune, 'p', tcell.ModCtrl|tcell.ModShift))
	if a.Dialog != nil && strings.HasPrefix(a.Dialog.Title, "Commands") {
		t.Fatal("old palette shortcut is still bound")
	}
	a.Dialog = nil
	a.insert("abc")
	key(a, tcell.KeyBackspace)
	key(a, tcell.KeyBackspace2)
	if a.Current().Doc.Text.String() != "a" || a.Dialog != nil {
		t.Fatal("Backspace must still delete")
	}
}

func TestModifiedPaletteShortcutOverrides(t *testing.T) {
	a := testApp(t)
	a.Config.Keybindings = map[string]string{"Ctrl+E": "", "Ctrl+H": "Command palette"}
	if err := a.configureKeys(); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []*tcell.EventKey{
		tcell.NewEventKey(tcell.KeyRune, 'e', tcell.ModCtrl),
		tcell.NewEventKey(tcell.KeyRune, 'E', tcell.ModCtrl),
		tcell.NewEventKey(tcell.KeyCtrlE, 0, 0),
	} {
		a.Handle(ev)
		if a.Dialog != nil {
			t.Fatal("disabled palette shortcut ran")
		}
	}
	key(a, tcell.KeyCtrlH)
	if a.Dialog == nil || !strings.HasPrefix(a.Dialog.Title, "Commands") {
		t.Fatal("help override ignored")
	}
}
