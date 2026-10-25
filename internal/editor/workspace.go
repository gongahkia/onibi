package editor

import (
	"context"
	"errors"
	"fmt"
	"github.com/gongahkia/shack/internal/project"
	"github.com/gongahkia/shack/internal/text"
	"path/filepath"
	"sort"
)

const workspaceBytes = 8 << 20
const workspaceMatches = 2000

type workspaceTarget struct {
	Buffer   *Buffer
	Document *text.Document
	Version  uint64
	Edits    []text.Edit
	New      bool
}
type workspaceBatch struct {
	Buffers  []*Buffer
	Versions []uint64
	Undone   bool
}
type jump struct {
	Buffer *Buffer
	Offset int
}

func (p *preview) bufferAt(i int) *Buffer {
	if p.Targets != nil {
		return p.Targets[i].Buffer
	}
	return p.Buffer
}
func (p *preview) documentAt(i int) *text.Document {
	if p.Targets != nil {
		return p.Targets[i].Document
	}
	return p.Buffer.Doc
}
func (a *App) workspaceCommands() {
	a.Commands = append(a.Commands,
		command{"Find in project", "", func() { a.projectPrompt(false) }}, command{"Preview replace in project", "", func() { a.projectPrompt(true) }},
		command{"Refresh project files", "", a.indexFiles}, command{"Project index status", "", func() { a.Status = a.indexMode }},
		command{"Undo last workspace edit", "", func() { a.undoWorkspace(false) }}, command{"Redo last workspace edit", "", func() { a.undoWorkspace(true) }},
		command{"Jump back", "", func() { a.jumpBy(-1) }}, command{"Jump forward", "", func() { a.jumpBy(1) }})
}
func (a *App) rememberJump() {
	b := a.Current()
	j := jump{b, b.Doc.Selections[0].Head}
	if len(a.jumps) > 0 && a.jumpIndex < len(a.jumps) && a.jumps[a.jumpIndex] == j {
		return
	}
	a.jumps = append(a.jumps[:min(a.jumpIndex+1, len(a.jumps))], j)
	if len(a.jumps) > 100 {
		a.jumps = a.jumps[len(a.jumps)-100:]
	}
	a.jumpIndex = len(a.jumps) - 1
}
func (a *App) jumpTo(b *Buffer, offset int) {
	a.rememberJump()
	a.finishJump(b, offset)
}
func (a *App) finishJump(b *Buffer, offset int) {
	for i, open := range a.Buffers {
		if open == b {
			a.Active = i
			break
		}
	}
	offset = min(max(0, offset), b.Doc.Text.Len())
	for !b.Doc.Text.Boundary(offset) {
		offset--
	}
	b.Doc.Selections = []text.Selection{{Anchor: offset, Head: offset}}
	a.reveal()
	a.schedule()
	a.rememberJump()
}
func (a *App) jumpBy(delta int) {
	if len(a.jumps) == 0 {
		a.Status = "No navigation history"
		return
	}
	a.jumpIndex = max(0, min(len(a.jumps)-1, a.jumpIndex+delta))
	j := a.jumps[a.jumpIndex]
	found := false
	for i, b := range a.Buffers {
		if b == j.Buffer {
			a.Active = i
			found = true
			break
		}
	}
	if !found {
		a.Status = "That buffer was closed"
		return
	}
	p := min(j.Offset, j.Buffer.Doc.Text.Len())
	for !j.Buffer.Doc.Text.Boundary(p) {
		p--
	}
	j.Buffer.Doc.Selections = []text.Selection{{Anchor: p, Head: p}}
	a.reveal()
	a.schedule()
}
func (a *App) projectPrompt(replace bool) {
	title := "Find in project"
	if replace {
		title = "Replace in project: find"
	}
	a.prompt(title+" ("+a.searchMode()+")", a.Query, func(q string) {
		a.Query = q
		if replace {
			a.prompt("Replace with · review every file before applying", "", func(r string) { a.searchProject(q, r, true) })
		} else {
			a.searchProject(q, "", false)
		}
	})
}
func (a *App) searchProject(query, replacement string, replace bool) {
	if query == "" {
		return
	}
	if a.workspaceCancel != nil {
		a.workspaceCancel()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.workspaceCancel = cancel
	a.workspaceSeq++
	seq := a.workspaceSeq
	root, regex := a.Root, a.Regex
	limit := min(1<<20, a.Config.MaxFileMiB<<20)
	opened := map[string]*workspaceTarget{}
	for _, b := range a.Buffers {
		if b.File.Path != "" && !b.Transient && project.Inside(root, b.File.Path) {
			opened[b.File.Path] = &workspaceTarget{Buffer: b, Document: b.Doc.Clone(), Version: b.Doc.Version}
		}
	}
	a.Status = "Searching project · Escape cancels"
	go func() {
		files, mode, err := project.Files(ctx, root)
		targets := []*workspaceTarget{}
		count, total, skipped := 0, 0, 0
		seen := map[string]bool{}
		paths := []string{}
		for _, f := range files {
			paths = append(paths, filepath.Join(root, f))
		}
		for path := range opened {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		if err == nil {
			for _, path := range paths {
				if canonical, e := filepath.EvalSymlinks(path); e == nil {
					path = canonical
				}
				if ctx.Err() != nil {
					err = ctx.Err()
					break
				}
				if seen[path] {
					continue
				}
				seen[path] = true
				if !project.Inside(root, path) {
					skipped++
					continue
				}
				target := opened[path]
				if target == nil {
					f, d, e := OpenFile(path, int64(limit))
					if e != nil || !f.exists {
						skipped++
						continue
					}
					target = &workspaceTarget{Buffer: &Buffer{File: f, Doc: d, Newline: detectNewline(d.Text)}, Document: d.Clone(), New: true}
				}
				if target.Document.Text.Len() > limit {
					skipped++
					continue
				}
				source := target.Document.Text.String()
				var edits []text.Edit
				if replace {
					edits, err = text.Replacements(ctx, source, query, replacement, regex)
				} else {
					edits, err = text.Search(ctx, source, query, regex)
				}
				if err != nil {
					break
				}
				if len(edits) == 0 {
					continue
				}
				count += len(edits)
				total += len(source)
				for _, e := range edits {
					total += len(e.Text)
				}
				if count > workspaceMatches || total > workspaceBytes {
					err = errors.New("project results exceed 2,000 matches or 8 MiB; narrow your query")
					break
				}
				target.Edits = edits
				targets = append(targets, target)
			}
		}
		a.post(func() {
			if seq != a.workspaceSeq {
				return
			}
			a.indexMode = mode
			if err != nil {
				a.Status = err.Error()
				return
			}
			for _, t := range targets {
				if !t.New && t.Buffer.Doc.Version != t.Version {
					a.Status = "Project search became stale; run it again"
					return
				}
			}
			a.Status = fmt.Sprintf("%d matches in %d files; %d files skipped (binary, large or unreadable)", count, len(targets), skipped)
			if replace {
				a.showWorkspacePreview("Replace in project", targets)
				return
			}
			items := []choice{}
			for _, target := range targets {
				for _, e := range target.Edits {
					t, e := target, e
					line := t.Document.Text.LineAt(e.Start)
					rel, _ := filepath.Rel(root, t.Buffer.File.Path)
					label := fmt.Sprintf("%s:%d  %s", rel, line+1, displayExcerpt(t.Document.Text.Slice(t.Document.Text.LineStart(line), min(t.Document.Text.LineEnd(line), t.Document.Text.LineStart(line)+160))))
					items = append(items, choice{label, func() {
						a.rememberJump()
						if err := a.Open(t.Buffer.File.Path); err != nil {
							a.Status = err.Error()
							return
						}
						b := a.Current()
						if !b.Doc.Text.Same(t.Document.Text) && b.Doc.Text.String() != t.Document.Text.String() {
							a.Status = "Search result changed; search again"
							return
						}
						a.finishJump(b, e.Start)
					}})
				}
			}
			if len(items) > 0 {
				a.list("Project matches · type to filter", items)
			}
		})
	}()
}
func (a *App) showWorkspacePreview(title string, targets []*workspaceTarget) {
	p := &preview{Title: title, Targets: []*workspaceTarget{}}
	for _, t := range targets {
		for _, e := range t.Edits {
			p.Edits = append(p.Edits, e)
			p.Targets = append(p.Targets, t)
			p.Checked = append(p.Checked, true)
		}
	}
	if len(p.Edits) == 0 {
		a.Status = "No changes to preview"
		return
	}
	a.Dialog = nil
	a.Preview = p
}
func (f *File) Unchanged(limit int64) error {
	if f.Path == "" {
		return nil
	}
	now, _, err := OpenFile(f.Path, limit)
	if err != nil {
		return err
	}
	if now.Path != f.Path || now.exists != f.exists || now.hash != f.hash {
		return fmt.Errorf("%s changed on disk; reload or save your edits elsewhere", filepath.Base(f.Path))
	}
	return nil
}
func (a *App) applyWorkspacePreview() {
	p := a.Preview
	if a.Readonly {
		a.Status = "Read-only mode"
		return
	}
	targets := []*workspaceTarget{}
	edits := map[*workspaceTarget][]text.Edit{}
	for i, e := range p.Edits {
		if p.Checked[i] {
			t := p.Targets[i]
			if _, ok := edits[t]; !ok {
				targets = append(targets, t)
			}
			edits[t] = append(edits[t], e)
		}
	}
	staged := map[*workspaceTarget]*text.Document{}
	// Validate and stage everything before publishing any new document or buffer.
	for _, t := range targets {
		found := t.New
		for _, b := range a.Buffers {
			found = found || b == t.Buffer
		}
		if !found {
			a.Status = "A preview target was closed; regenerate the preview"
			return
		}
		if t.Buffer.Doc.Version != t.Version {
			a.Status = "Workspace preview is stale; nothing applied"
			return
		}
		for _, b := range a.Buffers {
			if t.New && b.File.Path == t.Buffer.File.Path {
				a.Status = "A preview target was opened; regenerate the preview"
				return
			}
		}
		if err := t.Buffer.File.Unchanged(int64(a.Config.MaxFileMiB) << 20); err != nil {
			a.Status = err.Error()
			return
		}
		d := t.Buffer.Doc.Clone()
		if err := d.Apply(t.Version, edits[t]); err != nil {
			a.Status = err.Error()
			return
		}
		if d.Text.Len() > a.Config.MaxFileMiB<<20 {
			a.Status = "Workspace edit exceeds file limit"
			return
		}
		staged[t] = d
	}
	batch := &workspaceBatch{}
	old := a.Active
	for _, t := range targets {
		d := staged[t]
		if d.Version == t.Version {
			continue
		}
		t.Buffer.Doc = d
		if t.New {
			a.Buffers = append(a.Buffers, t.Buffer)
		}
		for i, b := range a.Buffers {
			if b == t.Buffer {
				a.Active = i
				break
			}
		}
		a.rebaseOtherPanes(t.Buffer)
		t.Buffer.SyntaxReady = false
		t.Buffer.Diagnostics = nil
		batch.Buffers = append(batch.Buffers, t.Buffer)
		batch.Versions = append(batch.Versions, d.Version)
	}
	a.Active = old
	a.batch = batch
	a.typingDoc = nil
	a.Preview = nil
	a.Matches = nil
	a.scheduleRecovery()
	a.schedule()
	a.reveal()
	a.Status = fmt.Sprintf("Changed %d buffers; Save all writes files · Undo last workspace edit reverts the group", len(batch.Buffers))
}
func (a *App) rebaseOtherPanes(b *Buffer) {
	for i := range a.panes {
		p := &a.panes[i]
		if p.Buffer != b || p.Version == b.Doc.Version {
			continue
		}
		for _, edits := range b.Doc.LastChanges {
			for j, s := range p.Selections {
				s.Anchor = text.Rebase(s.Anchor, edits)
				s.Head = text.Rebase(s.Head, edits)
				p.Selections[j] = s
			}
		}
		p.Version = b.Doc.Version
	}
}
func (a *App) undoWorkspace(redo bool) {
	g := a.batch
	if a.Readonly || g == nil || len(g.Buffers) == 0 || g.Undone != redo {
		a.Status = "No workspace edit available to undo/redo"
		return
	}
	for i, b := range g.Buffers {
		found := false
		for _, open := range a.Buffers {
			found = found || open == b
		}
		if !found || b.Doc.Version != g.Versions[i] {
			a.Status = "A target was edited or closed; use its buffer undo history"
			return
		}
	}
	for i, b := range g.Buffers {
		if redo {
			b.Doc.Redo()
		} else {
			b.Doc.Undo()
		}
		g.Versions[i] = b.Doc.Version
		b.SyntaxReady = false
		b.Diagnostics = nil
		a.rebaseOtherPanes(b)
	}
	g.Undone = !redo
	a.Matches = nil
	a.scheduleRecovery()
	a.schedule()
	a.reveal()
	a.Status = "Workspace undo/redo applied to all targets"
}

// Resolve prospective workspace targets without opening them in the UI.
func (a *App) targetForPath(path string) (*workspaceTarget, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if p, e := filepath.EvalSymlinks(path); e == nil {
		path = p
	}
	if !project.Inside(a.Root, path) {
		return nil, errors.New("workspace edit targets a file outside the project")
	}
	for _, b := range a.Buffers {
		if b.File.Path == path {
			return &workspaceTarget{Buffer: b, Document: b.Doc.Clone(), Version: b.Doc.Version}, nil
		}
	}
	f, d, err := OpenFile(path, 1<<20)
	if err != nil {
		return nil, err
	}
	if !f.exists {
		return nil, errors.New("workspace edit targets a missing file")
	}
	return &workspaceTarget{Buffer: &Buffer{File: f, Doc: d, Newline: detectNewline(d.Text)}, Document: d.Clone(), New: true}, nil
}
