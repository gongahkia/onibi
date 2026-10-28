package web

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/store"
)

func TestSessionsOnlyRequiresDaemonBackendNotNativeBackend(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	opts := Options{SessionsOnly: true, DB: db, PublicOrigin: testOrigin}
	if _, err := New(opts); err == nil {
		t.Fatal("accepted sessions-only mode without a sessions backend")
	}
	opts.Sessions = &fakeSessions{}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	b := pairBrowser(t, s)
	for _, path := range []string{"/api/targets", "/api/health", "/api/capabilities", "/api/sessions"} {
		if got := request(s, browser{}, http.MethodGet, path, nil); got.Code != http.StatusUnauthorized {
			t.Fatalf("unauthenticated %s: %d", path, got.Code)
		}
		if got := request(s, b, http.MethodGet, path, nil); got.Code != http.StatusOK {
			t.Fatalf("authenticated %s: %d %s", path, got.Code, got.Body)
		}
	}
	got := request(s, b, http.MethodGet, "/api/targets", nil)
	if !strings.Contains(got.Body.String(), `"targets":[]`) || !strings.Contains(got.Body.String(), `"sessionsOnly":true`) {
		t.Fatal(got.Body.String())
	}
	report := s.featureReport()
	if !report.SessionsOnly || report.NativeBackend || featureByID(t, report, "input").Availability != "unsupported_platform" || featureByID(t, report, "session_new").Availability != "available" {
		t.Fatalf("misleading capabilities: %#v", report)
	}
	if status := s.RuntimeStatus(); !status.SessionsOnly || status.Inventory.State != "not_applicable" || status.Capture.State != "not_applicable" {
		t.Fatalf("misleading health: %#v", status)
	}
	if got := request(s, b, http.MethodGet, "/api/snapshot?targetId=unused", nil); got.Code != http.StatusServiceUnavailable {
		t.Fatalf("native snapshot: %d", got.Code)
	}
	receipt := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", []byte(`{"id":"linux-native-action","action":{"kind":"new_window"}}`)))
	if receipt.State != "rejected" || !strings.Contains(receipt.Error, "macOS") {
		t.Fatalf("native action not rejected: %#v", receipt)
	}
}

func TestSessionsOnlyRetainsDeliveryReceipts(t *testing.T) {
	s, _, managed, b := testSessionServer(t)
	s.sessionsOnly, s.backend = true, nil
	managed.capture.Text = "literal output"
	got := request(s, b, http.MethodGet, "/api/sessions/snapshot?sessionId="+testSessionID, nil)
	var capture SessionCapture
	if json.Unmarshal(got.Body.Bytes(), &capture) != nil || capture.Text != managed.capture.Text {
		t.Fatal(got.Body.String())
	}
	input := sessionRequest("linux-session-input", SessionAction{Kind: "input", SessionID: testSessionID, Text: "日本語", Enter: true})
	for range 2 {
		if receipt := decodeReceipt(t, request(s, b, http.MethodPost, "/api/actions", input)); receipt.State != "completed" {
			t.Fatal(receipt)
		}
	}
	if len(managed.calls) != 1 || managed.calls[0].Text != "日本語" {
		t.Fatalf("input changed or repeated: %#v", managed.calls)
	}
}
