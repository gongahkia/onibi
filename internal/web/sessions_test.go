package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/ghostty"
	"github.com/gongahkia/onibi/internal/intake"
	"github.com/gongahkia/onibi/internal/prompt"
)

const testSessionID = "0123456789abcdef"

type fakeSessions struct {
	mu       sync.Mutex
	sessions []intake.SessionInfo
	listErr  error
	capture  SessionCapture
	capErr   error
	execErr  error
	calls    []SessionAction
	captures []int
}

func (f *fakeSessions) Sessions(context.Context) ([]intake.SessionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]intake.SessionInfo(nil), f.sessions...), f.listErr
}

func (f *fakeSessions) Capture(_ context.Context, id string, lines int) (SessionCapture, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.captures = append(f.captures, lines)
	c := f.capture
	c.SessionID = id
	return c, f.capErr
}

func (f *fakeSessions) Execute(_ context.Context, action SessionAction) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, action)
	if action.Kind == "new" {
		return "fedcba9876543210", f.execErr
	}
	return action.SessionID, f.execErr
}

func testSessionServer(t *testing.T) (*Server, *fakeBackend, *fakeSessions, browser) {
	t.Helper()
	s, backend := testServer(t, testOrigin)
	managed := &fakeSessions{sessions: []intake.SessionInfo{{ID: testSessionID, Name: "build", Transport: "tmux", Attention: intake.Attention{State: "input", Reason: "Yes/no prompt"}}}}
	s.managed = managed
	return s, backend, managed, pairBrowser(t, s)
}

func sessionRequest(id string, action SessionAction) []byte {
	raw, _ := json.Marshal(actionRequest{ID: id, Session: &action})
	return raw
}

func TestSessionListAndSnapshotWorkWithoutGhostty(t *testing.T) {
	s, backend, managed, b := testSessionServer(t)
	backend.targets = nil
	managed.capture = SessionCapture{Text: "Apply? [y/N]", ANSI: "\x1b[1mApply?\x1b[0m [y/N]"}
	response := request(s, b, http.MethodGet, "/api/sessions", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"input"`) {
		t.Fatalf("list=%d %s", response.Code, response.Body)
	}
	response = request(s, b, http.MethodGet, "/api/sessions/snapshot?sessionId="+testSessionID+"&lines=500", nil)
	var capture SessionCapture
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &capture) != nil || capture.ANSI != managed.capture.ANSI || capture.SessionID != testSessionID {
		t.Fatalf("snapshot=%d %s", response.Code, response.Body)
	}
	if len(managed.captures) != 1 || managed.captures[0] != 500 {
		t.Fatalf("captures=%v", managed.captures)
	}
	for _, query := range []string{"sessionId=build", "sessionId=" + testSessionID + "&lines=0", "sessionId=" + testSessionID + "&lines=2001"} {
		if response := request(s, b, http.MethodGet, "/api/sessions/snapshot?"+query, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", query, response.Code)
		}
	}
	managed.capErr = intake.Reject(&daemonRejection{message: "session ended"})
	if response := request(s, b, http.MethodGet, "/api/sessions/snapshot?sessionId="+testSessionID, nil); response.Code != http.StatusNotFound {
		t.Fatalf("ended=%d", response.Code)
	}
	managed.listErr = intake.Reject(errSessionsUnavailable)
	if response := request(s, b, http.MethodGet, "/api/sessions", nil); response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "/") {
		t.Fatalf("daemon down=%d %s", response.Code, response.Body)
	}
}

func TestSessionEndpointsRequirePairing(t *testing.T) {
	s, _, _, _ := testSessionServer(t)
	for _, path := range []string{"/api/sessions", "/api/sessions/snapshot?sessionId=" + testSessionID} {
		if response := request(s, browser{}, http.MethodGet, path, nil); response.Code != http.StatusUnauthorized {
			t.Fatalf("%s: %d", path, response.Code)
		}
	}
}

func TestSessionActionsUseReceiptsAndExactValidation(t *testing.T) {
	s, backend, managed, b := testSessionServer(t)
	action := SessionAction{Kind: "input", SessionID: testSessionID, Text: "y", Enter: true}
	receipt := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", sessionRequest("session-action-0001", action)))
	if receipt.State != "completed" || len(managed.calls) != 1 || !reflect.DeepEqual(managed.calls[0], action) || len(backend.calls) != 0 {
		t.Fatalf("receipt=%#v calls=%#v", receipt, managed.calls)
	}
	if replay := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", sessionRequest("session-action-0001", action))); replay.State != "completed" || len(managed.calls) != 1 {
		t.Fatalf("replay ran again: %#v", managed.calls)
	}
	other := action
	other.Text = "n"
	if response := request(s, b, http.MethodPost, "/api/actions", sessionRequest("session-action-0001", other)); response.Code != http.StatusConflict {
		t.Fatalf("conflict=%d", response.Code)
	}
	gone := SessionAction{Kind: "kill", SessionID: "fedcba9876543210"}
	if receipt := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", sessionRequest("session-action-0002", gone))); receipt.State != "rejected" || len(managed.calls) != 1 {
		t.Fatalf("unknown session receipt=%#v calls=%d", receipt, len(managed.calls))
	}
	for _, bad := range []SessionAction{
		{Kind: "input", SessionID: "build", Text: "x"},
		{Kind: "input", SessionID: testSessionID},
		{Kind: "key", SessionID: testSessionID, Key: "y", Enter: true},
		{Kind: "kill", SessionID: testSessionID, Text: "x"},
		{Kind: "resize", SessionID: testSessionID},
	} {
		if response := request(s, b, http.MethodPost, "/api/actions", sessionRequest("session-action-0003", bad)); response.Code != http.StatusBadRequest {
			t.Fatalf("%#v: %d", bad, response.Code)
		}
	}
	both, _ := json.Marshal(actionRequest{ID: "session-action-0004", Action: ghostty.Action{Kind: "input", TargetID: "surface-1", Text: "x"}, Session: &action})
	if response := request(s, b, http.MethodPost, "/api/actions", both); response.Code != http.StatusBadRequest {
		t.Fatalf("mixed=%d", response.Code)
	}
}

func TestSessionActionFailuresDistinguishRejectedFromUncertain(t *testing.T) {
	s, _, managed, b := testSessionServer(t)
	key := SessionAction{Kind: "key", SessionID: testSessionID, Key: "hyper-x"}
	managed.execErr = intake.Reject(&daemonRejection{message: "unsupported key"})
	receipt := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", sessionRequest("session-failure-001", key)))
	if receipt.State != "rejected" || receipt.Error != "unsupported key. No input was sent." {
		t.Fatalf("rejected=%#v", receipt)
	}
	managed.execErr = intake.Reject(errors.Join(errSessionsUnavailable, errors.New("dial unix /private/secret.sock: no such file")))
	receipt = decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", sessionRequest("session-failure-002", key)))
	if receipt.State != "rejected" || strings.Contains(receipt.Error, "/private") {
		t.Fatalf("transport rejection=%#v", receipt)
	}
	managed.execErr = errors.New("read unix: i/o timeout")
	receipt = decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", sessionRequest("session-failure-003", key)))
	if receipt.State != "uncertain" || receipt.Error != uncertainMessage {
		t.Fatalf("uncertain=%#v", receipt)
	}
	managed.execErr = nil
	managed.listErr = errors.New("daemon down")
	receipt = decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", sessionRequest("session-failure-004", key)))
	if receipt.State != "rejected" || len(managed.calls) != 3 {
		t.Fatalf("unchecked dispatch=%#v calls=%d", receipt, len(managed.calls))
	}
}

func TestTrimSnapshotStartsOnLineBoundary(t *testing.T) {
	long := strings.Repeat("\x1b[31mredredred\x1b[0m\n", maxSnapshotBytes/10)
	got := trimSnapshot(long)
	if len(got) > maxSnapshotBytes || !strings.HasPrefix(got, "\x1b[31m") {
		t.Fatalf("len=%d prefix=%q", len(got), got[:12])
	}
}

func TestDaemonSessionsMapsIntakeResponses(t *testing.T) {
	dir, err := os.MkdirTemp("", "onibi-web")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "s.sock")
	if _, err := (DaemonSessions{Socket: sock}).Sessions(t.Context()); !intake.IsRejected(err) || !errors.Is(err, errSessionsUnavailable) {
		t.Fatalf("daemon down err=%v", err)
	}
	var mu sync.Mutex
	var seen []intake.Event
	srv := intake.New(sock, nil)
	srv.SetRPCHandler(func(_ context.Context, ev intake.Event) (intake.Response, error) {
		mu.Lock()
		seen = append(seen, ev)
		mu.Unlock()
		switch ev.Type {
		case intake.TypeSessionList:
			return intake.Response{Sessions: []intake.SessionInfo{{ID: testSessionID}}}, nil
		case intake.TypeSessionKey:
			return intake.Response{}, intake.Reject(errors.New("unsupported key"))
		case intake.TypeSessionControl:
			return intake.Response{}, errors.New("tmux exited")
		case intake.TypeSessionNew:
			return intake.Response{SessionID: "fedcba9876543210", Text: "started " + ev.Name}, nil
		}
		return intake.Response{SessionID: ev.Session}, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() { _ = srv.Serve(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for !intake.SocketActive(sock, 100*time.Millisecond) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	d := DaemonSessions{Socket: sock}
	if sessions, err := d.Sessions(t.Context()); err != nil || len(sessions) != 1 {
		t.Fatalf("sessions=%#v err=%v", sessions, err)
	}
	if _, err := d.Execute(t.Context(), SessionAction{Kind: "input", SessionID: testSessionID, Text: "ls", Enter: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Execute(t.Context(), SessionAction{Kind: "key", SessionID: testSessionID, Key: "x"}); !intake.IsRejected(err) || sessionRejection(err) != "unsupported key. No input was sent." {
		t.Fatalf("key err=%v", err)
	}
	if _, err := d.Execute(t.Context(), SessionAction{Kind: "kill", SessionID: testSessionID}); err == nil || intake.IsRejected(err) {
		t.Fatalf("kill err=%v", err)
	}
	if id, err := d.Execute(t.Context(), SessionAction{Kind: "new", Program: "codex", Args: []string{"--model", "gpt-5"}, CWD: "/tmp", Name: "review"}); err != nil || id != "fedcba9876543210" {
		t.Fatalf("new id=%q err=%v", id, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !seen[1].Exact || seen[1].Type != intake.TypeSessionInput || !seen[3].Exact || seen[3].Action != "kill" {
		t.Fatalf("events=%#v", seen)
	}
	if ev := seen[4]; ev.Type != intake.TypeSessionNew || ev.Agent != "codex" || !reflect.DeepEqual(ev.Args, []string{"--model", "gpt-5"}) || ev.CWD != "/tmp" || ev.Name != "review" {
		t.Fatalf("new event=%#v", ev)
	}
}

func TestSnapshotsCarryTheWaitingPromptForGhosttyAndSessions(t *testing.T) {
	s, backend, managed, b := testSessionServer(t)
	backend.snapText = "Do you want to proceed?\n❯ 1. Yes\n  2. No (esc)\n"
	managed.capture = SessionCapture{Text: "Apply? [y/N]"}
	var ghosttySnap, sessionSnap struct {
		Prompt *prompt.Prompt `json:"prompt"`
	}
	if err := json.Unmarshal(request(s, b, http.MethodGet, "/api/snapshot?targetId=surface-1", nil).Body.Bytes(), &ghosttySnap); err != nil || ghosttySnap.Prompt == nil || len(ghosttySnap.Prompt.Choices) != 2 || ghosttySnap.Prompt.Choices[1].Key != "esc" {
		t.Fatalf("ghostty prompt=%#v err=%v", ghosttySnap.Prompt, err)
	}
	if err := json.Unmarshal(request(s, b, http.MethodGet, "/api/sessions/snapshot?sessionId="+testSessionID, nil).Body.Bytes(), &sessionSnap); err != nil || sessionSnap.Prompt == nil || sessionSnap.Prompt.Choices[0].Send != "y" {
		t.Fatalf("session prompt=%#v err=%v", sessionSnap.Prompt, err)
	}
	backend.snapText = "$ ls\n"
	if body := request(s, b, http.MethodGet, "/api/snapshot?targetId=surface-1", nil).Body.String(); strings.Contains(body, `"prompt"`) {
		t.Fatalf("prompt without one waiting: %s", body)
	}
}

func TestNewSessionStartsOnceAndReportsItsTarget(t *testing.T) {
	s, backend, managed, b := testSessionServer(t)
	action := SessionAction{Kind: "new", Program: "codex", Args: []string{"--model", "gpt-5"}, CWD: "/Users/me/code/app", Name: "review"}
	receipt := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", sessionRequest("session-new-000001", action)))
	if receipt.State != "completed" || receipt.Result == nil || receipt.Result.TargetID != "session:fedcba9876543210" || len(managed.calls) != 1 || !reflect.DeepEqual(managed.calls[0], action) || len(backend.calls) != 0 {
		t.Fatalf("receipt=%#v calls=%#v", receipt, managed.calls)
	}
	if replay := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", sessionRequest("session-new-000001", action))); replay.State != "completed" || len(managed.calls) != 1 {
		t.Fatalf("replay started another session: %#v calls=%d", replay, len(managed.calls))
	}
	for name, bad := range map[string]SessionAction{
		"relative folder":    {Kind: "new", Program: "shell", CWD: "code/app"},
		"no program":         {Kind: "new", CWD: "/tmp"},
		"control in program": {Kind: "new", Program: "sh\nrm", CWD: "/tmp"},
		"control in arg":     {Kind: "new", Program: "shell", Args: []string{"a\x1b[2J"}, CWD: "/tmp"},
		"text on new":        {Kind: "new", Program: "shell", CWD: "/tmp", Text: "ls"},
		"session id on new":  {Kind: "new", Program: "shell", CWD: "/tmp", SessionID: testSessionID},
		"program on input":   {Kind: "input", SessionID: testSessionID, Text: "y", Program: "shell"},
		"long name":          {Kind: "new", Program: "shell", CWD: "/tmp", Name: strings.Repeat("n", 65)},
	} {
		if got := request(s, b, http.MethodPost, "/api/actions", sessionRequest("session-new-bad-"+strings.ReplaceAll(name, " ", "-"), bad)); got.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", name, got.Code, got.Body)
		}
	}
	managed.execErr = intake.Reject(&daemonRejection{message: "nonexistent not found in PATH"})
	receipt = decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", sessionRequest("session-new-000002", SessionAction{Kind: "new", Program: "nonexistent", CWD: "/tmp"})))
	if receipt.State != "rejected" || receipt.Error != "nonexistent not found in PATH. Nothing was started." {
		t.Fatalf("rejection=%#v", receipt)
	}
}
