package editor

import (
	"context"
	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/config"
	"github.com/gongahkia/shack/internal/text"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testApp(t *testing.T) *App {
	t.Helper()
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(80, 24)
	a, err := New(s, config.Default(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.cancel(); s.Fini() })
	return a
}
func key(a *App, k tcell.Key) { a.Handle(tcell.NewEventKey(k, 0, 0)) }
func TestGraphemesPasteAndUndo(t *testing.T) {
	a := testApp(t)
	a.pasteText("你好 e\u0301 👩‍💻")
	key(a, tcell.KeyBackspace2)
	if a.Current().Doc.Text.String() != "你好 e\u0301 " {
		t.Fatal(a.Current().Doc.Text.String())
	}
	key(a, tcell.KeyCtrlZ)
	if a.Current().Doc.Text.String() != "你好 e\u0301 👩‍💻" {
		t.Fatal("grapheme undo")
	}
	a.NewBuffer()
	a.Handle(tcell.NewEventPaste(true))
	for _, r := range "a\n  b" {
		k := tcell.KeyRune
		if r == '\n' {
			k = tcell.KeyCtrlJ
		}
		a.Handle(tcell.NewEventKey(k, r, 0))
	}
	a.Handle(tcell.NewEventPaste(false))
	if a.Current().Doc.Text.String() != "a\n  b" {
		t.Fatal("paste changed whitespace")
	}
	key(a, tcell.KeyCtrlZ)
	if a.Current().Doc.Text.Len() != 0 {
		t.Fatal("paste was not one undo step")
	}
}
func TestPreviewSelectionAndStaleResult(t *testing.T) {
	a := testApp(t)
	a.insert("one one one")
	b := a.Current()
	edits, err := text.Replacements(context.Background(), b.Doc.Text.String(), "one", "two", false)
	if err != nil {
		t.Fatal(err)
	}
	a.showPreview("replace", b, b.Doc.Version, edits)
	a.Preview.Checked[1] = false
	key(a, tcell.KeyEnter)
	if b.Doc.Text.String() != "two one two" {
		t.Fatal(b.Doc.Text.String())
	}
	key(a, tcell.KeyCtrlZ)
	if b.Doc.Text.String() != "one one one" {
		t.Fatal("batch undo")
	}
	a.showPreview("stale", b, b.Doc.Version, edits)
	b.Doc.Insert("!")
	key(a, tcell.KeyEnter)
	if !strings.Contains(a.Status, "changed") {
		t.Fatal("stale preview applied")
	}
}
func TestViewportAndSmallScreen(t *testing.T) {
	a := testApp(t)
	a.insert(strings.Repeat("line\n", 100))
	a.draw()
	if a.Current().Top < 70 {
		t.Fatal("caret off screen")
	}
	a.Screen.(tcell.SimulationScreen).SetSize(3, 2)
	a.palette()
	a.draw()
	a.Screen.(tcell.SimulationScreen).SetSize(80, 24)
	a.draw()
	key(a, tcell.KeyEscape)
	a.Readonly = true
	before := a.Current().Doc.Text
	key(a, tcell.KeyBackspace2)
	if !a.Current().Doc.Text.Same(before) {
		t.Fatal("readonly changed")
	}
}
func TestBundledHighlightingAndCRLF(t *testing.T) {
	d, _ := text.NewDocument("package main\r\nfunc main() {}\r\n")
	spans := highlight(make(chan struct{}), syntaxJob{Text: d.Text, Path: "main.go", Theme: "dark"})
	if len(spans) == 0 {
		t.Fatal("missing bundled lexer")
	}
	for _, s := range spans {
		if s.Start < 0 || s.End > d.Text.Len() {
			t.Fatal("highlight offsets")
		}
	}
	p := filepath.Join(t.TempDir(), "file.txt")
	os.WriteFile(p, []byte("a\r\nb"), 0644)
	a := testApp(t)
	if err := a.Open(p); err != nil {
		t.Fatal(err)
	}
	a.pasteText("x\ny")
	if a.Current().Doc.Text.String() != "x\r\nya\r\nb" {
		t.Fatal("CRLF paste")
	}
}

func TestIndentBlockAndIgnoreStaleClipboard(t *testing.T) {
	a := testApp(t)
	a.insert("one\ntwo\n")
	key(a, tcell.KeyCtrlA)
	key(a, tcell.KeyTab)
	if a.Current().Doc.Text.String() != "    one\n    two\n" {
		t.Fatal("Tab replaced selected text")
	}
	key(a, tcell.KeyCtrlZ)
	if a.Current().Doc.Text.String() != "one\ntwo\n" {
		t.Fatal("indent undo")
	}
	a.clipboardRequested = true
	a.clipboardTarget = a.Current()
	a.clipboardVersion = a.Current().Doc.Version
	a.clipboardSelections = append([]text.Selection(nil), a.Current().Doc.Selections...)
	key(a, tcell.KeyLeft)
	a.Handle(tcell.NewEventClipboard([]byte("stale clipboard")))
	if strings.Contains(a.Current().Doc.Text.String(), "stale") {
		t.Fatal("late clipboard pasted at another location")
	}
}
