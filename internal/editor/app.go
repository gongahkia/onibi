package editor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/config"
	"github.com/gongahkia/shack/internal/lsp"
	"github.com/gongahkia/shack/internal/text"
)

type Buffer struct {
	LanguageKey, LanguageID, LanguageStatus string
	Synced, Syncing                         bool
	SyncedVersion                           uint64
	Diagnostics                             []lsp.Diagnostic
	DiagnosticVersion                       uint64
	File                                    *File
	Doc                                     *text.Document
	Top, Left                               int
	Spans                                   []span
	SyntaxVersion                           uint64
	SyntaxReady                             bool
	Newline                                 string
}
type command struct {
	Name, Key string
	Run       func()
}
type choice struct {
	Label string
	Run   func()
}
type dialog struct {
	Title, Input  string
	Cursor, Index int
	Items         []choice
	Accept        func(string)
}
type preview struct {
	Title   string
	Buffer  *Buffer
	Version uint64
	Edits   []text.Edit
	Checked []bool
	Index   int
}
type App struct {
	languages          map[string]*languageState
	languageBusy       bool
	Screen             tcell.Screen
	Config             config.Config
	Buffers            []*Buffer
	Active             int
	Root               string
	Readonly           bool
	Status             string
	Commands           []command
	Dialog             *dialog
	Preview            *preview
	Query              string
	Regex              bool
	Matches            []text.Edit
	matchVersion       uint64
	matchBuffer        *Buffer
	Files              []string
	clipboard          string
	clipboardRequested bool
	paste              strings.Builder
	pasting            bool
	dragging           bool
	quit               bool
	ctx                context.Context
	cancel             context.CancelFunc
	updates            chan func()
	syntaxJobs         chan syntaxJob
	serviceTimer       *time.Timer
	serviceC           <-chan time.Time
	searchCancel       context.CancelFunc
}

func New(screen tcell.Screen, cfg config.Config, paths []string, readonly bool) (*App, error) {
	ctx, cancel := context.WithCancel(context.Background())
	root, _ := os.Getwd()
	a := &App{Screen: screen, Config: cfg, Root: root, Readonly: readonly, ctx: ctx, cancel: cancel, updates: make(chan func(), 32), syntaxJobs: make(chan syntaxJob, 1)}
	for _, path := range paths {
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			a.Root, _ = filepath.Abs(path)
			continue
		}
		if err := a.Open(path); err != nil {
			cancel()
			return nil, err
		}
	}
	if len(a.Buffers) == 0 {
		a.NewBuffer()
	}
	a.registerCommands()
	a.languageCommands()
	a.Status = "Ctrl+E commands · Ctrl+P files · Ctrl+F find · F1 help"
	return a, nil
}
func (a *App) Current() *Buffer { return a.Buffers[a.Active] }
func (a *App) NewBuffer() {
	d, _ := text.NewDocument("")
	a.Buffers = append(a.Buffers, &Buffer{File: &File{}, Doc: d, Newline: "\n"})
	a.Active = len(a.Buffers) - 1
}
func (a *App) Open(path string) error {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}
	f, d, err := OpenFile(path, int64(a.Config.MaxFileMiB)<<20)
	if err != nil {
		return err
	}
	for i, b := range a.Buffers {
		if b.File.Path == f.Path {
			a.Active = i
			return nil
		}
	}
	nl := "\n"
	if strings.Contains(d.Text.Slice(0, min(d.Text.Len(), 8192)), "\r\n") {
		nl = "\r\n"
	}
	a.Buffers = append(a.Buffers, &Buffer{File: f, Doc: d, Newline: nl})
	a.Active = len(a.Buffers) - 1
	return nil
}
func (a *App) post(fn func()) {
	select {
	case a.updates <- fn:
	case <-a.ctx.Done():
	}
}
func (a *App) Run() error {
	if err := a.Screen.Init(); err != nil {
		a.cancel()
		return err
	}
	defer a.Screen.Fini()
	defer a.cancel()
	defer func() {
		for _, state := range a.languages {
			if state.Client != nil {
				state.Client.Close()
			}
		}
	}()
	if a.Config.Mouse {
		a.Screen.EnableMouse(tcell.MouseButtonEvents, tcell.MouseDragEvents)
	}
	a.Screen.EnablePaste()
	a.Screen.SetCursorStyle(tcell.CursorStyleSteadyBar)
	events := make(chan tcell.Event, 64)
	done := make(chan struct{})
	defer close(done)
	go a.Screen.ChannelEvents(events, done)
	go a.syntaxWorker()
	a.indexFiles()
	a.schedule()
	a.draw()
	for !a.quit {
		select {
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			a.Handle(ev)
		case update := <-a.updates:
			update()
		case <-a.serviceC:
			a.serviceC = nil
			a.services()
		case <-a.ctx.Done():
			return nil
		}
		if !a.pasting {
			a.draw()
		}
	}
	return nil
}
func (a *App) schedule() {
	if a.serviceTimer == nil {
		a.serviceTimer = time.NewTimer(200 * time.Millisecond)
	} else {
		if !a.serviceTimer.Stop() {
			select {
			case <-a.serviceTimer.C:
			default:
			}
		}
		a.serviceTimer.Reset(200 * time.Millisecond)
	}
	a.serviceC = a.serviceTimer.C
}
func (a *App) changed() {
	a.reveal()
	a.Matches = nil
	a.Current().SyntaxReady = false
	a.Current().Diagnostics = nil
	a.schedule()
}
func (a *App) apply(edits []text.Edit) {
	if a.Readonly {
		a.Status = "Read-only mode"
		return
	}
	if err := a.Current().Doc.Apply(a.Current().Doc.Version, edits); err != nil {
		a.Status = err.Error()
		return
	}
	a.changed()
}
func (a *App) insert(s string) {
	if a.Readonly {
		a.Status = "Read-only mode"
		return
	}
	if err := a.Current().Doc.Insert(s); err != nil {
		a.Status = err.Error()
		return
	}
	a.changed()
}
func (a *App) prompt(title, input string, accept func(string)) {
	a.Dialog = &dialog{Title: title, Input: input, Cursor: len([]rune(input)), Accept: accept}
	a.Preview = nil
}
func (a *App) list(title string, items []choice) {
	a.Dialog = &dialog{Title: title, Items: items}
	a.Preview = nil
}
func (a *App) filtered() []choice {
	if a.Dialog == nil {
		return nil
	}
	q := strings.ToLower(a.Dialog.Input)
	out := []choice{}
	for _, item := range a.Dialog.Items {
		if fuzzy(q, strings.ToLower(item.Label)) {
			out = append(out, item)
		}
	}
	return out
}
func fuzzy(query, value string) bool {
	for _, r := range query {
		i := strings.IndexRune(value, r)
		if i < 0 {
			return false
		}
		value = value[i+len(string(r)):]
	}
	return true
}
func (a *App) Save() {
	b := a.Current()
	if a.Readonly {
		a.Status = "Read-only mode"
		return
	}
	if b.File.Path == "" {
		a.saveAs()
		return
	}
	if err := b.File.Save(b.Doc); err != nil {
		a.Status = err.Error()
	} else {
		a.Status = "Saved " + filepath.Base(b.File.Path)
	}
}
func (a *App) saveAs() {
	b := a.Current()
	a.prompt("Save as (existing files are protected)", b.File.Path, func(path string) {
		if a.Readonly {
			a.Status = "Read-only mode"
			return
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			a.Status = err.Error()
			return
		}
		f := &File{Path: abs}
		if abs == b.File.Path {
			f = b.File
		}
		if err = f.Save(b.Doc); err != nil {
			a.Status = err.Error()
			return
		}
		b.File = f
		a.Status = "Saved " + abs
		a.schedule()
	})
}
func (a *App) closeBuffer() {
	b := a.Current()
	if b.Doc.Dirty() {
		a.list("Unsaved changes in "+bufferName(b), []choice{{"Keep editing", func() {}}, {"Save", a.Save}, {"Discard this buffer", a.discardBuffer}})
		return
	}
	a.discardBuffer()
}
func (a *App) discardBuffer() {
	a.Buffers = append(a.Buffers[:a.Active], a.Buffers[a.Active+1:]...)
	if len(a.Buffers) == 0 {
		a.NewBuffer()
	} else {
		a.Active = min(a.Active, len(a.Buffers)-1)
	}
	a.Matches = nil
	a.schedule()
}
func (a *App) requestQuit() {
	dirty := 0
	for _, b := range a.Buffers {
		if b.Doc.Dirty() {
			dirty++
		}
	}
	if dirty == 0 {
		a.quit = true
		return
	}
	a.list(fmt.Sprintf("%d unsaved buffer(s)", dirty), []choice{{"Keep editing", func() {}}, {"Save all and quit", func() {
		for i, b := range a.Buffers {
			if b.Doc.Dirty() {
				a.Active = i
				if b.File.Path == "" {
					a.saveAs()
					return
				}
				if err := b.File.Save(b.Doc); err != nil {
					a.Status = err.Error()
					return
				}
			}
		}
		a.quit = true
	}}, {"Discard all changes and quit", func() { a.quit = true }}})
}
func bufferName(b *Buffer) string {
	if b.File.Path == "" {
		return "Untitled"
	}
	return filepath.Base(b.File.Path)
}
