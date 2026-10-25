package editor

import (
	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/text"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func click(a *App, x, y int, button tcell.ButtonMask) {
	a.Handle(tcell.NewEventMouse(x, y, button, 0))
	a.Handle(tcell.NewEventMouse(x, y, tcell.ButtonNone, 0))
}
func TestNestedSplitsResizeCloseAndSession(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	a := testApp(t)
	a.Screen.(tcell.SimulationScreen).SetSize(120, 40)
	a.Root = t.TempDir()
	path := filepath.Join(a.Root, "file")
	os.WriteFile(path, []byte("one\ntwo"), 0600)
	a.Open(path)
	a.draw()
	a.split(false)
	a.split(true)
	a.draw()
	left, top, bottom := a.rect(0), a.rect(1), a.rect(2)
	if left.X >= top.X || top.X != bottom.X || bottom.Y <= top.Y || left.H <= top.H {
		t.Fatal("split lost nesting", left, top, bottom)
	}
	if !validLayout(a.layout, 3) {
		t.Fatal("invalid layout")
	}
	a.mouse(tcell.NewEventMouse(59, 8, tcell.Button1, 0))
	a.mouse(tcell.NewEventMouse(70, 8, tcell.Button1, 0))
	a.mouse(tcell.NewEventMouse(70, 8, 0, 0))
	if a.layout.Ratio <= 500 {
		t.Fatal("divider did not move")
	}
	if err := a.saveSession(); err != nil {
		t.Fatal(err)
	}
	b := testApp(t)
	b.Root = a.Root
	b.Screen.(tcell.SimulationScreen).SetSize(120, 40)
	b.restoreSession()
	if !validLayout(b.layout, 3) || b.layout.Ratio != a.layout.Ratio || !b.layout.Second.Horizontal {
		t.Fatal("lost nested session")
	}
	b.closePane()
	if !validLayout(b.layout, 2) || b.layout.Second == nil || !b.layout.Second.leaf() {
		t.Fatal("sibling did not expand")
	}
	b.Screen.(tcell.SimulationScreen).SetSize(20, 5)
	b.Handle(tcell.NewEventResize(20, 5))
	b.draw()
}
func TestTreeKeyboardMouseAndNarrowTerminal(t *testing.T) {
	a := testApp(t)
	a.Root = t.TempDir()
	os.Mkdir(filepath.Join(a.Root, "src"), 0700)
	os.WriteFile(filepath.Join(a.Root, "src", "main.go"), []byte("package main"), 0600)
	a.Files = []string{"README.txt", "src/main.go"}
	a.treeVisible = true
	a.rebuildTree()
	a.draw()
	if a.tree.Rows[0].Node.Name != "src" {
		t.Fatal("directories not first")
	}
	click(a, 3, 2, tcell.Button1)
	if !a.treeFocus || len(a.tree.Rows) != 3 {
		t.Fatal("directory did not expand")
	}
	a.draw()
	click(a, 5, 3, tcell.Button1)
	if filepath.Base(a.Current().File.Path) != "main.go" || a.treeFocus {
		t.Fatal("file did not open")
	}
	a.revealTree()
	a.treeFocus = true
	key(a, tcell.KeyLeft)
	if a.tree.Rows[a.tree.Index].Node.Name != "src" {
		t.Fatal("parent selection")
	}
	key(a, tcell.KeyLeft)
	if a.tree.Expanded["src"] {
		t.Fatal("folder did not collapse")
	}
	a.Screen.(tcell.SimulationScreen).SetSize(60, 24)
	a.draw()
	if a.sidebarWidth() != 0 || a.workspaceRect().W != 60 {
		t.Fatal("tree consumed narrow editor")
	}
}
func TestMouseContextSelectionAndPreview(t *testing.T) {
	a := testApp(t)
	a.insert("hello world")
	a.Current().Doc.Selections = []text.Selection{{Anchor: 0, Head: 5}}
	a.draw()
	click(a, 5, 1, tcell.Button2)
	if a.Menu == nil || a.Current().Doc.Selections[0].End() != 5 {
		t.Fatal("right-click lost selected text")
	}
	r := a.menuRect()
	click(a, r.X+2, r.Y+4, tcell.Button1)
	if a.Menu != nil || a.Current().Doc.Selections[0].End() != 11 {
		t.Fatal("context selection failed")
	}
	b := a.Current()
	a.showPreview("test", b, b.Doc.Version, []text.Edit{{Start: 0, End: 5, Text: "hi"}})
	a.draw()
	r = a.panelRect()
	click(a, r.X+r.W-14, r.Y+r.H-1, tcell.Button1)
	if b.Doc.Text.String() != "hi world" {
		t.Fatal("mouse apply failed", a.Status)
	}
}
func TestUnicodeDoubleClickAndShiftSelection(t *testing.T) {
	a := testApp(t)
	a.insert("你好_world rest")
	a.Current().Doc.Selections = []text.Selection{{}}
	a.draw()
	click(a, 4, 1, tcell.Button1)
	click(a, 4, 1, tcell.Button1)
	s := a.Current().Doc.Selections[0]
	if a.Current().Doc.Text.Slice(s.Start(), s.End()) != "你好_world" {
		t.Fatal(s)
	}
	a.Handle(tcell.NewEventMouse(15, 1, tcell.Button1, tcell.ModShift))
	if a.Current().Doc.Selections[0].Anchor != 0 {
		t.Fatal("shift click lost anchor")
	}
}
func TestMinimapSamplingAndNavigation(t *testing.T) {
	a := testApp(t)
	a.Screen.(tcell.SimulationScreen).SetSize(120, 30)
	a.insert(strings.Repeat("    你好 x := 12\n", 10000))
	b := a.Current()
	job := miniJob{b, b.Doc.Version, b.Doc.Text, 11, 26, 4}
	image := renderMini(job)
	if len(image.Rows) != 26 || image.Job.Text.Len() != 0 {
		t.Fatal("unbounded retained map")
	}
	if strings.TrimSpace(strings.Join(image.Rows, "")) == "" {
		t.Fatal("empty map")
	}
	a.minimapVisible = true
	a.miniCache = image
	a.draw()
	r := a.minimapRect()
	click(a, r.X+3, r.Y+r.H-1, tcell.Button1)
	if b.Doc.Text.LineAt(b.Doc.Selections[0].Head) < 9000 {
		t.Fatal("minimap did not navigate")
	}
}
func BenchmarkSampledMinimapLargeFile(b *testing.B) {
	r := text.New(strings.Repeat("    你好世界  x := 123\n", 500000))
	job := miniJob{Text: r, Width: 11, Height: 26, Tab: 4}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		renderMini(job)
	}
}
