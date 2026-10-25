package web

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestObservabilityExcludesContentAndCountsDispatchOnce(t *testing.T) {
	s, backend := testServer(t, testOrigin)
	var logs bytes.Buffer
	s.log = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	token := pairingToken(t, s)
	raw, _ := json.Marshal(map[string]any{"token": token, "name": "private-browser-name"})
	paired := request(s, browser{}, http.MethodPost, "/api/pair", raw)
	if paired.Code != 200 {
		t.Fatal(paired.Body.String())
	}
	var session browserSession
	json.Unmarshal(paired.Body.Bytes(), &session)
	b := browser{cookie: paired.Result().Cookies()[0], csrf: session.CSRF}
	backend.snapText = "private-terminal-output"
	snapshot := request(s, b, http.MethodGet, "/api/snapshot?targetId=surface-1&secret=private-query", nil)
	if snapshot.Code != 200 {
		t.Fatal(snapshot.Body.String())
	}
	for range 2 {
		got := request(s, b, http.MethodPost, "/api/actions", inputRequest("private-receipt-000001", "private-command-body"))
		if got.Code != 200 {
			t.Fatal(got.Body.String())
		}
		if id := got.Header().Get("X-Onibi-Request-ID"); id == "" || !strings.Contains(logs.String(), id) {
			t.Fatal("missing request correlation")
		}
	}
	request(s, b, http.MethodGet, "/api/private-path", nil)
	request(s, b, http.MethodGet, "/api/actions/private-receipt-000001?secret=private-query", nil)
	s.recordCapture(time.Now(), "private-pane-id", nil)
	calls := backend.listCalls
	status := s.RuntimeStatus()
	if status.Completed != 1 || status.Requests != 6 || status.ActiveRequests != 0 || len(backend.calls) != 1 {
		t.Fatalf("counters: %+v", status)
	}
	if status.Capture.TargetID != "" || backend.listCalls != calls {
		t.Fatal("status disclosed target or probed backend")
	}
	for _, secret := range []string{token, b.cookie.Value, b.csrf, "private-browser-name", "private-terminal-output", "private-query", "private-command-body", "private-receipt-000001", "private-path", "private-pane-id", "surface-1"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("sensitive value in telemetry: %q", secret)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if !json.Valid([]byte(line)) {
			t.Fatal("invalid JSON record")
		}
	}
	if strings.Count(logs.String(), `"msg":"web.action"`) != 1 {
		t.Fatal("receipt replay logged as new execution")
	}
}

func TestRuntimeStatusStartsUnknownWithoutNativeCalls(t *testing.T) {
	s, backend := testServer(t, testOrigin)
	status := s.RuntimeStatus()
	if status.Inventory.State != "unknown" || status.Capture.State != "unknown" || backend.listCalls != 0 || status.PID <= 0 {
		t.Fatalf("status: %+v", status)
	}
}
