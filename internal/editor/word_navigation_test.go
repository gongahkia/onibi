package editor

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/gongahkia/shack/internal/config"
	"github.com/gongahkia/shack/internal/text"
	"github.com/rivo/uniseg"
)

func TestWordNavigationMatchesReference(t *testing.T) {
	for _, source := range []string{"", " one_two... three\r\n\t界é next", "\u0600A abc 👩‍💻🇸🇬 test", strings.Repeat("abc界 é\t", 90) + "\nlast"} {
		d, _ := text.NewDocument(source)
		b := &Buffer{Doc: d}
		a := &App{Config: config.Default(), Buffers: []*Buffer{b}}
		offsets := []int{0}
		g := uniseg.NewGraphemes(source)
		for g.Next() {
			_, to := g.Positions()
			offsets = append(offsets, to)
		}
		for _, p := range offsets {
			for _, delta := range []int{-1, 1} {
				if got, want := a.wordOffset(b, p, delta), wordOffset(d.Text, p, delta); got != want {
					t.Fatalf("at %d direction %d: %d want %d in %q", p, delta, got, want, source)
				}
			}
			lo, hi := p, p
			for lo > 0 {
				prev := previous(d.Text, lo)
				r, _ := utf8.DecodeRuneInString(source[prev:lo])
				if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
					break
				}
				lo = prev
			}
			for hi < len(source) {
				next := next(d.Text, hi)
				r, _ := utf8.DecodeRuneInString(source[hi:next])
				if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
					break
				}
				hi = next
			}
			d.Selections = []text.Selection{{Anchor: p, Head: p}}
			a.wordSelection()
			if got := d.Selections[0]; got.Anchor != lo || got.Head != hi {
				t.Fatal("word selection", p, got, lo, hi)
			}
		}
	}
}
func TestLongWordAndWhitespaceAcrossWindows(t *testing.T) {
	word := strings.Repeat("界é", 180000)
	source := "before " + word + strings.Repeat(" \t", 3000) + "\r\n\tlast"
	d, _ := text.NewDocument(source)
	b := &Buffer{Doc: d}
	a := &App{Config: config.Default(), Buffers: []*Buffer{b}}
	at := len("before ") + len(word)
	if p := a.wordOffset(b, at, -1); p != len("before ") {
		t.Fatal("long word left", p)
	}
	if p := a.wordOffset(b, len("before "), 1); p != len(source)-len("last") {
		t.Fatal("long word right", p)
	}
	if p := a.wordOffset(b, len(source)-len("last"), -1); p != len("before ") {
		t.Fatal("whitespace and word left", p)
	}
	d.Selections = []text.Selection{{Anchor: at, Head: at}}
	a.wordSelection()
	if s := d.Selections[0]; s.Anchor != len("before ") || s.Head != at {
		t.Fatal("long selection", s)
	}
}
