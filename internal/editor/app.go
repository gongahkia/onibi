package editor

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/gongahkia/shack/internal/config"
	"github.com/gongahkia/shack/internal/lsp"
	"github.com/gongahkia/shack/internal/project"
	"github.com/gongahkia/shack/internal/recovery"
	"github.com/gongahkia/shack/internal/text"
)

type Buffer struct {
	Terminal                                *terminalView
	DiskConflict                            bool
	LanguageEpoch                           uint64
	Title                                   string
	WrapTop                                 int
	Transient                               bool
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
	Inline        bool
	Title, Input  string
	Cursor, Index int
	Items         []choice
	Accept        func(string)
}
type preview struct {
	Targets               []*workspaceTarget
	Detail                bool
	DetailTop, DetailLeft int
	Before, After         []string
	Title                 string
	Buffer                *Buffer
	Version               uint64
	Edits                 []text.Edit
	Checked               []bool
	Index                 int
}
type App struct {
	debug                                                      *debugSession
	breakpoints                                                map[string]map[int]*debugBreakpoint
	terminalSequence                                           int
	tree                                                       *fileTree
	treeVisible, treeFocus, treeDrag, minimapVisible, miniDrag bool
	treeWidth                                                  int
	miniWanted                                                 miniJob
	miniCache                                                  *miniImage
	miniJobs                                                   chan miniJob
	miniTimer                                                  *time.Timer
	miniC                                                      <-chan time.Time
	Menu                                                       *contextMenu
	mouseButtons                                               tcell.ButtonMask
	mouseClickX, mouseClickY, mouseClicks                      int
	mouseClickTime                                             time.Time

	layout                              *splitNode
	dividerDrag                         *divider
	taskOutput                          string
	fileCheckBusy                       bool
	languageSequence                    uint64
	RestoreOnStart                      bool
	wantCompletion, completionAutomatic bool
	snippet                             *snippetState
	taskCancel                          context.CancelFunc
	taskSeq                             uint64
	taskBuffer                          *Buffer
	taskRoot                            string
	bindings                            map[string]func()
	indexMode                           string
	workspaceSeq                        uint64
	workspaceCancel                     context.CancelFunc
	batch                               *workspaceBatch
	jumps                               []jump
	jumpIndex                           int
	panes                               []pane
	focus                               int
	horizontal, wrap                    bool
	recoveryBusy, recoveryDirty         bool
	recovery                            *recovery.Store
	recoverable                         []*recovery.Entry
	recoveryTimer                       *time.Timer
	recoveryC                           <-chan time.Time
	recoverySeq                         uint64
	typingDoc                           *text.Document
	typingVersion                       uint64
	typingHead                          int
	typingTime                          time.Time
	languages                           map[string]*languageState
	languageBusy                        bool
	Screen                              tcell.Screen
	Config                              config.Config
	Buffers                             []*Buffer
	Active                              int
	Root                                string
	Readonly                            bool
	Status                              string
	Commands                            []command
	Dialog                              *dialog
	Preview                             *preview
	Query                               string
	Regex                               bool
	Matches                             []text.Edit
	matchVersion                        uint64
	matchBuffer                         *Buffer
	Files                               []string
	clipboard                           string
	clipboardRequested                  bool
	clipboardTarget                     *Buffer
	clipboardVersion                    uint64
	clipboardSelections                 []text.Selection
	pasteOverflow                       bool
	paste                               strings.Builder
	pasting                             bool
	dragging                            bool
	quit                                bool
	ctx                                 context.Context
	cancel                              context.CancelFunc
	updates                             chan func()
	syntaxJobs                          chan syntaxJob
	serviceTimer                        *time.Timer
	serviceC                            <-chan time.Time
	searchCancel                        context.CancelFunc
}

func New(screen tcell.Screen, cfg config.Config, paths []string, readonly bool) (*App, error) {
	ctx, cancel := context.WithCancel(context.Background())
	root, _ := os.Getwd()
	a := &App{Screen: screen, Config: cfg, Root: root, Readonly: readonly, ctx: ctx, cancel: cancel, updates: make(chan func(), 32), syntaxJobs: make(chan syntaxJob, 1)}
	explicitRoot := false
	for _, path := range paths {
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			a.Root, _ = filepath.Abs(path)
			explicitRoot = true
			continue
		}
		if err := a.Open(path); err != nil {
			cancel()
			return nil, err
		}
	}
	if !explicitRoot && len(a.Buffers) > 0 && !project.Inside(a.Root, a.Buffers[0].File.Path) {
		a.Root = lsp.Root(a.Buffers[0].File.Path)
	}
	if canonical, e := filepath.EvalSymlinks(a.Root); e == nil {
		a.Root = canonical
	}
	if len(a.Buffers) == 0 {
		a.NewBuffer()
	}
	a.treeVisible = cfg.FileTree == "on" || (cfg.FileTree == "auto" && explicitRoot)
	a.treeWidth = cfg.TreeWidth
	a.minimapVisible = cfg.Minimap
	a.wrap = cfg.SoftWrap
	a.registerCommands()
	a.languageCommands()
	a.intelligenceCommands()
	a.paneCommands()
	a.treeCommands()
	a.workspaceCommands()
	a.editingCommands()
	a.developmentCommands()
	a.terminalCommands()
	a.debugCommands()
	a.Commands = append(a.Commands, command{"Recover unsaved buffers", "", a.recoveryPicker}, command{"Restore project session", "", a.restoreSession}, command{"Save project session", "", func() {
		if err := a.saveSession(); err != nil {
			a.Status = err.Error()
		} else {
			a.Status = "Project session saved"
		}
	}})
	if err := a.configureKeys(); err != nil {
		cancel()
		return nil, err
	}
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
	termination := make(chan os.Signal, 1)
	signal.Notify(termination, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(termination)
	if err := a.Screen.Init(); err != nil {
		a.cancel()
		return err
	}
	defer a.Screen.Fini()
	defer a.closeTerminals()
	defer a.cancel()
	defer a.closeDebugger()
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
	a.Screen.EnableFocus()
	a.Screen.SetCursorStyle(tcell.CursorStyleSteadyBar)
	events := make(chan tcell.Event, 64)
	done := make(chan struct{})
	defer close(done)
	go a.Screen.ChannelEvents(events, done)
	go a.syntaxWorker()
	if a.RestoreOnStart {
		a.restoreSession()
	}
	defer func() { _ = a.saveSession() }()
	a.indexFiles()
	a.schedule()
	a.draw()
	a.startRecovery()
	defer a.finishRecovery()
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
		case <-a.miniC:
			a.runMinimap()
		case <-a.serviceC:
			a.serviceC = nil
			a.services()
		case <-a.recoveryC:
			a.recoveryC = nil
			_ = a.writeRecovery(false)
		case <-termination:
			return nil
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
	a.rebaseDebug(a.Current())
	a.rebaseSnippet()
	a.rebasePanes()
	a.scheduleRecovery()
	a.reveal()
	a.Matches = nil
	a.Current().SyntaxReady = false
	a.Current().Diagnostics = nil
	a.schedule()
}
func (a *App) apply(edits []text.Edit) {
	if a.Readonly || a.Current().Transient {
		a.Status = "Read-only mode"
		return
	}
	version := a.Current().Doc.Version
	if err := a.Current().Doc.Apply(version, edits); err != nil {
		a.Status = err.Error()
		return
	}
	if a.Current().Doc.Version != version {
		a.changed()
	}
}
func (a *App) insert(s string) {
	if a.Readonly || a.Current().Transient {
		a.Status = "Read-only mode"
		return
	}
	d := a.Current().Doc
	version := d.Version
	r, n := utf8.DecodeRuneInString(s)
	typing := n == len(s) && !unicode.IsSpace(r) && len(d.Selections) == 1 && d.Selections[0].Start() == d.Selections[0].End()
	merge := typing && a.typingDoc == d && a.typingVersion == version && a.typingHead == d.Selections[0].Head && time.Since(a.typingTime) < 800*time.Millisecond
	if err := d.Insert(s); err != nil {
		a.Status = err.Error()
		return
	}
	if merge {
		d.Coalesce()
	}
	a.typingDoc = nil
	if typing {
		a.wantCompletion = true
		a.typingDoc = d
		a.typingVersion = d.Version
		a.typingHead = d.Selections[0].Head
		a.typingTime = time.Now()
	}
	if d.Version != version {
		a.changed()
	}
}
func (a *App) prompt(title, input string, accept func(string)) {
	a.Menu = nil
	a.Dialog = &dialog{Title: title, Input: input, Cursor: len([]rune(input)), Accept: accept}
	a.Preview = nil
}
func (a *App) list(title string, items []choice) {
	a.Menu = nil
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
	a.typingDoc = nil
	b := a.Current()
	if a.Readonly || a.Current().Transient {
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
		b.DiskConflict = false
		a.Status = "Saved " + filepath.Base(b.File.Path)
		a.syncBreakpoints(b.File.Path)
		a.scheduleRecovery()
	}
}
func (a *App) saveAs() { a.saveAsThen(nil) }
func (a *App) saveAsThen(done func()) {
	b := a.Current()
	if b.Terminal != nil {
		a.Status = "Use Export terminal screen to save terminal output"
		return
	}
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
		if b.File.Path != f.Path {
			a.forgetLanguage(b)
			b.LanguageEpoch = 0
		}
		b.File = f
		b.Transient = false
		b.Title = ""
		b.Synced = false
		b.LanguageKey = ""
		b.Diagnostics = nil
		a.Status = "Saved " + abs
		a.syncBreakpoints(abs)
		a.scheduleRecovery()
		a.schedule()
		if done != nil {
			done()
		}
	})
}
func (a *App) closeBuffer() {
	b := a.Current()
	if b.Doc.Dirty() {
		a.list("Unsaved changes in "+bufferName(b), []choice{{"Keep editing", func() {}}, {"Save and close", func() {
			if b.File.Path == "" {
				a.saveAsThen(a.discardBuffer)
				return
			}
			a.Save()
			if !b.Doc.Dirty() {
				a.discardBuffer()
			}
		}}, {"Discard this buffer", a.discardBuffer}})
		return
	}
	a.discardBuffer()
}
func (a *App) discardBuffer() {
	closed := a.Current()
	if d := a.debug; d != nil && d.OutputBuffer == closed {
		d.OutputBuffer = nil
		d.WantsOutput.Store(false)
	}
	for _, points := range a.breakpoints {
		for _, point := range points {
			if point.Buffer == closed {
				point.Buffer = nil
			}
		}
	}
	if v := closed.Terminal; v != nil && v.Session != nil {
		v.Session.Close()
	}
	a.forgetLanguage(closed)
	a.Buffers = append(a.Buffers[:a.Active], a.Buffers[a.Active+1:]...)
	if len(a.Buffers) == 0 {
		a.NewBuffer()
	} else {
		a.Active = min(a.Active, len(a.Buffers)-1)
	}
	for i := range a.panes {
		if a.panes[i].Buffer == closed {
			a.panes[i].Buffer = a.Current()
			a.panes[i].Selections = append([]text.Selection(nil), a.Current().Doc.Selections...)
			a.panes[i].Top = 0
			a.panes[i].Left = 0
			a.panes[i].WrapTop = 0
		}
	}
	a.scheduleRecovery()
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
		a.saveAll(func() { a.quit = true })
	}}, {"Discard all changes and quit", func() { a.quit = true }}})
}
func bufferName(b *Buffer) string {
	if b.Title != "" {
		return b.Title
	}
	if b.File.Path == "" {
		return "Untitled"
	}
	return filepath.Base(b.File.Path)
}
