package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionRecordingEndpointServesCast(t *testing.T) {
	srv, cleanup := testServer(t)
	defer cleanup()
	dir := t.TempDir()
	path := filepath.Join(dir, "s1.cast")
	if err := os.WriteFile(path, []byte(`{"version":2}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv.recordingPath = func(_ context.Context, id string) (string, bool, error) {
		if id != "s1" {
			return "", false, nil
		}
		return path, true, nil
	}
	rr := httptest.NewRecorder()
	if _, err := srv.CreateOwnerSession(context.Background(), rr, "test device"); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/sessions/s1/recording.cast", nil)
	req.AddCookie(rr.Result().Cookies()[0])
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"version":2`) {
		t.Fatalf("body = %q", w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/x-asciicast") {
		t.Fatalf("content-type = %q", ct)
	}
}
