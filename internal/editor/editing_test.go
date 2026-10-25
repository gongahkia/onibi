package editor

import (
	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/text"
	"os"
	"path/filepath"
	"testing"
)

func TestCustomShortcutsAndValidation(t *testing.T) {
	a := testApp(t)
	a.Config.Keybindings = map[string]string{"Alt+S": "Split vertically", "Ctrl+S": ""}
	if err := a.configureKeys(); err != nil {
		t.Fatal(err)
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
