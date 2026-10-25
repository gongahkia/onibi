package editor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gongahkia/shack/internal/gitmerge"
	"github.com/gongahkia/shack/internal/lsp"
	"github.com/gongahkia/shack/internal/text"
)

type mergeFile struct {
	Repo  gitmerge.Repository
	Entry gitmerge.Entry
	Path  string
}

func (a *App) mergeCommands() {
	a.Commands = append(a.Commands,
		command{"Git merge conflicts", "", a.mergeConflicts},
		command{"Merge file actions", "", a.mergeFileActions},
		command{"Resolve conflict at cursor", "", func() {
			a.mergeHunks(func(b *Buffer, source string, hunks []gitmerge.Hunk) {
				if len(hunks) == 0 {
					a.Status = "No conflict markers in this buffer"
					return
				}
				caret := b.Doc.Selections[0].Head
				i := sort.Search(len(hunks), func(i int) bool { return hunks[i].End > caret })
				if i == len(hunks) {
					i = 0
				}
				a.mergeHunkActions(b, source, hunks[i])
			})
		}},
		command{"Next merge conflict", "", func() { a.moveMergeConflict(1) }}, command{"Previous merge conflict", "", func() { a.moveMergeConflict(-1) }},
		command{"Inspect merge base", "", func() { a.inspectMergeStage(1) }}, command{"Inspect merge ours", "", func() { a.inspectMergeStage(2) }}, command{"Inspect merge theirs", "", func() { a.inspectMergeStage(3) }},
		command{"Stage saved merge resolution", "", a.stageMergeResolution})
}

func (a *App) mergeJob(title string, work func(context.Context) (func(), error)) {
	if a.mergeWriting {
		a.Status = "Git index update is running"
		return
	}
	if a.mergeCancel != nil {
		a.mergeCancel()
	}
	a.mergeSequence++
	sequence := a.mergeSequence
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	a.mergeCancel = cancel
	a.Status = title + " · Escape cancels"
	go func() {
		defer cancel()
		done, err := work(ctx)
		a.post(func() {
			if sequence != a.mergeSequence {
				return
			}
			a.mergeCancel = nil
			a.mergeWriting = false
			if err != nil {
				a.Status = err.Error()
				return
			}
			if done != nil {
				done()
			}
		})
	}()
}
func (a *App) cancelMerge() {
	if a.mergeCancel != nil {
		a.mergeCancel()
		if a.mergeWriting {
			a.Status = "Git cancellation requested; check Git status after it finishes"
			return
		}
		a.mergeCancel = nil
		a.mergeSequence++
		a.Status = "Merge inspection cancelled"
	}
}
func (a *App) mergeConflicts() {
	root := a.Root
	a.mergeJob("Finding Git conflicts", func(ctx context.Context) (func(), error) {
		repo, err := gitmerge.Open(ctx, root)
		if err != nil {
			return nil, err
		}
		entries, err := repo.Conflicts(ctx)
		if err != nil {
			return nil, err
		}
		return func() {
			if a.Dialog != nil || a.Preview != nil {
				return
			}
			items := []choice{}
			for _, entry := range entries {
				label := entry.Path
				if entry.Stages[2].OID == "" {
					label += " · absent in ours"
				}
				if entry.Stages[3].OID == "" {
					label += " · absent in theirs"
				}
				items = append(items, choice{label, func() { a.openMergeFile(repo, entry) }})
			}
			if len(items) == 0 {
				a.Status = "No unmerged Git paths in this project"
				return
			}
			a.list("Merge conflicts · choose a file", items)
		}, nil
	})
}
func (a *App) openMergeFile(repo gitmerge.Repository, entry gitmerge.Entry) {
	a.mergeJob("Opening conflict", func(ctx context.Context) (func(), error) {
		if err := repo.Current(ctx, entry); err != nil {
			return nil, err
		}
		path, err := repo.Path(entry)
		if err != nil {
			return nil, err
		}
		return func() {
			if a.Dialog != nil || a.Preview != nil {
				return
			}
			a.mergeFile = &mergeFile{repo, entry, path}
			if err := a.Open(path); err != nil {
				a.Status = err.Error()
				return
			}
			a.Current().Merge = a.mergeFile
			a.mergeFileActions()
		}, nil
	})
}
func (a *App) currentMergeFile(continueAction func()) *mergeFile {
	b := a.Current()
	if m := b.Merge; m != nil && (b.Transient || b.File.Path == m.Path) {
		return m
	}
	if m := a.mergeFile; m != nil && b.File.Path == m.Path {
		b.Merge = m
		return m
	}
	if b.File.Path == "" || b.Transient {
		a.Status = "Choose a file from Git merge conflicts first"
		return nil
	}
	root, path := a.Root, b.File.Path
	a.mergeJob("Looking up Git conflict", func(ctx context.Context) (func(), error) {
		repo, err := gitmerge.Open(ctx, root)
		if err != nil {
			return nil, err
		}
		entries, err := repo.Conflicts(ctx)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			candidate, e := repo.Path(entry)
			if e != nil {
				return nil, e
			}
			if candidate == path {
				return func() {
					if a.Current() != b || b.File.Path != path || a.Dialog != nil || a.Preview != nil {
						a.Status = "Conflict lookup became stale"
						return
					}
					m := &mergeFile{repo, entry, path}
					a.mergeFile = m
					b.Merge = m
					continueAction()
				}, nil
			}
		}
		return nil, errors.New("this file has no unmerged Git index stages")
	})
	return nil
}

func (a *App) mergeHunks(done func(*Buffer, string, []gitmerge.Hunk)) {
	a.inspectMergeHunks(false, done)
}
func (a *App) inspectMergeHunks(tolerateMalformed bool, done func(*Buffer, string, []gitmerge.Hunk)) {
	b := a.Current()
	if b.Transient {
		a.Status = "Return to the editable conflict file first"
		return
	}
	if b.Doc.Text.Len() > gitmerge.MaxBytes {
		a.Status = "Conflict inspection is limited to 8 MiB"
		return
	}
	snapshot, version := b.Doc.Text, b.Doc.Version
	a.mergeJob("Inspecting conflict markers", func(ctx context.Context) (func(), error) {
		source := snapshot.String()
		hunks, parseErr := gitmerge.Parse(source)
		if parseErr != nil && !tolerateMalformed {
			return nil, parseErr
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return func() {
			if b != a.Current() || b.Doc.Version != version || a.Dialog != nil || a.Preview != nil {
				a.Status = "Conflict inspection became stale"
				return
			}
			if parseErr != nil {
				a.Status = parseErr.Error()
			}
			done(b, source, hunks)
		}, nil
	})
}
func (a *App) moveMergeConflict(delta int) {
	a.mergeHunks(func(b *Buffer, source string, hunks []gitmerge.Hunk) {
		if len(hunks) == 0 {
			a.Status = "No conflict markers in this buffer"
			return
		}
		caret := b.Doc.Selections[0].Head
		index := 0
		if delta > 0 {
			index = sort.Search(len(hunks), func(i int) bool { return hunks[i].Start > caret })
			index %= len(hunks)
		} else {
			index = sort.Search(len(hunks), func(i int) bool { return hunks[i].Start >= caret }) - 1
			if index < 0 {
				index = len(hunks) - 1
			}
		}
		a.jumpTo(b, hunks[index].Start)
		a.Status = fmt.Sprintf("Conflict %d/%d · Resolve conflict at cursor", index+1, len(hunks))
	})
}
func (a *App) mergeHunkActions(b *Buffer, source string, h gitmerge.Hunk) {
	version := b.Doc.Version
	items := []choice{}
	for _, side := range []string{"ours", "theirs", "both", "base"} {
		if side == "base" && !h.HasBase {
			continue
		}
		label := "Preview " + side + " for this hunk"
		if side == "both" {
			label = "Preview both: ours then theirs"
		}
		items = append(items, choice{label, func() {
			if b.Doc.Version != version {
				a.Status = "Conflict changed; inspect again"
				return
			}
			a.previewMergeResolution(b, source, version, []gitmerge.Hunk{h}, side)
		}})
	}
	items = append(items, choice{"Inspect ours / base / theirs in this hunk", func() {
		ours, _ := h.Resolve(source, "ours")
		theirs, _ := h.Resolve(source, "theirs")
		base := "(No base section in these markers)\n"
		if h.HasBase {
			base, _ = h.Resolve(source, "base")
		}
		a.report("Conflict hunk: "+bufferName(b), "OURS · "+h.OursLabel+"\n"+ours+"\nBASE · "+h.BaseLabel+"\n"+base+"\nTHEIRS · "+h.TheirsLabel+"\n"+theirs)
		a.Current().Merge = b.Merge
	}}, choice{"Edit manually at this hunk", func() {
		if b.Doc.Version == version {
			a.jumpTo(b, h.Start)
			a.Status = "Edit the final text, remove markers, save, then stage the resolution"
		}
	}})
	a.list(fmt.Sprintf("Conflict at line %d · every choice is previewed", h.Line+1), items)
}
func (a *App) previewMergeResolution(b *Buffer, source string, version uint64, hunks []gitmerge.Hunk, side string) {
	if a.Readonly {
		a.Status = "Read-only mode"
		return
	}
	edits := make([]text.Edit, 0, len(hunks))
	for _, h := range hunks {
		value, err := h.Resolve(source, side)
		if err != nil {
			a.Status = err.Error()
			return
		}
		edits = append(edits, text.Edit{Start: h.Start, End: h.End, Text: value})
	}
	a.showPreview("Resolve conflict: "+side, b, version, edits)
}
func (a *App) mergeFileActions() {
	m := a.currentMergeFile(a.mergeFileActions)
	if m == nil {
		return
	}
	// A report may be active after inspecting a stage or accepting deletion.
	if a.Current().File.Path != m.Path {
		if err := a.Open(m.Path); err != nil {
			a.Status = err.Error()
			return
		}
	}
	a.inspectMergeHunks(true, func(b *Buffer, source string, hunks []gitmerge.Hunk) {
		version := b.Doc.Version
		items := []choice{}
		for i, h := range hunks {
			items = append(items, choice{fmt.Sprintf("Resolve hunk %d · line %d", i+1, h.Line+1), func() {
				if b.Doc.Version != version {
					a.Status = "Conflict changed; inspect again"
					return
				}
				a.jumpTo(b, h.Start)
				a.mergeHunkActions(b, source, h)
			}})
		}
		if len(hunks) > 0 {
			for _, side := range []string{"ours", "theirs", "both"} {
				items = append(items, choice{"Preview " + side + " for every marked hunk", func() { a.previewMergeResolution(b, source, version, hunks, side) }})
			}
		}
		for _, stage := range []int{1, 2, 3} {
			label := map[int]string{1: "base", 2: "ours (Git stage 2)", 3: "theirs (Git stage 3)"}[stage]
			if m.Entry.Stages[stage].OID != "" {
				items = append(items, choice{"Inspect entire " + label, func() { a.inspectMergeStage(stage) }}, choice{"Preview replacing entire file with " + label, func() { a.replaceMergeStage(stage) }})
			}
		}
		if m.Entry.Stages[2].OID == "" || m.Entry.Stages[3].OID == "" {
			items = append(items, choice{"Preview resolving with deletion (absent side)", a.deleteMergeFile})
		}
		items = append(items, choice{"Stage saved merge resolution", a.stageMergeResolution}, choice{"Return to editing", func() { a.Status = "Resolve remaining hunks, save, then stage; Git commit remains explicit" }})
		a.list(fmt.Sprintf("%s · %d marked conflicts", m.Entry.Path, len(hunks)), items)
	})
}

func (a *App) inspectMergeStage(stage int) {
	m := a.currentMergeFile(func() { a.inspectMergeStage(stage) })
	if m == nil {
		return
	}
	a.mergeJob("Reading Git merge stage", func(ctx context.Context) (func(), error) {
		source, err := m.Repo.Read(ctx, m.Entry, stage)
		if err != nil {
			return nil, err
		}
		return func() {
			if a.Dialog != nil || a.Preview != nil {
				return
			}
			label := map[int]string{1: "base", 2: "ours", 3: "theirs"}[stage]
			a.report("Merge "+label+": "+m.Entry.Path, source)
			a.Current().Merge = m
			a.Status = "Read-only Git index snapshot · Merge file actions returns to the working copy"
		}, nil
	})
}
func (a *App) replaceMergeStage(stage int) {
	m := a.currentMergeFile(func() { a.replaceMergeStage(stage) })
	if m == nil {
		return
	}
	b := a.Current()
	if b.File.Path != m.Path {
		a.Status = "Return to the conflict file first"
		return
	}
	version, newline := b.Doc.Version, b.Newline
	a.mergeJob("Reading replacement version", func(ctx context.Context) (func(), error) {
		if err := m.Repo.Current(ctx, m.Entry); err != nil {
			return nil, err
		}
		source, err := m.Repo.Read(ctx, m.Entry, stage)
		if err != nil {
			return nil, err
		}
		if newline == "\r\n" {
			source = strings.ReplaceAll(strings.ReplaceAll(source, "\r\n", "\n"), "\n", "\r\n")
		}
		return func() {
			if a.Current() != b || b.Doc.Version != version || a.Dialog != nil || a.Preview != nil {
				a.Status = "Conflict replacement became stale"
				return
			}
			a.showPreview("Replace entire file with Git stage "+fmt.Sprint(stage), b, version, []text.Edit{{Start: 0, End: b.Doc.Text.Len(), Text: source}})
		}, nil
	})
}
func (a *App) deleteMergeFile() {
	m := a.currentMergeFile(a.deleteMergeFile)
	if m == nil {
		return
	}
	if m.Entry.Stages[2].OID != "" && m.Entry.Stages[3].OID != "" {
		a.Status = "Both sides contain a file; choose a text resolution"
		return
	}
	raw, _ := json.Marshal(map[string]any{"documentChanges": []any{map[string]any{"kind": "delete", "uri": lsp.URI(m.Path), "options": map[string]bool{"ignoreIfNotExists": true}}}})
	a.mergeJob("Checking deletion conflict", func(ctx context.Context) (func(), error) {
		if err := m.Repo.Current(ctx, m.Entry); err != nil {
			return nil, err
		}
		return func() {
			if a.Dialog == nil && a.Preview == nil {
				a.workspaceEdit("Resolve merge with deletion", raw)
			}
		}, nil
	})
}
func (a *App) stageMergeResolution() {
	m := a.currentMergeFile(a.stageMergeResolution)
	if m == nil {
		return
	}
	if a.Readonly {
		a.Status = "Read-only mode"
		return
	}
	var known *File
	for _, b := range a.Buffers {
		if b.File.Path == m.Path {
			if b.Doc.Dirty() {
				a.Status = "Save the resolved file before staging"
				return
			}
			snapshot := *b.File
			known = &snapshot
		}
	}
	a.mergeJob("Staging saved merge resolution", func(ctx context.Context) (func(), error) {
		if known != nil {
			if err := known.Unchanged(gitmerge.MaxBytes); err != nil {
				return nil, err
			}
		}
		file, doc, err := OpenFile(m.Path, gitmerge.MaxBytes)
		if err != nil {
			return nil, err
		}
		if file.exists {
			hunks, err := gitmerge.Parse(doc.Text.String())
			if err != nil {
				return nil, err
			}
			if len(hunks) != 0 {
				return nil, errors.New("resolve all marked conflicts and save before staging")
			}
		}
		if err = m.Repo.Stage(ctx, m.Entry); err != nil {
			return nil, err
		}
		return func() {
			a.Status = "Staged resolution for " + filepath.Base(m.Path) + " · review Git staged diff; commit remains explicit"
			a.indexFiles()
		}, nil
	})
	if a.mergeCancel != nil {
		a.mergeWriting = true
	}
}
