package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gongahkia/onibi/internal/store"
)

func TestTrustRuntimePostRejectsUnauthenticated(t *testing.T) {
	srv := New(Options{
		TrustRuntime: func(context.Context, TrustRuntimeRequest) (string, error) {
			t.Fatal("trust runtime called")
			return "", nil
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/trust/runtime", strings.NewReader(`{"session_id":"s1"}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestTrustRuntimePostRejectsViewer(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv := New(Options{
		DB: db,
		TrustRuntime: func(context.Context, TrustRuntimeRequest) (string, error) {
			t.Fatal("trust runtime called")
			return "", nil
		},
	})
	rr := httptest.NewRecorder()
	if _, err := srv.CreateWebSession(context.Background(), rr, "viewer", store.PairRoleViewer); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/trust/runtime", strings.NewReader(`{"session_id":"s1","tool":"Edit"}`))
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
}

func TestTrustRuntimePostCallsCallback(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got TrustRuntimeRequest
	srv := New(Options{
		DB: db,
		TrustRuntime: func(_ context.Context, req TrustRuntimeRequest) (string, error) {
			got = req
			return "ok", nil
		},
	})
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/trust/runtime", strings.NewReader(`{"session_id":"s1","tool":"Edit","path":"src/**","agent":"claude","expires":"5m"}`))
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	if got.SessionID != "s1" || got.Tool != "Edit" || got.Path != "src/**" || got.Agent != "claude" || got.Expires != "5m" {
		t.Fatalf("request = %#v", got)
	}
	if !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("body = %q", w.Body.String())
	}
}
