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

func TestAnomalyAllowlistPostRejectsUnauthenticated(t *testing.T) {
	srv := New(Options{
		AnomalyAllow: func(context.Context, AnomalyAllowlistRequest) (string, error) {
			t.Fatal("anomaly allowlist called")
			return "", nil
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/anomaly/allowlist", strings.NewReader(`{"session_id":"s1"}`))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestAnomalyAllowlistPostCallsCallback(t *testing.T) {
	db, err := store.OpenEphemeral(filepath.Join(t.TempDir(), "onibi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got AnomalyAllowlistRequest
	srv := New(Options{
		DB: db,
		AnomalyAllow: func(_ context.Context, req AnomalyAllowlistRequest) (string, error) {
			got = req
			return "ok", nil
		},
	})
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/anomaly/allowlist", strings.NewReader(`{"session_id":"s1","rule_name":"fork-bomb","evidence":"hit"}`))
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	if got.SessionID != "s1" || got.RuleName != "fork-bomb" || got.Evidence != "hit" {
		t.Fatalf("request = %#v", got)
	}
	if !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("body = %q", w.Body.String())
	}
}
