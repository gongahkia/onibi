package folding

import (
	"context"
	"strings"
	"testing"
)

func TestRegions(t *testing.T) {
	cases := []struct {
		name, path, source string
		want               []string
	}{
		{"go", "main.go", "func main() {\n  println(\"} literal\")\n  if true {\n    work()\n  }\n}\nafter\n", []string{"func main() {\n  println(\"} literal\")\n  if true {\n    work()\n  }\n}\n", "  if true {\n    work()\n  }\n"}},
		{"comments", "main.c", "/*\n { comment\n */\n// one\n// two\ncode\n", []string{"/*\n { comment\n */\n", "// one\n// two\n"}},
		{"python", "main.py", "def f():\n    if True:\n        print('界')\n    return 1\nafter()\n", []string{"def f():\n    if True:\n        print('界')\n    return 1\n", "    if True:\n        print('界')\n"}},
		{"markdown", "note.md", "# A\ntext\n## B\n```go\n# fake heading\n```\n# C\ntext\n", []string{"# A\ntext\n## B\n```go\n# fake heading\n```\n", "## B\n```go\n# fake heading\n```\n", "```go\n# fake heading\n```\n", "# C\ntext\n"}},
		{"org", "note.org", "* A\n** B\n#+BEGIN_SRC go\n* ignored\n#+END_SRC\n* C\nbody", []string{"* A\n** B\n#+BEGIN_SRC go\n* ignored\n#+END_SRC\n", "** B\n#+BEGIN_SRC go\n* ignored\n#+END_SRC\n", "#+BEGIN_SRC go\n* ignored\n#+END_SRC\n", "* C\nbody"}},
		{"triple", "main.py", "text = \"\"\"\nnot code {\n\"\"\"\nafter\n", []string{"text = \"\"\"\nnot code {\n\"\"\"\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rs, e := Find(context.Background(), tc.source, tc.path)
			if e != nil {
				t.Fatal(e)
			}
			if len(rs) != len(tc.want) {
				t.Fatalf("%#v", rs)
			}
			for i, r := range rs {
				if tc.source[r.Start:r.End] != tc.want[i] {
					t.Fatalf("%q != %q", tc.source[r.Start:r.End], tc.want[i])
				}
			}
		})
	}
}
func TestBounds(t *testing.T) {
	if _, e := Find(context.Background(), strings.Repeat("x", MaxSource+1), "x.go"); e == nil {
		t.Fatal("unbounded")
	}
	if _, e := Find(context.Background(), strings.Repeat("{\nx\n}\n", MaxRanges+1), "x.go"); e == nil {
		t.Fatal("unbounded ranges")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Find(ctx, "hello", "x.md"); e != context.Canceled {
		t.Fatal(e)
	}
}
func FuzzFind(f *testing.F) {
	for _, s := range []string{"{\n}\n", "# hi\n\n## sub\ntext", "#+BEGIN_", "* Org\n#+BEGIN_SRC go\n* x\n#+END_SRC", "def f():\n   pass"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 16384 {
			return
		}
		for _, path := range []string{"x.go", "x.py", "x.org", "x.md"} {
			rs, e := Find(context.Background(), s, path)
			if e != nil {
				continue
			}
			previous := -1
			for _, r := range rs {
				if r.Start <= previous || r.End <= r.Start || r.End > len(s) {
					t.Fatalf("%#v", rs)
				}
				previous = r.Start
			}
		}
	})
}
func BenchmarkLargeCode(b *testing.B) {
	s := strings.Repeat("func f() {\n  println(\"世界\")\n}\n", 15000)
	b.SetBytes(int64(len(s)))
	b.ReportAllocs()
	for b.Loop() {
		if _, e := Find(context.Background(), s, "x.go"); e != nil {
			b.Fatal(e)
		}
	}
}
