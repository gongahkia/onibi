package editor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gongahkia/shack/internal/project"
	"github.com/gongahkia/shack/internal/recovery"
	"github.com/gongahkia/shack/internal/text"
	"io"
	"os"
	"path/filepath"
)

type sessionFile struct {
	Path               string
	Selections         []text.Selection
	Top, Left, WrapTop int
}
type sessionState struct {
	Layout           *splitNode
	Schema           int
	Root             string
	Files            []sessionFile
	Panes            []sessionFile
	Active           string
	Focus            int
	Horizontal, Wrap bool
}

func (a *App) sessionPath() string {
	root, _ := filepath.Abs(a.Root)
	if p, e := filepath.EvalSymlinks(root); e == nil {
		root = p
	}
	hash := sha256.Sum256([]byte(root))
	return filepath.Join(filepath.Dir(recovery.Directory()), "sessions", hex.EncodeToString(hash[:16])+".json")
}
func (a *App) saveSession() error {
	if !a.Config.Session || a.Readonly {
		return nil
	}
	a.ensurePanes()
	state := sessionState{Schema: 1, Root: a.Root, Active: a.Current().File.Path, Focus: a.focus, Horizontal: a.horizontal, Wrap: a.wrap}
	for _, b := range a.Buffers {
		if b.Transient || b.File.Path == "" || !project.Inside(a.Root, b.File.Path) {
			continue
		}
		state.Files = append(state.Files, sessionFile{b.File.Path, append([]text.Selection(nil), b.Doc.Selections[:min(32, len(b.Doc.Selections))]...), b.Top, b.Left, b.WrapTop})
		if len(state.Files) >= 64 {
			break
		}
	}
	indices := map[int]int{}
	for i, p := range a.panes {
		if p.Buffer.Transient || p.Buffer.File.Path == "" {
			continue
		}
		indices[i] = len(state.Panes)
		state.Panes = append(state.Panes, sessionFile{p.Buffer.File.Path, append([]text.Selection(nil), p.Selections[:min(32, len(p.Selections))]...), p.Top, p.Left, p.WrapTop})
	}
	state.Layout = remapLayout(a.layout, indices)
	if i, ok := indices[a.focus]; ok {
		state.Focus = i
	} else {
		state.Focus = 0
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(data) > 128<<10 {
		return errors.New("session exceeds 128 KiB")
	}
	path := a.sessionPath()
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".session-*")
	if err != nil {
		return err
	}
	defer f.Close()
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
func (a *App) restoreSession() {
	if !a.Config.Session {
		a.Status = "Session storage disabled"
		return
	}
	f, err := os.Open(a.sessionPath())
	if err != nil {
		a.Status = "Session: " + err.Error()
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (128<<10)+1))
	if err != nil || len(data) > 128<<10 {
		a.Status = "Session exceeds read limit"
		return
	}
	var state sessionState
	if json.Unmarshal(data, &state) != nil || state.Schema != 1 || len(state.Files) > 64 || len(state.Panes) > maxPanes {
		a.Status = "Invalid project session"
		return
	}
	total, skipped := 0, 0
	opened := map[string]*Buffer{}
	for _, saved := range state.Files {
		if !filepath.IsAbs(saved.Path) || !project.Inside(a.Root, saved.Path) {
			skipped++
			continue
		}
		existing := false
		for _, b := range a.Buffers {
			if b.File.Path == saved.Path {
				opened[saved.Path] = b
				existing = true
				break
			}
		}
		if existing {
			continue
		}
		info, e := os.Stat(saved.Path)
		if e != nil || !info.Mode().IsRegular() || info.Size() > int64((16<<20)-total) {
			skipped++
			continue
		}
		file, d, e := OpenFile(saved.Path, min(int64(a.Config.MaxFileMiB)<<20, int64((16<<20)-total)))
		if e != nil || !file.exists {
			skipped++
			continue
		}
		total += d.Text.Len()
		b := &Buffer{File: file, Doc: d, Newline: detectNewline(d.Text), Top: max(0, min(saved.Top, d.Text.Lines()-1)), Left: max(0, saved.Left), WrapTop: max(0, saved.WrapTop)}
		if len(saved.Selections) > 0 {
			b.Doc.Selections = append([]text.Selection(nil), saved.Selections[:min(32, len(saved.Selections))]...)
			a.clampSelections(b)
		}
		a.Buffers = append(a.Buffers, b)
		opened[saved.Path] = b
	}
	if len(opened) == 0 {
		a.Status = fmt.Sprintf("No session files restored; %d unavailable or beyond 16 MiB budget", skipped)
		return
	}
	if len(a.Buffers) > 1 && a.Buffers[0].File.Path == "" && a.Buffers[0].Doc.Text.Len() == 0 && !a.Buffers[0].Doc.Dirty() {
		a.Buffers = a.Buffers[1:]
	}
	a.panes = nil
	a.layout = nil
	indices := map[int]int{}
	for i, saved := range state.Panes {
		b := opened[saved.Path]
		if b == nil {
			continue
		}
		sel := saved.Selections
		if len(sel) == 0 {
			sel = b.Doc.Selections
		}
		indices[i] = len(a.panes)
		a.panes = append(a.panes, pane{Buffer: b, Top: max(0, min(saved.Top, b.Doc.Text.Lines()-1)), Left: max(0, saved.Left), WrapTop: max(0, saved.WrapTop), Selections: append([]text.Selection(nil), sel[:min(32, len(sel))]...), Version: b.Doc.Version})
	}
	if validLayout(state.Layout, len(state.Panes)) {
		a.layout = remapLayout(state.Layout, indices)
	}
	if a.layout == nil {
		a.layout = legacyLayout(len(a.panes), state.Horizontal)
	}
	a.horizontal = state.Horizontal
	a.wrap = state.Wrap
	a.Active = 0
	for i, b := range a.Buffers {
		if b.File.Path == state.Active {
			a.Active = i
			break
		}
	}
	if len(a.panes) > 0 {
		a.focus = 0
		if i, ok := indices[state.Focus]; ok {
			a.focus = i
		}
		p := a.panes[a.focus]
		for i, b := range a.Buffers {
			if b == p.Buffer {
				a.Active = i
				break
			}
		}
		b := a.Current()
		b.Top = p.Top
		b.Left = p.Left
		b.WrapTop = p.WrapTop
		b.Doc.Selections = append([]text.Selection(nil), p.Selections...)
		a.clampSelections(b)
	} else {
		a.focus = 0
		a.ensurePanes()
	}
	a.Matches = nil
	a.schedule()
	a.reveal()
	a.Status = fmt.Sprintf("Restored %d files; %d skipped; unsaved text uses Recover unsaved buffers", len(opened), skipped)
}
