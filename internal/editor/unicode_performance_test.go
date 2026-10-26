package editor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/config"
	"github.com/gongahkia/shack/internal/text"
)

func BenchmarkUnicodeLongLineViewport(b *testing.B) {
	for _, size := range []int{128 << 10, 1 << 20} {
		for _, wrap := range []bool{false, true} {
			b.Run(fmt.Sprintf("bytes=%d/wrap=%t", size, wrap), func(b *testing.B) {
				screen := tcell.NewSimulationScreen("UTF-8")
				screen.Init()
				defer screen.Fini()
				screen.SetSize(100, 30)
				a, e := New(screen, config.Default(), nil, false)
				if e != nil {
					b.Fatal(e)
				}
				defer a.cancel()
				unit := "界 é 👩‍💻 🇸🇬\t"
				source := strings.Repeat(unit, size/len(unit))
				d, _ := text.NewDocument(source)
				a.Current().Doc = d
				a.wrap = wrap
				d.Selections = []text.Selection{{Anchor: len(source), Head: len(source)}}
				a.reveal()
				a.draw()
				b.ReportAllocs()
				b.SetBytes(int64(len(source)))
				b.ResetTimer()
				for b.Loop() {
					a.reveal()
					a.draw()
				}
			})
		}
	}
}

func BenchmarkUnicodeLongLineIndex(b *testing.B) {
	unit := "界 é 👩‍💻 🇸🇬\t"
	source := strings.Repeat(unit, (1<<20)/len(unit))
	for _, kind := range []string{"cold-end", "warm-left", "edit-end", "edit-middle", "cold-start"} {
		b.Run(kind, func(b *testing.B) {
			d, _ := text.NewDocument(source)
			buffer := &Buffer{Doc: d}
			a := &App{Config: config.Default()}
			l := a.lineLayouts.line(buffer, 0, 96, 4)
			l.count()
			a.columnAt(buffer, d.Text.Len())
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				switch kind {
				case "cold-end":
					a.lineLayouts.forget(buffer)
					a.rowAt(buffer, 0, d.Text.Len(), 96)
				case "cold-start":
					a.lineLayouts.forget(buffer)
					a.rowSegment(buffer, 0, 0, 96)
				case "warm-left":
					a.previous(buffer, d.Text.Len())
				default:
					at := d.Text.Len()
					if kind == "edit-middle" {
						at = len(source) / 2
						for !d.Text.Boundary(at) {
							at--
						}
					}
					if err := d.Apply(d.Version, []text.Edit{{Start: at, End: at, Text: "x"}}); err != nil {
						b.Fatal(err)
					}
					a.rowAt(buffer, 0, at+1, 96)
					a.columnAt(buffer, at+1)
					d.Undo()
					a.rowAt(buffer, 0, at, 96)
					a.columnAt(buffer, at)
				}
			}
		})
	}
}

func BenchmarkUnicodeLongWord(b *testing.B) {
	source := strings.Repeat("界é", (1<<20)/len("界é"))
	for _, direction := range []int{-1, 1} {
		b.Run(fmt.Sprint(direction), func(b *testing.B) {
			d, _ := text.NewDocument(source)
			buffer := &Buffer{Doc: d}
			a := &App{Config: config.Default(), Buffers: []*Buffer{buffer}}
			a.columnAt(buffer, d.Text.Len())
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				p := 0
				if direction < 0 {
					p = d.Text.Len()
				}
				a.wordOffset(buffer, p, direction)
			}
		})
	}
}

func BenchmarkUnicodeManyLongLines(b *testing.B) {
	for _, size := range []int{8 << 10, 128 << 10} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			screen := tcell.NewSimulationScreen("UTF-8")
			screen.Init()
			defer screen.Fini()
			screen.SetSize(100, 40)
			a, err := New(screen, config.Default(), nil, false)
			if err != nil {
				b.Fatal(err)
			}
			defer a.cancel()
			unit := "界 é 👩‍💻 🇸🇬\t"
			line := strings.Repeat(unit, size/len(unit))
			d, _ := text.NewDocument(strings.Repeat(line+"\n", 40))
			a.Current().Doc = d
			d.Selections = []text.Selection{{Anchor: len(line), Head: len(line)}}
			a.reveal()
			a.draw()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				a.reveal()
				a.draw()
			}
		})
	}
}

func BenchmarkOversizedClusterViewport(b *testing.B) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	defer screen.Fini()
	screen.SetSize(100, 30)
	a, err := New(screen, config.Default(), nil, false)
	if err != nil {
		b.Fatal(err)
	}
	defer a.cancel()
	source := "a" + strings.Repeat("́", 1<<19) + "x"
	d, _ := text.NewDocument(source)
	a.Current().Doc = d
	a.wrap = true
	d.Selections = []text.Selection{{Anchor: len(source), Head: len(source)}}
	a.reveal()
	a.draw()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		a.reveal()
		a.draw()
	}
}
