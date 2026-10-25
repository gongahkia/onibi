package editor

import (
	"fmt"
	"slices"
	"sort"

	"github.com/gongahkia/shack/internal/folding"
	"github.com/gongahkia/shack/internal/text"
)

type foldCache struct {
	Path    string
	Doc     *text.Document
	Version uint64
	Ranges  []folding.Range
}
type foldLine struct{ Start, End int }
type foldState struct {
	Buffer           *Buffer
	Doc              *text.Document
	Version, Request uint64
	Closed           []folding.Range
	Visible          []foldLine
}
type foldJob struct {
	Selections       []text.Selection
	Buffer           *Buffer
	State            *foldState
	Doc              *text.Document
	Version, Request uint64
	Text             text.Rope
	Action           string
	Line             int
	Path             string
}

func (a *App) foldCommands() {
	a.Commands = append(a.Commands,
		command{"Toggle fold", "F6", func() { a.foldCommand("toggle", -1) }}, command{"Fold all", "", func() { a.foldCommand("all", -1) }}, command{"Unfold all", "Shift+F6", func() { a.foldCommand("unfold", -1) }},
	)
}
func (a *App) foldState() *foldState {
	a.ensurePanes()
	p := &a.panes[a.focus]
	b := a.Current()
	if p.Folds == nil || p.Folds.Buffer != b || p.Folds.Doc != b.Doc {
		p.Folds = &foldState{Buffer: b, Doc: b.Doc, Version: b.Doc.Version}
	}
	return p.Folds
}
func (a *App) foldCommand(action string, line int) {
	b := a.Current()
	if b.Terminal != nil || b.Reading != nil {
		a.Status = "Fold the source buffer; previews and terminals have separate layouts"
		return
	}
	state := a.foldState()
	state.Request++
	if action == "unfold" {
		state.Closed = nil
		state.Visible = nil
		a.Status = "All folds expanded in this pane"
		a.reveal()
		return
	}
	if line < 0 {
		line = b.Doc.Text.LineAt(b.Doc.Selections[0].Head)
	}
	if action == "toggle" {
		for i, r := range state.Closed {
			if b.Doc.Text.LineAt(r.Start) == line {
				state.Closed = slices.Delete(state.Closed, i, i+1)
				state.project()
				a.Status = "Fold expanded"
				a.reveal()
				return
			}
		}
	}
	if cache := b.FoldCache; cache != nil && cache.Doc == b.Doc && cache.Version == b.Doc.Version && cache.Path == b.File.Path {
		a.applyFold(state, cache.Ranges, action, line)
		return
	}
	if b.Doc.Text.Len() > folding.MaxSource {
		a.Status = "Fold scanning is limited to 16 MiB; source remains editable"
		return
	}
	a.foldPending = &foldJob{Buffer: b, State: state, Doc: b.Doc, Version: b.Doc.Version, Request: state.Request, Text: b.Doc.Text, Action: action, Line: line, Path: b.File.Path, Selections: append([]text.Selection(nil), b.Doc.Selections...)}
	a.Status = "Finding folds…"
	a.startFold()
}
func (a *App) startFold() {
	if a.foldBusy || a.foldPending == nil {
		return
	}
	job := *a.foldPending
	a.foldPending = nil
	a.foldBusy = true
	go func() {
		ranges, err := folding.Find(a.ctx, job.Text.String(), job.Path)
		a.post(func() {
			a.foldBusy = false
			if slices.Contains(a.Buffers, job.Buffer) && job.Buffer.Doc == job.Doc && job.Buffer.Doc.Version == job.Version && job.Buffer.File.Path == job.Path {
				if err == nil {
					job.Buffer.FoldCache = &foldCache{job.Path, job.Doc, job.Version, ranges}
				}
				if a.Current() == job.Buffer && a.focus < len(a.panes) && a.panes[a.focus].Folds == job.State && job.State.Request == job.Request && slices.Equal(job.Doc.Selections, job.Selections) {
					if err != nil {
						a.Status = err.Error()
					} else {
						a.applyFold(job.State, ranges, job.Action, job.Line)
					}
				}
			}
			a.startFold()
		})
	}()
}
func (s *foldState) project() {
	sort.Slice(s.Closed, func(i, j int) bool {
		if s.Closed[i].Start == s.Closed[j].Start {
			return s.Closed[i].End > s.Closed[j].End
		}
		return s.Closed[i].Start < s.Closed[j].Start
	})
	s.Visible = nil
	r := s.Buffer.Doc.Text
	for _, f := range s.Closed {
		start, end := r.LineAt(f.Start), r.LineAt(max(f.Start, f.End-1))
		if end <= start {
			continue
		}
		if len(s.Visible) > 0 && start <= s.Visible[len(s.Visible)-1].End {
			continue
		}
		s.Visible = append(s.Visible, foldLine{start, end})
	}
}
func (a *App) applyFold(s *foldState, ranges []folding.Range, action string, line int) {
	b := s.Buffer
	if action == "all" {
		s.Closed = append([]folding.Range(nil), ranges...)
	} else {
		index := -1
		for i, r := range ranges {
			start, end := b.Doc.Text.LineAt(r.Start), b.Doc.Text.LineAt(max(r.Start, r.End-1))
			if start > line {
				break
			}
			if start <= line && line <= end {
				index = i
				if start == line {
					break
				}
			}
		}
		if index < 0 {
			a.Status = "No structural fold at this line"
			return
		}
		r := ranges[index]
		found := slices.Index(s.Closed, r)
		if found >= 0 {
			s.Closed = slices.Delete(s.Closed, found, found+1)
		} else {
			s.Closed = append(s.Closed, r)
		}
	}
	s.project()
	for i, selection := range b.Doc.Selections {
		line := b.Doc.Text.LineAt(selection.Head)
		if f, ok := s.hidden(line); ok {
			p := b.Doc.Text.LineEnd(f.Start)
			b.Doc.Selections[i] = text.Selection{Anchor: p, Head: p}
		}
	}
	a.normalizeSelections()
	a.reveal()
	a.Status = fmt.Sprintf("%d collapsed regions in this pane · F6 toggle · Shift+F6 expand all", len(s.Closed))
}
func (s *foldState) hidden(line int) (foldLine, bool) {
	if s == nil {
		return foldLine{}, false
	}
	i := sort.Search(len(s.Visible), func(i int) bool { return s.Visible[i].Start >= line }) - 1
	if i >= 0 && line <= s.Visible[i].End {
		return s.Visible[i], true
	}
	return foldLine{}, false
}
func (a *App) nextVisible(index, line int) int {
	line++
	if index < len(a.panes) {
		if f, ok := a.panes[index].Folds.hidden(line); ok {
			return f.End + 1
		}
	}
	return line
}
func (a *App) previousVisible(index, line int) int {
	line = max(0, line-1)
	if index < len(a.panes) {
		if f, ok := a.panes[index].Folds.hidden(line); ok {
			return f.Start
		}
	}
	return line
}
func (a *App) visibleStart(index, line int) int {
	if index < len(a.panes) {
		if f, ok := a.panes[index].Folds.hidden(line); ok {
			return f.Start
		}
	}
	return line
}
func (a *App) stepVisible(line, delta int) int {
	b := a.Current()
	for delta > 0 {
		next := a.nextVisible(a.focus, line)
		if next >= b.Doc.Text.Lines() {
			break
		}
		line = next
		delta--
	}
	for delta < 0 && line > 0 {
		line = a.previousVisible(a.focus, line)
		delta++
	}
	return line
}
func (a *App) unfoldCursor(b *Buffer) {
	if a.focus >= len(a.panes) || a.panes[a.focus].Buffer != b {
		return
	}
	s := a.panes[a.focus].Folds
	if s == nil {
		return
	}
	if s.Doc != b.Doc || s.Version != b.Doc.Version {
		a.rebaseFolds(b)
		s = a.panes[a.focus].Folds
		if s == nil {
			return
		}
	}
	line := b.Doc.Text.LineAt(b.Doc.Selections[0].Head)
	b.Top = a.visibleStart(a.focus, b.Top)
	if _, hidden := s.hidden(line); !hidden {
		return
	}
	before := len(s.Closed)
	s.Closed = slices.DeleteFunc(s.Closed, func(f folding.Range) bool {
		return line > b.Doc.Text.LineAt(f.Start) && line <= b.Doc.Text.LineAt(max(f.Start, f.End-1))
	})
	if len(s.Closed) != before {
		s.project()
	}
	b.Top = a.visibleStart(a.focus, b.Top)
}
func (a *App) rebaseFolds(b *Buffer) {
	if b.FoldCache != nil && (b.FoldCache.Doc != b.Doc || b.FoldCache.Version != b.Doc.Version || b.FoldCache.Path != b.File.Path) {
		b.FoldCache = nil
	}
	for i := range a.panes {
		p := &a.panes[i]
		s := p.Folds
		if s == nil || p.Buffer != b {
			continue
		}
		if s.Doc != b.Doc {
			p.Folds = nil
			continue
		}
		if s.Version == b.Doc.Version {
			continue
		}
		for _, edits := range b.Doc.LastChanges {
			out := s.Closed[:0]
			ei, delta := 0, 0
			for _, r := range s.Closed {
				// Both ranges and edits are ordered. Rebase in one sweep;
				// thousands of selections must not multiply the fold count.
				for ei < len(edits) && edits[ei].End < r.Start {
					e := edits[ei]
					delta += len(e.Text) - (e.End - e.Start)
					ei++
				}
				if ei < len(edits) && edits[ei].Start < r.End {
					continue
				}
				r.Start += delta
				r.End += delta
				out = append(out, r)
			}
			s.Closed = out
		}
		s.Version = b.Doc.Version
		s.project()
	}
}
func (a *App) foldMarker(index, line int) string {
	p := &a.panes[index]
	if p.Folds != nil {
		visible := p.Folds.Visible
		i := sort.Search(len(visible), func(i int) bool { return visible[i].Start >= line })
		if i < len(visible) && visible[i].Start == line {
			return "▸"
		}
	}
	b := p.Buffer
	c := b.FoldCache
	if c == nil || c.Doc != b.Doc || c.Version != b.Doc.Version || c.Path != b.File.Path {
		return ""
	}
	start := b.Doc.Text.LineStart(line)
	i := sort.Search(len(c.Ranges), func(i int) bool { return c.Ranges[i].Start >= start })
	if i < len(c.Ranges) && c.Ranges[i].Start == start {
		return "▾"
	}
	return ""
}
