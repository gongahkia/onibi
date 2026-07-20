package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gongahkia/onibi/internal/approval"
	"github.com/gongahkia/onibi/internal/store"
)

func addCSRF(req *http.Request, sessionID string) {
	req.Header.Set(csrfHeaderName, csrfTokenForSession(sessionID))
}

func TestCSRFMissingAndMismatchedControlRejected(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.scroll = func(context.Context, string, string) error {
		t.Fatal("scroll resolver should not be called")
		return nil
	}
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "bad-token"} {
		req := httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(`{"session_id":"s1","action":"page_up"}`))
		req.AddCookie(rr.Result().Cookies()[0])
		if token != "" {
			req.Header.Set(csrfHeaderName, token)
		}
		w := httptest.NewRecorder()
		srv.handleControl(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("token %q status = %d body = %q", token, w.Code, w.Body.String())
		}
	}
}

func TestCSRFMissingAndMismatchedApprovalRejected(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := approval.New(db, time.Minute)
	id, _, err := q.Request(context.Background(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{DB: db, ApprovalQueue: q})
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "bad-token"} {
		req := httptest.NewRequest(http.MethodPost, "/approval/"+id, strings.NewReader(`{"verdict":"approve"}`))
		req.AddCookie(rr.Result().Cookies()[0])
		if token != "" {
			req.Header.Set(csrfHeaderName, token)
		}
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("token %q status = %d body = %q", token, w.Code, w.Body.String())
		}
	}
}

func TestCSRFAllMutatingEndpointsRejectMissing(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/control", `{"session_id":"s1","action":"page_up"}`},
		{http.MethodPost, "/approval/a1", `{"verdict":"approve"}`},
		{http.MethodPost, "/handover", `{"session_id":"s1","target":"mac"}`},
		{http.MethodPost, "/push/subscribe", `{"endpoint":"e","keys":{"p256dh":"p","auth":"a"}}`},
		{http.MethodPost, "/snapshots/restore", `{"name":"snap"}`},
		{http.MethodPost, "/snapshots/fork", `{"name":"snap","turn":1,"new_prompt":"go"}`},
		{http.MethodPost, "/snapshots/snap/restore", `{"turn":1,"new_prompt":"go"}`},
		{http.MethodPost, "/snapshots/snap/fork", `{"turn":1,"new_prompt":"go"}`},
		{http.MethodPost, "/attachments/images", `{"mime":"image/png","data":"iVBORw0KGgo="}`},
	} {
		if tc.path == "/push/subscribe" && csrfPushUnavailable {
			continue
		}
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.AddCookie(rr.Result().Cookies()[0])
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s %s status = %d body = %q", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

func TestCSRFSessionInfoTokenAccepted(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	srv.sessionIDs = func() []string { return []string{"s1"} }
	called := false
	srv.scroll = func(_ context.Context, sessionID, action string) error {
		called = sessionID == "s1" && action == "page_up"
		return nil
	}
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	infoReq := httptest.NewRequest(http.MethodGet, "/session-info", nil)
	infoReq.AddCookie(rr.Result().Cookies()[0])
	infoW := httptest.NewRecorder()
	srv.handleSessionInfo(infoW, infoReq)
	if infoW.Code != http.StatusOK {
		t.Fatalf("session-info status = %d body = %q", infoW.Code, infoW.Body.String())
	}
	var info struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.NewDecoder(infoW.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.CSRFToken == "" {
		t.Fatal("session-info missing csrf_token")
	}
	req := httptest.NewRequest(http.MethodPost, "/control", strings.NewReader(`{"session_id":"s1","action":"page_up"}`))
	req.AddCookie(rr.Result().Cookies()[0])
	req.Header.Set(csrfHeaderName, info.CSRFToken)
	w := httptest.NewRecorder()
	srv.handleControl(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("control did not reach scroll resolver")
	}
}

func TestCSRFEventsSessionInfoIncludesToken(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	rr := httptest.NewRecorder()
	sessionID, err := srv.CreateOwnerSession(context.Background(), rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/session-info?events=1", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.handleSessionInfo(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	var got map[string]string
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["csrf_token"] != csrfTokenForSession(sessionID) {
		t.Fatalf("session-info = %#v", got)
	}
}
