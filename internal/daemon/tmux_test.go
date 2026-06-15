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
