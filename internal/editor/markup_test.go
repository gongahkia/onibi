package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/config"
	"github.com/gongahkia/shack/internal/markup"
	"github.com/gongahkia/shack/internal/text"
)

func awaitReading(t *testing.T, a *App, b *Buffer) {
	t.Helper()
	actionUntil(t, a, func() bool {
		return !a.readingBusy && len(a.readingPending) == 0 && b.Reading != nil && (b.Reading.Image != nil || b.Reading.Error != "")
	})
}
func TestDocumentPreviewWorkflow(t *testing.T) {
	a := testApp(t)
	source := a.Current()
	source.File.Path = filepath.Join(t.TempDir(), "note.md")
	a.insert("# Hello **世界**\n\n- [x] A task\n\n[site](https://example.com)\n\nReference[^note].\n\n[^note]: A **footnote**.\n\nTerm\n: Definition text.\n")
	original := source.Doc.Text.String()
	a.documentPreview("", true)
	preview := a.Current()
	awaitReading(t, a, preview)
	if len(a.panes) != 2 || a.panes[0].Buffer != source || preview == source || !preview.Transient || !strings.Contains(preview.Doc.Text.String(), "Hello 世界") {
		t.Fatalf("preview layout/text: %#v %q", a.panes, preview.Doc.Text.String())
	}
	if a.wrap || !a.wraps(preview) {
		t.Fatal("preview changed source wrapping")
	}
	a.insert("bad")
	key(a, tcell.KeyCtrlZ)
	key(a, tcell.KeyCtrlS)
	if source.Doc.Text.String() != original || strings.Contains(preview.Doc.Text.String(), "bad") {
		t.Fatal("preview edited source or rendered text")
	}
	for _, expected := range []string{"Reference[1].", "[1] A footnote.", "Definition text."} {
		if !strings.Contains(preview.Doc.Text.String(), expected) {
			t.Fatal("missing Markdown extension", expected)
		}
	}
	a.draw()
	foundBold := false
	cells, _, _ := a.Screen.(tcell.SimulationScreen).GetContents()
	for _, c := range cells {
		_, _, attrs := c.Style.Decompose()
		foundBold = foundBold || attrs&tcell.AttrBold != 0
	}
	if !foundBold {
		t.Fatal("no styled preview")
	}
	a.readingLinks()
	items := a.Dialog.Items
	if len(items) != 3 || items[0].Label != "https://example.com" {
		t.Fatal(items)
	}
	a.Dialog = nil
	items[0].Run()
	if a.clipboard != "https://example.com" {
		t.Fatal(a.clipboard)
	}
	a.readingHeadings()
	if len(a.Dialog.Items) != 1 {
		t.Fatal(a.Dialog)
	}
	a.Dialog = nil
	key(a, tcell.KeyF4)
	if a.Current() != source || a.focus != 0 {
		t.Fatal("F4 did not focus source")
	}
	source.Doc.Selections = []text.Selection{{Anchor: source.Doc.Text.Len(), Head: source.Doc.Text.Len()}}
	a.insert("\n## Another\n")
	a.refreshReading()
	awaitReading(t, a, preview)
	if !strings.Contains(preview.Doc.Text.String(), "Another") || len(preview.Reading.Image.Headings) != 2 {
		t.Fatal(preview.Doc.Text.String())
	}
	key(a, tcell.KeyF4)
	if a.Current() != preview || a.focus != 1 || len(a.Buffers) != 2 {
		t.Fatal("preview was duplicated")
	}
	a.runCommand("Toggle soft wrapping")
	if a.wraps(preview) || a.wrap {
		t.Fatal("per-preview wrapping")
	}
	a.closeBuffer()
	if a.readingBusy || preview.Reading != nil {
		t.Fatal("preview lifetime")
	}
}
func TestReadingStaleAndClosedSources(t *testing.T) {
	a := testApp(t)
	source := a.Current()
	a.insert("# Old")
	a.documentPreview("markdown", false)
	preview := a.Current()
	// Do not drain the worker reply until the source is different.
	source.Doc.Selections = []text.Selection{{Anchor: 0, Head: source.Doc.Text.Len()}}
	if e := source.Doc.Insert("# New"); e != nil {
		t.Fatal(e)
	}
	a.queueReading(preview)
	awaitReading(t, a, preview)
	if preview.Reading.Image.Text != "New\n\n" {
		t.Fatal(preview.Doc.Text.String())
	}
	a.documentPreview("", false)
	a.discardBuffer()
	if preview.Reading.Source != nil || preview.Reading.SourceDoc != nil {
		t.Fatal("closed source retained")
	}
	a.documentPreview("", false)
	if !strings.Contains(a.Status, "closed") {
		t.Fatal(a.Status)
	}
}
func TestReadingLimitAndExport(t *testing.T) {
	a := testApp(t)
	a.insert(strings.Repeat("x", markup.MaxOrgSource+1))
	a.documentPreview("org", false)
	preview := a.Current()
	awaitReading(t, a, preview)
	if preview.Reading.Error == "" || !strings.Contains(preview.Doc.Text.String(), "limit") {
		t.Fatal("missing size error")
	}
	path := filepath.Join(t.TempDir(), "export.txt")
	a.saveAs()
	a.Dialog.Accept(path)
	a.Dialog = nil
	if preview.Reading != nil || preview.Transient {
		t.Fatal("export remains live")
	}
	saved, e := os.ReadFile(path)
	if e != nil || string(saved) != preview.Doc.Text.String() {
		t.Fatal(e)
	}
}
func TestReadingRefreshErrorPreservesSnapshot(t *testing.T) {
	a := testApp(t)
	source := a.Current()
	a.insert("* Good")
	a.documentPreview("org", false)
	preview := a.Current()
	awaitReading(t, a, preview)
	old := preview.Doc.Text.String()
	source.Doc, _ = text.NewDocument(strings.Repeat("x", markup.MaxOrgSource+1))
	a.queueReading(preview)
	awaitReading(t, a, preview)
	if preview.Doc.Text.String() != old || preview.Reading.Error == "" {
		t.Fatal("lost snapshot or silently stale")
	}
}
func TestReadingCloseInFlight(t *testing.T) {
	a := testApp(t)
	a.insert("# Hello")
	a.documentPreview("markdown", false)
	b := a.Current()
	a.discardBuffer()
	actionUntil(t, a, func() bool { return !a.readingBusy })
	if b.Reading != nil || len(a.Buffers) != 1 {
		t.Fatal("closed view revived")
	}
}
func BenchmarkReadingViewport(b *testing.B) {
	image, e := markup.Render(b.Context(), strings.Repeat("## 世界\n\nThis is **bold** text with 👩‍💻 and [a link](https://example.com).\n\n", 500), "markdown")
	if e != nil {
		b.Fatal(e)
	}
	s := tcell.NewSimulationScreen("UTF-8")
	s.Init()
	defer s.Fini()
	s.SetSize(100, 32)
	a, e := New(s, config.Default(), nil, false)
	if e != nil {
		b.Fatal(e)
	}
	defer a.cancel()
	a.Current().Doc, _ = text.NewDocument(image.Text)
	a.Current().Reading = &readingView{Image: &image, Format: "markdown", Wrap: true}
	a.Current().Transient = true
	a.draw()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		a.draw()
	}
}
