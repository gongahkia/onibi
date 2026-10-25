package editor

import (
	"context"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/folding"
	"github.com/gongahkia/shack/internal/text"
)

func foldedApp(t *testing.T) (*App, *Buffer) {
	t.Helper()
	a := testApp(t)
	b := a.Current()
	b.File.Path = "/tmp/fold.go"
	a.insert("func f() {\n  if true {\n    println(\"世界\")\n  }\n}\nafter\n")
	b.Doc.Selections = []text.Selection{{}}
	a.draw()
	return a, b
}
func finishFold(t *testing.T, a *App) {
	t.Helper()
	actionUntil(t, a, func() bool { return !a.foldBusy && a.foldPending == nil })
}
func TestFoldingNavigationAndText(t *testing.T) {
	a, b := foldedApp(t)
	original := b.Doc.Text.String()
	version := b.Doc.Version
	key(a, tcell.KeyF6)
	finishFold(t, a)
	a.draw()
	s := a.panes[0].Folds
	if len(s.Closed) != 1 || len(a.panes[0].Rows) < 2 || a.panes[0].Rows[1].Line != 5 {
		t.Fatalf("%#v %#v", s, a.panes[0].Rows)
	}
	key(a, tcell.KeyDown)
	if line := b.Doc.Text.LineAt(b.Doc.Selections[0].Head); line != 5 {
		t.Fatal(line)
	}
	key(a, tcell.KeyUp)
	if b.Doc.Text.LineAt(b.Doc.Selections[0].Head) != 0 {
		t.Fatal(b.Doc.Selections)
	}
	a.scrollRows(1)
	if b.Top != 5 {
		t.Fatal(b.Top)
	}
	a.scrollRows(-1)
	if b.Top != 0 {
		t.Fatal(b.Top)
	}
	if b.Doc.Text.String() != original || b.Doc.Version != version {
		t.Fatal("folding edited source")
	}
	// Search/debug/go-to navigation into a hidden range opens its ancestors.
	p := b.Doc.Text.LineStart(2)
	b.Doc.Selections = []text.Selection{{Anchor: p, Head: p}}
	a.reveal()
	if len(s.Closed) != 0 {
		t.Fatal("hidden caret")
	}
	a.foldCommand("all", -1)
	if len(s.Closed) != 2 {
		t.Fatal(s.Closed)
	}
	a.foldCommand("toggle", 0)
	if len(s.Closed) != 1 || s.Visible[0].Start != 1 {
		t.Fatal("nested child fold lost", s.Closed)
	}
	a.foldCommand("unfold", -1)
	if len(s.Closed) != 0 {
		t.Fatal(s.Closed)
	}
}
func TestFoldsIndependentAcrossPanesAndEdits(t *testing.T) {
	a, b := foldedApp(t)
	a.foldCommand("all", -1)
	finishFold(t, a)
	left := a.panes[0].Folds
	a.split(false)
	right := a.panes[1].Folds
	a.foldCommand("unfold", -1)
	if len(left.Closed) != 2 || len(right.Closed) != 0 {
		t.Fatal("shared fold state")
	}
	// Editing before another folded region rebases it; touching it expands it.
	a.insert("// before\n")
	if len(left.Closed) != 1 || left.Visible[0].Start != 2 {
		t.Fatal("header edit did not expand parent while preserving child", left.Visible)
	}
	a.foldCommand("all", -1)
	finishFold(t, a)
	right = a.panes[1].Folds
	b.Doc.Selections = []text.Selection{{}}
	a.insert("// extra\n")
	if len(right.Closed) != 2 || right.Visible[0].Start != 2 {
		t.Fatal(right.Closed, right.Visible)
	}
	a.focusPane(0)
	p := b.Doc.Text.LineStart(4)
	b.Doc.Selections = []text.Selection{{Anchor: p, Head: p}}
	a.insert("changed")
	if len(right.Closed) != 0 {
		t.Fatal("other pane hid edited lines")
	}
}
func TestFoldingWrapMouseAndStaleResult(t *testing.T) {
	a, b := foldedApp(t)
	a.wrap = true
	a.foldCommand("toggle", 0)
	finishFold(t, a)
	a.draw()
	key(a, tcell.KeyDown)
	if b.Doc.Text.LineAt(b.Doc.Selections[0].Head) != 5 {
		t.Fatal("wrapped navigation entered fold")
	}
	key(a, tcell.KeyUp)
	a.draw()
	r := a.rect(0)
	click(a, r.X+a.gutter()-1, r.Y, tcell.Button1)
	if len(a.panes[0].Folds.Closed) != 0 {
		t.Fatal("gutter failed")
	}
	// Invalidating a requested scan must not fold the replacement document.
	b.FoldCache = nil
	a.foldCommand("all", -1)
	b.Doc, _ = text.NewDocument("replacement\n")
	finishFold(t, a)
	a.draw()
	if a.panes[0].Folds != nil || b.FoldCache != nil {
		t.Fatal("stale scan applied")
	}
}
func TestFoldingSelectionAndUndo(t *testing.T) {
	a, b := foldedApp(t)
	a.foldCommand("all", -1)
	finishFold(t, a)
	b.Doc.Selections = []text.Selection{{Anchor: 0, Head: b.Doc.Text.Len()}}
	a.copy(false)
	if a.clipboard != b.Doc.Text.String() {
		t.Fatal("hidden text omitted from copy")
	}
	a.insert("replacement")
	if len(a.panes[0].Folds.Closed) != 0 {
		t.Fatal("edited folds persisted")
	}
	key(a, tcell.KeyCtrlZ)
	if !strings.Contains(b.Doc.Text.String(), "世界") {
		t.Fatal("undo lost hidden text")
	}
}
func TestFoldLargeProjection(t *testing.T) {
	a := testApp(t)
	b := a.Current()
	src := "func f() {\n" + strings.Repeat("  世界\n", 100000) + "}\nafter\n"
	b.Doc, _ = text.NewDocument(src)
	b.File.Path = "large.go"
	a.foldCommand("all", -1)
	finishFold(t, a)
	a.draw()
	if len(a.panes[0].Rows) < 2 || a.panes[0].Rows[1].Line != 100002 {
		t.Fatal(a.panes[0].Rows)
	}
	key(a, tcell.KeyDown)
	if b.Doc.Text.LineAt(b.Doc.Selections[0].Head) != 100002 || b.Top != 0 {
		t.Fatal("large fold reveal", b.Top)
	}
}
func BenchmarkFoldProjection(b *testing.B) {
	r := text.New("func f() {\n" + strings.Repeat("  世界\n", 100000) + "}\nafter\n")
	ranges, e := folding.Find(context.Background(), r.String(), "x.go")
	if e != nil {
		b.Fatal(e)
	}
	buffer := &Buffer{Doc: &text.Document{Text: r}}
	s := &foldState{Buffer: buffer, Closed: ranges}
	s.project()
	b.ReportAllocs()
	for b.Loop() {
		for line := 0; line < 100005; {
			if f, ok := s.hidden(line + 1); ok {
				line = f.End + 1
			} else {
				line++
			}
		}
	}
}
