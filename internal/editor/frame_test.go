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
