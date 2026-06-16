package daemon

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/gongahkia/onibi/internal/telegram"
	"github.com/gongahkia/onibi/internal/tmux"
)

type daemonTmuxRunner struct {
	mu          sync.Mutex
	calls       [][]string
	failCapture map[string]bool
}

func (r *daemonTmuxRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	call := append([]string{name}, args...)
	r.calls = append(r.calls, call)
	if len(args) > 0 && args[0] == "capture-pane" {
		target := tmuxTargetArg(args)
		if r.failCapture != nil && r.failCapture[target] {
			return nil, errors.New("missing target")
		}
		return []byte("tail\n"), nil
	}
	return nil, nil
}

func tmuxTargetArg(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-t" {
			return args[i+1]
		}
	}
	return ""
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

func TestStartupRestoresLiveTmuxAndEndsStaleRows(t *testing.T) {
	d := newApprovalDaemon(t)
	r := &daemonTmuxRunner{failCapture: map[string]bool{"%2": true}}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })

	ctx := context.Background()
	started := time.Now().Add(-time.Minute)
	if err := d.DB.SessionUpsertStart(ctx, "pty1", "old", "shell", "/tmp", "zsh", "pty", "", started); err != nil {
		t.Fatal(err)
	}
	if err := d.DB.SessionUpsertStart(ctx, "tmux1", "live", "codex", "/tmp", "codex", "tmux", "%1", started); err != nil {
		t.Fatal(err)
	}
	if err := d.DB.SessionUpsertStart(ctx, "tmux2", "gone", "codex", "/tmp", "codex", "tmux", "%2", started); err != nil {
		t.Fatal(err)
	}

	d.restoreSessions(ctx)
	live := d.liveSessions()
	if len(live) != 1 || live[0].ID != "tmux1" || live[0].TmuxTarget != "%1" {
		t.Fatalf("live = %#v", live)
	}
	active, err := d.DB.SessionsActive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != "tmux1" {
		t.Fatalf("active = %#v", active)
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

func TestNewCommandRequiresProjectOrCWD(t *testing.T) {
	d := newApprovalDaemon(t)
	r := &daemonTmuxRunner{}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })
	t.Setenv("SHELL", "/bin/sh")

	mock := telegram.NewMock(nil)
	if !d.handleTextCommand(context.Background(), mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "/new --headless shell",
	}) {
		t.Fatal("command not handled")
	}
	if live := d.liveSessions(); len(live) != 0 {
		t.Fatalf("live = %#v", live)
	}
	if sent := mock.Sent(); len(sent) != 1 || !strings.Contains(sent[0].Text, "Choose a project") {
		t.Fatalf("sent = %#v", sent)
	}
	if containsCallPrefix(r.Calls(), []string{"tmux", "new-session"}) {
		t.Fatalf("unexpected tmux start: %#v", r.Calls())
	}
}

func TestNewCommandUsesProjectAlias(t *testing.T) {
	d := newApprovalDaemon(t)
	r := &daemonTmuxRunner{}
	old := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(r) }
	t.Cleanup(func() { newTmuxController = old })
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	ctx := context.Background()
	if err := d.DB.KVSetString(ctx, projectAliasKey("repo"), dir); err != nil {
		t.Fatal(err)
	}

	mock := telegram.NewMock(nil)
	if !d.handleTextCommand(ctx, mock, &models.Message{
		From: &models.User{ID: 100},
		Chat: models.Chat{ID: 100},
		Text: "/new --headless --project repo shell",
	}) {
		t.Fatal("command not handled")
	}
	if live := d.liveSessions(); len(live) != 1 {
		t.Fatalf("live = %#v", live)
	}
	if !containsTmuxCWD(r.Calls(), dir) {
		t.Fatalf("missing cwd %q: %#v", dir, r.Calls())
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
	states := map[string]string{}
	for _, p := range queued {
		states[p.Text] = p.State
	}
	if len(queued) != 2 || states["clear"] != "sent" || states["ls"] != "queued" {
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

func containsTmuxCWD(calls [][]string, dir string) bool {
	for _, call := range calls {
		for i := 0; i+1 < len(call); i++ {
			if call[i] == "-c" && call[i+1] == dir {
				return true
			}
		}
	}
	return false
}
