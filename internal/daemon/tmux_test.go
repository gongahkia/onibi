package daemon

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/telegram"
	"github.com/gongahkia/onibi/internal/tmux"
)

type daemonTmuxRunner struct {
	mu    sync.Mutex
	calls [][]string
}

func (r *daemonTmuxRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	if len(args) > 0 && args[0] == "capture-pane" {
		return []byte("tail\n"), nil
	}
	return nil, nil
}

func (r *daemonTmuxRunner) Calls() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]string, len(r.calls))
	for i := range r.calls {
		out[i] = append([]string(nil), r.calls[i]...)
	}
	return out
}

func TestAttachTmuxRegistersSessionAndWrites(t *testing.T) {
	d := newApprovalDaemon(t)
	r := &daemonTmuxRunner{}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := d.AttachTmux(ctx, "main", "%1")
	if err != nil {
		t.Fatal(err)
	}
	if s.Transport != "tmux" || s.TmuxTarget != "%1" {
		t.Fatalf("session = %+v", s)
	}
	rows, err := d.DB.SessionsRecent(context.Background(), 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Transport != "tmux" || rows[0].TmuxTarget != "%1" {
		t.Fatalf("rows = %#v", rows)
	}
	if _, err := s.Host.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	want := []string{"tmux", "send-keys", "-t", "%1", "-l", "--", "hello"}
	calls := r.Calls()
	if len(calls) < 2 || !reflect.DeepEqual(calls[1], want) {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestTmuxHostMapsControlKeys(t *testing.T) {
	d := newApprovalDaemon(t)
	r := &daemonTmuxRunner{}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := d.AttachTmux(ctx, "main", "%1")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range []byte{'\n', 0x1b, 3} {
		if _, err := s.Host.Write([]byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	calls := r.Calls()
	if !containsCall(calls, []string{"tmux", "send-keys", "-t", "%1", "Enter"}) {
		t.Fatalf("missing Enter: %#v", calls)
	}
	if !containsCall(calls, []string{"tmux", "send-keys", "-t", "%1", "Escape"}) {
		t.Fatalf("missing Escape: %#v", calls)
	}
	if !containsCall(calls, []string{"tmux", "send-keys", "-t", "%1", "C-c"}) {
		t.Fatalf("missing C-c: %#v", calls)
	}
}

func TestNewTmuxCommandAttachesTarget(t *testing.T) {
	d := newApprovalDaemon(t)
	r := &daemonTmuxRunner{}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mock := telegram.NewMock(nil)
	if !d.handleTextCommand(ctx, mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "/new tmux %1",
	}) {
		t.Fatal("command not handled")
	}
	sent := mock.Sent()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "Attached tmux %1") {
		t.Fatalf("sent = %#v", sent)
	}
	live := d.liveSessions()
	if len(live) != 1 || live[0].Transport != "tmux" || live[0].TmuxTarget != "%1" {
		t.Fatalf("live = %#v", live)
	}
	if got := d.defaultTarget(ctx, 100); got != live[0].ID {
		t.Fatalf("default target = %q want %q", got, live[0].ID)
	}
}

func TestNewHeadlessCommandStartsTmuxSession(t *testing.T) {
	d := newApprovalDaemon(t)
	r := &daemonTmuxRunner{}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })
	t.Setenv("SHELL", "/bin/sh")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mock := telegram.NewMock(nil)
	if !d.handleTextCommand(ctx, mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
			Text: "/new --headless --cwd " + t.TempDir() + " shell",
	}) {
		t.Fatal("command not handled")
	}
	live := d.liveSessions()
	if len(live) != 1 || live[0].Transport != "tmux" || !strings.HasPrefix(live[0].TmuxTarget, "onibi-") {
		t.Fatalf("live = %#v", live)
	}
	if got := d.defaultTarget(ctx, 100); got != live[0].ID {
		t.Fatalf("default target = %q want %q", got, live[0].ID)
	}
	if !containsCallPrefix(r.Calls(), []string{"tmux", "new-session", "-d", "-s"}) {
		t.Fatalf("missing new-session: %#v", r.Calls())
	}
}

func TestTmuxPromptsQueueUntilReady(t *testing.T) {
	d := newApprovalDaemon(t)
	r := &daemonTmuxRunner{}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := d.AttachTmux(ctx, "tmux:%1", "%1")
	if err != nil {
		t.Fatal(err)
	}
	d.setDefaultTarget(ctx, 100, s.ID)
	mock := telegram.NewMock(nil)
	for _, text := range []string{"clear", "ls"} {
		if err := d.onText(ctx, mock, &models.Message{
			From: &models.User{ID: 100},
			Chat: models.Chat{ID: 100},
			Text: text,
		}); err != nil {
			t.Fatal(err)
		}
	}
	calls := r.Calls()
	wantClear := []string{"tmux", "send-keys", "-t", "%1", "-l", "--", "clear"}
	wantLS := []string{"tmux", "send-keys", "-t", "%1", "-l", "--", "ls"}
	if !containsCall(calls, wantClear) || containsCall(calls, wantLS) {
		t.Fatalf("calls = %#v", calls)
	}
	queued, err := d.DB.PromptList(ctx, s.ID, true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 2 || queued[0].State != "sent" || queued[1].State != "queued" {
		t.Fatalf("queued = %#v", queued)
	}
	d.threadMu.RLock()
	busy := d.busySessions[s.ID]
	d.threadMu.RUnlock()
	if !busy {
		t.Fatal("tmux session not marked busy")
	}
	d.markSessionReady(ctx, mock, s)
	if !containsCall(r.Calls(), wantLS) {
		t.Fatalf("second prompt not dispatched: %#v", r.Calls())
	}
}

func containsCall(calls [][]string, want []string) bool {
	for _, call := range calls {
		if reflect.DeepEqual(call, want) {
			return true
		}
	}
	return false
}

func containsCallPrefix(calls [][]string, want []string) bool {
	for _, call := range calls {
		if len(call) < len(want) {
			continue
		}
		if reflect.DeepEqual(call[:len(want)], want) {
			return true
		}
	}
	return false
}
