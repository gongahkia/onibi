package editor

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"time"
	"unicode"
	"unicode/utf8"

	snippets "github.com/gongahkia/shack/internal/snippet"
	"github.com/gongahkia/shack/internal/text"
)

type snippetStop struct {
	Number  int
	Ranges  []text.Selection
	Choices []string
}
type snippetState struct {
	Buffer          *Buffer
	Version         uint64
	Stops           []snippetStop
	Index, Base     int
	Program         *snippets.Program
	Expansion       snippets.Expansion
	Values          map[int]string
	Primary         text.Selection
	Expected        []text.Selection
	Pending         bool
	Edited          bool
	LastType        time.Time
	LastTypeVersion uint64
}

func snippetStops(expansion snippets.Expansion, base int) []snippetStop {
	result := make([]snippetStop, 0, len(expansion.Stops))
	for _, stop := range expansion.Stops {
		item := snippetStop{Number: stop.Number, Choices: stop.Choices}
		for _, r := range stop.Ranges {
			item.Ranges = append(item.Ranges, text.Selection{Anchor: base + r.Start, Head: base + r.End})
		}
		result = append(result, item)
	}
	return result
}

// Kept as a small parser entry point for grammar/regression tests. Interactive
// completion uses the background path below, including variable resolution.
func parseSnippet(source string) (string, []snippetStop, error) {
	p, err := snippets.Compile(source)
	if err != nil {
		return "", nil, err
	}
	p, err = p.Resolve(nil)
	if err != nil {
		return "", nil, err
	}
	result, err := p.Expand(context.Background(), nil, nil, true)
	return result.Text, snippetStops(result, 0), err
}
func (a *App) snippetCompletion(b *Buffer, version uint64, primary text.Edit, additional []text.Edit) {
	source := b.Doc.Text
	selections := append([]text.Selection(nil), b.Doc.Selections...)
	path, root, clipboard, now := b.File.Path, a.Root, a.clipboard, time.Now()
	a.snippetRequest++
	request := a.snippetRequest
	if a.snippetCancel != nil {
		a.snippetCancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.snippetCancel = cancel
	a.Status = "Preparing snippet…"
	go func() {
		defer cancel()
		p, err := snippets.Compile(primary.Text)
		if err == nil {
			p, err = p.Resolve(snippetVariables(source, selections[0], path, root, clipboard, now))
		}
		var result snippets.Expansion
		if err == nil {
			result, err = p.Expand(ctx, nil, nil, true)
		}
		a.post(func() {
			if request != a.snippetRequest || a.Current() != b || b.Doc.Version != version || !slices.Equal(selections, b.Doc.Selections) || a.Dialog != nil || a.Preview != nil || a.Menu != nil {
				return
			}
			if err != nil {
				a.Status = err.Error()
				return
			}
			base := primary.Start
			for _, edit := range additional {
				if edit.End <= primary.Start {
					base += len(edit.Text) - (edit.End - edit.Start)
				}
			}
			primary.Text = result.Text
			if a.Readonly || b.Transient {
				a.Status = "Read-only mode"
				return
			}
			if err := b.Doc.Apply(version, append([]text.Edit{primary}, additional...)); err != nil {
				a.Status = err.Error()
				return
			}
			if b.Doc.Version != version {
				a.changed()
			}
			a.snippet = &snippetState{Buffer: b, Version: b.Doc.Version, Program: p, Expansion: result, Stops: snippetStops(result, base), Base: base, Values: map[int]string{}}
			a.Status = "Snippet · Tab/Shift+Tab moves · Escape finishes · Snippet choices opens alternatives"
			a.selectSnippetStop()
		})
	}()
}
func (a *App) snippetTab(back bool) bool {
	s := a.snippet
	if s == nil || s.Buffer != a.Current() || s.Version != a.Current().Doc.Version {
		a.snippet = nil
		return false
	}
	if s.Pending {
		a.Status = "Snippet transform is still running"
		return true
	}
	move := func() {
		if a.snippet != s {
			return
		}
		if back {
			s.Index = max(0, s.Index-1)
		} else {
			s.Index++
		}
		if s.Index >= len(s.Stops) {
			a.snippet = nil
			return
		}
		a.selectSnippetStop()
	}
	if len(s.Expansion.Mirrors) == 0 {
		move()
		return true
	}
	// Regex execution is isolated from input. If typing changes this snapshot,
	// discard it and leave the current tabstop selected for the next Tab attempt.
	s.Pending = true
	version := s.Version
	selections := append([]text.Selection(nil), s.Buffer.Doc.Selections...)
	values := maps.Clone(s.Values)
	cache := s.Expansion.Cache
	program := s.Program
	if a.snippetCancel != nil {
		a.snippetCancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.snippetCancel = cancel
	go func() {
		defer cancel()
		result, err := program.Expand(ctx, values, cache, true)
		a.post(func() {
			s.Pending = false
			if a.snippet != s || a.Current() != s.Buffer {
				return
			}
			if s.Buffer.Doc.Version != version || !slices.Equal(selections, s.Buffer.Doc.Selections) || a.Dialog != nil || a.Preview != nil || a.Menu != nil {
				a.Status = "Snippet changed during transform; press Tab again"
				return
			}
			if err != nil {
				a.Status = err.Error()
				return
			}
			edits := []text.Edit{}
			for _, mirror := range s.Expansion.Mirrors {
				value := result.Cache[mirror.ID]
				r := mirror.Range
				if s.Expansion.Text[r.Start:r.End] != value {
					edits = append(edits, text.Edit{Start: s.Base + r.Start, End: s.Base + r.End, Text: value})
				}
			}
			// Restore the active range before navigation; transform updates are a part
			// of the preceding placeholder edit in undo history.
			if !a.applySnippet(s, result, values, edits, 0, 0, s.Edited) {
				return
			}
			move()
		})
	}()
	return true
}
func (a *App) selectSnippetStop() {
	s := a.snippet
	if s == nil || s.Index >= len(s.Stops) {
		return
	}
	stop := s.Stops[s.Index]
	s.Primary = stop.Ranges[0]
	s.Buffer.Doc.Selections = append([]text.Selection(nil), stop.Ranges...)
	a.normalizeSelections()
	s.Expected = append([]text.Selection(nil), s.Buffer.Doc.Selections...)
	if stop.Number == 0 {
		a.snippet = nil
		a.reveal()
		return
	}
	a.reveal()
	if len(stop.Choices) > 0 {
		a.snippetChoices()
	}
}
func (a *App) snippetChoices() {
	s := a.snippet
	if s == nil || s.Buffer != a.Current() || s.Version != s.Buffer.Doc.Version {
		a.Status = "No active snippet"
		return
	}
	stop := s.Stops[s.Index]
	if len(stop.Choices) == 0 {
		a.Status = "This tabstop has no fixed choices"
		return
	}
	number := stop.Number
	version := s.Version
	items := []choice{}
	for _, value := range stop.Choices {
		items = append(items, choice{value, func() {
			if a.snippet != s || s.Version != version || s.Stops[s.Index].Number != number {
				return
			}
			s.Primary = s.Stops[s.Index].Ranges[0]
			s.Expected = append([]text.Selection(nil), s.Buffer.Doc.Selections...)
			a.snippetEdit(value, s.Primary)
		}})
	}
	a.list("Snippet choices · type your own value or choose", items)
	a.Dialog.Inline = true
	current := s.Buffer.Doc.Text.Slice(stop.Ranges[0].Start(), stop.Ranges[0].End())
	for i, value := range stop.Choices {
		if value == current {
			a.Dialog.Index = i
			break
		}
	}
}
func (a *App) snippetSelection() (text.Selection, bool) {
	s := a.snippet
	if s == nil || s.Buffer != a.Current() || s.Version != s.Buffer.Doc.Version {
		return text.Selection{}, false
	}
	selections := s.Buffer.Doc.Selections
	if slices.Equal(selections, s.Expected) {
		return s.Primary, true
	}
	// Moving within a placeholder keeps its links. Moving outside it ends the
	// session; arbitrary mouse/key selections never edit an old hidden caret.
	if len(selections) == 1 {
		for _, r := range s.Stops[s.Index].Ranges {
			sel := selections[0]
			if sel.Start() >= r.Start() && sel.End() <= r.End() {
				first := s.Stops[s.Index].Ranges[0]
				delta := first.Start() - r.Start()
				return text.Selection{Anchor: sel.Anchor + delta, Head: sel.Head + delta}, true
			}
		}
	}
	if len(selections) == len(s.Stops[s.Index].Ranges) {
		first := s.Stops[s.Index].Ranges[0]
		primary := selections[0]
		lo, hi := primary.Start()-first.Start(), primary.End()-first.Start()
		if lo >= 0 && hi <= first.End()-first.Start() {
			valid := true
			for i, r := range s.Stops[s.Index].Ranges {
				valid = valid && selections[i].Start()-r.Start() == lo && selections[i].End()-r.Start() == hi
			}
			if valid {
				return primary, true
			}
		}
	}
	return text.Selection{}, false
}
func (a *App) snippetInsert(value string) bool {
	if a.snippet == nil {
		return false
	}
	selection, ok := a.snippetSelection()
	if !ok {
		a.snippet = nil
		return false
	}
	return a.snippetEdit(value, selection)
}
func (a *App) snippetDelete(forward bool) bool {
	if a.snippet == nil {
		return false
	}
	selection, ok := a.snippetSelection()
	if !ok {
		a.snippet = nil
		return false
	}
	if selection.Start() == selection.End() {
		r := a.snippet.Stops[a.snippet.Index].Ranges[0]
		if forward {
			if selection.Head == r.End() {
				a.snippet = nil
				return false
			}
			selection.Head = a.next(a.Current(), selection.Head)
		} else {
			if selection.Head == r.Start() {
				a.snippet = nil
				return false
			}
			selection.Anchor = a.previous(a.Current(), selection.Anchor)
		}
	}
	return a.snippetEdit("", selection)
}
func (a *App) snippetEdit(value string, selection text.Selection) bool {
	s := a.snippet
	if s == nil || a.Readonly || s.Buffer.Transient {
		return false
	}
	stop := s.Stops[s.Index]
	first := stop.Ranges[0]
	lo, hi := selection.Start()-first.Start(), selection.End()-first.Start()
	if lo < 0 || hi > first.End()-first.Start() {
		a.snippet = nil
		return false
	}
	current := s.Buffer.Doc.Text.Slice(first.Start(), first.End())
	values := maps.Clone(s.Values)
	values[stop.Number] = current[:lo] + value + current[hi:]
	result, err := s.Program.Expand(a.ctx, values, s.Expansion.Cache, false)
	if err != nil {
		a.Status = err.Error()
		return true
	}
	edits := []text.Edit{}
	for _, r := range stop.Ranges {
		edits = append(edits, text.Edit{Start: r.Start() + lo, End: r.Start() + hi, Text: value})
	}
	runeValue, size := utf8.DecodeRuneInString(value)
	typing := size == len(value) && size > 0 && !unicode.IsSpace(runeValue)
	coalesce := typing && selection.Start() == selection.End() && s.LastTypeVersion == s.Version && time.Since(s.LastType) < 800*time.Millisecond
	if a.applySnippet(s, result, values, edits, lo+len(value), lo+len(value), coalesce) {
		s.Edited = true
		if typing {
			s.LastType = time.Now()
			s.LastTypeVersion = s.Version
		} else {
			s.LastTypeVersion = 0
		}
	}
	return true
}
func mergeSnippetEdits(edits []text.Edit) ([]text.Edit, error) {
	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].Start == edits[j].Start {
			return edits[i].End < edits[j].End
		}
		return edits[i].Start < edits[j].Start
	})
	merged := []text.Edit{}
	for _, edit := range edits {
		if len(merged) > 0 {
			last := &merged[len(merged)-1]
			if edit.Start == last.Start && last.Start == last.End {
				last.Text += edit.Text
				last.End = edit.End
				continue
			}
			if edit.Start < last.End {
				return nil, fmt.Errorf("snippet ranges overlap")
			}
		}
		merged = append(merged, edit)
	}
	return merged, nil
}
func (a *App) applySnippet(s *snippetState, result snippets.Expansion, values map[int]string, edits []text.Edit, anchor, head int, coalesce bool) bool {
	edits, err := mergeSnippetEdits(edits)
	if err != nil {
		a.Status = err.Error()
		return false
	}
	// Verify the precise edits against the rendered model before touching text.
	// This also protects adjacent empty linked placeholders and transform ranges.
	candidate := text.New(s.Expansion.Text)
	for i := len(edits) - 1; i >= 0; i-- {
		edit := edits[i]
		lo, hi := edit.Start-s.Base, edit.End-s.Base
		if lo < 0 || hi > candidate.Len() {
			a.Status = "Snippet range is stale"
			a.snippet = nil
			return false
		}
		candidate = candidate.Replace(lo, hi, edit.Text, uint64(i+1))
	}
	if candidate.String() != result.Text {
		a.Status = "Snippet edit could not preserve linked ranges"
		return false
	}
	d := s.Buffer.Doc
	version := d.Version
	if err = d.Apply(version, edits); err != nil {
		a.Status = err.Error()
		return false
	}
	if coalesce && d.Version != version {
		d.Coalesce()
	}
	number := s.Stops[s.Index].Number
	s.Version = d.Version
	s.Expansion = result
	s.Values = values
	s.Stops = snippetStops(result, s.Base)
	for i, stop := range s.Stops {
		if stop.Number == number {
			s.Index = i
			break
		}
	}
	ranges := s.Stops[s.Index].Ranges
	d.Selections = nil
	for _, r := range ranges {
		lo := r.Start() + min(anchor, r.End()-r.Start())
		hi := r.Start() + min(head, r.End()-r.Start())
		d.Selections = append(d.Selections, text.Selection{Anchor: lo, Head: hi})
	}
	s.Primary = d.Selections[0]
	a.normalizeSelections()
	s.Expected = append([]text.Selection(nil), d.Selections...)
	a.typingDoc = nil
	if d.Version != version {
		a.changed()
	} else {
		a.reveal()
	}
	return true
}
func (a *App) rebaseSnippet() {
	s := a.snippet
	if s != nil && (s.Buffer != a.Current() || s.Version != a.Current().Doc.Version) {
		a.snippet = nil
	}
}
