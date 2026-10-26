package markup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func validate(t *testing.T, d Document) {
	t.Helper()
	last := 0
	for _, s := range d.Spans {
		if s.Start < last || s.Start >= s.End || s.End > len(d.Text) || !utf8.ValidString(d.Text[:s.Start]) || !utf8.ValidString(d.Text[:s.End]) {
			t.Fatalf("invalid span %#v in %q", s, d.Text)
		}
		last = s.End
	}
	for _, h := range d.Headings {
		if !strings.HasPrefix(d.Text[h.Offset:], h.Title) {
			t.Fatalf("heading %#v", h)
		}
	}
}
func TestMarkdown(t *testing.T) {
	src := "# Hello **世界**\n\nText *italic* ~~gone~~ [site](https://example.com) &amp; \\*literal\\*.\n\n- [x] Done\n- [ ] Next\n\n| Name | Value |\n|---|---:|\n| 界 | 42 |\n\n```go\nfmt.Println(\"hi\")\n```\n\n> quote\n\n![alt](image.png)\n"
	d, e := Render(context.Background(), src, "markdown")
	if e != nil {
		t.Fatal(e)
	}
	validate(t, d)
	for _, want := range []string{"Hello 世界", "Text italic gone site & *literal*.", "☑ Done", "☐ Next", "│ 界", "fmt.Println(\"hi\")", "▎ quote", "[image: alt]"} {
		if !strings.Contains(d.Text, want) {
			t.Fatalf("missing %q in %q", want, d.Text)
		}
	}
	flags := Style{}
	for _, s := range d.Spans {
		flags.Bold = flags.Bold || s.Style.Bold
		flags.Italic = flags.Italic || s.Style.Italic
		flags.Strike = flags.Strike || s.Style.Strike
		flags.Code = flags.Code || s.Style.Code
		if s.Style.Link != "" {
			flags.Link = s.Style.Link
		}
	}
	if !flags.Bold || !flags.Italic || !flags.Strike || !flags.Code || flags.Link != "https://example.com" {
		t.Fatal(flags)
	}
	if len(d.Headings) != 1 || d.Headings[0].Title != "Hello 世界" {
		t.Fatal(d.Headings)
	}
}
func TestOrg(t *testing.T) {
	src := "#+TITLE: Notebook\n* TODO Heading :tag:\nA *bold* /italic/ +gone+ =code= [[https://example.com][site]].\n\n- [X] Done\n- [ ] Next\n\n| Name | Value |\n|------+-------|\n| 界 | 42 |\n\n#+BEGIN_SRC go\nfmt.Println(\"hi\")\n#+END_SRC\n\n** Child\nBody\n"
	d, e := Render(context.Background(), src, "org")
	if e != nil {
		t.Fatal(e)
	}
	validate(t, d)
	for _, want := range []string{"Notebook", "TODO Heading", "A bold italic gone code site.", "☑ Done", "☐ Next", "│ 界", "fmt.Println(\"hi\")", "Child"} {
		if !strings.Contains(d.Text, want) {
			t.Fatalf("missing %q in %q", want, d.Text)
		}
	}
	if len(d.Headings) != 3 {
		t.Fatal(d.Headings)
	}
}
func TestInertDocuments(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "secret.org")
	if err := os.WriteFile(secret, []byte("PRIVATE DOCUMENT"), 0600); err != nil {
		t.Fatal(err)
	}
	src := "#+INCLUDE: \"" + secret + "\"\n#+SETUPFILE: " + secret + "\n#+MACRO: danger {{{danger}}}\n{{{danger}}}\n#+BEGIN_SRC sh\ntouch should-not-exist\n#+END_SRC\n"
	d, e := Render(context.Background(), src, "org")
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(d.Text, "PRIVATE DOCUMENT") || !strings.Contains(d.Text, "not expanded") || !strings.Contains(d.Text, "touch should-not-exist") {
		t.Fatal(d.Text)
	}
	d, e = Render(context.Background(), "[bad](javascript:alert) <script>alert(1)</script>\n\nESC \x1b]52;secret\x07", "markdown")
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range d.Spans {
		if s.Style.Link != "" {
			t.Fatal(s)
		}
	}
	if strings.ContainsAny(d.Text, "\x1b\x07") {
		t.Fatal(d.Text)
	}
	for _, u := range []string{"javascript:alert(1)", "file:///etc/passwd", "https://x\x1b]52;secret", "/tmp/local", "https://"} {
		if SafeURL(u) != "" {
			t.Fatal(u)
		}
	}
}
func TestLimitsAndCancellation(t *testing.T) {
	for _, tc := range []struct{ source, format string }{{strings.Repeat("x", MaxSource+1), "markdown"}, {strings.Repeat("x", MaxOrgSource+1), "org"}, {strings.Repeat("\n", 20001), "markdown"}, {strings.Repeat(" ", 300) + "x", "org"}, {strings.Repeat("#+BEGIN_QUOTE\n", 65), "org"}} {
		if _, e := Render(context.Background(), tc.source, tc.format); e == nil {
			t.Fatalf("accepted oversized %s", tc.format)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Render(ctx, "# hello", "markdown"); e != context.Canceled {
		t.Fatal(e)
	}
}
func TestUnicodeAndCodeLiterals(t *testing.T) {
	d, e := Render(context.Background(), "## é 👩‍💻\n\n`a &amp; \\*b*` and ` a `\n\n```text\n# literal **bold**\n```", "markdown")
	if e != nil {
		t.Fatal(e)
	}
	validate(t, d)
	if !strings.Contains(d.Text, "a &amp; \\*b*") || !strings.Contains(d.Text, "# literal **bold**") {
		t.Fatal(d.Text)
	}
}
func BenchmarkRenderMarkdown(b *testing.B) {
	benchmarkRender(b, "markdown", "## Heading 世界\n\nSome **bold** and [link](https://example.com).\n\n- one\n- two\n\n")
}
func BenchmarkRenderOrg(b *testing.B) {
	benchmarkRender(b, "org", "* Heading 世界\n\nSome *bold* and [[https://example.com][link]].\n\n- one\n- two\n\n")
}
func benchmarkRender(b *testing.B, format, chunk string) {
	src := strings.Repeat(chunk, 300)
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Render(context.Background(), src, format); err != nil {
			b.Fatal(err)
		}
	}
}
func FuzzRender(f *testing.F) {
	for _, s := range []string{"# hello **world**", "Note[^a].\n\n[^a]: Footnote\n\nTerm\n: Definition\n", "> - # Nested\n>   - item\n", "* TODO org\n#+BEGIN_SRC go\nx\n#+END_SRC", "[x](https://example.com)", "| a | b |\n|---|---|\n| c | d |", "😀 é 👩‍💻", strings.Repeat("[", 1000)} {
		f.Add(s, false)
		f.Add(s, true)
	}
	f.Fuzz(func(t *testing.T, s string, isOrg bool) {
		if len(s) > 8192 {
			return
		}
		format := "markdown"
		if isOrg {
			format = "org"
		}
		d, e := Render(context.Background(), s, format)
		if e == nil {
			validate(t, d)
			if !utf8.ValidString(d.Text) {
				t.Fatal("invalid output")
			}
		}
	})
}
