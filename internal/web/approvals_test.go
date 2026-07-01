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

func TestPendingApprovalsRejectsUnauthenticated(t *testing.T) {
	srv, cleanup := testApprovalInboxServer(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/approvals/pending", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
}

func TestPendingApprovalsAggregatesAcrossSessions(t *testing.T) {
	srv, cleanup := testApprovalInboxServer(t)
	defer cleanup()
	if _, _, err := srv.approvalQueue.Request(context.Background(), "s1", "claude", "Bash", `{"command":"ls"}`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.approvalQueue.Request(context.Background(), "s2", "codex", "Write", `{"file_path":"/tmp/a","content":"x"}`); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/approvals/pending", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	var got PendingApprovalsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Approvals) != 2 {
		t.Fatalf("approvals = %#v", got)
	}
	if got.Approvals[0]["session_id"] != "s1" || got.Approvals[0]["session_url"] != "/s/s1" {
		t.Fatalf("first = %#v", got.Approvals[0])
	}
	if got.Approvals[1]["session_id"] != "s2" || got.Approvals[1]["session_url"] != "/s/s2" {
		t.Fatalf("second = %#v", got.Approvals[1])
	}
}

func TestApprovalEndpointRejectsViewer(t *testing.T) {
	srv, cleanup := testApprovalInboxServer(t)
	defer cleanup()
	id, _, err := srv.approvalQueue.Request(context.Background(), "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	if _, err := srv.CreateWebSession(context.Background(), rr, "viewer", store.PairRoleViewer); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method string
		body   string
	}{
		{http.MethodGet, ""},
		{http.MethodPost, `{"verdict":"approve"}`},
	} {
		req := httptest.NewRequest(tc.method, "/approval/"+id, strings.NewReader(tc.body))
		req.AddCookie(rr.Result().Cookies()[0])
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s status = %d body = %q", tc.method, w.Code, w.Body.String())
		}
	}
}

func testApprovalInboxServer(t *testing.T) (*Server, func()) {
	t.Helper()
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	q := approval.New(db, time.Minute)
	return New(Options{DB: db, ApprovalQueue: q}), func() { _ = db.Close() }
}
