package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/mux"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/tmux"
)

type fakeMuxController struct {
	started mux.StartOptions
	killed  bool
}

func (f *fakeMuxController) Kind() string { return "zellij" }
func (f *fakeMuxController) Start(_ context.Context, _ string, opts mux.StartOptions) (mux.Target, error) {
	f.started = opts
	return mux.Target{Session: "onibi-test", Pane: "terminal_0"}, nil
}
func (f *fakeMuxController) Has(context.Context, mux.Target) (bool, error) { return true, nil }
func (f *fakeMuxController) Capture(context.Context, mux.Target, int) (string, error) {
	return "ready", nil
}
func (f *fakeMuxController) Resize(context.Context, mux.Target, int, int) error { return nil }
func (f *fakeMuxController) SendText(context.Context, mux.Target, string, bool) error {
	return nil
}
func (f *fakeMuxController) SendKey(context.Context, mux.Target, string) error { return nil }
func (f *fakeMuxController) Kill(context.Context, mux.Target) error {
	f.killed = true
	return nil
}

func TestTerminalKeySupportsNavigationAndModifiers(t *testing.T) {
	for input, want := range map[string]string{
		"up": "up", "shift-tab": "shift-tab", "pgdn": "pgdn", "ctrl-d": "ctrl-d", "meta-q": "meta-q", "f12": "f12",
	} {
		got, err := terminalKey(input)
		if err != nil || got != want {
			t.Fatalf("%s: got=%q err=%v", input, got, err)
		}
	}
	if _, err := terminalKey("ctrl-delete"); err == nil {
		t.Fatal("accepted unsupported key")
	}
}

func TestStartTerminalSessionPersistsSelectedMultiplexer(t *testing.T) {
	state, cwd := t.TempDir(), t.TempDir()
	cwd, err := normalizeSessionCWD(cwd)
	if err != nil {
		t.Fatal(err)
	}
	d := New(Options{Paths: config.Paths{StateDir: state}})
	ctrl := &fakeMuxController{}
	oldChoose := chooseMultiplexer
	chooseMultiplexer = func(requested string, _ config.Multiplexer, gotCWD string) (mux.Controller, string, error) {
		if requested != "zellij" || gotCWD != cwd {
			t.Fatalf("requested=%q cwd=%q", requested, gotCWD)
		}
		return ctrl, "zellij", nil
	}
	defer func() { chooseMultiplexer = oldChoose }()
	s, err := d.StartTerminalSession(t.Context(), "", "shell", "/bin/sh", []string{"-i"}, cwd, "zellij")
	if err != nil {
		t.Fatal(err)
	}
	if s.Transport != "zellij" || s.TmuxTarget != "onibi-test|terminal_0" {
		t.Fatalf("session=%#v", s)
	}
	if ctrl.started.CWD != cwd || ctrl.started.Command != "/bin/sh" {
		t.Fatalf("start options=%#v", ctrl.started)
	}
}

func TestAdoptTerminalSessionPersistsExistingTarget(t *testing.T) {
	state, cwd := t.TempDir(), t.TempDir()
	cwd, err := normalizeSessionCWD(cwd)
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(state, "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := New(Options{DB: db, Paths: config.Paths{StateDir: state}})
	oldOpen := openMultiplexer
	openMultiplexer = func(kind string, _ config.MuxBackend, gotCWD string) (mux.Controller, error) {
		if kind != "zellij" || gotCWD != cwd {
			t.Fatalf("kind=%q cwd=%q", kind, gotCWD)
		}
		return &fakeMuxController{}, nil
	}
	defer func() { openMultiplexer = oldOpen }()
	s, err := d.AdoptTerminalSession(t.Context(), "existing", "zellij", "onibi-test|terminal_0", cwd)
	if err != nil {
		t.Fatal(err)
	}
	if s.TmuxTarget != "onibi-test|terminal_0" || s.Transport != "zellij" || s.Agent != "shell" {
		t.Fatalf("adopted session=%#v", s)
	}
	entry, found, err := db.Session(t.Context(), s.ID)
	if err != nil || !found || entry.TmuxTarget != s.TmuxTarget {
		t.Fatalf("persisted session=%#v found=%t err=%v", entry, found, err)
	}
	if _, err := d.AdoptTerminalSession(t.Context(), "other", "zellij", s.TmuxTarget, cwd); err == nil {
		t.Fatal("duplicate target was accepted")
	}
}

func TestStartTerminalSessionFailsClosedWhenPersistenceFails(t *testing.T) {
	state, cwd := t.TempDir(), t.TempDir()
	db, err := store.Open(filepath.Join(state, "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	d := New(Options{DB: db, Paths: config.Paths{StateDir: state}})
	ctrl := &fakeMuxController{}
	oldChoose := chooseMultiplexer
	chooseMultiplexer = func(string, config.Multiplexer, string) (mux.Controller, string, error) {
		return ctrl, "zellij", nil
	}
	defer func() { chooseMultiplexer = oldChoose }()
	if _, err := d.StartTerminalSession(t.Context(), "", "shell", "/bin/sh", nil, cwd, "zellij"); err == nil {
		t.Fatal("started session without durable state")
	}
	if !ctrl.killed || len(d.Registry.List()) != 0 {
		t.Fatalf("cleanup failed: killed=%t registry=%#v", ctrl.killed, d.Registry.List())
	}
}

func TestResizeSessionUsesTmuxPreset(t *testing.T) {
	d, runner := testDaemon(t)
	s := NewSession("resize-1", "work", "shell")
	s.TmuxTarget = "onibi-resize-1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	cols, rows, err := d.ResizeSession(t.Context(), s.ID, "large")
	if err != nil || cols != 120 || rows != 40 {
		t.Fatalf("size=%dx%d err=%v", cols, rows, err)
	}
	want := []string{"tmux", "resize-window", "-t", s.TmuxTarget, "-x", "120", "-y", "40"}
	if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0], want) {
		t.Fatalf("calls=%#v", runner.calls)
	}
	if _, _, err := d.ResizeSession(t.Context(), s.ID, "huge"); err == nil {
		t.Fatal("accepted unsupported size")
	}
}

func TestLivenessCheckEndsMissingTmuxSession(t *testing.T) {
	state := t.TempDir()
	db, err := store.Open(filepath.Join(state, "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := New(Options{DB: db, Paths: config.Paths{StateDir: state}})
	s := NewSession("dead-1", "work", "pi")
	s.TmuxTarget = "onibi-dead-1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	oldController := newTmuxController
	newTmuxController = func() *tmux.Controller {
		return tmux.NewWithRunner(&fakeTmuxRunner{err: errors.New("no server running on /private/tmp/tmux-501/default")})
	}
	defer func() { newTmuxController = oldController }()
	d.checkTmuxSessions(context.Background())
	if !s.Ended() {
		t.Fatal("session was not ended")
	}
	select {
	case event := <-d.SessionEvents():
		if event.SessionID != s.ID || event.Reason != "tmux session exited" {
			t.Fatalf("event=%#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("missing liveness event")
	}
}

func TestRestoreMarksTmuxSessionsEndedWhenServerIsGone(t *testing.T) {
	state := t.TempDir()
	db, err := store.Open(filepath.Join(state, "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := db.SessionUpsertStart(ctx, "restore-dead", "work", "shell", state, "zsh", "tmux", "onibi-restore-dead", time.Now()); err != nil {
		t.Fatal(err)
	}
	d := New(Options{DB: db, Paths: config.Paths{StateDir: state}})
	oldController := newTmuxController
	newTmuxController = func() *tmux.Controller {
		return tmux.NewWithRunner(&fakeTmuxRunner{err: errors.New("no server running on /private/tmp/tmux-501/default")})
	}
	defer func() { newTmuxController = oldController }()
	d.restoreSessions(ctx)
	entry, found, err := db.Session(ctx, "restore-dead")
	if err != nil || !found || !entry.Ended {
		t.Fatalf("entry=%#v found=%t err=%v", entry, found, err)
	}
}

func TestTmuxControlAndRestoreUseProjectConfiguration(t *testing.T) {
	state, cwd := t.TempDir(), t.TempDir()
	configPath := filepath.Join(cwd, "tmux.conf")
	if err := os.WriteFile(configPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, config.LocalConfigName), []byte("multiplexer:\n  tmux:\n    bin: /configured/tmux\n    config: tmux.conf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(state, "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	if err := db.SessionUpsertStart(ctx, "configured", "work", "shell", cwd, "sh", "tmux", "onibi-configured", time.Now()); err != nil {
		t.Fatal(err)
	}
	runner := &fakeTmuxRunner{}
	oldController := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(runner) }
	t.Cleanup(func() { newTmuxController = oldController })
	d := New(Options{DB: db, Paths: config.Paths{StateDir: state}})
	d.restoreSessions(ctx)
	s, err := d.Registry.Get("configured")
	if err != nil {
		t.Fatal(err)
	}
	d.checkTmuxSessions(ctx)
	if err := d.SendSessionText(ctx, s.ID, "echo ready", true); err != nil {
		t.Fatal(err)
	}
	if err := d.SendSessionKey(ctx, s.ID, "up"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.ResizeSession(ctx, s.ID, "large"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.CaptureSessionTail(ctx, s.ID, 80); err != nil {
		t.Fatal(err)
	}
	if err := d.ControlSession(ctx, s.ID, "interrupt"); err != nil {
		t.Fatal(err)
	}
	if err := d.ControlSession(ctx, s.ID, "kill"); err != nil {
		t.Fatal(err)
	}
	if !s.Ended() {
		t.Fatal("killed session remains live")
	}
	calls := runner.snapshot()
	seen := map[string]bool{}
	for _, call := range calls {
		if len(call) < 4 || call[0] != "/configured/tmux" || call[1] != "-f" || call[2] != configPath {
			t.Fatalf("control lost project configuration: %#v", call)
		}
		seen[call[3]] = true
	}
	for _, verb := range []string{"has-session", "send-keys", "resize-window", "capture-pane", "kill-session"} {
		if !seen[verb] {
			t.Fatalf("missing %s call: %#v", verb, calls)
		}
	}
}
