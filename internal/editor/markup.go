package editor

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/markup"
	"github.com/gongahkia/shack/internal/text"
)

type readingView struct {
	Source    *Buffer
	Format    string
	Image     *markup.Document
	SourceDoc *text.Document
	Version   uint64
	Requested bool
	Error     string
	Wrap      bool
}
type readingJob struct {
	Target  *Buffer
	View    *readingView
	Source  *Buffer
	Doc     *text.Document
	Version uint64
	Text    text.Rope
}

func (a *App) readingCommands() {
	a.Commands = append(a.Commands,
		command{"Toggle document preview", "F4", func() { a.documentPreview("", false) }},
		command{"Document preview beside source", "", func() { a.documentPreview("", true) }},
		command{"Preview as Markdown", "", func() { a.documentPreview("markdown", false) }},
		command{"Preview as Org", "", func() { a.documentPreview("org", false) }},
		command{"Refresh document preview", "", func() {
			if v := a.Current().Reading; v != nil {
				v.Requested = false
				a.queueReading(a.Current())
			} else {
				a.documentPreview("", false)
			}
		}},
		command{"Document preview headings", "", a.readingHeadings},
		command{"Document preview links", "", a.readingLinks},
	)
}
func (a *App) selectReadingBuffer(b *Buffer) {
	a.savePane()
	for i, p := range a.panes {
		if p.Buffer == b {
			a.focusPane(i)
			return
		}
	}
	if i := slices.Index(a.Buffers, b); i >= 0 {
		a.Active = i
		a.Matches = nil
		a.typingDoc = nil
		a.snippet = nil
		a.reveal()
		a.schedule()
	}
}
func (a *App) documentPreview(format string, beside bool) {
	source := a.Current()
	if v := source.Reading; v != nil {
		if v.Source == nil {
			a.Status = "Source buffer was closed; preview is a snapshot"
			return
		}
		a.selectReadingBuffer(v.Source)
		return
	}
	if source.Terminal != nil {
		a.Status = "Choose a Markdown or Org source buffer"
		return
	}
	if format == "" {
		format = markup.Format(source.File.Path)
	}
	if format == "" {
		a.list("Document preview format", []choice{{"Markdown", func() { a.documentPreview("markdown", beside) }}, {"Org", func() { a.documentPreview("org", beside) }}})
		return
	}
	a.ensurePanes()
	count := 0
	for _, b := range a.Buffers {
		if v := b.Reading; v != nil {
			count++
			if v.Source == source && v.Format == format {
				if beside && !a.readingVisible(b) {
					before := len(a.panes)
					a.split(false)
					if len(a.panes) == before {
						return
					}
				}
				a.selectReadingBuffer(b)
				a.queueReading(b)
				return
			}
		}
	}
	if count >= 8 {
		a.Status = "Up to eight document previews; close one before opening another"
		return
	}
	a.ensurePanes()
	a.savePane()
	if beside {
		before := len(a.panes)
		a.split(false)
		if len(a.panes) == before {
			return
		}
	}
	a.NewBuffer()
	b := a.Current()
	b.Transient = true
	b.Title = "Preview · " + bufferName(source)
	b.Reading = &readingView{Source: source, Format: format, Wrap: true}
	b.Doc, _ = text.NewDocument("Rendering document…\n")
	a.savePane()
	a.queueReading(b)
	a.Status = "Rendering " + format + " · F4 returns to source"
}
func (a *App) readingVisible(b *Buffer) bool {
	if a.Current() == b {
		return true
	}
	for _, p := range a.panes {
		if p.Buffer == b {
			return true
		}
	}
	return false
}
func (a *App) refreshReading() {
	for _, b := range a.Buffers {
		if b.Reading != nil && a.readingVisible(b) {
			a.queueReading(b)
		}
	}
}
func (a *App) queueReading(b *Buffer) {
	v := b.Reading
	if v == nil || v.Source == nil {
		return
	}
	source := v.Source
	if v.Requested && v.SourceDoc == source.Doc && v.Version == source.Doc.Version {
		return
	}
	v.Requested = true
	v.SourceDoc = source.Doc
	v.Version = source.Doc.Version
	job := readingJob{b, v, source, source.Doc, source.Doc.Version, source.Doc.Text}
	for i, old := range a.readingPending {
		if old.Target == b {
			a.readingPending[i] = job
			a.startReading()
			return
		}
	}
	a.readingPending = append(a.readingPending, job)
	a.startReading()
}
func (a *App) startReading() {
	if a.readingBusy || len(a.readingPending) == 0 {
		return
	}
	job := a.readingPending[0]
	a.readingPending = append(a.readingPending[:0], a.readingPending[1:]...)
	// Release the retained rope in the vacated queue slot.
	if len(a.readingPending) < cap(a.readingPending) {
		a.readingPending[:cap(a.readingPending)][len(a.readingPending)] = readingJob{}
	}
	if !slices.Contains(a.Buffers, job.Target) || job.Target.Reading != job.View || job.View.Source != job.Source {
		a.startReading()
		return
	}
	a.readingBusy = true
	go func() {
		limit := markup.MaxSource
		if job.View.Format == "org" {
			limit = markup.MaxOrgSource
		}
		var image markup.Document
		var err error
		if job.Text.Len() > limit {
			err = fmt.Errorf("%s preview limit is %d KiB; source remains editable", job.View.Format, limit>>10)
		} else {
			image, err = markup.Render(a.ctx, job.Text.String(), job.View.Format)
		}
		var doc *text.Document
		if err == nil {
			doc, err = text.NewDocument(image.Text)
		}
		a.post(func() {
			a.readingBusy = false
			if slices.Contains(a.Buffers, job.Target) && job.Target.Reading == job.View && job.View.Source == job.Source && job.Source.Doc == job.Doc && job.Source.Doc.Version == job.Version {
				v := job.View
				v.Error = ""
				if err != nil {
					v.Error = err.Error()
					if v.Image == nil {
						doc, _ = text.NewDocument("Preview unavailable: " + v.Error + "\n\nF4 returns to source.\n")
					}
					if a.Current() == job.Target {
						a.Status = "Preview: " + v.Error
					}
				} else {
					v.Image = &image
				}
				if doc != nil {
					a.installReading(job.Target, doc)
				}
			}
			a.startReading()
		})
	}()
}
func (a *App) installReading(b *Buffer, d *text.Document) {
	old := b.Doc
	// Preserve approximate reading positions by line/column rather than using a
	// stale byte offset after a live re-render. Other panes retain their own view.
	rebase := func(selections []text.Selection) []text.Selection {
		out := make([]text.Selection, len(selections))
		for i, s := range selections {
			project := func(p int) int {
				p = min(max(0, p), old.Text.Len())
				line := min(old.Text.LineAt(p), d.Text.Lines()-1)
				return lineOffset(d.Text, line, columnAt(old.Text, p, a.Config.TabSize), a.Config.TabSize)
			}
			out[i] = text.Selection{Anchor: project(s.Anchor), Head: project(s.Head)}
		}
		if len(out) == 0 {
			out = []text.Selection{{}}
		}
		return out
	}
	d.Selections = rebase(old.Selections)
	b.Doc = d
	b.Top = min(b.Top, d.Text.Lines()-1)
	b.WrapTop = 0
	b.Spans = nil
	b.SyntaxReady = false
	for i := range a.panes {
		p := &a.panes[i]
		if p.Buffer == b {
			p.Selections = rebase(p.Selections)
			p.Top = min(p.Top, d.Text.Lines()-1)
			p.WrapTop = 0
			p.Version = d.Version
			p.Rows = nil
		}
	}
	if a.matchBuffer == b {
		a.Matches = nil
		a.matchBuffer = nil
	}
	if a.Current() == b {
		a.reveal()
	}
}
func (a *App) closeReading(b *Buffer) {
	b.Reading = nil
	for _, other := range a.Buffers {
		if v := other.Reading; v != nil && v.Source == b {
			v.Source = nil
			v.SourceDoc = nil
			v.Error = "Source buffer closed; this is a snapshot"
		}
	}
	a.readingPending = slices.DeleteFunc(a.readingPending, func(j readingJob) bool { return j.Target == b || j.Source == b })
}
func (a *App) wraps(b *Buffer) bool {
	if v := b.Reading; v != nil {
		return v.Wrap
	}
	return a.wrap
}
func readingStyle(v *readingView, offset int, style tcell.Style, p palette) tcell.Style {
	if v == nil || v.Image == nil {
		return style
	}
	spans := v.Image.Spans
	i := sort.Search(len(spans), func(i int) bool { return spans[i].End > offset })
	if i == len(spans) || spans[i].Start > offset {
		return style
	}
	s := spans[i].Style
	if s.Heading || s.Link != "" {
		fg, _, _ := p.accent.Decompose()
		style = style.Foreground(fg)
	} else if s.Dim {
		fg, _, _ := p.dim.Decompose()
		style = style.Foreground(fg)
	}
	if s.Code {
		_, bg, _ := p.bar.Decompose()
		style = style.Background(bg)
	}
	return style.Bold(s.Bold).Italic(s.Italic).Underline(s.Underline).StrikeThrough(s.Strike).Url(s.Link)
}
func (a *App) readingHeadings() {
	b := a.Current()
	v := b.Reading
	if v == nil || v.Image == nil {
		a.Status = "Open a document preview first (F4)"
		return
	}
	image := v.Image
	items := []choice{}
	for _, h := range image.Headings {
		items = append(items, choice{strings.Repeat("  ", min(8, h.Level-1)) + h.Title, func() {
			if b.Reading == v && v.Image == image {
				a.selectReadingBuffer(b)
				b.Doc.Selections = []text.Selection{{Anchor: h.Offset, Head: h.Offset}}
				a.reveal()
			}
		}})
	}
	a.list("Document headings", items)
}
func (a *App) readingLinks() {
	b := a.Current()
	v := b.Reading
	if v == nil || v.Image == nil {
		a.Status = "Open a document preview first (F4)"
		return
	}
	items := []choice{}
	seen := map[string]bool{}
	for _, s := range v.Image.Spans {
		u := s.Style.Link
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		items = append(items, choice{u, func() {
			a.clipboard = u
			a.Screen.SetClipboard([]byte(u))
			a.Status = "Link copied internally; terminal clipboard write requested"
		}})
	}
	a.list("Document links · Enter copies URL", items)
}
