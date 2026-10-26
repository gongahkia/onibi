package editor

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/markup"
	"github.com/gongahkia/shack/internal/text"
)

func openTestPreview(t *testing.T, a *App, path, source string) *Buffer {
	t.Helper()
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Open(path); err != nil {
		t.Fatal(err)
	}
	a.documentPreview("markdown", false)
	b := a.Current()
	awaitReading(t, a, b)
	if b.Reading.Error != "" {
		t.Fatal(b.Reading.Error)
	}
	return b
}

func TestReadingHeadingFootnoteNavigationAndHistory(t *testing.T) {
	a := testApp(t)
	b := openTestPreview(t, a, filepath.Join(t.TempDir(), "note.md"), "[heading](#destination)\n\nText[^a].\n\n# Destination\n\n[^a]: Footnote body.\n")
	key(a, tcell.KeyEnter)
	if p := b.Doc.Selections[0].Head; p != b.Reading.Image.Anchors["heading:destination"] {
		t.Fatal("heading jump", p)
	}
	a.jumpBy(-1)
	if b.Doc.Selections[0].Head != 0 {
		t.Fatal("jump back")
	}
	a.jumpBy(1)
	if b.Doc.Selections[0].Head == 0 {
		t.Fatal("jump forward")
	}
	for _, s := range b.Reading.Image.Spans {
		if s.Style.Anchor == "footnote:1" {
			b.Doc.Selections = []text.Selection{{Anchor: s.Start, Head: s.Start}}
			key(a, tcell.KeyEnter)
			break
		}
	}
	if p := b.Doc.Selections[0].Head; p != b.Reading.Image.Anchors["footnote:1"] {
		t.Fatal("footnote jump", p)
	}
	for _, s := range b.Reading.Image.Spans {
		if s.Style.Anchor == "reference:1:0" {
			b.Doc.Selections = []text.Selection{{Anchor: s.Start, Head: s.Start}}
			key(a, tcell.KeyEnter)
			break
		}
	}
	if p := b.Doc.Selections[0].Head; p != b.Reading.Image.Anchors["reference:1:0"] {
		t.Fatal("backlink jump", p)
	}
	key(a, tcell.KeyTab)
	if _, ok := a.currentReadingLink(); !ok {
		t.Fatal("Tab did not select a link")
	}
	key(a, tcell.KeyBacktab)
	if _, ok := a.currentReadingLink(); !ok {
		t.Fatal("Backtab did not select a link")
	}
}

func TestReadingRelativeFileAndDirtyBufferNavigation(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	target := filepath.Join(root, "other note.md")
	if err := os.WriteFile(target, []byte("# On disk\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Open(target); err != nil {
		t.Fatal(err)
	}
	dirty := a.Current()
	dirty.Doc.Selections = []text.Selection{{Anchor: 0, Head: dirty.Doc.Text.Len()}}
	a.insert("# Unsaved heading\nKeep these edits.\n")
	if err := os.Mkdir(filepath.Join(root, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	origin := openTestPreview(t, a, filepath.Join(root, "sub", "index.md"), "[next](../other%20note.md#unsaved-heading)\n")
	key(a, tcell.KeyEnter)
	destination := a.Current()
	awaitReading(t, a, destination)
	if destination == origin || destination.Reading.Source != dirty || !strings.HasPrefix(destination.Doc.Text.Slice(destination.Doc.Selections[0].Head, destination.Doc.Text.Len()), "Unsaved heading") {
		t.Fatal("relative link did not reach dirty buffer", a.Status)
	}
	if !strings.Contains(dirty.Doc.Text.String(), "Keep these edits.") {
		t.Fatal("dirty text replaced")
	}
	a.jumpBy(-1)
	if a.Current() != origin {
		t.Fatal("cross-document history")
	}
	bytes, _ := os.ReadFile(target)
	if string(bytes) != "# On disk\n" {
		t.Fatal("navigation wrote to disk")
	}
}

func TestReadingLinksErrorsAndStaleChoices(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	b := openTestPreview(t, a, filepath.Join(root, "index.md"), "[missing](missing.md)\n\n# Heading\n")
	count := len(a.Buffers)
	key(a, tcell.KeyEnter)
	if a.Current() != b || len(a.Buffers) != count || !strings.Contains(a.Status, "Document link:") {
		t.Fatal("missing link created a file/buffer", a.Status)
	}
	a.followReadingTarget(b, markup.Style{Target: "#unknown"})
	if !strings.Contains(a.Status, "not found") {
		t.Fatal(a.Status)
	}
	a.readingLinks()
	item := a.Dialog.Items[0]
	a.Dialog = nil
	b.Reading.Image = &markup.Document{Text: "updated"}
	item.Run()
	if !strings.Contains(a.Status, "Preview changed") {
		t.Fatal("stale link choice ran")
	}
	for _, target := range []string{"javascript:alert(1)", "file:///tmp/a", "//host/file", "/tmp/a", "a%00.md"} {
		a.followReadingTarget(b, markup.Style{Target: target})
		if a.Current() != b || len(a.Buffers) != count {
			t.Fatal(target)
		}
	}
}

func TestReadingPendingAnchorDoesNotStealFocus(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "other.md"), []byte("# Target\n"), 0600); err != nil {
		t.Fatal(err)
	}
	openTestPreview(t, a, filepath.Join(root, "index.md"), "[next](other.md#target)\n")
	key(a, tcell.KeyEnter)
	pending := a.Current()
	a.NewBuffer()
	active := a.Current()
	awaitReading(t, a, pending)
	if a.Current() != active || pending.Reading.PendingAnchor != "" {
		t.Fatal("late result stole focus or retained navigation")
	}
}

func TestReadingCodeThemeAndPixelStyles(t *testing.T) {
	a := testApp(t)
	b := openTestPreview(t, a, filepath.Join(t.TempDir(), "note.md"), "```go\npackage main\n```\n")
	offset := strings.Index(b.Doc.Text.String(), "package")
	dark := readingStyle(b.Reading, offset, a.styles().text, a.styles())
	a.Config.Theme = "light"
	light := readingStyle(b.Reading, offset, a.styles().text, a.styles())
	dfg, _, _ := dark.Decompose()
	lfg, _, _ := light.Decompose()
	if dfg == lfg {
		t.Fatal("code colors did not follow theme")
	}
	v := &readingView{Image: &markup.Document{Spans: []markup.Span{{Start: 0, End: 3, Style: markup.Style{Pixel: true, Foreground: 0xff0000, Background: 0x0000ff}}}}}
	fg, bg, _ := readingStyle(v, 0, a.styles().text, a.styles()).Decompose()
	if fg != tcell.NewHexColor(0xff0000) || bg != tcell.NewHexColor(0x0000ff) {
		t.Fatal("image colors lost")
	}
}

func TestReadingStandaloneImageAndMediaToggle(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	bitmap := image.NewRGBA(image.Rect(0, 0, 4, 4))
	bitmap.Set(0, 0, color.RGBA{R: 255, A: 255})
	f, err := os.Create(filepath.Join(root, "image.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, bitmap); err != nil {
		t.Fatal(err)
	}
	f.Close()
	origin := openTestPreview(t, a, filepath.Join(root, "index.md"), "[picture](image.png)\n")
	key(a, tcell.KeyEnter)
	asset := a.Current()
	awaitReading(t, a, asset)
	if asset.Reading.AssetPath == "" || !asset.Reading.Image.Media || !asset.Transient || !strings.Contains(asset.Doc.Text.String(), "▀") {
		t.Fatal("image link did not render", asset.Doc.Text.String())
	}
	a.configureReadingMedia(false)
	awaitReading(t, a, asset)
	if strings.Contains(asset.Doc.Text.String(), "▀") {
		t.Fatal("media toggle ignored")
	}
	a.jumpBy(-1)
	if a.Current() != origin {
		t.Fatal("image history")
	}
}
