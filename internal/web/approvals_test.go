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

func TestApprovalEndpointTerminalStates(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name       string
		postBody   string
		transition func(*Server, string) error
		wantState  string
		wantReason string
	}{
		{
			name:      "approved",
			postBody:  `{"verdict":"approve"}`,
			wantState: approval.StateApproved,
		},
		{
			name:       "denied",
			postBody:   `{"verdict":"deny","reason":"no"}`,
			wantState:  approval.StateDenied,
			wantReason: "no",
		},
		{
			name:      "edited",
			postBody:  `{"verdict":"edit","edited_input":"{\"command\":\"echo ok\"}"}`,
			wantState: approval.StateEdited,
		},
		{
			name: "expired",
			transition: func(s *Server, id string) error {
				if _, err := s.db.SQL().ExecContext(ctx, `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Unix(), id); err != nil {
					return err
				}
				_, err := s.approvalQueue.ExpireOverdue(ctx)
				return err
			},
			wantState:  approval.StateExpired,
			wantReason: "approval expired (5 min TTL)",
		},
		{
			name: "cancelled",
			transition: func(s *Server, id string) error {
				return s.approvalQueue.Cancel(ctx, id, "shutdown")
			},
			wantState:  approval.StateCancelled,
			wantReason: "shutdown",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, cleanup := testApprovalInboxServer(t)
			defer cleanup()
			id, _, err := srv.approvalQueue.Request(ctx, "s1", "claude", "Bash", `{"command":"ls"}`)
			if err != nil {
				t.Fatal(err)
			}
			rr := httptest.NewRecorder()
			ownerID, err := srv.CreateOwnerSession(ctx, rr, "test device")
			if err != nil {
				t.Fatal(err)
			}
			cookie := rr.Result().Cookies()[0]
			if tc.postBody != "" {
				req := httptest.NewRequest(http.MethodPost, "/approval/"+id, strings.NewReader(tc.postBody))
				req.AddCookie(cookie)
				addCSRF(req, ownerID)
				w := httptest.NewRecorder()
				srv.Handler().ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					t.Fatalf("post status = %d body = %q", w.Code, w.Body.String())
				}
			}
			if tc.transition != nil {
				if err := tc.transition(srv, id); err != nil {
					t.Fatal(err)
				}
			}
			req := httptest.NewRequest(http.MethodGet, "/approval/"+id, nil)
			req.AddCookie(cookie)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("get status = %d body = %q", w.Code, w.Body.String())
			}
			var got map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got["state"] != tc.wantState {
				t.Fatalf("state = %#v want %q; body = %#v", got["state"], tc.wantState, got)
			}
			if tc.wantReason != "" && got["reason"] != tc.wantReason {
				t.Fatalf("reason = %#v want %q; body = %#v", got["reason"], tc.wantReason, got)
			}
		})
	}
}

func TestApprovalEndpointRetriesSameDecision(t *testing.T) {
	ctx := context.Background()
	srv, cleanup := testApprovalInboxServer(t)
	defer cleanup()
	id, ch, err := srv.approvalQueue.Request(ctx, "s1", "claude", "Bash", `{"command":"ls"}`)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	ownerID, err := srv.CreateOwnerSession(ctx, rr, "test device")
	if err != nil {
		t.Fatal(err)
	}
	cookie := rr.Result().Cookies()[0]
	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/approval/"+id, strings.NewReader(`{"verdict":"approve"}`))
		req.AddCookie(cookie)
		addCSRF(req, ownerID)
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
		}
	}
	if got := <-ch; got.Verdict != approval.VerdictApprove {
		t.Fatalf("decision = %#v", got)
	}
	n, err := srv.db.AuditCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("audit count = %d", n)
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
