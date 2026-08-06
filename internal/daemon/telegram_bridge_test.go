package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
	"github.com/gongahkia/onibi/internal/tmux"
)

type bridgeRunner struct {
	mu    sync.Mutex
	calls [][]string
	err   error
}

func (r *bridgeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string{name}, args...))
	r.mu.Unlock()
	if len(args) > 0 && args[0] == "capture-pane" {
		return []byte("ready\n"), r.err
	}
	return nil, r.err
}

func testTelegramBridge(t *testing.T) (*telegramBridge, *bridgeRunner, func()) {
	t.Helper()
	state := t.TempDir()
	db, err := store.Open(filepath.Join(state, "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	d := New(Options{DB: db, Paths: config.Paths{StateDir: state, Socket: filepath.Join(state, "onibi.sock"), Config: filepath.Join(state, "config.yaml")}, OutputBufferSize: 4096})
	runner := &bridgeRunner{}
	oldController := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(runner) }
	var nextID int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendPhoto") {
			_ = r.ParseMultipartForm(1 << 20)
			writeBridgeTelegram(w, true)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
		}
		nextID++
		writeBridgeTelegram(w, telegram.Message{MessageID: nextID, Chat: telegram.Chat{ID: 42, Type: "private"}})
	}))
	client := telegram.NewClient("123456:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi")
	client.BaseURL = server.URL
	client.RetrySleep = func(context.Context, time.Duration) error { return nil }
	b := &telegramBridge{d: d, client: client, ownerID: 42, ownerUserID: 7, seen: map[string]bool{}, sending: map[string]bool{}, killArmed: map[int64]time.Time{}, cards: map[string]telegramCard{}, statuses: map[string]codexStatus{}, agentStatuses: map[string]agentStatus{}}
	cleanup := func() { newTmuxController = oldController; server.Close(); _ = db.Close() }
	return b, runner, cleanup
}

func writeBridgeTelegram(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func TestTelegramInputUsesLiteralTextEnterAndScreen(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("session-1", "work", "shell", 4096)
	s.TmuxTarget = "onibi-session-1"
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	b.setTarget(t.Context(), 42, s.ID)
	b.handleInput(t.Context(), &telegram.Message{Chat: telegram.Chat{ID: 42, Type: "private"}, From: &telegram.User{ID: 7}, Text: "printf ok"})
	want := [][]string{
		{"tmux", "send-keys", "-t", "onibi-session-1", "-l", "--", "printf ok"},
		{"tmux", "send-keys", "-t", "onibi-session-1", "Enter"},
		{"tmux", "capture-pane", "-e", "-p", "-t", "onibi-session-1", "-S", "-80"},
		{"tmux", "capture-pane", "-e", "-p", "-t", "onibi-session-1", "-S", "-160"},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls=%#v", runner.calls)
	}
}

func TestTelegramUnknownSlashCommandReachesSelectedSession(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("session-slash", "pi", "pi", 4096)
	s.TmuxTarget = "onibi-session-slash"
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	b.setTarget(t.Context(), 42, s.ID)
	b.handleUpdate(t.Context(), telegram.Update{Message: &telegram.Message{Chat: telegram.Chat{ID: 42, Type: "private"}, From: &telegram.User{ID: 7}, Text: "/usage"}})
	if len(runner.calls) != 3 || !reflect.DeepEqual(runner.calls[0], []string{"tmux", "send-keys", "-t", "onibi-session-slash", "-l", "--", "/usage"}) {
		t.Fatalf("calls=%#v", runner.calls)
	}
}

func TestTelegramDoubleSlashForcesKnownCommandToSelectedSession(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("session-double-slash", "pi", "pi", 4096)
	s.TmuxTarget = "onibi-session-double-slash"
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	b.setTarget(t.Context(), 42, s.ID)
	b.handleUpdate(t.Context(), telegram.Update{Message: &telegram.Message{Chat: telegram.Chat{ID: 42, Type: "private"}, From: &telegram.User{ID: 7}, Text: "//help"}})
	if len(runner.calls) != 3 || !reflect.DeepEqual(runner.calls[0], []string{"tmux", "send-keys", "-t", "onibi-session-double-slash", "-l", "--", "/help"}) {
		t.Fatalf("calls=%#v", runner.calls)
	}
}

func TestTelegramScreenMarksExitedSessionAndClearsTarget(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("session-ended", "pi", "pi", 4096)
	s.TmuxTarget = "onibi-session-ended"
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	runner.err = errors.New("no server running on /private/tmp/tmux-501/default")
	b.setTarget(t.Context(), 42, s.ID)
	b.sendScreen(t.Context(), 42, s.ID, "Screen")
	if !s.Ended() {
		t.Fatal("session was not marked ended")
	}
	if got := b.target(t.Context(), 42); got != "" {
		t.Fatalf("target=%q", got)
	}
}

func TestPiFinalScreenWaitsForAgentEnd(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("pi-1", "pi", "pi", 4096)
	s.TmuxTarget = "onibi-pi-1"
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	b.setTarget(t.Context(), 42, s.ID)
	b.handleInput(t.Context(), &telegram.Message{Chat: telegram.Chat{ID: 42, Type: "private"}, From: &telegram.User{ID: 7}, Text: "summarize"})
	if len(runner.calls) != 3 || runner.calls[2][1] != "capture-pane" || runner.calls[2][len(runner.calls[2])-1] != "-80" {
		t.Fatalf("premature Pi completion=%#v", runner.calls)
	}
	b.updateAgentStatus(t.Context(), AgentEvent{SessionID: s.ID, Agent: "pi", Kind: "agent_start", RunID: "run-1"})
	b.updateAgentStatus(t.Context(), AgentEvent{SessionID: s.ID, Agent: "pi", Kind: "agent_end", RunID: "run-1"})
	if len(runner.calls) != 5 || runner.calls[4][len(runner.calls[4])-1] != "-160" {
		t.Fatalf("Pi final capture=%#v", runner.calls)
	}
}

func TestClaudeFinalScreenFollowsStopHook(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("claude-1", "claude", "claude", 4096)
	s.TmuxTarget = "onibi-claude-1"
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	b.setTarget(t.Context(), 42, s.ID)
	b.handleInput(t.Context(), &telegram.Message{Chat: telegram.Chat{ID: 42, Type: "private"}, From: &telegram.User{ID: 7}, Text: "summarize"})
	b.updateAgentStatus(t.Context(), AgentEvent{SessionID: s.ID, Agent: "claude", Kind: "agent_end"})
	if len(runner.calls) != 5 || runner.calls[4][len(runner.calls[4])-1] != "-160" {
		t.Fatalf("Claude final capture=%#v", runner.calls)
	}
}

func TestScreenFontPersistsWithoutRestart(t *testing.T) {
	b, _, cleanup := testTelegramBridge(t)
	defer cleanup()
	if err := b.d.SetScreenFont("go-mono-nerd"); err != nil {
		t.Fatal(err)
	}
	if opts := b.d.screenPNGOptions(2, 2); opts.Font != "go-mono-nerd" {
		t.Fatalf("font=%q", opts.Font)
	}
	cfg, _, err := config.Load(b.d.Paths)
	if err != nil || cfg.Screen.Font != "go-mono-nerd" {
		t.Fatalf("config=%#v err=%v", cfg.Screen, err)
	}
}

func TestTmuxSessionsAutoNameAndPersist(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	first, err := b.d.StartTmuxSession(t.Context(), "", "shell", "/bin/sh", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.d.StartTmuxSession(t.Context(), "", "shell", "/bin/sh", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != "shell" || second.Name != "shell-2" {
		t.Fatalf("names=%q,%q", first.Name, second.Name)
	}
	rows, err := b.d.DB.SessionsActive(t.Context())
	if err != nil || len(rows) != 2 {
		t.Fatalf("sessions=%#v err=%v", rows, err)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("tmux calls=%#v", runner.calls)
	}
}

func TestTelegramTargetCardBindsSession(t *testing.T) {
	b, _, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("session-2", "build", "shell", 4096)
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	token, err := b.newCard(t.Context(), telegramCard{Kind: "target", SessionID: s.ID})
	if err != nil {
		t.Fatal(err)
	}
	b.handleCallback(t.Context(), &telegram.CallbackQuery{ID: "callback-1", From: telegram.User{ID: 7}, Data: "c:" + token, Message: &telegram.Message{MessageID: 9, Chat: telegram.Chat{ID: 42, Type: "private"}}})
	if got := b.target(t.Context(), 42); got != s.ID {
		t.Fatalf("target=%q", got)
	}
}

func TestTelegramCardsAreSingleUse(t *testing.T) {
	b, _, cleanup := testTelegramBridge(t)
	defer cleanup()
	token, err := b.newCard(t.Context(), telegramCard{Kind: "screen", SessionID: "session-3"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.takeCard(t.Context(), token); !ok {
		t.Fatal("first card use failed")
	}
	if _, ok := b.takeCard(t.Context(), token); ok {
		t.Fatal("card could be replayed")
	}
}

func TestTelegramCardsExpireInMemory(t *testing.T) {
	b, _, cleanup := testTelegramBridge(t)
	defer cleanup()
	token, err := b.newCard(t.Context(), telegramCard{Kind: "screen", SessionID: "session-5"})
	if err != nil {
		t.Fatal(err)
	}
	b.mu.Lock()
	card := b.cards[token]
	card.ExpiresAt = time.Now().Add(-time.Second).Unix()
	b.cards[token] = card
	b.mu.Unlock()
	if _, ok := b.takeCard(t.Context(), token); ok {
		t.Fatal("expired card accepted")
	}
}

func TestCodexStatusCoalescesProgress(t *testing.T) {
	b, _, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("session-4", "codex-work", "codex", 4096)
	s.Transport = "codex"
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	oldDelay := codexStatusDelay
	codexStatusDelay = time.Millisecond
	defer func() { codexStatusDelay = oldDelay }()
	b.updateCodexStatus(t.Context(), CodexEvent{SessionID: s.ID, Kind: "progress", Text: "one"})
	b.updateCodexStatus(t.Context(), CodexEvent{SessionID: s.ID, Kind: "progress", Text: "two"})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		b.mu.Lock()
		state := b.statuses[s.ID]
		b.mu.Unlock()
		if state.MessageID != 0 {
			if !strings.Contains(state.Text, "one") || !strings.Contains(state.Text, "two") {
				t.Fatalf("text=%q", state.Text)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("coalesced status was not sent")
}

func TestNewSessionSyntaxAndCodexApprovalRedaction(t *testing.T) {
	agent, name, args, err := parseNewSession("shell --name build -x")
	if err != nil || agent != "shell" || name != "build" || !reflect.DeepEqual(args, []string{"-x"}) {
		t.Fatalf("agent=%q name=%q args=%#v err=%v", agent, name, args, err)
	}
	text := formatCodexApproval("item/commandExecution/requestApproval", &Session{Name: "work"}, map[string]any{"command": "deploy --token raw-sensitive-value"})
	if strings.Contains(text, "raw-sensitive-value") {
		t.Fatalf("approval leaked secret: %s", text)
	}
	piText := formatApproval(&approval.Approval{SessionID: "session-1", Agent: "pi", Tool: "bash", InputJSON: `{"command":"deploy --token raw-sensitive-value"}`, State: approval.StatePending}, "work")
	if strings.Contains(piText, "raw-sensitive-value") || !strings.Contains(piText, "Session: work") {
		t.Fatalf("Pi approval=%q", piText)
	}
}

func TestExtractNewCWD(t *testing.T) {
	cwd, args, err := extractNewCWD([]string{"--cwd", "/tmp/project", "--model", "fast"})
	if err != nil || cwd != "/tmp/project" || !reflect.DeepEqual(args, []string{"--model", "fast"}) {
		t.Fatalf("cwd=%q args=%#v err=%v", cwd, args, err)
	}
	if _, _, err := extractNewCWD([]string{"--cwd"}); err == nil {
		t.Fatal("missing cwd accepted")
	}
}

func TestParseNewSessionSupportsQuotedArguments(t *testing.T) {
	agent, name, args, err := parseNewSession(`shell --name work-tree --cwd '/tmp/a project' --flag "value here"`)
	if err != nil || agent != "shell" || name != "work-tree" {
		t.Fatalf("agent=%q name=%q args=%#v err=%v", agent, name, args, err)
	}
	cwd, args, err := extractNewCWD(args)
	if err != nil || cwd != "/tmp/a project" || !reflect.DeepEqual(args, []string{"--flag", "value here"}) {
		t.Fatalf("cwd=%q args=%#v err=%v", cwd, args, err)
	}
	if _, _, _, err := parseNewSession(`shell --cwd "unfinished`); err == nil {
		t.Fatal("unterminated quote accepted")
	}
}
