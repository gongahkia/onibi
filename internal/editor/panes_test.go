package editor

import (
	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/text"
	"strings"
	"testing"
)

func TestPaneSelectionsTrackEditsAndUndo(t *testing.T) {
	a := testApp(t)
	a.insert("abcdef")
	b := a.Current()
	b.Doc.Selections = []text.Selection{{Anchor: 4, Head: 4}}
	a.draw()
	a.split(false)
	b.Doc.Selections = []text.Selection{{Anchor: 0, Head: 0}}
	a.insert("X")
	a.draw()
	a.focusPane(0)
	if b.Doc.Selections[0].Head != 5 {
		t.Fatal(b.Doc.Selections)
	}
	key(a, tcell.KeyCtrlZ)
	a.focusPane(1)
	if b.Doc.Selections[0].Head != 0 {
		t.Fatal(b.Doc.Selections)
	}
	a.closePane()
	a.draw()
	if len(a.panes) != 1 {
		t.Fatal("close")
	}
}
func TestWrapMovementAndMouse(t *testing.T) {
	a := testApp(t)
	a.Screen.(tcell.SimulationScreen).SetSize(20, 12)
	a.insert(strings.Repeat("x", 100))
	a.wrap = true
	b := a.Current()
	b.Doc.Selections = []text.Selection{{}}
	a.draw()
	width := a.contentWidth(b, a.rect(0))
	a.move(0, 1, false)
	if b.Doc.Selections[0].Head != width {
		t.Fatal(b.Doc.Selections, width)
	}
	a.draw()
	a.mouse(tcell.NewEventMouse(a.gutter()+2, 2, tcell.Button1, 0))
	if b.Doc.Selections[0].Head != width+2 {
		t.Fatal(b.Doc.Selections, width)
	}
}
func TestUnicodeWrapBoundaries(t *testing.T) {
	r := text.New("你好e\u0301👩‍💻\tend")
	rows := segments(r, 0, 4, 4)
	for i, s := range rows {
		if !r.Boundary(s.Start) || !r.Boundary(s.End) {
			t.Fatal(s)
		}
		if i > 0 && rows[i-1].End != s.Start {
			t.Fatal(rows)
		}
	}
	if rows[0].End != len("你好") || rows[1].End != len("你好e\u0301👩‍💻\t") {
		t.Fatal(rows)
	}
}
