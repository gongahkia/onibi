package text

import (
	"context"
	"math/rand"
	"strings"
	"testing"
)

func TestPersistentRopeRandomEdits(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	source := "hello\nworld\n"
	r := New(source)
	original := r
	for n := 0; n < 3000; n++ {
		a := rng.Intn(len(source) + 1)
		b := a + rng.Intn(len(source)-a+1)
		insert := []string{"abc", "\n", "", strings.Repeat("x", 5000)}[rng.Intn(4)]
		r = r.Replace(a, b, insert, uint64(n+1))
		source = source[:a] + insert + source[b:]
		if r.String() != source {
			t.Fatalf("edit %d: mismatch", n)
		}
		if r.Lines() != strings.Count(source, "\n")+1 {
			t.Fatal("line count")
		}
		for k := 0; k < 5; k++ {
			pos := rng.Intn(len(source) + 1)
			if r.LineAt(pos) != strings.Count(source[:pos], "\n") {
				t.Fatalf("line index at %d", pos)
			}
		}
		offset := 0
		for i, line := range strings.Split(source, "\n") {
			if r.LineStart(i) != offset || r.Line(i) != line {
				t.Fatal("line start/slice")
			}
			offset += len(line) + 1
		}
	}
	if original.String() != "hello\nworld\n" {
		t.Fatal("snapshot mutated")
	}
}
func TestAtomicEditsHistoryAndUTF8(t *testing.T) {
	d, _ := NewDocument("你好 world\nworld")
	original := d.Text
	if err := d.Apply(0, []Edit{{1, 3, "x"}}); err == nil {
		t.Fatal("split UTF-8 accepted")
	}
	if err := d.Apply(0, []Edit{{0, 6, "a"}, {3, 7, "b"}}); err == nil {
		t.Fatal("overlap accepted")
	}
	if !d.Text.Same(original) || d.Version != 0 {
		t.Fatal("failed batch mutated document")
	}
	if err := d.Apply(0, []Edit{{7, 12, "earth"}, {13, 18, "earth"}}); err != nil {
		t.Fatal(err)
	}
	if d.Text.String() != "你好 earth\nearth" || !d.Dirty() {
		t.Fatal("edit failed")
	}
	if err := d.Apply(0, []Edit{{0, 0, "stale"}}); err == nil {
		t.Fatal("stale preview accepted")
	}
	if !d.Undo() || d.Dirty() || !d.Text.Same(original) {
		t.Fatal("undo failed")
	}
	if !d.Redo() || !d.Dirty() {
		t.Fatal("redo failed")
	}
	d.MarkSaved()
	if d.Dirty() {
		t.Fatal("save failed")
	}
}
func TestSearchAnchorsCapturesAndLimits(t *testing.T) {
	ctx := context.Background()
	edits, err := Replacements(ctx, "one\ntwo\nthree", `(?m)^(\w+)`, "[$1]", true)
	if err != nil || len(edits) != 3 || edits[1].Text != "[two]" {
		t.Fatalf("%v %v", edits, err)
	}
	edits, err = Search(ctx, "a a a", "^a", true)
	if err != nil || len(edits) != 1 {
		t.Fatal("anchor is not document-relative")
	}
	if _, err = Search(ctx, "abc", "a*", true); err == nil {
		t.Fatal("zero width accepted")
	}
	if _, err = Search(ctx, strings.Repeat("x", MaxMatches+1), "x", false); err == nil {
		t.Fatal("unbounded matches")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = Search(cancelled, "abc", "a", false); err == nil {
		t.Fatal("cancellation ignored")
	}
}

var benchmarkRopeSink Rope
var benchmarkIntSink int

func BenchmarkLargeFileEdit(b *testing.B) {
	r := New(strings.Repeat("a reasonably ordinary line of text\n", 300000))
	at := r.Len() / 2
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkRopeSink = r.Replace(at, at, "x", uint64(i))
	}
}
func BenchmarkLargeFileLineLookup(b *testing.B) {
	r := New(strings.Repeat("ordinary text\n", 1000000))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchmarkIntSink = r.LineStart(i % 1000000)
	}
}

func TestGroupedHistoryAndStaging(t *testing.T) {
	d, _ := NewDocument("abc")
	d.Selections = []Selection{{3, 3}}
	d.Insert("x")
	d.Insert("y")
	d.Coalesce()
	d.Insert("z")
	d.Coalesce()
	if !d.Undo() || d.Text.String() != "abc" {
		t.Fatal("typing was not grouped")
	}
	if len(d.LastChanges) != 3 {
		t.Fatal("missing reverse changes")
	}
	if !d.Redo() || d.Text.String() != "abcxyz" {
		t.Fatal("group redo")
	}
	clone := d.Clone()
	clone.Insert("!")
	if d.Text.String() != "abcxyz" {
		t.Fatal("stage mutated live state")
	}
	version := d.Version
	if err := d.Apply(version, []Edit{{0, 3, "abc"}}); err != nil || d.Version != version {
		t.Fatal("no-op dirtied document")
	}
}
func TestIndexedPlainText(t *testing.T) {
	r := New(strings.Repeat("x", 1000000) + "\t你好\nend")
	if !r.Plain(0, 1000000) || r.Plain(0, 1000001) || r.Plain(1000001, 1000007) {
		t.Fatal("ASCII index")
	}
	if r.LineEnd(0) != 1000007 {
		t.Fatal(r.LineEnd(0))
	}
}
