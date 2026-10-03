package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/mux"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/tmux"
)

type fakeMuxController struct {
	started mux.StartOptions
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
func (f *fakeMuxController) Kill(context.Context, mux.Target) error            { return nil }

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

func TestResizeSessionUsesTmuxPreset(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("resize-1", "work", "shell", 4096)
	s.TmuxTarget = "onibi-resize-1"
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	cols, rows, err := b.d.ResizeSession(t.Context(), s.ID, "large")
	if err != nil || cols != 120 || rows != 40 {
		t.Fatalf("size=%dx%d err=%v", cols, rows, err)
	}
	want := []string{"tmux", "resize-window", "-t", s.TmuxTarget, "-x", "120", "-y", "40"}
	if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0], want) {
		t.Fatalf("calls=%#v", runner.calls)
	}
	if _, _, err := b.d.ResizeSession(t.Context(), s.ID, "huge"); err == nil {
		t.Fatal("accepted unsupported size")
	}
}

func TestLivenessCheckEndsAndQueuesMissingTmuxSession(t *testing.T) {
	state := t.TempDir()
	db, err := store.Open(filepath.Join(state, "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d := New(Options{DB: db, Paths: config.Paths{StateDir: state}, TelegramOwnerID: 42})
	s := NewSession("dead-1", "work", "pi", 4096)
	s.TmuxTarget = "onibi-dead-1"
	if err := d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	oldController := newTmuxController
	newTmuxController = func() *tmux.Controller {
		return tmux.NewWithRunner(&bridgeRunner{err: errors.New("no server running on /private/tmp/tmux-501/default")})
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
	item, err := db.TelegramOutboxClaim(context.Background())
	if err != nil || item == nil || item.Kind != "ended" || !strings.Contains(item.Title, "work ended") {
		t.Fatalf("item=%#v err=%v", item, err)
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
	d := New(Options{DB: db, Paths: config.Paths{StateDir: state}, TelegramOwnerID: 42})
	oldController := newTmuxController
	newTmuxController = func() *tmux.Controller {
		return tmux.NewWithRunner(&bridgeRunner{err: errors.New("no server running on /private/tmp/tmux-501/default")})
	}
	defer func() { newTmuxController = oldController }()
	d.restoreSessions(ctx)
	entry, found, err := db.Session(ctx, "restore-dead")
	if err != nil || !found || !entry.Ended {
		t.Fatalf("entry=%#v found=%t err=%v", entry, found, err)
	}
	item, err := db.TelegramOutboxClaim(ctx)
	if err != nil || item == nil || item.SessionID != "restore-dead" {
		t.Fatalf("item=%#v err=%v", item, err)
	}
}
