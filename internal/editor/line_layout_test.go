package editor

import (
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/gongahkia/shack/internal/text"
	"github.com/rivo/uniseg"
)

func checkLineLayout(t *testing.T, source string, width, tab int) {
	t.Helper()
	d, err := text.NewDocument(source)
	if err != nil {
		t.Fatal(err)
	}
	b := &Buffer{Doc: d}
	var cache layoutCache
	for line := 0; line < d.Text.Lines(); line++ {
		l := cache.line(b, line, width, tab)
		want := segments(d.Text, line, width, tab)
		// Exercise both cold requests and queries moving backwards through the index.
		for pass := 0; pass < 2; pass++ {
			for i := len(want) - 1; i >= 0; i-- {
				got, row := l.segment(i)
				if got != want[i] || row != i {
					t.Fatalf("width %d line %d segment %d: %v/%d want %v", width, line, i, got, row, want[i])
				}
			}
			if l.count() != len(want) {
				t.Fatalf("count %d want %d", l.count(), len(want))
			}
			g := uniseg.NewGraphemes(d.Text.Slice(l.start, l.end))
			col := 0
			for g.Next() {
				from, to := g.Positions()
				from += l.start
				to += l.start
				if got := l.column(from); got != col {
					t.Fatalf("column at %d: %d want %d", from, got, col)
				}
				if got := l.rowAt(from); got != rowAt(d.Text, line, from, width, tab) {
					t.Fatalf("row at %d: %d", from, got)
				}
				w := g.Width()
				if g.Str() == "\t" {
					w = tab - col%tab
				}
				col += w
				if l.column(to) != col {
					t.Fatalf("end column %d", to)
				}
			}
			for col := 0; col <= min(300, visualColumn(d.Text.Line(line), tab)+2); col++ {
				if got, want := l.offset(col), lineOffset(d.Text, line, col, tab); got != want {
					t.Fatalf("offset(%d)=%d want %d", col, got, want)
				}
			}
		}
	}
}
func TestLineLayoutMatchesReference(t *testing.T) {
	samples := []string{"", "hello world", "界 é 👩‍💻 🇸🇬\tend\r\n\t界", "\u0600A\u0301\x00\x01", strings.Repeat("界 é 👩‍💻 🇸🇬\t", 180)}
	for _, s := range samples {
		for _, w := range []int{1, 2, 7, 80} {
			checkLineLayout(t, s, w, 4)
		}
	}
}
func TestGraphemeReaderChunkBoundaries(t *testing.T) {
	for _, cluster := range []string{"é", "👩‍💻", "🇸🇬", "\u0600A", "a" + strings.Repeat("́", 12000)} {
		for _, prefix := range []int{4091, 4093, 4095, 4096} {
			s := strings.Repeat("x", prefix) + cluster + "z"
			r := newGraphemeReader(text.New(s), 0, len(s))
			g := uniseg.NewGraphemes(s)
			for g.Next() {
				v, from, w, ok := r.next()
				want, _ := g.Positions()
				if !ok || v != g.Str() || from != want || w != g.Width() {
					t.Fatalf("reader mismatch at %d (%d-byte cluster)", from, len(cluster))
				}
			}
			if _, _, _, ok := r.next(); ok {
				t.Fatal("extra cluster")
			}
		}
	}
}
func TestLineLayoutRebasesAndBounds(t *testing.T) {
	source := strings.Repeat("界é\t", 180000)
	d, _ := text.NewDocument(source)
	b := &Buffer{Doc: d}
	var cache layoutCache
	l := cache.line(b, 0, 80, 4)
	l.count()
	if len(l.points) > layoutPoints || l.stride <= layoutStride {
		t.Fatal("index did not compact", len(l.points), l.stride)
	}
	prefix := l.points[10]
	p := len(source) - len("界é\t")
	if err := d.Apply(d.Version, []text.Edit{{Start: p, End: p, Text: "\n🇸🇬"}}); err != nil {
		t.Fatal(err)
	}
	next := cache.line(b, 0, 80, 4)
	if next != l || next.points[10] != prefix {
		t.Fatal("unchanged prefix lost")
	}
	got := []segment{}
	for i := 0; i < next.count(); i++ {
		s, _ := next.segment(i)
		got = append(got, s)
	}
	if !reflect.DeepEqual(got, segments(d.Text, 0, 80, 4)) {
		t.Fatal("edit stale rows")
	}
	d.Undo()
	l = cache.line(b, 0, 80, 4)
	if l.count() != len(segments(d.Text, 0, 80, 4)) {
		t.Fatal("undo stale rows")
	}
	for i := 1; i <= layoutEntries+3; i++ {
		cache.line(b, 0, i, 4).column(100)
	}
	if len(cache.entries) != layoutEntries {
		t.Fatal("cache unbounded")
	}
	cache.forget(b)
	if len(cache.entries) != 0 {
		t.Fatal("closed buffer retained")
	}
}
func TestLineLayoutRandomEdits(t *testing.T) {
	random := rand.New(rand.NewPCG(42, 17))
	tokens := []string{"a", "界", "é", "🇸🇬", "👩‍💻", "\t", "\u0600", "\u0301", "\n"}
	d, _ := text.NewDocument(strings.Repeat("界 é\t", 200))
	b := &Buffer{Doc: d}
	var cache layoutCache
	for i := 0; i < 100; i++ {
		line := random.IntN(d.Text.Lines())
		width := 1 + random.IntN(30)
		l := cache.line(b, line, width, 4)
		want := segments(d.Text, line, width, 4)
		for row := range want {
			got, n := l.segment(row)
			if got != want[row] || n != row {
				t.Fatal("random rows", i, row, got, want[row])
			}
		}
		p := random.IntN(d.Text.Len() + 1)
		for !d.Text.Boundary(p) {
			p--
		}
		if err := d.Apply(d.Version, []text.Edit{{Start: p, End: p, Text: tokens[random.IntN(len(tokens))]}}); err != nil {
			t.Fatal(err)
		}
		l = cache.line(b, line, width, 4)
		want = segments(d.Text, line, width, 4)
		for row := range want {
			got, n := l.segment(row)
			if got != want[row] || n != row {
				t.Fatal("random edited rows", i, row, got, want[row])
			}
		}
	}
}

func TestIndexedCursorGraphemes(t *testing.T) {
	for _, source := range []string{"\u0600A", "a\u0301🇸🇬👩‍💻界\r\nnext\r", strings.Repeat("x", 4095) + "👩‍💻é\n🇸🇬"} {
		d, _ := text.NewDocument(source)
		b := &Buffer{Doc: d}
		a := &App{}
		a.Config.TabSize = 4
		g := uniseg.NewGraphemes(source)
		for g.Next() {
			from, to := g.Positions()
			for _, offset := range []int{from, to - 1} {
				if got := a.next(b, offset); got != to {
					t.Fatalf("next at %d=%d want %d", offset, got, to)
				}
			}
			if got := a.previous(b, to); got != from {
				t.Fatalf("previous at %d=%d want %d", to, got, from)
			}
		}
	}
}
func TestLayoutInvalidatesCombiningInsertionAndSkippedVersions(t *testing.T) {
	d, _ := text.NewDocument(strings.Repeat("a", 512) + "z")
	b := &Buffer{Doc: d}
	var c layoutCache
	l := c.line(b, 0, 80, 4)
	l.count()
	d.Apply(d.Version, []text.Edit{{Start: 512, End: 512, Text: "́"}})
	l = c.line(b, 0, 80, 4)
	if l.column(d.Text.Len()) != 513 {
		t.Fatal("combining insertion changed width")
	}
	before, _ := l.atOffset(512)
	if before.Offset != 511 {
		t.Fatal("stale grapheme boundary", before)
	}
	d.Insert("界")
	d.Insert("界")
	l = c.line(b, 0, 80, 4)
	if l.column(d.Text.Len()) != visualColumn(d.Text.String(), 4) {
		t.Fatal("skipped versions retained stale checkpoints")
	}
	d2, _ := text.NewDocument("replacement界")
	b.Doc = d2
	if c.line(b, 0, 80, 4).column(d2.Text.Len()) != 13 {
		t.Fatal("replacement document retained cache")
	}
}
func FuzzLineLayout(f *testing.F) {
	for _, s := range []string{"界\té👩‍💻🇸🇬", "\u0600A\r\n\t", "a" + strings.Repeat("́", 512)} {
		f.Add(s, uint8(7), uint16(12))
	}
	f.Fuzz(func(t *testing.T, source string, widthByte uint8, offset uint16) {
		if len(source) > 4096 {
			return
		}
		d, err := text.NewDocument(source)
		if err != nil {
			return
		}
		var c layoutCache
		b := &Buffer{Doc: d}
		width := 1 + int(widthByte)%100
		pos := int(offset) % (len(source) + 1)
		line := d.Text.LineAt(pos)
		l := c.line(b, line, width, 4)
		want := segments(d.Text, line, width, 4)
		if l.count() != len(want) {
			t.Fatal("count")
		}
		for _, row := range []int{0, len(want) / 2, len(want) - 1} {
			got, actual := l.segment(row)
			if got != want[row] || actual != row {
				t.Fatal("segment", got, want[row])
			}
		}
		pos = min(pos, l.end)
		if l.rowAt(pos) != rowAt(d.Text, line, pos, width, 4) {
			t.Fatal("row offset")
		}
	})
}
