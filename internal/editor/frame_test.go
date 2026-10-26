package editor

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/text"
)

type countedScreen struct {
	tcell.Screen
	writes int
}

func (s *countedScreen) Put(x, y int, value string, style tcell.Style) (string, int) {
	s.writes++
	return s.Screen.Put(x, y, value, style)
}
func TestFrameOnlyPublishesFinalDifferences(t *testing.T) {
	a := testApp(t)
	screen := &countedScreen{Screen: a.Screen}
	a.Screen = screen
	a.Current().Doc, _ = text.NewDocument("marker\n" + strings.Repeat("unchanged plain text\n", 1000))
	a.draw()
	screen.writes = 0
	a.draw()
	if screen.writes != 0 {
		t.Fatal("unchanged frame wrote cells", screen.writes)
	}
	a.insert("x")
	screen.writes = 0
	a.draw()
	if screen.writes > 40 {
		t.Fatal("ordinary edit repainted unrelated rows", screen.writes)
	}
}
func TestFrameWideCellsOverlaysAndResizeMatchFullRefresh(t *testing.T) {
	a := testApp(t)
	s := a.Screen.(tcell.SimulationScreen)
	check := func() {
		t.Helper()
		a.draw()
		before, _, _ := s.GetContents()
		before = append([]tcell.SimCell(nil), before...)
		s.Sync()
		after, _, _ := s.GetContents()
		width, height := s.Size()
		for y := 0; y < height; y++ {
			for x := 0; x < width; {
				i := y*width + x
				if !reflect.DeepEqual(before[i], after[i]) {
					t.Fatalf("incremental frame differs at %d: before=%+v after=%+v", i, before[i], after[i])
				}
				_, _, span := s.Get(x, y)
				x += max(1, span)
			}
		}
	}
	for _, value := range []string{"界界 é 👩‍💻", "a界", "abc", strings.Repeat("界", 50), "", "🇸🇬x"} {
		a.Current().Doc, _ = text.NewDocument(value)
		check()
		a.list("Dialog covering wide cells", []choice{{"one", func() {}}})
		check()
		a.Dialog = nil
		check()
		s.SetSize(25, 8)
		check()
		s.SetSize(80, 24)
		check()
	}
}

func TestTypingPreservesUnchangedSyntaxUntilRefresh(t *testing.T) {
	a := testApp(t)
	b := a.Current()
	b.File.Path = "example.go"
	source := "// marker\npackage main\n\nfunc main() { println(42) }\n"
	b.Doc, _ = text.NewDocument(source)
	b.Spans = highlight(a.ctx.Done(), syntaxJob{Buffer: b, Version: b.Doc.Version, Text: b.Doc.Text, Path: b.File.Path})
	b.SyntaxReady = true
	b.SyntaxVersion = b.Doc.Version
	before := append([]span(nil), b.Spans...)
	b.Doc.Selections = []text.Selection{{Anchor: 3, Head: 3}}
	a.insert("x")
	if !b.SyntaxReady || b.SyntaxVersion != b.Doc.Version {
		t.Fatal("discarded all colors")
	}
	if len(b.Spans) != len(before)-1 {
		t.Fatal("only edited comment should be discarded")
	}
	for i, s := range b.Spans {
		want := before[i+1]
		want.Start++
		want.End++
		if s != want {
			t.Fatal("unaffected token shifted incorrectly", s, want)
		}
	}
	b.Doc.Undo()
	a.changed()
	for i, s := range b.Spans {
		if s != before[i+1] {
			t.Fatal("undo changed unaffected colors")
		}
	}
	b.Doc.Insert("a")
	b.Doc.Insert("b")
	a.changed()
	if b.SyntaxReady {
		t.Fatal("skipped versions retained stale offsets")
	}
}
