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
	"sync/atomic"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/codexapp"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/telegram"
	"github.com/gongahkia/onibi/internal/tmux"
)

type bridgeRunner struct {
	mu      sync.Mutex
	calls   [][]string
	err     error
	capture func(context.Context) ([]byte, error)
}

func (r *bridgeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string{name}, args...))
	r.mu.Unlock()
	if len(args) > 0 && args[0] == "capture-pane" {
		if r.capture != nil {
			return r.capture(ctx)
		}
		return []byte("ready\n"), r.err
	}
	return nil, r.err
}
func (r *bridgeRunner) snapshot() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]string, len(r.calls))
	for i := range r.calls {
		out[i] = append([]string(nil), r.calls[i]...)
	}
	return out
}

func testTelegramBridge(t *testing.T) (*telegramBridge, *bridgeRunner, func()) {
	t.Helper()
	state := t.TempDir()
	db, err := store.Open(filepath.Join(state, "onibi.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	d := New(Options{DB: db, Paths: config.Paths{StateDir: state, Socket: filepath.Join(state, "onibi.sock"), Config: filepath.Join(state, "config.yaml")}, OutputBufferSize: 4096})
	// Use an explicit empty path so mock calls cannot load the host's tmux config.
	cfg := config.Default()
	cfg.Multiplexer.Tmux.Config = ""
	if err := config.Save(d.Paths.Config, cfg); err != nil {
		t.Fatal(err)
	}
	runner := &bridgeRunner{}
	oldController := newTmuxController
	newTmuxController = func() *tmux.Controller { return tmux.NewWithRunner(runner) }
	var nextID atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendPhoto") {
			_ = r.ParseMultipartForm(1 << 20)
			writeBridgeTelegram(w, true)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
		}
		writeBridgeTelegram(w, telegram.Message{MessageID: nextID.Add(1), Chat: telegram.Chat{ID: 42, Type: "private"}})
	}))
	client := telegram.NewClient("123456:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi")
	client.BaseURL = server.URL
	client.RetrySleep = func(context.Context, time.Duration) error { return nil }
	b := &telegramBridge{d: d, client: client, ownerID: 42, ownerUserID: 7, seen: map[string]bool{}, sending: map[string]bool{}, killArmed: map[int64]killConfirmation{}, cards: map[string]telegramCard{}, statuses: map[string]codexStatus{}, agentStatuses: map[string]agentStatus{}}
	// Unrelated input assertions do not need an automatic capture. Tests of the
	// debounce explicitly shorten this delay and wait for the resulting work.
	b.screenDelay = time.Hour
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			b.stopAsync()
			newTmuxController = oldController
			server.Close()
			_ = db.Close()
		})
	}
	t.Cleanup(cleanup)
	return b, runner, cleanup
}

func waitForBridge(t *testing.T, condition func() bool, description string) {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !condition() {
		select {
		case <-timeout.C:
			t.Fatalf("timed out waiting for %s", description)
		case <-tick.C:
		}
	}
}

func writeBridgeTelegram(w http.ResponseWriter, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func TestTelegramInputUsesLiteralTextEnterAndScreen(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	b.screenDelay = time.Millisecond
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
	}
	if calls := runner.snapshot(); len(calls) < len(want) || !reflect.DeepEqual(calls[:len(want)], want) {
		t.Fatalf("calls=%#v", calls)
	}
	waitForBridge(t, func() bool { return len(runner.snapshot()) >= 4 }, "automatic screen capture")
	if calls := runner.snapshot(); len(calls) != 4 || calls[2][1] != "display-message" || calls[3][1] != "capture-pane" || calls[3][len(calls[3])-1] != "-160" {
		t.Fatalf("screen calls=%#v", calls)
	}
}

func TestTelegramShutdownCancelsPendingScreen(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("screen-pending", "work", "shell", 4096)
	s.TmuxTarget = "onibi-screen-pending"
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	b.scheduleAutoScreen(t.Context(), 42, s.ID, "work", 0)
	b.stopAsync()
	if calls := runner.snapshot(); len(calls) != 0 {
		t.Fatalf("shutdown captured a pending screen: %#v", calls)
	}
	b.mu.Lock()
	pending := len(b.autoScreens)
	b.mu.Unlock()
	if pending != 0 {
		t.Fatalf("shutdown retained %d screen workers", pending)
	}
	if b.startAsync(t.Context(), func(context.Context) { t.Error("work started after shutdown") }) {
		t.Fatal("shutdown accepted a new worker")
	}
}

func TestTelegramShutdownWaitsForRunningCapture(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	b.screenDelay = time.Millisecond
	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	runner.capture = func(ctx context.Context) ([]byte, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release
		return nil, ctx.Err()
	}
	s := NewSession("screen-running", "work", "shell", 4096)
	s.TmuxTarget = "onibi-screen-running"
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	b.scheduleAutoScreen(t.Context(), 42, s.ID, "work", 0)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("capture did not start")
	}
	stopped := make(chan struct{})
	go func() {
		b.stopAsync()
		close(stopped)
	}()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel capture")
	}
	select {
	case <-stopped:
		t.Fatal("shutdown returned before the running capture exited")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not join capture")
	}
}

func TestTelegramReplacedScreenCannotFlushLatestSchedule(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("screen-replaced", "work", "shell", 4096)
	s.TmuxTarget = "onibi-screen-replaced"
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	key := "42:" + s.ID
	b.scheduleAutoScreen(t.Context(), 42, s.ID, "first", 10)
	b.mu.Lock()
	previous := b.autoScreens[key]
	b.mu.Unlock()
	b.scheduleAutoScreen(t.Context(), 42, s.ID, "second", 11)
	// Simulate an old timer that fired just as the second input arrived.
	b.flushAutoScreen(t.Context(), key, previous)
	if calls := runner.snapshot(); len(calls) != 0 {
		t.Fatalf("superseded timer captured output: %#v", calls)
	}
	b.mu.Lock()
	latest := b.autoScreens[key]
	b.mu.Unlock()
	if latest == nil || latest == previous || latest.Title != "second" || !reflect.DeepEqual(latest.Statuses, []int64{10, 11}) {
		t.Fatalf("superseded timer consumed latest schedule: %#v", latest)
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
	if calls := runner.snapshot(); len(calls) != 2 || !reflect.DeepEqual(calls[0], []string{"tmux", "send-keys", "-t", "onibi-session-slash", "-l", "--", "/usage"}) {
		t.Fatalf("calls=%#v", calls)
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
	if calls := runner.snapshot(); len(calls) != 2 || !reflect.DeepEqual(calls[0], []string{"tmux", "send-keys", "-t", "onibi-session-double-slash", "-l", "--", "/help"}) {
		t.Fatalf("calls=%#v", calls)
	}
}

func TestTelegramDisableRejectsUpdatesAndOutbox(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("session-disabled", "work", "shell", 4096)
	s.TmuxTarget = "onibi-session-disabled"
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	b.setTarget(t.Context(), 42, s.ID)
	if err := b.d.DB.KVSetString(t.Context(), TelegramKVEnabled, "0"); err != nil {
		t.Fatal(err)
	}
	b.handleUpdate(t.Context(), telegram.Update{Message: &telegram.Message{Chat: telegram.Chat{ID: 42, Type: "private"}, From: &telegram.User{ID: 7}, Text: "echo blocked"}})
	if calls := runner.snapshot(); len(calls) != 0 {
		t.Fatalf("disabled update reached terminal: %#v", calls)
	}
	b.enqueueOutbox(t.Context(), "notice", 42, "", "blocked notice", 0, "disabled-notice")
	b.deliverOutbox(t.Context())
	item, err := b.d.DB.TelegramOutboxClaim(t.Context())
	if err != nil || item == nil || item.Title != "blocked notice" {
		t.Fatalf("disabled outbox was delivered: item=%#v err=%v", item, err)
	}
}

func TestTelegramOutboxRequiresCurrentPairedOwner(t *testing.T) {
	b, _, cleanup := testTelegramBridge(t)
	defer cleanup()
	ctx := t.Context()
	if err := b.d.DB.TelegramOutboxUpsert(ctx, store.TelegramOutboxIntent{ID: "old-owner", DedupeKey: "old-owner", Kind: "notice", ChatID: 42, Title: "private terminal output"}); err != nil {
		t.Fatal(err)
	}
	b.ownerID, b.ownerUserID = 0, 0
	b.deliverOutbox(ctx)
	stats, err := b.d.DB.TelegramOutboxStats(ctx)
	if err != nil || stats.Pending != 1 || stats.Delivered != 0 {
		t.Fatalf("unpaired bridge delivered old output: stats=%#v err=%v", stats, err)
	}
	b.ownerID, b.ownerUserID = 99, 99
	b.deliverOutbox(ctx)
	stats, err = b.d.DB.TelegramOutboxStats(ctx)
	if err != nil || stats.Expired != 1 || stats.Delivered != 0 {
		t.Fatalf("revoked owner's output was not expired: stats=%#v err=%v", stats, err)
	}
	if err := b.d.DB.TelegramOutboxUpsert(ctx, store.TelegramOutboxIntent{ID: "new-owner", DedupeKey: "new-owner", Kind: "notice", ChatID: 99, Title: "current output"}); err != nil {
		t.Fatal(err)
	}
	b.deliverOutbox(ctx)
	stats, err = b.d.DB.TelegramOutboxStats(ctx)
	if err != nil || stats.Delivered != 1 || stats.Expired != 1 {
		t.Fatalf("current owner delivery failed: stats=%#v err=%v", stats, err)
	}
}

func TestTelegramToSendsWithoutChangingSelectedSession(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	for _, s := range []*Session{NewSession("one", "work", "shell", 4096), NewSession("two", "other", "shell", 4096)} {
		s.TmuxTarget = "onibi-" + s.ID
		if err := b.d.Registry.Add(s); err != nil {
			t.Fatal(err)
		}
	}
	b.setTarget(t.Context(), 42, "two")
	b.handleUpdate(t.Context(), telegram.Update{Message: &telegram.Message{Chat: telegram.Chat{ID: 42, Type: "private"}, From: &telegram.User{ID: 7}, Text: "/to work echo hi"}})
	calls := runner.snapshot()
	if len(calls) < 2 || !strings.Contains(strings.Join(calls[0], " "), "onibi-one") || !strings.Contains(strings.Join(calls[1], " "), "onibi-one") {
		t.Fatalf("/to calls=%#v", calls)
	}
	if got := b.target(t.Context(), 42); got != "two" {
		t.Fatalf("selected target changed to %q", got)
	}
}

func TestCodexApprovalCardSurvivesResponseFailure(t *testing.T) {
	b, _, cleanup := testTelegramBridge(t)
	defer cleanup()
	token, err := b.newCard(t.Context(), telegramCard{Kind: "codex_approval", SessionID: "missing", RequestID: json.RawMessage(`1`), Decision: "decline", Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	b.handleCallback(t.Context(), &telegram.CallbackQuery{ID: "callback", From: telegram.User{ID: 7}, Message: &telegram.Message{MessageID: 1, Chat: telegram.Chat{ID: 42, Type: "private"}}, Data: "c:" + token})
	if _, ok := b.peekCard(t.Context(), token); !ok {
		t.Fatal("failed Codex decision consumed retry button")
	}
}

func TestCodexDeltasJoinWithoutInsertedNewlines(t *testing.T) {
	if got := appendCodexDelta(appendCodexDelta("", "Hel"), "lo"); got != "Hello" {
		t.Fatalf("joined delta=%q", got)
	}
	if kind, text, _ := classifyCodexNotification(codexapp.Notification{Method: "item/agentMessage/delta", Params: json.RawMessage(`{"delta":"lo"}`)}); kind != "delta" || text != "lo" {
		t.Fatalf("kind=%q text=%q", kind, text)
	}
}

func TestQueuedScreenSendsTextFallbackAfterSessionEnds(t *testing.T) {
	b, _, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("ended-screen", "work", "shell", 4096)
	s.TmuxTarget = "onibi-ended-screen"
	s.MarkEnded()
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	item := store.TelegramOutboxIntent{Kind: "screen", ChatID: 42, SessionID: s.ID, Title: "Updated · work", FallbackText: "last visible output"}
	if err := b.dispatchOutbox(t.Context(), item); err != nil {
		t.Fatal(err)
	}
	queued, err := b.d.DB.TelegramOutboxClaim(t.Context())
	if err != nil || queued != nil {
		t.Fatalf("unexpected replacement notice: item=%#v err=%v", queued, err)
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
	var piCalls int
	for _, call := range runner.snapshot() {
		if strings.Contains(strings.Join(call, " "), "onibi-pi-1") {
			piCalls++
		}
	}
	if piCalls != 2 {
		t.Fatalf("premature Pi completion=%#v", runner.snapshot())
	}
	b.updateAgentStatus(t.Context(), AgentEvent{SessionID: s.ID, Agent: "pi", Kind: "agent_start", RunID: "run-1"})
	b.updateAgentStatus(t.Context(), AgentEvent{SessionID: s.ID, Agent: "pi", Kind: "agent_end", RunID: "run-1"})
	var finalCapture bool
	for _, call := range runner.snapshot() {
		if len(call) > 1 && call[1] == "capture-pane" && call[len(call)-1] == "-160" && strings.Contains(strings.Join(call, " "), "onibi-pi-1") {
			finalCapture = true
		}
	}
	if !finalCapture {
		t.Fatalf("Pi final capture=%#v", runner.snapshot())
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
	if calls := runner.snapshot(); len(calls) != 4 || calls[2][1] != "display-message" || calls[3][len(calls[3])-1] != "-160" {
		t.Fatalf("Claude final capture=%#v", calls)
	}
}

func TestCodexEndedStatusClearsTarget(t *testing.T) {
	b, _, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("codex-ended", "codex", "codex", 4096)
	s.Transport = "codex"
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	if !s.MarkEnded() {
		t.Fatal("could not end Codex session")
	}
	b.setTarget(t.Context(), 42, s.ID)
	b.statusDelay = time.Millisecond
	b.updateCodexStatus(t.Context(), CodexEvent{SessionID: s.ID, Kind: "failed", Text: "Codex App Server stopped."})
	waitForBridge(t, func() bool { return b.target(t.Context(), 42) == "" }, "ended session target to clear")
	if got := b.target(t.Context(), 42); got != "" {
		t.Fatalf("target=%q", got)
	}
	if controls := b.sessionControls(t.Context(), s.ID); controls != nil {
		t.Fatalf("ended session controls=%#v", controls)
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
	if calls := runner.snapshot(); len(calls) != 4 {
		t.Fatalf("tmux calls=%#v", calls)
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

func TestTelegramPairCodeCannotReplaceOwner(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	b.d.TelegramPair = "123456"
	m := &telegram.Message{Chat: telegram.Chat{ID: 99, Type: "private"}, From: &telegram.User{ID: 99}, Text: "/start 123456"}
	b.handleUpdate(t.Context(), telegram.Update{Message: m})
	if b.owner() != 42 || b.ownerUser() != 7 {
		t.Fatalf("pairing code replaced owner: chat=%d user=%d", b.owner(), b.ownerUser())
	}
	if len(runner.snapshot()) != 0 {
		t.Fatal("unpaired input reached a terminal")
	}
}

func TestTelegramPairingFailsClosedWhenOwnerCannotBeSaved(t *testing.T) {
	b, _, cleanup := testTelegramBridge(t)
	defer cleanup()
	b.ownerID, b.ownerUserID = 0, 0
	b.d.TelegramPair = "123456"
	if err := b.d.DB.Close(); err != nil {
		t.Fatal(err)
	}
	m := &telegram.Message{Chat: telegram.Chat{ID: 42, Type: "private"}, From: &telegram.User{ID: 7}, Text: "/start 123456"}
	if b.authorizedOrPair(t.Context(), m) {
		t.Fatal("failed persistence authorized the caller")
	}
	if b.owner() != 0 || b.ownerUser() != 0 || b.d.TelegramOwnerID != 0 || b.d.TelegramOwnerUserID != 0 || b.d.TelegramPair != "123456" {
		t.Fatal("failed persistence changed the in-memory owner or consumed its code")
	}
}

func TestTelegramUnauthorizedCallbackDoesNotConsumeControl(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	token, err := b.newCard(t.Context(), telegramCard{Kind: "key", SessionID: "s-private", Decision: "enter"})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []*telegram.CallbackQuery{
		{ID: "wrong-user", From: telegram.User{ID: 99}, Message: &telegram.Message{Chat: telegram.Chat{ID: 42, Type: "private"}}},
		{ID: "wrong-chat", From: telegram.User{ID: 7}, Message: &telegram.Message{Chat: telegram.Chat{ID: 99, Type: "private"}}},
		{ID: "group", From: telegram.User{ID: 7}, Message: &telegram.Message{Chat: telegram.Chat{ID: 42, Type: "group"}}},
	} {
		q.Data = "c:" + token
		b.handleCallback(t.Context(), q)
		if _, ok := b.peekCard(t.Context(), token); !ok {
			t.Fatalf("%s consumed an authorized control", q.ID)
		}
	}
	if len(runner.snapshot()) != 0 {
		t.Fatal("unauthorized callback reached a terminal")
	}
}

func TestTelegramPasteTreatsNextTextLiterally(t *testing.T) {
	for _, text := range []string{"/help", "/kill", "//help", "  \n\t"} {
		t.Run(text, func(t *testing.T) {
			b, runner, cleanup := testTelegramBridge(t)
			defer cleanup()
			s := NewSession("paste-session", "work", "shell", 4096)
			s.TmuxTarget = "onibi-paste-session"
			if err := b.d.Registry.Add(s); err != nil {
				t.Fatal(err)
			}
			b.setTarget(t.Context(), 42, s.ID)
			b.handleUpdate(t.Context(), telegram.Update{Message: &telegram.Message{Chat: telegram.Chat{ID: 42, Type: "private"}, From: &telegram.User{ID: 7}, Text: "/paste"}})
			b.handleUpdate(t.Context(), telegram.Update{Message: &telegram.Message{Chat: telegram.Chat{ID: 42, Type: "private"}, From: &telegram.User{ID: 7}, Text: text}})
			want := [][]string{{"tmux", "send-keys", "-t", s.TmuxTarget, "-l", "--", text}}
			if got := runner.snapshot(); !reflect.DeepEqual(got, want) {
				t.Fatalf("literal input=%#v, want %#v", got, want)
			}
			if armed, err := b.hasPaste(t.Context(), 42); err != nil || armed {
				t.Fatalf("paste mode remained armed=%t err=%v", armed, err)
			}
			b.handleUpdate(t.Context(), telegram.Update{Message: &telegram.Message{Chat: telegram.Chat{ID: 42, Type: "private"}, From: &telegram.User{ID: 7}, Text: "/enter"}})
			if calls := runner.snapshot(); len(calls) != 2 || calls[1][len(calls[1])-1] != "Enter" {
				t.Fatalf("command routing did not resume: %#v", calls)
			}
		})
	}
}

func TestTelegramKillConfirmationBindsResolvedSession(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	first := NewSession("kill-first", "first", "shell", 4096)
	second := NewSession("kill-second", "second", "shell", 4096)
	first.TmuxTarget, second.TmuxTarget = "onibi-first", "onibi-second"
	for _, s := range []*Session{first, second} {
		if err := b.d.Registry.Add(s); err != nil {
			t.Fatal(err)
		}
	}
	b.setTarget(t.Context(), 42, first.ID)
	b.handleKill(t.Context(), 42)
	b.setTarget(t.Context(), 42, second.ID)
	b.handleKill(t.Context(), 42)
	if len(runner.snapshot()) != 0 || first.Ended() || second.Ended() {
		t.Fatal("changing the target reused a kill confirmation")
	}
	b.handleKill(t.Context(), 42)
	if first.Ended() || !second.Ended() {
		t.Fatalf("confirmed target: first ended=%t second ended=%t", first.Ended(), second.Ended())
	}
	if calls := runner.snapshot(); !reflect.DeepEqual(calls, [][]string{{"tmux", "kill-session", "-t", second.TmuxTarget}}) {
		t.Fatalf("kill calls=%#v", calls)
	}
}

func TestTelegramPasteStorageFailureDoesNotExecuteInput(t *testing.T) {
	b, runner, cleanup := testTelegramBridge(t)
	defer cleanup()
	s := NewSession("paste-storage", "work", "shell", 4096)
	s.TmuxTarget = "onibi-paste-storage"
	if err := b.d.Registry.Add(s); err != nil {
		t.Fatal(err)
	}
	b.setTarget(t.Context(), 42, s.ID)
	if err := b.setPaste(t.Context(), 42); err != nil {
		t.Fatal(err)
	}
	if _, err := b.d.DB.SQL().ExecContext(t.Context(), `CREATE TRIGGER fail_paste BEFORE DELETE ON kv WHEN OLD.key='telegram.paste.42' BEGIN SELECT RAISE(ABORT,'disk failure'); END`); err != nil {
		t.Fatal(err)
	}
	m := &telegram.Message{Chat: telegram.Chat{ID: 42, Type: "private"}, From: &telegram.User{ID: 7}, Text: "dangerous command"}
	b.handleUpdate(t.Context(), telegram.Update{Message: m})
	if len(runner.snapshot()) != 0 {
		t.Fatal("paste consumption failure executed input")
	}
	if err := b.d.DB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.hasPaste(t.Context(), 42); err == nil {
		t.Fatal("paste read failure was hidden")
	}
	if err := b.setPaste(t.Context(), 42); err == nil {
		t.Fatal("paste arming failure was hidden")
	}
	b.handleInputForSession(t.Context(), m, s.ID)
	if len(runner.snapshot()) != 0 {
		t.Fatal("paste read failure executed input")
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
	b.statusDelay = time.Millisecond
	b.updateCodexStatus(t.Context(), CodexEvent{SessionID: s.ID, Kind: "progress", Text: "one"})
	b.updateCodexStatus(t.Context(), CodexEvent{SessionID: s.ID, Kind: "progress", Text: "two"})
	waitForBridge(t, func() bool {
		b.mu.Lock()
		state := b.statuses[s.ID]
		b.mu.Unlock()
		return state.MessageID != 0 && strings.Contains(state.Text, "one") && strings.Contains(state.Text, "two")
	}, "coalesced Codex status")
	b.mu.Lock()
	state := b.statuses[s.ID]
	b.mu.Unlock()
	if !strings.Contains(state.Text, "one") || !strings.Contains(state.Text, "two") {
		t.Fatalf("text=%q", state.Text)
	}
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
