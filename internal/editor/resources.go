package editor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/gongahkia/shack/internal/fileops"
)

type resourcePreparation struct {
	Cancel context.CancelFunc
	Finish func(bool, string)
}

func snapshotResource(b *Buffer) resourceSnapshot {
	return resourceSnapshot{Buffer: b, File: *b.File, Doc: b.Doc.Clone(), Title: b.Title, Newline: b.Newline, Transient: b.Transient}
}

func (a *App) prepareResourcePreview(title string, raw json.RawMessage, finish func(bool, string), ready func(*preview)) *resourcePreparation {
	if a.Readonly {
		a.Status = "Read-only mode"
		finish(false, a.Status)
		return nil
	}
	if a.Preview != nil && a.Preview.CommitCancel != nil {
		finish(false, "A filesystem operation is still running")
		return nil
	}
	a.cancelPreview("Preview replaced by another edit")
	ctx, cancel := context.WithCancel(a.ctx)
	job := &resourcePreparation{cancel, finish}
	a.resourcePreparation = job
	sources := []resourceSnapshot{}
	for _, b := range a.Buffers {
		if !b.Transient && b.File.Path != "" {
			sources = append(sources, snapshotResource(b))
		}
	}
	root := a.Root
	a.Dialog = nil
	a.Status = "Preparing file operations and text edits · Escape cancels"
	go func() {
		defer cancel()
		plan, err := prepareResources(ctx, root, raw, sources)
		a.post(func() {
			if a.resourcePreparation != job {
				return
			}
			a.resourcePreparation = nil
			if err == nil {
				err = a.validateResourceBuffers(plan, nil, false, true)
				for _, item := range plan.Buffers {
					if item.After.Doc.Text.Len() > a.Config.MaxFileMiB<<20 {
						err = errors.New("workspace edit exceeds configured file limit")
						break
					}
				}
			}
			if err != nil {
				a.Status = err.Error()
				finish(false, a.Status)
				return
			}
			suffix := " · text edits"
			if plan.HasFiles {
				suffix = " · writes file operations to disk"
			}
			a.showWorkspacePreview(title+suffix, plan.Rows)
			p := a.Preview
			if p == nil {
				finish(true, "")
				return
			}
			p.Resources = plan
			p.Labels = plan.Labels
			p.Strict = true
			p.Finish = finish
			if ready != nil {
				ready(p)
			}
		})
	}()
	return job
}

func (a *App) validateResourceBuffers(plan *resourcePlan, versions []uint64, after, initial bool) error {
	involved := map[*Buffer]bool{}
	paths := map[string]bool{}
	for path, n := range plan.Before {
		if final := plan.After[path]; n.Kind != final.Kind || n.Data != final.Data {
			paths[path] = true
		}
	}
	for i, item := range plan.Buffers {
		s := item.Before
		if after {
			s = item.After
		}
		b := s.Buffer
		involved[b] = true
		if item.Before.File.Path != "" {
			paths[item.Before.File.Path] = true
		}
		if item.After.File.Path != "" {
			paths[item.After.File.Path] = true
		}
		found := false
		for _, open := range a.Buffers {
			found = found || open == b
		}
		if initial && item.New {
			if found {
				return errors.New("a preview target was opened")
			}
			continue
		}
		version := s.Doc.Version
		if versions != nil {
			version = versions[i]
		}
		if !found || b.Doc.Version != version || b.File.Path != s.File.Path || b.Transient != s.Transient {
			return errors.New("a workspace target was edited, moved or closed; regenerate the operation")
		}
		if initial && *b.File != s.File {
			return errors.New("a workspace target was saved; regenerate the preview")
		}
	}
	for _, b := range a.Buffers {
		affected := paths[b.File.Path]
		for path := range paths {
			if plan.Before[path].Kind == fileops.Directory && fileops.Inside(path, b.File.Path) {
				affected = true
			}
		}
		if !involved[b] && affected {
			return errors.New("a workspace target was opened separately; regenerate the operation")
		}
	}
	return nil
}

func (a *App) applyResourcePreview() {
	p := a.Preview
	if p.CommitCancel != nil {
		return
	}
	if a.Readonly {
		a.Status = "Read-only mode"
		return
	}
	if p.Valid != nil {
		if err := p.Valid(); err != nil {
			a.Status = err.Error()
			a.cancelPreview(a.Status)
			return
		}
	}
	if err := a.validateResourceBuffers(p.Resources, nil, false, true); err != nil {
		a.Status = err.Error()
		return
	}
	for _, checked := range p.Checked {
		if !checked {
			a.Status = "Apply the whole action or cancel"
			return
		}
	}
	a.commitResources(p, p.Resources.Before, p.Resources.After, func() {
		versions := a.publishResources(p.Resources, true)
		a.batch = &workspaceBatch{Resources: p.Resources, Versions: versions}
		a.Status = "File operations applied on disk; text edits may remain unsaved · Undo last workspace edit reverts the group"
		if !p.Resources.HasFiles {
			a.batch.Resources = nil
			for _, item := range p.Resources.Buffers {
				a.batch.Buffers = append(a.batch.Buffers, item.Before.Buffer)
			}
			a.Status = "Workspace text edits applied; Save all writes files"
		}
	})
}

func (a *App) commitResources(p *preview, before, after map[string]fileops.Node, publish func(), savedNew ...string) {
	ctx, cancel := context.WithCancel(a.ctx)
	p.CommitCancel = cancel
	result := make(chan func(), 1)
	a.resourceResult = result
	a.resourceCommitCancel = cancel
	a.Status = "Applying file operations · Escape requests cancellation"
	go func() {
		defer cancel()
		var err error
		for _, path := range savedNew {
			var n fileops.Node
			n, err = fileops.SavedFile(p.Resources.Root, path, before[path].Data)
			if err != nil {
				break
			}
			before[path] = n
		}
		if err == nil {
			err = fileops.Commit(ctx, p.Resources.Root, before, after)
		}
		result <- func() {
			p.CommitCancel = nil
			a.resourceCommitCancel = nil
			a.resourceResult = nil
			a.Menu = nil
			if err != nil {
				a.Status = err.Error()
				a.Preview = nil
				if p.Finish != nil {
					p.Finish(false, a.Status)
				}
				return
			}
			publish()
			a.Preview = nil
			if p.Finish != nil {
				p.Finish(true, "")
			}
		}
		a.post(func() {
			select {
			case finish := <-result:
				finish()
			default:
			}
		})
	}()
}

func (a *App) publishResources(plan *resourcePlan, after bool) []uint64 {
	versions := make([]uint64, len(plan.Buffers))
	for i, item := range plan.Buffers {
		s := item.Before
		changes := item.Reverse
		if after {
			s = item.After
			changes = item.Forward
		}
		b := s.Buffer
		found := false
		for _, open := range a.Buffers {
			found = found || open == b
		}
		version := uint64(0)
		if found {
			version = b.Doc.Version
			if b.File.Path != s.File.Path || b.Transient != s.Transient {
				a.forgetLanguage(b)
				for _, point := range a.breakpoints[b.File.Path] {
					if point.Buffer == b {
						point.Buffer = nil
						point.Verified = false
					}
				}
				b.LanguageKey = ""
				b.LanguageEpoch = 0
			}
		} else {
			a.Buffers = append(a.Buffers, b)
		}
		d := s.Doc.Clone()
		d.Version = max(d.Version, version+1)
		d.LastChanges = changes
		file := s.File
		b.Doc = d
		b.File = &file
		b.Title = s.Title
		b.Newline = s.Newline
		b.Transient = s.Transient
		b.SyntaxReady = false
		b.DiskConflict = false
		b.Diagnostics = nil
		b.Synced = false
		b.Syncing = false
		versions[i] = d.Version
		a.rebaseOtherPanes(b)
		if !b.Transient {
			a.adoptDebugBreakpoints(b)
		}
	}
	a.typingDoc = nil
	a.snippet = nil
	a.Matches = nil
	a.scheduleRecovery()
	a.schedule()
	a.reveal()
	if !a.resourceClosing {
		a.indexFiles()
	}
	return versions
}

func (a *App) undoResources(redo bool) {
	g := a.batch
	if a.Readonly || g == nil || g.Resources == nil || g.Undone != redo {
		a.Status = "No resource operation available to undo/redo"
		return
	}
	if a.Preview != nil || a.Dialog != nil || a.resourcePreparation != nil {
		a.Status = "Close the current preview/dialog first"
		return
	}
	plan := g.Resources
	if err := a.validateResourceBuffers(plan, g.Versions, !g.Undone, false); err != nil {
		a.Status = err.Error()
		return
	}
	before, after := plan.After, plan.Before
	if redo {
		before, after = plan.Before, plan.After
	}
	expected := make(map[string]fileops.Node, len(before))
	savedNew := []string{}
	for path, n := range before {
		expected[path] = n
	}
	// Save All after an accepted action is allowed. Only recognize saves whose
	// bytes still match both the guarded buffer version and its current file hash.
	for _, item := range plan.Buffers {
		b := item.Before.Buffer
		n, ok := expected[b.File.Path]
		if !ok || n.Kind == fileops.Directory || !b.File.exists {
			continue
		}
		if n.Kind == fileops.File && sha256.Sum256([]byte(n.Data)) == b.File.hash {
			continue
		}
		data := b.Doc.Text.String()
		if sha256.Sum256([]byte(data)) != b.File.hash {
			a.Status = "Workspace save state changed; cannot undo file operations"
			return
		}
		n.Data = data
		if n.Kind == fileops.Missing {
			savedNew = append(savedNew, b.File.Path)
			parent := filepath.Dir(b.File.Path)
			if directory, ok := expected[parent]; ok && directory.Kind == fileops.Directory && directory.Listed {
				directory.Children = append(append([]string(nil), directory.Children...), filepath.Base(b.File.Path))
				slices.Sort(directory.Children)
				expected[parent] = directory
			}
		}
		expected[b.File.Path] = n
	}
	a.showWorkspacePreview("Undo/redo file operations", plan.Rows)
	p := a.Preview
	if p == nil {
		p = &preview{}
		a.Preview = p
	}
	p.Resources = plan
	p.Labels = plan.Labels
	p.Strict = true
	a.commitResources(p, expected, after, func() {
		g.Versions = a.publishResources(plan, redo)
		g.Undone = !redo
		a.Status = fmt.Sprintf("Workspace %s restored file operations and buffers", map[bool]string{true: "redo", false: "undo"}[redo])
	}, savedNew...)
}

func (a *App) closeResourceWork() {
	a.resourceClosing = true
	if a.resourcePreparation != nil {
		a.cancelPreview("Editor is closing")
	}
	if a.resourceCommitCancel != nil {
		a.resourceCommitCancel()
	}
	if result := a.resourceResult; result != nil {
		select {
		case finish := <-result:
			finish()
		case <-time.After(10 * time.Second):
		}
	}
}
