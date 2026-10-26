package editor

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/markup"
	"github.com/gongahkia/shack/internal/text"
)

func (a *App) readingKey(ev *tcell.EventKey) bool {
	if a.Current().Reading == nil {
		return false
	}
	if ev.Modifiers()&tcell.ModAlt != 0 {
		switch ev.Key() {
		case tcell.KeyLeft:
			a.jumpBy(-1)
			return true
		case tcell.KeyRight:
			a.jumpBy(1)
			return true
		}
	}
	switch ev.Key() {
	case tcell.KeyEnter:
		a.followReadingLink()
	case tcell.KeyTab:
		a.nextReadingLink(1)
	case tcell.KeyBacktab:
		a.nextReadingLink(-1)
	default:
		return false
	}
	return true
}

func readingLinkSpans(v *readingView) []markup.Span {
	if v == nil || v.Image == nil {
		return nil
	}
	var out []markup.Span
	for _, s := range v.Image.Spans {
		if s.Style.Target == "" && s.Style.Anchor == "" && s.Style.Link == "" {
			continue
		}
		if len(out) > 0 {
			last := &out[len(out)-1]
			if last.End == s.Start && last.Style.Target == s.Style.Target && last.Style.Anchor == s.Style.Anchor && last.Style.Link == s.Style.Link {
				last.End = s.End
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

func (a *App) currentReadingLink() (markup.Style, bool) {
	b := a.Current()
	pos := b.Doc.Selections[0].Head
	for _, s := range readingLinkSpans(b.Reading) {
		if pos >= s.Start && pos < s.End {
			return s.Style, true
		}
	}
	return markup.Style{}, false
}

func linkLabel(s markup.Style) string {
	if s.Anchor != "" {
		if strings.HasPrefix(s.Anchor, "footnote:") {
			return "Footnote " + strings.TrimPrefix(s.Anchor, "footnote:")
		}
		return "Back to footnote reference " + strings.TrimPrefix(s.Anchor, "reference:")
	}
	if s.Target != "" {
		return s.Target
	}
	return s.Link
}

func (a *App) nextReadingLink(direction int) {
	b := a.Current()
	links := readingLinkSpans(b.Reading)
	if len(links) == 0 {
		a.Status = "No document links"
		return
	}
	pos := b.Doc.Selections[0].Head
	index := 0
	if direction > 0 {
		for i, s := range links {
			if s.Start > pos {
				index = i
				break
			}
		}
	} else {
		index = len(links) - 1
		for i := len(links) - 1; i >= 0; i-- {
			if links[i].Start < pos {
				index = i
				break
			}
		}
	}
	s := links[index]
	b.Doc.Selections = []text.Selection{{Anchor: s.Start, Head: s.Start}}
	a.Status = "Enter follows · " + linkLabel(s.Style)
	a.reveal()
}

func (a *App) copyReadingLink() {
	s, ok := a.currentReadingLink()
	if !ok {
		a.Status = "Place the cursor on a document link"
		return
	}
	a.copyReadingTarget(linkLabel(s))
}
func (a *App) copyReadingTarget(value string) {
	a.clipboard = value
	a.Screen.SetClipboard([]byte(value))
	a.Status = "Link copied internally; terminal clipboard write requested"
}
func (a *App) followReadingLink() {
	s, ok := a.currentReadingLink()
	if !ok {
		a.Status = "No link at cursor · Tab moves to the next link"
		return
	}
	a.followReadingTarget(a.Current(), s)
}
func (a *App) readingLinks() {
	b := a.Current()
	v := b.Reading
	if v == nil || v.Image == nil {
		a.Status = "Open a document preview first (F4)"
		return
	}
	image := v.Image
	items := []choice{}
	seen := map[string]bool{}
	for _, span := range readingLinkSpans(v) {
		s := span.Style
		id := s.Target + "\x00" + s.Anchor + "\x00" + s.Link
		if seen[id] {
			continue
		}
		seen[id] = true
		items = append(items, choice{linkLabel(s), func() {
			if b.Reading != v || v.Image != image {
				a.Status = "Preview changed; choose the link again"
				return
			}
			a.selectReadingBuffer(b)
			a.followReadingTarget(b, s)
		}})
	}
	a.list("Document links · Enter follows; web URLs copy", items)
}

func (a *App) jumpReadingAnchor(b *Buffer, key string, remember bool) {
	v := b.Reading
	if v == nil || v.Image == nil {
		a.Status = "Preview is still rendering"
		return
	}
	offset, ok := v.Image.Anchors[key]
	if !ok {
		a.Status = "Document anchor not found: " + strings.TrimPrefix(key, "heading:")
		return
	}
	if remember {
		a.rememberJump()
	}
	a.selectReadingBuffer(b)
	if !remember && a.jumpIndex >= 0 && a.jumpIndex < len(a.jumps) && a.jumps[a.jumpIndex].Buffer == b {
		a.jumps[a.jumpIndex].Offset = offset
	}
	a.finishJump(b, offset)
	a.Status = "Document link · " + strings.TrimPrefix(key, "heading:")
}

func (a *App) followReadingTarget(b *Buffer, s markup.Style) {
	if b.Reading == nil {
		return
	}
	if s.Anchor != "" {
		a.jumpReadingAnchor(b, s.Anchor, true)
		return
	}
	target := s.Target
	if target == "" {
		target = s.Link
	}
	if markup.SafeURL(target) != "" {
		a.copyReadingTarget(target)
		return
	}
	if markup.LinkTarget(target) == "" {
		a.Status = "Unsupported document link"
		return
	}
	u, err := url.Parse(target)
	if err != nil {
		a.Status = err.Error()
		return
	}
	if u.Path == "" {
		if u.Fragment == "" {
			a.jumpTo(b, 0)
			return
		}
		a.jumpReadingAnchor(b, "heading:"+u.Fragment, true)
		return
	}
	v := b.Reading
	if v.Source == nil || v.Source.File.Path == "" {
		a.Status = "Save the source document before following relative file links"
		return
	}
	path := filepath.Join(filepath.Dir(v.Source.File.Path), filepath.FromSlash(u.Path))
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		a.Status = fmt.Sprintf("Document link: %v", err)
		return
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		a.Status = "Document link must point to an existing regular file"
		return
	}
	a.rememberJump()
	if imagePath(resolved) {
		a.openReadingImage(resolved)
		return
	}
	a.savePane()
	// Prefer an open buffer, including unsaved changes, over reading disk again.
	var opened *Buffer
	for _, candidate := range a.Buffers {
		if !candidate.Transient && candidate.File.Path == resolved {
			opened = candidate
			break
		}
	}
	if opened != nil {
		a.selectReadingBuffer(opened)
	} else if err = a.Open(resolved); err != nil {
		a.Status = err.Error()
		return
	}
	a.Matches = nil
	if markup.Format(resolved) == "" {
		a.finishJump(a.Current(), 0)
		a.Status = "Opened " + filepath.Base(resolved)
		return
	}
	a.documentPreview("", false)
	destination := a.Current()
	if destination.Reading == nil {
		return
	}
	a.finishJump(destination, 0)
	if u.Fragment == "" {
		return
	}
	key := "heading:" + u.Fragment
	dv := destination.Reading
	if dv.Image != nil && dv.ImageDoc == dv.Source.Doc && dv.ImageVersion == dv.Source.Doc.Version {
		a.jumpReadingAnchor(destination, key, false)
	} else {
		dv.PendingAnchor = key
	}
}

func imagePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif":
		return true
	}
	return false
}
func (a *App) configureReadingMedia(remote bool) {
	v := a.Current().Reading
	if v == nil {
		a.Status = "Open a document preview first (F4)"
		return
	}
	if v.Source == nil {
		a.Status = "Source buffer closed; reopen it to change media"
		return
	}
	if remote {
		v.Media = true
		v.RemoteImages = true
		a.Status = "Loading remote images for this preview"
	} else {
		v.Media = !v.Media
		a.Status = fmt.Sprintf("Preview media: %t", v.Media)
	}
	v.Requested = false
	a.queueReading(a.Current())
}
func (a *App) openReadingImage(path string) {
	count := 0
	for _, b := range a.Buffers {
		if v := b.Reading; v != nil {
			count++
			if v.AssetPath == path {
				a.selectReadingBuffer(b)
				a.finishJump(b, 0)
				return
			}
		}
	}
	if count >= 8 {
		a.Status = "Up to eight document previews; close one before opening another"
		return
	}
	// A private immutable Markdown source drives the ordinary preview worker.
	// It is never an editable/file-saving buffer and creates no file on disk.
	doc, _ := text.NewDocument("![](" + url.PathEscape(filepath.Base(path)) + ")")
	source := &Buffer{File: &File{Path: filepath.Join(filepath.Dir(path), ".shack-image-preview.md")}, Doc: doc, Transient: true}
	a.NewBuffer()
	b := a.Current()
	b.Transient = true
	b.Title = "Image · " + filepath.Base(path)
	b.Reading = &readingView{Source: source, Format: "markdown", Wrap: false, Media: true, Cache: &markup.MediaCache{}, AssetPath: path}
	b.Doc, _ = text.NewDocument("Rendering image…\n")
	a.savePane()
	a.queueReading(b)
	a.rememberJump()
}
