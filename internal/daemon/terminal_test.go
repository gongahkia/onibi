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
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/tmux"
)

func TestTerminalKeySupportsNavigationAndModifiers(t *testing.T) {
	for input, want := range map[string]string{
		"up": "Up", "shift-tab": "BTab", "pgdn": "NPage", "ctrl-d": "C-d", "meta-q": "M-q", "f12": "F12",
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
