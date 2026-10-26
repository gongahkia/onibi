package markup

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHeadingAndFootnoteAnchors(t *testing.T) {
	source := "[go](#hello-%E4%B8%96%E7%95%8C)\n\n# Hello 世界!\n# Hello 世界!\n# Hello 世界-1\n# Hello 世界!\n\nA[^n], B[^n].\n\n| Ref |\n|---|\n| C[^n] |\n\n[^n]: The note.\n"
	d, err := Render(context.Background(), source, "markdown")
	if err != nil {
		t.Fatal(err)
	}
	validate(t, d)
	for key, want := range map[string]string{"heading:hello-世界": "Hello 世界!", "heading:hello-世界-1": "Hello 世界!", "heading:hello-世界-1-1": "Hello 世界-1", "heading:hello-世界-2": "Hello 世界!", "footnote:1": "[1] The note.", "reference:1:0": "[1]", "reference:1:1": "[1]", "reference:1:2": "[1]"} {
		offset, ok := d.Anchors[key]
		if !ok || !strings.HasPrefix(d.Text[offset:], want) {
			t.Fatalf("anchor %s -> %d: %q", key, offset, d.Text)
		}
	}
	refs, backs := 0, 0
	for _, s := range d.Spans {
		if s.Style.Anchor == "footnote:1" {
			refs++
		}
		if strings.HasPrefix(s.Style.Anchor, "reference:") {
			backs++
		}
	}
	if refs != 3 || backs != 3 {
		t.Fatalf("references/backlinks %d/%d", refs, backs)
	}
}

func TestLocalTargetsStayOutOfTerminalURLs(t *testing.T) {
	for _, target := range []string{"../guide%20one.md#section", "#hello", "src/main.go", "https://example.com"} {
		if LinkTarget(target) != target {
			t.Fatal(target)
		}
	}
	for _, target := range []string{"file:///etc/passwd", "javascript:alert(1)", "//example.com/a", "/etc/passwd", "a%00.md", "a.md#%1b", "a.md?query=1", "C:\\path", "a\\b"} {
		if LinkTarget(target) != "" {
			t.Fatal(target)
		}
	}
	d, err := Render(context.Background(), "[local](../guide.md#hello)", "markdown")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Spans) != 1 || d.Spans[0].Style.Target != "../guide.md#hello" || d.Spans[0].Style.Link != "" {
		t.Fatal(d.Spans)
	}
}

func TestFencedCodeColorsAndLiteralFallback(t *testing.T) {
	source := "package main\n// comment\nfunc main() { println(\"hello\", 42) }\n"
	d, err := Render(context.Background(), "```go\n"+source+"```\n", "markdown")
	if err != nil {
		t.Fatal(err)
	}
	validate(t, d)
	if !strings.Contains(d.Text, source) {
		t.Fatal("code text changed", d.Text)
	}
	found := map[TokenStyle]bool{}
	for _, s := range d.Spans {
		if s.Style.Code {
			found[s.Style.Token] = true
		}
	}
	for _, kind := range []TokenStyle{TokenKeyword, TokenComment, TokenString, TokenNumber} {
		if !found[kind] {
			t.Fatal("missing token", kind)
		}
	}
	for _, tc := range []struct{ language, code string }{{"unknown_language", source}, {"go", strings.Repeat("// unchanged\n", 6000)}} {
		// The fallback is checked directly so the fixture stays below the
		// Markdown preflight line-size limit and exercises the lexer budget.
		r := &renderer{ctx: context.Background()}
		r.codeBlock(tc.code, tc.language)
		if r.out.String() != tc.code {
			t.Fatal("literal fallback changed code")
		}
		for _, s := range r.spans {
			if s.Style.Token != TokenPlain {
				t.Fatal("fallback had partial colors")
			}
		}
	}
}

func TestMermaidDrawingsAndFallback(t *testing.T) {
	for _, source := range []string{"flowchart LR\nA[Start] --> B[Done]\n", "sequenceDiagram\nparticipant A as Alice\nparticipant B as Bob\nA->>B: Hello\nB-->>A: Hi\n"} {
		d, err := Render(context.Background(), "```mermaid\n"+source+"```", "markdown")
		if err != nil {
			t.Fatal(err)
		}
		validate(t, d)
		if !d.Media || !strings.ContainsAny(d.Text, "┌┐└┘│─") {
			t.Fatal(d.Text)
		}
	}
	for _, source := range []string{"pie\n\"A\": 2", "flowchart BT\nA --> B", "flowchart TD\nsubgraph one\nA --> B\nend", "flowchart TD\nA[世界] --> B", "flowchart TD\n" + strings.Repeat("A --> B\n", 65)} {
		d, err := Render(context.Background(), "```mermaid\n"+source+"\n```", "markdown")
		if err != nil {
			t.Fatal(err)
		}
		if d.Media || !strings.Contains(d.Text, "[Mermaid source:") || !strings.Contains(d.Text, source) {
			t.Fatal(d.Text)
		}
	}
}

func testBitmap() *image.NRGBA {
	i := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	i.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	i.Set(1, 0, color.NRGBA{0, 255, 0, 255})
	i.Set(0, 1, color.NRGBA{0, 0, 255, 255})
	i.Set(1, 1, color.NRGBA{0, 0, 0, 0})
	return i
}

func TestLocalImageFormatsAndBounds(t *testing.T) {
	root := t.TempDir()
	for _, format := range []string{"png", "jpg", "gif"} {
		var data bytes.Buffer
		switch format {
		case "png":
			png.Encode(&data, testBitmap())
		case "jpg":
			jpeg.Encode(&data, testBitmap(), nil)
		case "gif":
			gif.Encode(&data, testBitmap(), nil)
		}
		name := "sample image." + format
		if err := os.WriteFile(filepath.Join(root, name), data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		d, err := RenderWithOptions(context.Background(), "![sample](sample%20image."+format+")", "markdown", Options{BaseDir: root, Media: true})
		if err != nil {
			t.Fatal(err)
		}
		validate(t, d)
		if !d.Media || !strings.Contains(d.Text, "▀") {
			t.Fatal(d.Text)
		}
		if format == "png" {
			found := false
			for _, s := range d.Spans {
				found = found || s.Style.Pixel && s.Style.Foreground == 0xff0000 && s.Style.Background == 0x0000ff
			}
			if !found {
				t.Fatal("pixel colors lost")
			}
		}
	}
	if err := os.WriteFile(filepath.Join(root, "bad.svg"), []byte("<svg/>"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"missing.png", "bad.svg", "."} {
		d, err := RenderWithOptions(context.Background(), "![sample]("+target+")", "markdown", Options{BaseDir: root, Media: true})
		if err != nil {
			t.Fatal(err)
		}
		if d.Media || !strings.Contains(d.Text, "[image:") {
			t.Fatal("failed image must remain a placeholder", d.Text)
		}
	}
}

func TestRemoteImagesRequireOptInAndCache(t *testing.T) {
	var data bytes.Buffer
	png.Encode(&data, testBitmap())
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.Write(data.Bytes()) }))
	defer server.Close()
	source := "![remote](" + server.URL + "/picture.png)"
	options := Options{Media: true, Cache: &MediaCache{}}
	d, err := RenderWithOptions(context.Background(), source, "markdown", options)
	if err != nil {
		t.Fatal(err)
	}
	if d.Media || calls.Load() != 0 {
		t.Fatal("fetched without opt-in")
	}
	options.RemoteImages = true
	for range 2 {
		d, err = RenderWithOptions(context.Background(), source, "markdown", options)
		if err != nil {
			t.Fatal(err)
		}
		if !d.Media {
			t.Fatal(d.Text)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("remote image was not cached", calls.Load())
	}
}
